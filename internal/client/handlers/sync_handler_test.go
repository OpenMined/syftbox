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

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSyncHandler_Status_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewSyncHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sync/status", nil)

	handler.Status(c)

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

func TestSyncHandler_StatusByPath_NoPath(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewSyncHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sync/status/file", nil)

	handler.StatusByPath(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestSyncHandler_StatusByPath_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewSyncHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sync/status/file?path=test.txt", nil)

	handler.StatusByPath(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestSyncHandler_TriggerSync_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewSyncHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/sync/now", nil)

	handler.TriggerSync(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestSyncHandler_Events_NoDatasite(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewSyncHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sync/events", nil)

	handler.Events(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestSyncFileStatus_JSON(t *testing.T) {
	status := SyncFileStatus{
		Path:          "user@example.com/test.txt",
		State:         "syncing",
		ConflictState: "none",
		Progress:      50.5,
		Error:         "",
		ErrorCount:    0,
		UpdatedAt:     time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled SyncFileStatus
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.Path != status.Path {
		t.Errorf("expected path=%s, got %s", status.Path, unmarshaled.Path)
	}
	if unmarshaled.State != status.State {
		t.Errorf("expected state=%s, got %s", status.State, unmarshaled.State)
	}
	if unmarshaled.Progress != status.Progress {
		t.Errorf("expected progress=%f, got %f", status.Progress, unmarshaled.Progress)
	}
}

func TestSyncStatusResponse_JSON(t *testing.T) {
	resp := SyncStatusResponse{
		Files: []SyncFileStatus{
			{Path: "file1.txt", State: "syncing", Progress: 50},
			{Path: "file2.txt", State: "completed", Progress: 100},
		},
		Summary: SyncSummary{
			Pending:   1,
			Syncing:   2,
			Completed: 3,
			Error:     0,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled SyncStatusResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(unmarshaled.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(unmarshaled.Files))
	}
	if unmarshaled.Summary.Pending != 1 {
		t.Errorf("expected pending=1, got %d", unmarshaled.Summary.Pending)
	}
	if unmarshaled.Summary.Syncing != 2 {
		t.Errorf("expected syncing=2, got %d", unmarshaled.Summary.Syncing)
	}
}
