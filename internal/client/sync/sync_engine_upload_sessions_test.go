package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openmined/syftbox/internal/client/workspace"
	"github.com/openmined/syftbox/internal/syftsdk"
)

func TestUploadSessionDirConsistency(t *testing.T) {
	if uploadSessionsDirName != "upload-sessions" {
		t.Fatalf("unexpected upload sessions dir name: %s", uploadSessionsDirName)
	}

	root := t.TempDir()
	ws, err := workspace.NewWorkspace(root, "alice@example.com")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if err := os.MkdirAll(ws.MetadataDir, 0o755); err != nil {
		t.Fatalf("mkdir metadata: %v", err)
	}

	sdk, err := syftsdk.New(&syftsdk.SyftSDKConfig{
		BaseURL:      "http://localhost",
		Email:        "alice@example.com",
		RefreshToken: "test-refresh",
	})
	if err != nil {
		t.Fatalf("sdk: %v", err)
	}

	ignore := NewSyncIgnoreList(ws.DatasitesDir)
	priority := NewSyncPriorityList(ws.DatasitesDir)
	se, err := NewSyncEngine(ws, sdk, ignore, priority)
	if err != nil {
		t.Fatalf("sync engine: %v", err)
	}

	expected := filepath.Join(ws.MetadataDir, uploadSessionsDirName)
	if se.uploadRegistry.resumeDir != expected {
		t.Fatalf("resumeDir mismatch: expected %s, got %s", expected, se.uploadRegistry.resumeDir)
	}
}
