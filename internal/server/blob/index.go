package blob

import (
	"fmt"
	"iter"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS blobs (
	key TEXT PRIMARY KEY,
	etag TEXT NOT NULL,
	size INTEGER NOT NULL,
	last_modified TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_blobs_etag ON blobs(etag);
CREATE INDEX IF NOT EXISTS idx_blobs_last_modified ON blobs(last_modified);
`

// BlobIndex provides access to the blob metadata stored in SQLite
type BlobIndex struct {
	db *sqlx.DB
}

// newBlobIndex creates a new index using an existing database connection
func newBlobIndex(db *sqlx.DB) (*BlobIndex, error) {
	idx := &BlobIndex{db: db}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("failed to initialize index: %w", err)
	}

	return idx, nil
}

// Close releases resources used by the index
func (bi *BlobIndex) Close() error {
	return bi.db.Close()
}

// Get retrieves blob info by key
func (bi *BlobIndex) Get(key string) (*BlobInfo, bool) {
	var blob BlobInfo
	err := bi.db.Get(&blob, "SELECT key, etag, size, last_modified FROM blobs WHERE key = ?", key)
	if err != nil {
		return nil, false
	}

	return &blob, true
}

// Set adds or updates a blob in the index
func (bi *BlobIndex) Set(blob *BlobInfo) error {
	_, err := bi.db.Exec(
		`INSERT OR REPLACE INTO blobs (key, etag, size, last_modified) VALUES (?, ?, ?, ?)`,
		blob.Key, blob.ETag, blob.Size, blob.LastModified,
	)
	return err
}

// SetMany adds or updates multiple blobs in the index in a single transaction
func (bi *BlobIndex) SetMany(blobs []*BlobInfo) error {
	if len(blobs) == 0 {
		return nil
	}

	tx, err := bi.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Preparex(
		`INSERT OR REPLACE INTO blobs (key, etag, size, last_modified) VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	for _, blob := range blobs {
		_, err := stmt.Exec(blob.Key, blob.ETag, blob.Size, blob.LastModified)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert blob %s: %w", blob.Key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Remove deletes a blob from the index
func (bi *BlobIndex) Remove(key string) error {
	_, err := bi.db.Exec("DELETE FROM blobs WHERE key = ?", key)
	return err
}

// List returns all blobs in the index
func (bi *BlobIndex) List() ([]*BlobInfo, error) {
	var blobs []*BlobInfo
	err := bi.db.Select(&blobs, "SELECT key, etag, size, last_modified FROM blobs")
	if err != nil {
		return nil, fmt.Errorf("failed to list blobs: %w", err)
	}

	return blobs, nil
}

// Iter returns an iterator over all blobs in the index
func (bi *BlobIndex) Iter() iter.Seq[*BlobInfo] {
	return func(yield func(*BlobInfo) bool) {
		// Use a prepared statement for better performance
		stmt, err := bi.db.Preparex("SELECT key, etag, size, last_modified FROM blobs")
		if err != nil {
			slog.Error("failed to prepare blob query", "error", err)
			return
		}
		defer stmt.Close()

		// Execute the query with a cursor for lower memory usage
		rows, err := stmt.Queryx()
		if err != nil {
			slog.Error("failed to query blobs", "error", err)
			return
		}
		defer rows.Close()

		// Use direct field mapping to avoid reflection overhead from StructScan
		var key, etag, lastModified string
		var size int64

		// Get raw columns to avoid StructScan overhead
		for rows.Next() {
			err := rows.Scan(&key, &etag, &size, &lastModified)
			if err != nil {
				slog.Error("failed to scan blob row", "error", err)
				continue
			}

			// Create a new blob for each row to ensure safety when passing pointers
			blob := &BlobInfo{
				Key:          key,
				ETag:         etag,
				Size:         size,
				LastModified: lastModified,
			}

			if !yield(blob) {
				break
			}
		}

		if err := rows.Err(); err != nil {
			slog.Error("error during blob iteration", "error", err)
		}
	}
}

// Count returns the number of blobs in the index
func (bi *BlobIndex) Count() int {
	var count int
	if err := bi.db.Get(&count, "SELECT COUNT(*) FROM blobs"); err != nil {
		return 0
	}
	return count
}

// FilterByKeyGlob returns blobs with keys matching the given SQL LIKE pattern
func (bi *BlobIndex) FilterByKeyGlob(pattern string) ([]*BlobInfo, error) {
	var blobs []*BlobInfo
	err := bi.db.Select(&blobs, "SELECT key, etag, size, last_modified FROM blobs WHERE key GLOB ?", pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to filter blobs by pattern: %w", err)
	}
	return blobs, nil
}

func (bi *BlobIndex) FilterBySuffix(suffix string) ([]*BlobInfo, error) {
	return bi.FilterByKeyGlob("*" + suffix)
}

func (bi *BlobIndex) FilterByPrefix(prefix string) ([]*BlobInfo, error) {
	return bi.FilterByKeyGlob(prefix + "*")
}

// FilterByTime returns blobs modified after the given time
func (bi *BlobIndex) FilterByTime(filter TimeFilter) ([]*BlobInfo, error) {
	query := "SELECT key, etag, size, last_modified FROM blobs WHERE 1=1"

	if filter.Before != nil {
		query += " AND last_modified < '" + filter.Before.Format(time.RFC3339) + "'"
	}

	if filter.After != nil {
		query += " AND last_modified > '" + filter.After.Format(time.RFC3339) + "'"
	}

	var blobs []*BlobInfo
	err := bi.db.Select(&blobs, query)
	if err != nil {
		return nil, fmt.Errorf("failed to filter blobs by time: %w", err)
	}
	return blobs, nil
}

// FilterAfterTime returns blobs modified after the given time (maintains backward compatibility)
func (bi *BlobIndex) FilterAfterTime(after time.Time) ([]*BlobInfo, error) {
	return bi.FilterByTime(TimeFilter{After: &after})
}

// FilterBeforeTime returns blobs modified before the given time
func (bi *BlobIndex) FilterBeforeTime(before time.Time) ([]*BlobInfo, error) {
	return bi.FilterByTime(TimeFilter{Before: &before})
}

// bulkUpdate updates the index with a set of blobs, adding new ones, updating changed ones,
// and removing blobs that no longer exist
func (bi *BlobIndex) bulkUpdate(blobs []*BlobInfo) (*bulkUpdateResult, error) {
	// Begin transaction for the entire update process
	tx, err := bi.db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Create temporary table with same structure as blobs
	_, err = tx.Exec(`
		CREATE TEMPORARY TABLE temp_blobs (
			key TEXT PRIMARY KEY,
			etag TEXT NOT NULL,
			size INTEGER NOT NULL,
			last_modified TEXT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary table: %w", err)
	}

	// Insert fetched blobs into temporary table
	insertStmt, err := tx.Preparex(
		`INSERT INTO temp_blobs (key, etag, size, last_modified) VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}

	for _, blob := range blobs {
		_, err := insertStmt.Exec(blob.Key, blob.ETag, blob.Size, blob.LastModified)
		if err != nil {
			return nil, fmt.Errorf("failed to insert blob %s into temp table: %w", blob.Key, err)
		}
	}

	result := &bulkUpdateResult{}

	// Count blobs to be deleted (exist in main table but not in temp table)
	err = tx.Get(&result.Deleted, `
		SELECT COUNT(*) FROM blobs b
		LEFT JOIN temp_blobs t ON b.key = t.key
		WHERE t.key IS NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to count deleted blobs: %w", err)
	}

	// Delete those blobs using LEFT JOIN for better index usage
	_, err = tx.Exec(`
		DELETE FROM blobs
		WHERE key IN (
			SELECT b.key FROM blobs b
			LEFT JOIN temp_blobs t ON b.key = t.key
			WHERE t.key IS NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to remove deleted blobs: %w", err)
	}

	// Count new blobs (exist in temp table but not in main table)
	err = tx.Get(&result.Added, `
		SELECT COUNT(*) FROM temp_blobs t
		LEFT JOIN blobs b ON t.key = b.key
		WHERE b.key IS NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to count new blobs: %w", err)
	}

	// Count updated blobs (exist in both tables but with different attributes)
	err = tx.Get(&result.Updated, `
		SELECT COUNT(*) FROM temp_blobs t
		JOIN blobs b ON t.key = b.key
		WHERE t.etag != b.etag OR t.last_modified != b.last_modified OR t.size != b.size
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to count updated blobs: %w", err)
	}

	// Update or insert blobs that have changed or are new
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO blobs (key, etag, size, last_modified)
		SELECT t.key, t.etag, t.size, t.last_modified
		FROM temp_blobs t
		LEFT JOIN blobs b ON t.key = b.key
		WHERE b.key IS NULL OR t.etag != b.etag OR t.last_modified != b.last_modified OR t.size != b.size
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to update or insert blobs: %w", err)
	}

	_, err = tx.Exec(`DROP TABLE temp_blobs`)
	if err != nil {
		return nil, fmt.Errorf("failed to drop temporary table: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return result, nil
}

// soft check interface, incase we want to add a different implementation
var _ IBlobIndex = (*BlobIndex)(nil)
