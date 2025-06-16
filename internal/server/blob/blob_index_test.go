package blob

import (
	"math/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/db"
	"github.com/openmined/syftbox/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobIndexBurstInsert(t *testing.T) {
	path := t.TempDir()
	dbPath := filepath.Join(path, "test.db")

	db, err := db.NewSqliteDB(db.WithPath(dbPath), db.WithMaxOpenConns(1))
	assert.NoError(t, err)
	defer db.Close()

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
				Key:          utils.TokenHex(16),
				ETag:         utils.TokenHex(32),
				Size:         size,
				LastModified: time.Now().Format(time.RFC3339),
			}

			// Store for later verification
			blobMap.Store(blob.Key, blob)

			// Insert into database
			if err := index.Set(blob); err != nil {
				require.NoError(t, err)
			}
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify all blobs were properly stored
	blobMap.Range(func(key, value any) bool {
		blobKey := key.(string)
		expectedBlob := value.(*BlobInfo)
		require.NotNil(t, expectedBlob)

		// Retrieve from database and verify
		actualBlob, ok := index.Get(blobKey)
		require.NotNil(t, actualBlob)
		assert.True(t, ok, "Blob with key %s not found", blobKey)
		assert.Equal(t, expectedBlob.Key, actualBlob.Key)
		assert.Equal(t, expectedBlob.ETag, actualBlob.ETag)
		assert.Equal(t, expectedBlob.Size, actualBlob.Size)
		assert.Equal(t, expectedBlob.LastModified, actualBlob.LastModified)
		return true
	})
}
