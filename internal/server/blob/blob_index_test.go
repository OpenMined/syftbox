package blob

import (
	"math/rand"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/openmined/syftbox/internal/db"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
)

func TestBlobIndexBurstInsert(t *testing.T) {
	path := t.TempDir()
	dbPath := filepath.Join(path, "test.db")

	// there's a deep lore here - https://github.com/mattn/go-sqlite3/issues/274
	db, err := db.NewSqliteDb(db.WithPath(dbPath), db.WithMaxOpenConns(runtime.NumCPU()))
	assert.NoError(t, err)

	index, err := newBlobIndex(db)
	assert.NoError(t, err)

	const numOperations = 10000

	// Create a sync map to store all blobs for later verification
	var blobMap sync.Map
	var wg sync.WaitGroup
	wg.Add(numOperations)

	// Launch goroutines to hammer the database
	for range numOperations {
		go func() {
			defer wg.Done()

			// Generate random blob data
			size := rand.Int63n(10000000000)
			blob := &BlobInfo{
				Key:  utils.TokenHex(16),
				ETag: utils.TokenHex(32),
				Size: size,
			}

			// Store for later verification
			blobMap.Store(blob.Key, blob)

			// Insert into database
			err = index.Set(blob)
			assert.NoError(t, err)
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify all blobs were properly stored
	blobMap.Range(func(key, value interface{}) bool {
		blobKey := key.(string)
		expectedBlob := value.(*BlobInfo)

		// Retrieve from database and verify
		actualBlob, ok := index.Get(blobKey)
		assert.True(t, ok, "Blob with key %s not found", blobKey)
		assert.Equal(t, expectedBlob.Key, actualBlob.Key)
		assert.Equal(t, expectedBlob.ETag, actualBlob.ETag)
		assert.Equal(t, expectedBlob.Size, actualBlob.Size)
		assert.Equal(t, expectedBlob.LastModified, actualBlob.LastModified)
		return true
	})
}
