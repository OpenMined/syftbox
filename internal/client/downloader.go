package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"resty.dev/v3"
)

const (
	AutoDetectWorkers = 0
	DefaultWorkers    = 8
)

type Download struct {
	URL      string
	FileName string
}

type DownloadResult struct {
	Download
	DownloadPath string
	Error        error
}

type Downloader struct {
	client     *resty.Client
	tempDir    string
	numWorkers int
}

func NewDownloader(numWorkers int) (*Downloader, error) {
	tempDir, err := os.MkdirTemp("", "syftbox-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	if numWorkers <= AutoDetectWorkers {
		numWorkers = runtime.NumCPU()
	}

	r := resty.New().
		SetRetryCount(5).
		SetRetryWaitTime(1).
		SetRetryMaxWaitTime(5)

	return &Downloader{
		client:     r,
		tempDir:    tempDir,
		numWorkers: numWorkers,
	}, nil
}

func (d *Downloader) Stop() error {
	// Remove temp directory
	return os.RemoveAll(d.tempDir)
}

// DownloadFile downloads a single file from the provided URL to the temp directory
// Returns the path to the downloaded file or an error
func (d *Downloader) DownloadFile(ctx context.Context, url string, name string) (string, error) {
	// If no filename is provided, use the last part of the URL
	if name == "" {
		name = filepath.Base(url)
	}

	destPath := filepath.Join(d.tempDir, name)

	// Use context for cancelation
	resp, err := d.client.R().
		SetDoNotParseResponse(true).
		SetSaveResponse(true).
		SetOutputFileName(destPath).
		SetContext(ctx).
		Get(url)

	if err != nil {
		return "", fmt.Errorf("failed to download file from %s: %w", url, err)
	} else if resp.IsError() {
		return "", fmt.Errorf("failed to download file from %s: %s", url, resp.Status())
	}

	return destPath, nil
}

func (d *Downloader) DownloadBulkChan(ctx context.Context, files <-chan *Download) <-chan *DownloadResult {
	results := make(chan *DownloadResult)

	var wg sync.WaitGroup
	wg.Add(d.numWorkers)

	// Start worker pool
	for range d.numWorkers {
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case file, ok := <-files:
					if !ok {
						// Input channel closed, worker can exit
						return
					}
					filePath, err := d.DownloadFile(ctx, file.URL, file.FileName)
					results <- &DownloadResult{
						Download:     *file,
						DownloadPath: filePath,
						Error:        err,
					}
				}
			}
		}()
	}

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

func (d *Downloader) DownloadBulk(ctx context.Context, files []*Download) <-chan *DownloadResult {

	jobs := make(chan *Download, len(files))
	results := make(chan *DownloadResult, len(files))

	// Start exactly maxWorkers workers
	var wg sync.WaitGroup
	wg.Add(d.numWorkers)

	// Launch worker pool
	for range d.numWorkers {
		go func() {
			defer wg.Done()
			for file := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					filePath, err := d.DownloadFile(ctx, file.URL, file.FileName)
					results <- &DownloadResult{
						Download:     *file,
						DownloadPath: filePath,
						Error:        err,
					}
				}
			}
		}()
	}

	// Feed the work queue in a separate goroutine
	go func() {
		// Close work queue when all files are queued
		defer close(jobs)

		// Queue all files
		for _, file := range files {
			select {
			case <-ctx.Done():
				return // Context canceled
			case jobs <- file:
				// File queued successfully
			}
		}
	}()

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}
