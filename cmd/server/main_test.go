package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigFileLoading(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
SYFTBOX_BLOB_BUCKET: test-bucket
SYFTBOX_BLOB_REGION: test-region
SYFTBOX_BLOB_ENDPOINT: http://test-endpoint
SYFTBOX_BLOB_ACCESS_KEY: test-access-key
SYFTBOX_BLOB_SECRET_KEY: test-secret-key
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Build the binary
	binaryPath := filepath.Join(tmpDir, "server")
	buildCmd := exec.Command("go", "build", "-o", binaryPath)
	err = buildCmd.Run()
	require.NoError(t, err)

	// Run the binary with the config file
	cmd := exec.Command(binaryPath, "--config", configPath)
	output, err := cmd.CombinedOutput()

	// The binary will keep running, so we expect it to be killed
	// We're only interested in the startup logs
	require.Error(t, err)

	// Convert output to string for easier assertions
	outputStr := string(output)

	// Verify that the config values were loaded correctly
	require.Contains(t, outputStr, "Using config file")
	require.Contains(t, outputStr, "Server configuration loaded")
	require.Contains(t, outputStr, "blob.bucket_name=test-bucket")
	require.Contains(t, outputStr, "blob.region=test-region")
	require.Contains(t, outputStr, "blob.endpoint=http://test-endpoint")
	require.Contains(t, outputStr, "blob.access_key=test***")
	require.Contains(t, outputStr, "blob.secret_key=test***")
}

func TestConfigFileNotFound(t *testing.T) {
	// Build the binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "server")
	buildCmd := exec.Command("go", "build", "-o", binaryPath)
	err := buildCmd.Run()
	require.NoError(t, err)

	// Run the binary with a non-existent config file
	cmd := exec.Command(binaryPath, "--config", "nonexistent.yaml")
	output, err := cmd.CombinedOutput()

	require.Error(t, err)
	require.Contains(t, string(output), "Error: open nonexistent.yaml: no such file or directory")
}
