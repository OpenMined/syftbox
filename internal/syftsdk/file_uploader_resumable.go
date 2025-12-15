package syftsdk

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imroc/req/v3"
)

const (
	defaultMultipartPartSize = int64(64 * 1024 * 1024) // 64MB keeps part count manageable
	minMultipartPartSize     = int64(5 * 1024 * 1024)  // S3/MinIO minimum
	maxMultipartParts        = 10000
)

type uploadSession struct {
	UploadID    string            `json:"uploadId"`
	Key         string            `json:"key"`
	FilePath    string            `json:"filePath"`
	Fingerprint string            `json:"fingerprint"`
	Size        int64             `json:"size"`
	PartSize    int64             `json:"partSize"`
	PartCount   int               `json:"partCount"`
	Completed   map[int]string    `json:"completed"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type resumableUploader struct {
	client      *req.Client
	params      *UploadParams
	fileInfo    os.FileInfo
	resumeDir   string
	fingerprint string
	session     *uploadSession
	stats       *httpStats
}

func newResumableUploader(client *req.Client, params *UploadParams, info os.FileInfo) *resumableUploader {
	resumeDir := params.ResumeDir
	if resumeDir == "" {
		resumeDir = filepath.Join(os.TempDir(), "syftbox-upload-cache")
	}

	fp := params.Fingerprint
	if fp == "" {
		fp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	}

	return &resumableUploader{
		client:      client,
		params:      params,
		fileInfo:    info,
		resumeDir:   resumeDir,
		fingerprint: fp,
		stats:       getHTTPStats(),
	}
}

func (u *resumableUploader) Upload(ctx context.Context) (*UploadResponse, error) {
	if err := u.prepareSession(); err != nil {
		return nil, err
	}

	uploadedBytes := u.completedBytes()
	if u.params.Callback != nil && uploadedBytes > 0 {
		u.params.Callback(uploadedBytes, u.session.Size)
	}

	file, err := os.Open(u.params.FilePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		remaining := u.remainingParts()
		if len(remaining) == 0 {
			break
		}

		resp, err := u.requestPartURLs(ctx, remaining)
		if err != nil {
			return nil, err
		}

		if u.session.UploadID == "" {
			u.session.UploadID = resp.UploadID
			u.session.PartCount = resp.PartCount
			u.session.PartSize = resp.PartSize
			if err := u.saveSession(); err != nil {
				return nil, err
			}
		}

		if err := u.uploadParts(ctx, file, resp.URLs); err != nil {
			return nil, err
		}
	}

	result, err := u.complete(ctx)
	if err != nil {
		return nil, err
	}

	_ = u.cleanup()
	return result, nil
}

func (u *resumableUploader) prepareSession() error {
	if err := os.MkdirAll(u.resumeDir, 0o755); err != nil {
		return fmt.Errorf("ensure resume dir: %w", err)
	}

	if err := u.loadSession(); err != nil {
		return err
	}

	if u.session != nil {
		return nil
	}

	partSize, partCount := u.selectPartSize(u.fileInfo.Size(), u.params.PartSize)

	u.session = &uploadSession{
		Key:         u.params.Key,
		FilePath:    u.params.FilePath,
		Fingerprint: u.fingerprint,
		Size:        u.fileInfo.Size(),
		PartSize:    partSize,
		PartCount:   partCount,
		Completed:   make(map[int]string),
	}

	return u.saveSession()
}

func (u *resumableUploader) requestPartURLs(ctx context.Context, parts []int) (*MultipartUploadResponse, error) {
	var resp *MultipartUploadResponse
	apiResp, err := u.client.R().
		SetContext(ctx).
		SetBody(&MultipartUploadRequest{
			Key:         u.params.Key,
			Size:        u.session.Size,
			PartSize:    u.session.PartSize,
			UploadID:    u.session.UploadID,
			PartNumbers: parts,
		}).
		SetSuccessResult(&resp).
		Post(v1BlobUploadMultipart)
	if err := handleAPIError(apiResp, err, "blob multipart upload"); err != nil {
		return nil, err
	}
	if resp == nil || resp.UploadID == "" || len(resp.URLs) == 0 {
		return nil, fmt.Errorf("invalid multipart upload response")
	}
	return resp, nil
}

func (u *resumableUploader) uploadParts(ctx context.Context, file *os.File, urls map[int]string) error {
	partNumbers := make([]int, 0, len(urls))
	for part := range urls {
		partNumbers = append(partNumbers, part)
	}
	sort.Ints(partNumbers)

	uploaded := u.completedBytes()

	for _, part := range partNumbers {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		url, ok := urls[part]
		if !ok {
			return fmt.Errorf("missing url for part %d", part)
		}

		offset := int64(part-1) * u.session.PartSize
		chunkSize := u.partSizeFor(part)
		sectionReader := io.NewSectionReader(file, offset, chunkSize)
		bodyReader := io.Reader(sectionReader)
		if u.stats != nil {
			bodyReader = &countingReader{r: sectionReader, onRead: u.stats.onSend}
		}

		partCtx := ctx
		cancel := func() {}
		if u.params.PartUploadTimeout > 0 {
			partCtx, cancel = context.WithTimeout(ctx, u.params.PartUploadTimeout)
		}

		req, err := http.NewRequestWithContext(partCtx, http.MethodPut, url, bodyReader)
		if err != nil {
			cancel()
			return fmt.Errorf("create request: %w", err)
		}
		req.ContentLength = chunkSize
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			if u.stats != nil {
				u.stats.setLastError(err)
			}
			return fmt.Errorf("upload part %d: %w", part, err)
		}
		if u.stats != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, &countingReader{r: resp.Body, onRead: u.stats.onRecv})
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("upload part %d failed with status %d", part, resp.StatusCode)
		}

		etag := strings.Trim(resp.Header.Get("ETag"), "\"")
		if etag == "" {
			etag = fmt.Sprintf("%d-%d", part, chunkSize)
		}

		u.session.Completed[part] = etag
		if err := u.saveSession(); err != nil {
			return err
		}

		// Optional artificial slowdown for demos/tests.
		if v := os.Getenv("SYFTBOX_UPLOAD_PART_SLEEP_MS"); v != "" {
			if ms, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && ms > 0 {
				time.Sleep(time.Duration(ms) * time.Millisecond)
			}
		}

		uploaded += chunkSize
		if u.params.Callback != nil {
			u.params.Callback(uploaded, u.session.Size)
		}
		u.emitAdvancedProgress(uploaded)
	}

	return nil
}

func (u *resumableUploader) emitAdvancedProgress(uploaded int64) {
	if u.params.AdvancedCallback == nil || u.session == nil {
		return
	}
	completedParts := make([]int, 0, len(u.session.Completed))
	for p := range u.session.Completed {
		completedParts = append(completedParts, p)
	}
	sort.Ints(completedParts)
	u.params.AdvancedCallback(UploadProgress{
		UploadedBytes:  uploaded,
		TotalBytes:     u.session.Size,
		CompletedParts: completedParts,
		PartSize:       u.session.PartSize,
		PartCount:      u.session.PartCount,
	})
}

func (u *resumableUploader) complete(ctx context.Context) (*UploadResponse, error) {
	parts := make([]*CompletedPart, 0, len(u.session.Completed))
	for partNumber, etag := range u.session.Completed {
		parts = append(parts, &CompletedPart{
			PartNumber: partNumber,
			ETag:       etag,
		})
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	var apiResp *UploadResponse
	resp, err := u.client.R().
		SetContext(ctx).
		SetBody(&CompleteMultipartUploadRequest{
			Key:      u.params.Key,
			UploadID: u.session.UploadID,
			Parts:    parts,
		}).
		SetSuccessResult(&apiResp).
		Post(v1BlobUploadComplete)

	if err := handleAPIError(resp, err, "blob upload complete"); err != nil {
		return nil, err
	}

	return apiResp, nil
}

func (u *resumableUploader) selectPartSize(size int64, override int64) (int64, int) {
	partSize := defaultMultipartPartSize
	if override > 0 {
		partSize = override
	}
	if partSize < minMultipartPartSize {
		partSize = minMultipartPartSize
	}

	partCount := int(divideAndCeil(size, partSize))
	for partCount > maxMultipartParts {
		partSize *= 2
		partCount = int(divideAndCeil(size, partSize))
	}

	return partSize, partCount
}

func (u *resumableUploader) partSizeFor(part int) int64 {
	offset := int64(part-1) * u.session.PartSize
	if offset >= u.session.Size {
		return 0
	}

	remaining := u.session.Size - offset
	if remaining < u.session.PartSize {
		return remaining
	}
	return u.session.PartSize
}

func (u *resumableUploader) remainingParts() []int {
	parts := make([]int, 0, u.session.PartCount)
	for i := 1; i <= u.session.PartCount; i++ {
		if _, ok := u.session.Completed[i]; !ok {
			parts = append(parts, i)
		}
	}
	return parts
}

func (u *resumableUploader) completedBytes() int64 {
	var total int64
	for part := range u.session.Completed {
		total += u.partSizeFor(part)
	}
	return total
}

func (u *resumableUploader) loadSession() error {
	data, err := os.ReadFile(u.sessionFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read resume file: %w", err)
	}

	var s uploadSession
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode resume file: %w", err)
	}

	if s.Key != u.params.Key || s.FilePath != u.params.FilePath || s.Fingerprint != u.fingerprint || s.Size != u.fileInfo.Size() {
		_ = os.Remove(u.sessionFilePath())
		return nil
	}

	if s.Completed == nil {
		s.Completed = make(map[int]string)
	}

	u.session = &s
	return nil
}

func (u *resumableUploader) saveSession() error {
	if u.session == nil {
		return nil
	}
	if u.session.Completed == nil {
		u.session.Completed = make(map[int]string)
	}
	data, err := json.Marshal(u.session)
	if err != nil {
		return fmt.Errorf("encode resume file: %w", err)
	}
	return os.WriteFile(u.sessionFilePath(), data, 0o644)
}

func (u *resumableUploader) cleanup() error {
	return os.Remove(u.sessionFilePath())
}

func (u *resumableUploader) sessionFilePath() string {
	hash := sha1.Sum([]byte(u.params.Key + "|" + u.params.FilePath))
	filename := hex.EncodeToString(hash[:]) + ".json"
	return filepath.Join(u.resumeDir, filename)
}

func divideAndCeil(numerator, denominator int64) int64 {
	if denominator == 0 {
		return 0
	}
	quotient := numerator / denominator
	if numerator%denominator != 0 {
		quotient++
	}
	return quotient
}

// CleanupStaleSessions removes session files older than maxAge from the resume directory.
// It also attempts to abort the corresponding server-side multipart uploads.
func CleanupStaleSessions(client *req.Client, resumeDir string, maxAge time.Duration) (cleaned int, errs []error) {
	entries, err := os.ReadDir(resumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, []error{fmt.Errorf("read resume dir: %w", err)}
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(resumeDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("stat %s: %w", entry.Name(), err))
			continue
		}

		if now.Sub(info.ModTime()) < maxAge {
			continue
		}

		// Try to abort server-side multipart upload
		if client != nil {
			if abortErr := abortSessionUpload(client, path); abortErr != nil {
				errs = append(errs, fmt.Errorf("abort %s: %w", entry.Name(), abortErr))
			}
		}

		// Remove the session file
		if removeErr := os.Remove(path); removeErr != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", entry.Name(), removeErr))
		} else {
			cleaned++
		}
	}

	return cleaned, errs
}

func abortSessionUpload(client *req.Client, sessionPath string) error {
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return err
	}

	var session uploadSession
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}

	if session.UploadID == "" || session.Key == "" {
		return nil // No server-side upload to abort
	}

	resp, err := client.R().
		SetBody(&AbortMultipartUploadRequest{
			Key:      session.Key,
			UploadID: session.UploadID,
		}).
		Post(v1BlobUploadAbort)

	if err != nil {
		return err
	}
	if resp.IsErrorState() {
		return fmt.Errorf("abort failed: %s", resp.Status)
	}
	return nil
}
