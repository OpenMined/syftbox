package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmined/syftbox/internal/client/datasitemgr"
)

func TestStatusHandler_Status_NoDatasite_IncludesClientRuntime(t *testing.T) {
	mgr := datasitemgr.New()
	handler := NewStatusHandler(mgr)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/status", nil)

	handler.Status(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Datasite == nil || resp.Datasite.Status != string(datasitemgr.DatasiteStatusUnprovisioned) {
		t.Fatalf("expected datasite status UNPROVISIONED, got %+v", resp.Datasite)
	}

	if resp.Runtime == nil || resp.Runtime.Client == nil {
		t.Fatalf("expected runtime.client to be present, got %+v", resp.Runtime)
	}
	if resp.Runtime.Client.Version == "" || resp.Runtime.Client.StartedAt == "" {
		t.Fatalf("expected version and started_at set, got %+v", resp.Runtime.Client)
	}
}

