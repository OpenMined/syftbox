package main

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary dummy.yaml file with expected structure
	dummyConfigContent := `
blob:
  bucket_name: test-bucket
  region: test-region
  endpoint: http://test-endpoint
  access_key: test-access-key
  secret_key: test-secret-key
`
	dummyConfigFile := os.TempDir() + "/dummy.yaml"
	err := os.WriteFile(dummyConfigFile, []byte(dummyConfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write dummy config file: %v", err)
	}
	defer os.Remove(dummyConfigFile) // Clean up after test

	// Create a dummy command
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "Path to config file")

	// Set the dummy config file path
	cmd.Flags().Set("config", dummyConfigFile)

	// Call loadConfig
	err = loadConfig(cmd)

	// Assert no error occurred
	assert.NoError(t, err)

	// Assert that Viper is set up correctly
	assert.Equal(t, "SYFTBOX", viper.GetEnvPrefix())
	assert.Equal(t, viper.GetString("blob.bucket_name"), "test-bucket")
	assert.Equal(t, viper.GetString("blob.region"), "test-region")
	assert.Equal(t, viper.GetString("blob.endpoint"), "http://test-endpoint")
	assert.Equal(t, viper.GetString("blob.access_key"), "test-access-key")
	assert.Equal(t, viper.GetString("blob.secret_key"), "test-secret-key")
}
