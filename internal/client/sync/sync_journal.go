package sync

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/openmined/syftbox/internal/db"
	"github.com/openmined/syftbox/internal/utils"
)

const schema = `
CREATE TABLE IF NOT EXISTS sync_journal (
    path TEXT PRIMARY KEY,
    etag TEXT NOT NULL,
    version TEXT NOT NULL,
    size INTEGER NOT NULL,
    last_modified TEXT NOT NULL -- Store as RFC3339 string
);

CREATE INDEX IF NOT EXISTS idx_journal_path ON sync_journal(path);
CREATE INDEX IF NOT EXISTS idx_journal_etag ON sync_journal(etag);
CREATE INDEX IF NOT EXISTS idx_journal_last_modified ON sync_journal(last_modified);
`

// dbFileMetadata is used for scanning from the database where time is stored as TEXT.
type dbFileMetadata struct {
	Path         string `db:"path"`
	Size         int64  `db:"size"`
	ETag         string `db:"etag"`
	Version      string `db:"version"`
	LastModified string `db:"last_modified"`
}

// SyncJournal manages the persistent state of synced files using SQLite.
type SyncJournal struct {
	db     *sqlx.DB
	dbPath string
}

// NewSyncJournal creates or opens a SyncJournal backed by an SQLite database.
func NewSyncJournal(dbPath string) (*SyncJournal, error) {
	return &SyncJournal{
		dbPath: dbPath,
	}, nil
}

// Open the sync journal and the underlying database
func (s *SyncJournal) Open() error {
	if s.db != nil {
		return fmt.Errorf("sync journal already open")
	}

	// Ensure the directory exists
	dbDir := filepath.Dir(s.dbPath)
	if err := utils.EnsureDir(dbDir); err != nil {
		return fmt.Errorf("failed to create journal directory %s: %w", dbDir, err)
	}

	db, err := db.NewSqliteDb(db.WithPath(s.dbPath), db.WithMaxOpenConns(1))
	if err != nil {
		return fmt.Errorf("failed to create sync journal: %w", err)
	}

	// Create table if it doesn't exist
	if _, err := db.Exec(schema); err != nil {
		db.Close() // Close the connection if schema init fails
		return fmt.Errorf("failed to initialize journal schema: %w", err)
	}

	s.db = db
	return nil
}

// Close closes the underlying database connection.
func (s *SyncJournal) Close() error {
	if s.db == nil {
		return fmt.Errorf("sync journal not open")
	}
	if err := s.db.Close(); err != nil {
		slog.Error("Failed to close sync journal database", "error", err)
		return err
	}
	slog.Debug("sync journal closed")
	return nil
}

// Get retrieves the metadata for a specific path.
func (s *SyncJournal) Get(path string) (*FileMetadata, error) {
	var dbMeta dbFileMetadata
	err := s.db.Get(&dbMeta, "SELECT path, size, etag, version, last_modified FROM sync_journal WHERE path = ?", path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query path %s: %w", path, err)
	}

	// Convert the string timestamp to time.Time
	modTime, err := time.Parse(time.RFC3339, dbMeta.LastModified)
	if err != nil {
		slog.Error("Failed to parse last_modified timestamp", "path", path, "value", dbMeta.LastModified, "error", err)
		return nil, fmt.Errorf("failed to parse stored timestamp for %s: %w", path, err)
	}

	metadata := &FileMetadata{
		Path:         dbMeta.Path,
		Size:         dbMeta.Size,
		ETag:         dbMeta.ETag,
		Version:      dbMeta.Version,
		LastModified: modTime,
	}
	return metadata, nil
}

func (s *SyncJournal) ContentsChanged(path string, etag string) (bool, error) {
	// select etag from sync_journal where path = ?
	var dbEtag string
	err := s.db.Get(&dbEtag, "SELECT etag FROM sync_journal WHERE path = ?", path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("failed to query path %s: %w", path, err)
	}
	return dbEtag != etag, nil
}

// Set inserts or updates the metadata for a specific path using named parameters.
func (s *SyncJournal) Set(state *FileMetadata) error {
	if state == nil {
		return fmt.Errorf("cannot set nil state")
	}

	data := dbFileMetadata{
		Path:         state.Path,
		Size:         state.Size,
		ETag:         state.ETag,
		Version:      state.Version,
		LastModified: state.LastModified.Format(time.RFC3339),
	}

	query := `INSERT OR REPLACE INTO sync_journal (path, size, etag, version, last_modified) 
	          VALUES (:path, :size, :etag, :version, :last_modified)`
	_, err := s.db.NamedExec(query, data)
	if err != nil {
		return fmt.Errorf("failed to set state for path %s: %w", state.Path, err)
	}
	slog.Debug("sync journal set", "path", state.Path, "etag", state.ETag)
	return nil
}

// GetPaths retrieves all paths known to the journal.
func (s *SyncJournal) GetPaths() ([]string, error) {
	var paths []string
	err := s.db.Select(&paths, "SELECT path FROM sync_journal")
	if err != nil {
		return nil, fmt.Errorf("failed to query paths: %w", err)
	}
	return paths, nil
}

// GetState retrieves the entire state map from the journal.
func (s *SyncJournal) GetState() (map[string]*FileMetadata, error) {
	var dbMetas []dbFileMetadata
	err := s.db.Select(&dbMetas, "SELECT path, size, etag, version, last_modified FROM sync_journal")
	if err != nil {
		return nil, fmt.Errorf("failed to query full state: %w", err)
	}

	// Convert dbFileMetadata slice to the final map[string]*FileMetadata
	state := make(map[string]*FileMetadata, len(dbMetas))
	for _, dbMeta := range dbMetas {
		modTime, err := time.Parse(time.RFC3339, dbMeta.LastModified)
		if err != nil {
			slog.Error("Failed to parse last_modified timestamp", "path", dbMeta.Path, "value", dbMeta.LastModified, "error", err)
			continue // Skip this entry if timestamp is corrupt
		}
		state[dbMeta.Path] = &FileMetadata{ // Store pointer
			Path:         dbMeta.Path,
			Size:         dbMeta.Size,
			ETag:         dbMeta.ETag,
			Version:      dbMeta.Version,
			LastModified: modTime,
		}
	}

	return state, nil
}

// Count returns the number of entries in the journal.
func (s *SyncJournal) Count() (int, error) {
	var count int
	err := s.db.Get(&count, "SELECT COUNT(*) FROM sync_journal")
	if err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}
	return count, nil
}

// Delete removes an entry from the journal by its key (path).
func (s *SyncJournal) Delete(path string) error {
	_, err := s.db.Exec("DELETE FROM sync_journal WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete path %s: %w", path, err)
	}
	return nil
}

func (s *SyncJournal) Destroy() error {
	if err := s.Close(); err != nil {
		return fmt.Errorf("failed to clear journal: %w", err)
	}

	// move file to sql.db.timestamp
	timestamp := time.Now().Format("20060102150405")
	if err := os.Rename(s.dbPath, fmt.Sprintf("%s.%s.bak", s.dbPath, timestamp)); err != nil {
		return fmt.Errorf("failed to rename journal file: %w", err)
	}
	return nil
}
