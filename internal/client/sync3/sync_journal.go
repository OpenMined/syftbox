package sync3

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

// SyncJournal manages the persistent state of synced files using SQLite.
type SyncJournal struct {
	db     *sql.DB
	mu     sync.RWMutex // Used for operations that might need broader locking, though most ops use transactions.
	dbPath string
}

// NewSyncJournal creates or opens a SyncJournal backed by an SQLite database.
func NewSyncJournal(dbPath string) (*SyncJournal, error) {
	// Ensure the directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create journal directory %s: %w", dbDir, err)
	}

	// Add WAL mode for better concurrency
	dsn := fmt.Sprintf("file:%s?mode=rwc&_journal_mode=WAL&_foreign_keys=1&_synchronous=NORMAL&_busy_timeout=5000&_temp_store=MEMORY&_mmap_size=268435456", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db at %s: %w", dbPath, err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite best practice for WAL mode

	// Create table if it doesn't exist
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize journal schema: %w", err)
	}

	return &SyncJournal{
		db:     db,
		dbPath: dbPath,
	}, nil
}

// Close closes the underlying database connection.
func (s *SyncJournal) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	slog.Debug("sync journal closed")
	return nil
}

// Get retrieves the metadata for a specific path.
func (s *SyncJournal) Get(path string) (*FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var metadata FileMetadata
	var modTimeString string
	err := s.db.QueryRow("SELECT path, size, etag, version, last_modified FROM sync_journal WHERE path = ?", path).Scan(
		&metadata.Path, &metadata.Size, &metadata.ETag, &metadata.Version, &modTimeString,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found is not an error in this context
		}
		return nil, fmt.Errorf("failed to query path %s: %w", path, err)
	}
	metadata.LastModified, err = time.Parse(time.RFC3339, modTimeString)
	if err != nil {
		// Log the error, but potentially return the metadata with zero time? Or return error?
		slog.Error("Failed to parse last_modified timestamp", "path", path, "value", modTimeString, "error", err)
		// Returning error might be safer, depends on desired behavior for corrupt data.
		return nil, fmt.Errorf("failed to parse stored timestamp for %s: %w", path, err)
	}
	return &metadata, nil
}

// Set inserts or updates the metadata for a specific path.
func (s *SyncJournal) Set(state *FileMetadata) error {
	if state == nil {
		return fmt.Errorf("cannot set nil state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	modTimeString := state.LastModified.Format(time.RFC3339)
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO sync_journal (path, size, etag, version, last_modified) VALUES (?, ?, ?, ?, ?)",
		state.Path, state.Size, state.ETag, state.Version, modTimeString,
	)
	if err != nil {
		return fmt.Errorf("failed to set state: %w", err)
	}
	return nil
}

// GetPaths retrieves all paths known to the journal.
func (s *SyncJournal) GetPaths() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT path FROM sync_journal")
	if err != nil {
		return nil, fmt.Errorf("failed to query paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("failed to scan path: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during path iteration: %w", err)
	}
	return paths, nil
}

// GetState retrieves the entire state map from the journal.
func (s *SyncJournal) GetState() (map[string]*FileMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT path, size, etag, version, last_modified FROM sync_journal")
	if err != nil {
		return nil, fmt.Errorf("failed to query full state: %w", err)
	}
	defer rows.Close()

	state := make(map[string]*FileMetadata)
	for rows.Next() {
		var metadata FileMetadata
		var modTimeString string
		if err := rows.Scan(&metadata.Path, &metadata.Size, &metadata.ETag, &metadata.Version, &modTimeString); err != nil {
			return nil, fmt.Errorf("failed to scan state row: %w", err)
		}
		metadata.LastModified, err = time.Parse(time.RFC3339, modTimeString)
		if err != nil {
			// Log error for the specific row, maybe skip it or return partial results?
			slog.Error("Failed to parse last_modified timestamp during full state retrieval", "path", metadata.Path, "value", modTimeString, "error", err)
			// Skipping the entry for now, could accumulate errors and return them.
			continue
		}
		state[metadata.Path] = &metadata // Store pointer
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during state iteration: %w", err)
	}
	return state, nil
}

// Count returns the number of entries in the journal.
func (s *SyncJournal) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sync_journal").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}
	return count, nil
}

// Delete removes an entry from the journal by its key (path).
func (s *SyncJournal) Delete(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM sync_journal WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("failed to delete path %s: %w", path, err)
	}
	return nil
}
