package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewUploadRegistry(t *testing.T) {
	r := NewUploadRegistry("/tmp/test-resume")
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.resumeDir != "/tmp/test-resume" {
		t.Errorf("expected resumeDir=/tmp/test-resume, got %s", r.resumeDir)
	}
}

func TestUploadRegistry_Register(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, ctx, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1024*1024)
	defer cancel()

	if info == nil {
		t.Fatal("expected non-nil upload info")
	}
	if info.Key != "user@example.com/file.txt" {
		t.Errorf("expected key=user@example.com/file.txt, got %s", info.Key)
	}
	if info.LocalPath != "/local/path/file.txt" {
		t.Errorf("expected localPath=/local/path/file.txt, got %s", info.LocalPath)
	}
	if info.Size != 1024*1024 {
		t.Errorf("expected size=1048576, got %d", info.Size)
	}
	if info.State != UploadStateUploading {
		t.Errorf("expected state=uploading, got %s", info.State)
	}
	if info.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ctx == nil {
		t.Error("expected non-nil context")
	}
}

func TestUploadRegistry_RegisterExisting(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info1, _, cancel1 := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1024)
	defer cancel1()

	info2, _, cancel2 := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1024)
	defer cancel2()

	if info1.ID != info2.ID {
		t.Errorf("expected same ID for same key/path, got %s != %s", info1.ID, info2.ID)
	}
}

func TestUploadRegistry_UpdateProgress(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	r.UpdateProgress(info.ID, 500, []int{1, 2, 3}, 200, 5)

	updated, err := r.Get(info.ID)
	if err != nil {
		t.Fatalf("failed to get upload: %v", err)
	}

	if updated.UploadedBytes != 500 {
		t.Errorf("expected uploadedBytes=500, got %d", updated.UploadedBytes)
	}
	if len(updated.CompletedParts) != 3 {
		t.Errorf("expected 3 completed parts, got %d", len(updated.CompletedParts))
	}
	if updated.PartSize != 200 {
		t.Errorf("expected partSize=200, got %d", updated.PartSize)
	}
	if updated.PartCount != 5 {
		t.Errorf("expected partCount=5, got %d", updated.PartCount)
	}
	if updated.Progress != 50.0 {
		t.Errorf("expected progress=50.0, got %f", updated.Progress)
	}
}

func TestUploadRegistry_SetCompleted(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	r.SetCompleted(info.ID)

	_, err := r.Get(info.ID)
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound after completion, got %v", err)
	}

	uploads := r.List()
	if len(uploads) != 0 {
		t.Errorf("expected 0 uploads after completion, got %d", len(uploads))
	}
}

func TestUploadRegistry_SetError(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	testErr := context.DeadlineExceeded
	r.SetError(info.ID, testErr)

	updated, err := r.Get(info.ID)
	if err != nil {
		t.Fatalf("failed to get upload: %v", err)
	}

	if updated.State != UploadStateError {
		t.Errorf("expected state=error, got %s", updated.State)
	}
	if updated.Error != testErr.Error() {
		t.Errorf("expected error=%s, got %s", testErr.Error(), updated.Error)
	}
}

func TestUploadRegistry_Pause(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	err := r.Pause(info.ID)
	if err != nil {
		t.Fatalf("failed to pause: %v", err)
	}

	updated, err := r.Get(info.ID)
	if err != nil {
		t.Fatalf("failed to get upload: %v", err)
	}

	if updated.State != UploadStatePaused {
		t.Errorf("expected state=paused, got %s", updated.State)
	}
	if !updated.Paused {
		t.Error("expected Paused=true")
	}
}

func TestUploadRegistry_PauseNotFound(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	err := r.Pause("nonexistent")
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound, got %v", err)
	}
}

func TestUploadRegistry_PauseAlreadyPaused(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	_ = r.Pause(info.ID)
	err := r.Pause(info.ID)
	if err != nil {
		t.Errorf("expected no error when pausing already paused upload, got %v", err)
	}
}

func TestUploadRegistry_Resume(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	_ = r.Pause(info.ID)

	err := r.Resume(info.ID)
	if err != nil {
		t.Fatalf("failed to resume: %v", err)
	}

	updated, err := r.Get(info.ID)
	if err != nil {
		t.Fatalf("failed to get upload: %v", err)
	}

	if updated.State != UploadStatePending {
		t.Errorf("expected state=pending, got %s", updated.State)
	}
	if updated.Paused {
		t.Error("expected Paused=false")
	}
}

func TestUploadRegistry_ResumeNotPaused(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	err := r.Resume(info.ID)
	if err == nil {
		t.Error("expected error when resuming non-paused upload")
	}
}

func TestUploadRegistry_Cancel(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	err := r.Cancel(info.ID)
	if err != nil {
		t.Fatalf("failed to cancel: %v", err)
	}

	_, err = r.Get(info.ID)
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound after cancel, got %v", err)
	}
}

func TestUploadRegistry_Restart(t *testing.T) {
	resumeDir := t.TempDir()
	r := NewUploadRegistry(resumeDir)

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	r.UpdateProgress(info.ID, 500, []int{1, 2}, 250, 4)

	sessionFile := filepath.Join(resumeDir, info.ID+".json")
	sessionData := map[string]interface{}{
		"uploadId":  "test-upload-id",
		"key":       "user@example.com/file.txt",
		"filePath":  "/local/path/file.txt",
		"size":      1000,
		"partSize":  250,
		"partCount": 4,
		"completed": map[string]string{"1": "etag1", "2": "etag2"},
	}
	data, _ := json.Marshal(sessionData)
	_ = os.WriteFile(sessionFile, data, 0644)

	err := r.Restart(info.ID)
	if err != nil {
		t.Fatalf("failed to restart: %v", err)
	}

	updated, err := r.Get(info.ID)
	if err != nil {
		t.Fatalf("failed to get upload: %v", err)
	}

	if updated.State != UploadStatePending {
		t.Errorf("expected state=pending, got %s", updated.State)
	}
	if updated.UploadedBytes != 0 {
		t.Errorf("expected uploadedBytes=0, got %d", updated.UploadedBytes)
	}
	if len(updated.CompletedParts) != 0 {
		t.Errorf("expected 0 completed parts, got %d", len(updated.CompletedParts))
	}
	if updated.Progress != 0 {
		t.Errorf("expected progress=0, got %f", updated.Progress)
	}

	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Error("expected session file to be deleted")
	}
}

func TestUploadRegistry_List(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	_, _, cancel1 := r.Register("user@example.com/file1.txt", "/local/path/file1.txt", 1000)
	defer cancel1()

	_, _, cancel2 := r.Register("user@example.com/file2.txt", "/local/path/file2.txt", 2000)
	defer cancel2()

	uploads := r.List()
	if len(uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(uploads))
	}
}

func TestUploadRegistry_GetByPath(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	found, err := r.GetByPath("user@example.com/file.txt")
	if err != nil {
		t.Fatalf("failed to get by path: %v", err)
	}

	if found.ID != info.ID {
		t.Errorf("expected ID=%s, got %s", info.ID, found.ID)
	}
}

func TestUploadRegistry_GetByPathNotFound(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	_, err := r.GetByPath("nonexistent")
	if err != ErrUploadNotFound {
		t.Errorf("expected ErrUploadNotFound, got %v", err)
	}
}

func TestUploadRegistry_IsPaused(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	if r.IsPaused(info.ID) {
		t.Error("expected IsPaused=false for new upload")
	}

	_ = r.Pause(info.ID)

	if !r.IsPaused(info.ID) {
		t.Error("expected IsPaused=true after pause")
	}
}

func TestUploadRegistry_LoadFromDisk(t *testing.T) {
	resumeDir := t.TempDir()

	sessionData := map[string]interface{}{
		"uploadId":  "test-upload-id",
		"key":       "user@example.com/file.txt",
		"filePath":  "/local/path/file.txt",
		"size":      int64(1000),
		"partSize":  int64(250),
		"partCount": 4,
		"completed": map[string]string{"1": "etag1", "2": "etag2"},
	}
	data, _ := json.Marshal(sessionData)

	r := NewUploadRegistry(resumeDir)
	expectedID := r.generateID("user@example.com/file.txt", "/local/path/file.txt")

	sessionFile := filepath.Join(resumeDir, expectedID+".json")
	_ = os.WriteFile(sessionFile, data, 0644)

	err := r.LoadFromDisk()
	if err != nil {
		t.Fatalf("failed to load from disk: %v", err)
	}

	uploads := r.List()
	if len(uploads) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(uploads))
	}

	info := uploads[0]
	if info.Key != "user@example.com/file.txt" {
		t.Errorf("expected key=user@example.com/file.txt, got %s", info.Key)
	}
	if info.State != UploadStatePaused {
		t.Errorf("expected state=paused, got %s", info.State)
	}
	if len(info.CompletedParts) != 2 {
		t.Errorf("expected 2 completed parts, got %d", len(info.CompletedParts))
	}
	if info.UploadedBytes != 500 {
		t.Errorf("expected uploadedBytes=500, got %d", info.UploadedBytes)
	}
}

func TestUploadRegistry_LoadFromDiskEmptyDir(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	err := r.LoadFromDisk()
	if err != nil {
		t.Fatalf("expected no error for empty dir, got %v", err)
	}
}

func TestUploadRegistry_LoadFromDiskNonExistent(t *testing.T) {
	r := NewUploadRegistry("/nonexistent/path")

	err := r.LoadFromDisk()
	if err != nil {
		t.Fatalf("expected no error for non-existent dir, got %v", err)
	}
}

func TestUploadRegistry_Close(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	_, _, cancel1 := r.Register("user@example.com/file1.txt", "/local/path/file1.txt", 1000)
	defer cancel1()

	_, _, cancel2 := r.Register("user@example.com/file2.txt", "/local/path/file2.txt", 2000)
	defer cancel2()

	r.Close()

	uploads := r.List()
	if len(uploads) != 0 {
		t.Errorf("expected 0 uploads after close, got %d", len(uploads))
	}
}

func TestUploadRegistry_ConcurrentAccess(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
			r.UpdateProgress(info.ID, int64(i*10), []int{i}, 100, 10)
			cancel()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			r.List()
			r.IsPaused("nonexistent")
		}
		done <- true
	}()

	<-done
	<-done
}

func TestUploadRegistry_ProgressCalculation(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()

	r.UpdateProgress(info.ID, 250, []int{1}, 250, 4)
	updated, _ := r.Get(info.ID)
	if updated.Progress != 25.0 {
		t.Errorf("expected progress=25.0, got %f", updated.Progress)
	}

	r.UpdateProgress(info.ID, 750, []int{1, 2, 3}, 250, 4)
	updated, _ = r.Get(info.ID)
	if updated.Progress != 75.0 {
		t.Errorf("expected progress=75.0, got %f", updated.Progress)
	}

	r.UpdateProgress(info.ID, 1000, []int{1, 2, 3, 4}, 250, 4)
	updated, _ = r.Get(info.ID)
	if updated.Progress != 100.0 {
		t.Errorf("expected progress=100.0, got %f", updated.Progress)
	}
}

func TestUploadRegistry_ZeroSizeFile(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	info, _, cancel := r.Register("user@example.com/empty.txt", "/local/path/empty.txt", 0)
	defer cancel()

	r.UpdateProgress(info.ID, 0, nil, 0, 0)

	updated, _ := r.Get(info.ID)
	if updated.Progress != 0 {
		t.Errorf("expected progress=0 for zero-size file, got %f", updated.Progress)
	}
}

func TestUploadRegistry_Timestamps(t *testing.T) {
	r := NewUploadRegistry(t.TempDir())

	before := time.Now()
	info, _, cancel := r.Register("user@example.com/file.txt", "/local/path/file.txt", 1000)
	defer cancel()
	after := time.Now()

	if info.StartedAt.Before(before) || info.StartedAt.After(after) {
		t.Error("StartedAt should be between before and after")
	}
	if info.UpdatedAt.Before(before) || info.UpdatedAt.After(after) {
		t.Error("UpdatedAt should be between before and after")
	}

	originalUpdatedAt := info.UpdatedAt
	time.Sleep(50 * time.Millisecond)
	r.UpdateProgress(info.ID, 500, []int{1}, 500, 2)

	updated, _ := r.Get(info.ID)
	if !updated.UpdatedAt.After(originalUpdatedAt) {
		t.Errorf("UpdatedAt should be updated after progress update: %v vs %v", updated.UpdatedAt, originalUpdatedAt)
	}
}
