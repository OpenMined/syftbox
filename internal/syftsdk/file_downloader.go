package syftsdk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
	"github.com/openmined/syftbox/internal/utils"
)

// DownloadFile downloads a single file from the provided URL to the temp directory
// Returns the path to the downloaded file or an error
func DownloadFile(ctx context.Context, job *DownloadJob) (string, error) {
	if err := utils.EnsureDir(job.TargetDir); err != nil {
		return "", fmt.Errorf("sdk: download file: %q: %w", job.URL, err)
	}

	// If no filename is provided, use the last part of the URL
	if job.Name == "" {
		job.Name = filepath.Base(job.URL)
	}

	destPath := filepath.Join(job.TargetDir, job.Name)

	// Use context for cancelation
	resp, err := HTTPClient.R().
		DisableAutoReadResponse().
		SetContext(ctx).
		SetOutputFile(destPath).
		SetDownloadCallbackWithInterval(func(info req.DownloadInfo) {
			if info.Response.Response != nil && job.Callback != nil {
				job.Callback(job, info.DownloadedSize, info.Response.ContentLength)
			}
		}, time.Second).
		Get(job.URL)

	if err != nil {
		return "", fmt.Errorf("sdk: download file: '%s': %w", job.URL, err)
	}

	if resp.IsErrorState() {
		var errorCode string

		// the error body is actually dumped in the destPath because of SetOutputFile (lol)
		respContent, err := os.ReadFile(destPath)
		respStr := string(respContent)
		if err != nil {
			slog.Error("download error", "url", job.URL, "error", err)
		}

		// presigned url specific errors
		switch resp.GetStatusCode() {
		case 403:
			// Check if it's an expiration error
			if strings.Contains(respStr, "expired") {
				errorCode = CodePresignedURLExpired
				respStr = "expired"
			} else if strings.Contains(respStr, "SignatureDoesNotMatch") {
				errorCode = CodePresignedURLInvalid
				respStr = "invalid"
			} else {
				errorCode = CodePresignedURLForbidden
				respStr = "access denied"
			}
		case 404:
			errorCode = CodePresignedURLNotFound
			respStr = "not found"
		case 429:
			errorCode = CodePresignedURLRateLimit
			respStr = "rate limit exceeded"
		case 500, 502, 503, 504:
			errorCode = CodeInternalError
		default:
			errorCode = CodeUnknownError
		}

		return "", fmt.Errorf("sdk: download file: '%s': %w", job.URL, NewPresignedURLError(errorCode, respStr))
	}

	return destPath, nil
}

func Downloader(ctx context.Context, opts *DownloadOpts) <-chan *DownloadResult {
	jobs := make(chan *DownloadJob, len(opts.Jobs))
	results := make(chan *DownloadResult, len(opts.Jobs))

	// If no workers are provided, use the default number of workers
	workers := opts.Workers
	if workers == 0 {
		workers = DefaultWorkers
	}

	// Start exactly maxWorkers workers
	var wg sync.WaitGroup
	wg.Add(workers)

	// Launch worker pool
	for range workers {
		go func() {
			defer wg.Done()
			for file := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					filePath, err := DownloadFile(ctx, file)
					results <- &DownloadResult{
						DownloadJob:  *file,
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
		for _, job := range opts.Jobs {
			select {
			case <-ctx.Done():
				return // Context canceled
			case jobs <- job:
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
