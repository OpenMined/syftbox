package syftsdk

import (
	"io"
	"time"
)

type ProgressCallback func(uploaded int64, total int64)

// progressReader is a wrapper around an io.Reader that tracks the number of bytes read
// and triggers a callback function.
type progressReader struct {
	reader           io.Reader        // The underlying reader (e.g., *os.File)
	bytesUploaded    int64            // Counter for bytes read so far
	totalSize        int64            // Total size of the file (for context/percentage)
	callback         ProgressCallback // Function to call with progress updates
	lastCallbackTime time.Time        // Last time the callback was called
}

// Read implements the io.Reader interface for progressReader.
// It reads from the underlying reader, updates the progress counter,
// triggers the callback, and returns the results.
func (pr *progressReader) Read(p []byte) (n int, err error) {
	// Read from the underlying reader passed during initialization
	n, err = pr.reader.Read(p)

	// Update the total bytes read counter with the number of bytes read in this call
	if n > 0 {
		pr.bytesUploaded += int64(n)
	}

	// Trigger the callback function if it's defined
	if pr.callback != nil {
		now := time.Now()
		if now.Sub(pr.lastCallbackTime) > 500*time.Millisecond || err == io.EOF {
			pr.callback(pr.bytesUploaded, pr.totalSize)
			pr.lastCallbackTime = now
		}
	}

	// Return the number of bytes read and any error encountered (including io.EOF)
	return n, err
}
