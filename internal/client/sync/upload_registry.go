package sync

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrUploadNotFound = errors.New("upload not found")
	ErrUploadPaused   = errors.New("upload is paused")
	ErrUploadActive   = errors.New("upload is active")
)

type UploadState string

const (
	UploadStatePending   UploadState = "pending"
	UploadStateUploading UploadState = "uploading"
	UploadStatePaused    UploadState = "paused"
	UploadStateCompleted UploadState = "completed"
	UploadStateError     UploadState = "error"
)

const uploadSessionsDirName = "upload-sessions"

type UploadInfo struct {
	ID             string      `json:"id"`
	Key            string      `json:"key"`
	LocalPath      string      `json:"localPath"`
	State          UploadState `json:"state"`
	Size           int64       `json:"size"`
	UploadedBytes  int64       `json:"uploadedBytes"`
	PartSize       int64       `json:"partSize"`
	PartCount      int         `json:"partCount"`
	CompletedParts []int       `json:"completedParts"`
	Progress       float64     `json:"progress"`
	Error          string      `json:"error,omitempty"`
	StartedAt      time.Time   `json:"startedAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	Paused         bool        `json:"paused"`
}

type uploadEntry struct {
	info     *UploadInfo
	cancel   context.CancelFunc
	pauseCh  chan struct{}
	resumeCh chan struct{}
	mu       sync.RWMutex
}

type UploadRegistry struct {
	uploads   map[string]*uploadEntry
	byPath    map[string]string // key path -> upload ID
	resumeDir string
	mu        sync.RWMutex
}

func NewUploadRegistry(resumeDir string) *UploadRegistry {
	return &UploadRegistry{
		uploads:   make(map[string]*uploadEntry),
		byPath:    make(map[string]string),
		resumeDir: resumeDir,
	}
}

func (r *UploadRegistry) generateID(key, localPath string) string {
	hash := sha1.Sum([]byte(key + "|" + localPath))
	return hex.EncodeToString(hash[:8])
}

func (r *UploadRegistry) Register(key, localPath string, size int64) (*UploadInfo, context.Context, context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.generateID(key, localPath)

	if existing, exists := r.uploads[id]; exists {
		existing.mu.Lock()
		existing.info.State = UploadStateUploading
		existing.info.UpdatedAt = time.Now()
		existing.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		existing.cancel = cancel
		return existing.info, ctx, cancel
	}

	ctx, cancel := context.WithCancel(context.Background())

	info := &UploadInfo{
		ID:        id,
		Key:       key,
		LocalPath: localPath,
		State:     UploadStateUploading,
		Size:      size,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	entry := &uploadEntry{
		info:     info,
		cancel:   cancel,
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
	}

	r.uploads[id] = entry
	r.byPath[key] = id

	return info, ctx, cancel
}

// TryRegister is like Register, but returns alreadyActive=true if an upload for this key/localPath
// is already in-flight (uploading/pending/paused). In that case ctx/cancel are nil and caller
// should skip starting a duplicate upload goroutine.
func (r *UploadRegistry) TryRegister(key, localPath string, size int64) (*UploadInfo, context.Context, context.CancelFunc, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.generateID(key, localPath)

	if existing, exists := r.uploads[id]; exists {
		// Snapshot current state under entry lock.
		existing.mu.RLock()
		state := existing.info.State
		existing.mu.RUnlock()

		switch state {
		case UploadStateUploading, UploadStatePending, UploadStatePaused:
			// If we loaded a paused session from disk (no live cancel ctx), allow it
			// to be re-registered so the next sync can resume automatically.
			if state == UploadStatePaused && existing.cancel == nil {
				existing.mu.Lock()
				existing.info.State = UploadStateUploading
				existing.info.UpdatedAt = time.Now()
				existing.info.Paused = false
				existing.mu.Unlock()

				ctx, cancel := context.WithCancel(context.Background())
				existing.cancel = cancel
				return existing.info, ctx, cancel, false
			}
			// Already tracked and active/paused; don't start another goroutine.
			return existing.info, nil, nil, true
		default:
			// For error or other non-active states, allow retry by re-arming context.
			existing.mu.Lock()
			existing.info.State = UploadStateUploading
			existing.info.UpdatedAt = time.Now()
			existing.info.Paused = false
			existing.mu.Unlock()

			ctx, cancel := context.WithCancel(context.Background())
			existing.cancel = cancel
			return existing.info, ctx, cancel, false
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	info := &UploadInfo{
		ID:        id,
		Key:       key,
		LocalPath: localPath,
		State:     UploadStateUploading,
		Size:      size,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	entry := &uploadEntry{
		info:     info,
		cancel:   cancel,
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
	}

	r.uploads[id] = entry
	r.byPath[key] = id

	return info, ctx, cancel, false
}

func (r *UploadRegistry) UpdateProgress(id string, uploadedBytes int64, completedParts []int, partSize int64, partCount int) {
	r.mu.RLock()
	entry, exists := r.uploads[id]
	r.mu.RUnlock()
	if !exists {
		return
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.info.UploadedBytes = uploadedBytes
	entry.info.CompletedParts = completedParts
	entry.info.PartSize = partSize
	entry.info.PartCount = partCount
	if entry.info.Size > 0 {
		entry.info.Progress = float64(uploadedBytes) / float64(entry.info.Size) * 100.0
	}
	entry.info.UpdatedAt = time.Now()
}

func (r *UploadRegistry) SetCompleted(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.uploads[id]
	if !exists {
		return
	}

	entry.mu.Lock()
	entry.info.State = UploadStateCompleted
	entry.info.Progress = 100.0
	entry.info.UploadedBytes = entry.info.Size
	entry.info.UpdatedAt = time.Now()
	entry.mu.Unlock()

	delete(r.byPath, entry.info.Key)
	delete(r.uploads, id)
}

func (r *UploadRegistry) SetError(id string, err error) {
	r.mu.RLock()
	entry, exists := r.uploads[id]
	r.mu.RUnlock()
	if !exists {
		return
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.info.State = UploadStateError
	if err != nil {
		entry.info.Error = err.Error()
	}
	entry.info.UpdatedAt = time.Now()
}

func (r *UploadRegistry) Pause(id string) error {
	r.mu.RLock()
	entry, exists := r.uploads[id]
	r.mu.RUnlock()
	if !exists {
		return ErrUploadNotFound
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.info.State == UploadStatePaused {
		return nil
	}

	if entry.info.State != UploadStateUploading {
		return errors.New("can only pause uploading state")
	}

	entry.info.State = UploadStatePaused
	entry.info.Paused = true
	entry.info.UpdatedAt = time.Now()

	if entry.cancel != nil {
		entry.cancel()
	}

	return nil
}

func (r *UploadRegistry) Resume(id string) error {
	r.mu.RLock()
	entry, exists := r.uploads[id]
	r.mu.RUnlock()
	if !exists {
		return ErrUploadNotFound
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.info.State != UploadStatePaused {
		return errors.New("can only resume paused uploads")
	}

	entry.info.State = UploadStatePending
	entry.info.Paused = false
	entry.info.UpdatedAt = time.Now()

	select {
	case entry.resumeCh <- struct{}{}:
	default:
	}

	return nil
}

func (r *UploadRegistry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.uploads[id]
	if !exists {
		return ErrUploadNotFound
	}

	if entry.cancel != nil {
		entry.cancel()
	}

	delete(r.byPath, entry.info.Key)
	delete(r.uploads, id)

	return nil
}

func (r *UploadRegistry) Restart(id string) error {
	r.mu.Lock()
	entry, exists := r.uploads[id]
	r.mu.Unlock()
	if !exists {
		return ErrUploadNotFound
	}

	entry.mu.Lock()
	key := entry.info.Key
	localPath := entry.info.LocalPath
	entry.mu.Unlock()

	sessionFile := filepath.Join(r.resumeDir, r.generateID(key, localPath)+".json")
	_ = os.Remove(sessionFile)

	r.mu.Lock()
	if entry.cancel != nil {
		entry.cancel()
	}

	entry.info.State = UploadStatePending
	entry.info.UploadedBytes = 0
	entry.info.CompletedParts = nil
	entry.info.Progress = 0
	entry.info.Error = ""
	entry.info.Paused = false
	entry.info.UpdatedAt = time.Now()
	r.mu.Unlock()

	return nil
}

func (r *UploadRegistry) Get(id string) (*UploadInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.uploads[id]
	if !exists {
		return nil, ErrUploadNotFound
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()

	infoCopy := *entry.info
	if entry.info.CompletedParts != nil {
		infoCopy.CompletedParts = make([]int, len(entry.info.CompletedParts))
		copy(infoCopy.CompletedParts, entry.info.CompletedParts)
	}
	return &infoCopy, nil
}

func (r *UploadRegistry) GetByPath(key string) (*UploadInfo, error) {
	r.mu.RLock()
	id, exists := r.byPath[key]
	r.mu.RUnlock()
	if !exists {
		return nil, ErrUploadNotFound
	}
	return r.Get(id)
}

func (r *UploadRegistry) List() []*UploadInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*UploadInfo, 0, len(r.uploads))
	for _, entry := range r.uploads {
		entry.mu.RLock()
		infoCopy := *entry.info
		if entry.info.CompletedParts != nil {
			infoCopy.CompletedParts = make([]int, len(entry.info.CompletedParts))
			copy(infoCopy.CompletedParts, entry.info.CompletedParts)
		}
		entry.mu.RUnlock()
		result = append(result, &infoCopy)
	}
	return result
}

func (r *UploadRegistry) IsPaused(id string) bool {
	r.mu.RLock()
	entry, exists := r.uploads[id]
	r.mu.RUnlock()
	if !exists {
		return false
	}

	entry.mu.RLock()
	defer entry.mu.RUnlock()
	return entry.info.Paused
}

func (r *UploadRegistry) LoadFromDisk() error {
	entries, err := os.ReadDir(r.resumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(r.resumeDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var session struct {
			UploadID  string         `json:"uploadId"`
			Key       string         `json:"key"`
			FilePath  string         `json:"filePath"`
			Size      int64          `json:"size"`
			PartSize  int64          `json:"partSize"`
			PartCount int            `json:"partCount"`
			Completed map[int]string `json:"completed"`
		}
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		if session.Key == "" || session.FilePath == "" {
			continue
		}

		id := r.generateID(session.Key, session.FilePath)
		completedParts := make([]int, 0, len(session.Completed))
		var uploadedBytes int64
		for partNum := range session.Completed {
			completedParts = append(completedParts, partNum)
			if partNum == session.PartCount {
				uploadedBytes += session.Size - int64(partNum-1)*session.PartSize
			} else {
				uploadedBytes += session.PartSize
			}
		}

		info := &UploadInfo{
			ID:             id,
			Key:            session.Key,
			LocalPath:      session.FilePath,
			State:          UploadStatePaused,
			Size:           session.Size,
			UploadedBytes:  uploadedBytes,
			PartSize:       session.PartSize,
			PartCount:      session.PartCount,
			CompletedParts: completedParts,
			Paused:         true,
			UpdatedAt:      time.Now(),
		}
		if info.Size > 0 {
			info.Progress = float64(uploadedBytes) / float64(info.Size) * 100.0
		}

		r.mu.Lock()
		r.uploads[id] = &uploadEntry{
			info:     info,
			pauseCh:  make(chan struct{}),
			resumeCh: make(chan struct{}),
		}
		r.byPath[session.Key] = id
		r.mu.Unlock()
	}

	return nil
}

func (r *UploadRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.uploads {
		if entry.cancel != nil {
			entry.cancel()
		}
	}

	r.uploads = make(map[string]*uploadEntry)
	r.byPath = make(map[string]string)
}
