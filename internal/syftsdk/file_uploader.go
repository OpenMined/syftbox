package syftsdk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
)

// Upload a file to a presigned url
func UploadPresigned(ctx context.Context, url string, path string, callback ProgressCallback) (*http.Response, error) {
	/*
		not using `resty` as it's a bit problematic:
		- SetBody() reads the whole io.Reader into memory. we want to avoid that.
		- SetFile() doesn't set Content-Length is not set correctly.
		- Even if we manually set it, Resty is not honoring HTTP 100 when uploading a file, resulting in closed io.Pipe
		- We don't need auth for presigned urls
	*/

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	progressReader := &progressReader{
		reader:    file,
		totalSize: fileInfo.Size(),
		callback:  callback,
	}
	bodyReader := io.Reader(progressReader)
	if stats := getHTTPStats(); stats != nil {
		bodyReader = &countingReader{r: progressReader, onRead: stats.onSend}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.ContentLength = fileInfo.Size() // THIS IS IMPORTANT FOR PRESIGNED URLS
	req.Header.Set("Content-Type", "application/octet-stream")

	dump, _ := httputil.DumpRequestOut(req, false)
	fmt.Println(string(dump))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if stats := getHTTPStats(); stats != nil {
			stats.setLastError(err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if stats := getHTTPStats(); stats != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, &countingReader{r: resp.Body, onRead: stats.onRecv})
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to upload blob: %v", resp.Status)
	}

	return resp, nil
}
