package acl

import (
	"os"
	"path/filepath"
	"testing"

	mapset "github.com/deckarep/golang-set/v2"
)

func TestLoad(t *testing.T) {
	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "syft-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test case 1: Valid permissions file
	validAcl := `
terminal: true
rules:
  - path: "**/*"
    access:
      admin: ["user1"]
      read: ["*"]
      write: ["user2"]
    limits:
      maxFiles: 100
      maxFileSize: 1024
      allowDirs: true
      allowSymlinks: false
`
	validPath := filepath.Join(tmpDir, AclFileName)
	if err := os.WriteFile(validPath, []byte(validAcl), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Load(tmpDir)
	if err != nil {
		t.Errorf("Load() error = %v", err)
	}
	if p == nil {
		t.Fatal("Load() returned nil permissions")
	}
	if !p.Terminal {
		t.Error("Load() didn't parse Terminal field correctly")
	}
	if len(p.Rules) != 1 {
		t.Error("Load() didn't parse Rules correctly")
	}

	// Test case 2: Missing file
	_, err = Load("/nonexistent")
	if err == nil {
		t.Error("Load() should fail with nonexistent file")
	}

	// Test case 3: Invalid YAML
	invalidPath := filepath.Join(tmpDir, AclFileName)
	if err := os.WriteFile(invalidPath, []byte("invalid: }: yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = Load(invalidPath)
	if err == nil {
		t.Error("Load() should fail with invalid YAML")
	}
}

func TestSave(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "syft-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	perm := &RuleSet{
		Terminal: true,
		Rules: []*Rule{
			{
				Pattern: "**/*",
				Access: &Access{
					Admin: mapset.NewSet("user1"),
					Read:  mapset.NewSet(Everyone),
					Write: mapset.NewSet("user2"),
				},
				Limits: &Limit{
					MaxFiles:      100,
					MaxFileSize:   1024,
					AllowDirs:     true,
					AllowSymlinks: false,
				},
			},
		},
	}

	savePath := filepath.Join(tmpDir, AclFileName)
	if err := perm.Save(savePath); err != nil {
		t.Errorf("Save() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		t.Error("Save() didn't create file")
	}

	// Verify contents by loading
	loaded, err := Load(savePath)
	if err != nil {
		t.Errorf("Couldn't load saved file: %v", err)
	}
	if !loaded.Terminal {
		t.Error("Saved file doesn't match original")
	}
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/path/to/dir", "/path/to/dir/" + AclFileName},
		{"/path/to/" + AclFileName, "/path/to/" + AclFileName},
		{".", AclFileName},
	}

	for _, tt := range tests {
		result := resolvePath(tt.input)
		if result != tt.expected {
			t.Errorf("resolvePath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsAclFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/path/to/" + AclFileName, true},
		{"/path/to/other.yaml", false},
		{AclFileName, true},
		{"", false},
	}

	for _, tt := range tests {
		result := isAclFile(tt.path)
		if result != tt.expected {
			t.Errorf("isAclFile(%q) = %v, want %v", tt.path, result, tt.expected)
		}
	}
}
