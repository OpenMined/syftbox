package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

func TestUploadHandler_List_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/uploads", nil)

	handler.List(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var resp ControlPlaneError
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.ErrorCode != ErrCodeDatasiteNotReady {
		t.Errorf("expected error code %s, got %s", ErrCodeDatasiteNotReady, resp.ErrorCode)
	}
}

func TestUploadHandler_Get_NoID(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/uploads/", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.Get(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUploadHandler_Get_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/uploads/abc123", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc123"}}

	handler.Get(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestUploadHandler_Pause_NoID(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads//pause", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.Pause(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUploadHandler_Pause_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads/abc123/pause", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc123"}}

	handler.Pause(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestUploadHandler_Resume_NoID(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads//resume", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.Resume(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUploadHandler_Resume_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads/abc123/resume", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc123"}}

	handler.Resume(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestUploadHandler_Restart_NoID(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads//restart", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.Restart(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUploadHandler_Restart_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads/abc123/restart", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc123"}}

	handler.Restart(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestUploadHandler_Cancel_NoID(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v1/uploads/", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.Cancel(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestUploadHandler_Cancel_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewUploadHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v1/uploads/abc123", nil)
	c.Params = gin.Params{{Key: "id", Value: "abc123"}}

	handler.Cancel(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestUploadInfoResponse_JSON(t *testing.T) {
	now := time.Now()
	resp := UploadInfoResponse{
		ID:             "abc123",
		Key:            "user@example.com/large-file.bin",
		LocalPath:      "/local/path/large-file.bin",
		State:          "uploading",
		Size:           1073741824, // 1GB
		UploadedBytes:  536870912,  // 512MB
		PartSize:       67108864,   // 64MB
		PartCount:      16,
		CompletedParts: []int{1, 2, 3, 4, 5, 6, 7, 8},
		Progress:       50.0,
		Error:          "",
		StartedAt:      now,
		UpdatedAt:      now,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled UploadInfoResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.ID != resp.ID {
		t.Errorf("expected ID=%s, got %s", resp.ID, unmarshaled.ID)
	}
	if unmarshaled.Key != resp.Key {
		t.Errorf("expected Key=%s, got %s", resp.Key, unmarshaled.Key)
	}
	if unmarshaled.Size != resp.Size {
		t.Errorf("expected Size=%d, got %d", resp.Size, unmarshaled.Size)
	}
	if unmarshaled.UploadedBytes != resp.UploadedBytes {
		t.Errorf("expected UploadedBytes=%d, got %d", resp.UploadedBytes, unmarshaled.UploadedBytes)
	}
	if len(unmarshaled.CompletedParts) != len(resp.CompletedParts) {
		t.Errorf("expected %d completed parts, got %d", len(resp.CompletedParts), len(unmarshaled.CompletedParts))
	}
	if unmarshaled.Progress != resp.Progress {
		t.Errorf("expected Progress=%f, got %f", resp.Progress, unmarshaled.Progress)
	}
}

func TestUploadListResponse_JSON(t *testing.T) {
	resp := UploadListResponse{
		Uploads: []UploadInfoResponse{
			{ID: "upload1", Key: "file1.bin", State: "uploading", Progress: 25.0},
			{ID: "upload2", Key: "file2.bin", State: "paused", Progress: 75.0},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled UploadListResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(unmarshaled.Uploads) != 2 {
		t.Errorf("expected 2 uploads, got %d", len(unmarshaled.Uploads))
	}
	if unmarshaled.Uploads[0].ID != "upload1" {
		t.Errorf("expected first upload ID=upload1, got %s", unmarshaled.Uploads[0].ID)
	}
	if unmarshaled.Uploads[1].State != "paused" {
		t.Errorf("expected second upload state=paused, got %s", unmarshaled.Uploads[1].State)
	}
}

func TestUploadInfoResponse_JSONOmitEmpty(t *testing.T) {
	resp := UploadInfoResponse{
		ID:    "abc123",
		Key:   "file.txt",
		State: "completed",
		Size:  1000,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Check that omitempty fields are not present
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["error"]; exists {
		t.Error("expected 'error' field to be omitted when empty")
	}
	if _, exists := raw["completedParts"]; exists {
		t.Error("expected 'completedParts' field to be omitted when nil")
	}
}
