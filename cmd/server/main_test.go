package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfigEnv(t *testing.T) {
	t.Setenv("SYFTBOX_HTTP_ADDR", ":8080")
	t.Setenv("SYFTBOX_HTTP_CERT_FILE", "test-cert.pem")
	t.Setenv("SYFTBOX_HTTP_KEY_FILE", "test-key.pem")

	t.Setenv("SYFTBOX_BLOB_BUCKET_NAME", "test-bucket")
	t.Setenv("SYFTBOX_BLOB_REGION", "test-region")
	t.Setenv("SYFTBOX_BLOB_ENDPOINT", "http://test-endpoint")
	t.Setenv("SYFTBOX_BLOB_ACCESS_KEY", "test-access-key")
	t.Setenv("SYFTBOX_BLOB_SECRET_KEY", "test-secret-key")

	t.Setenv("SYFTBOX_AUTH_ENABLED", "true")
	t.Setenv("SYFTBOX_AUTH_TOKEN_ISSUER", "test-issuer")
	t.Setenv("SYFTBOX_AUTH_EMAIL_OTP_LENGTH", "6")
	t.Setenv("SYFTBOX_AUTH_EMAIL_OTP_EXPIRY", "5m")
	t.Setenv("SYFTBOX_AUTH_REFRESH_TOKEN_SECRET", "test-refresh-secret")
	t.Setenv("SYFTBOX_AUTH_REFRESH_TOKEN_EXPIRY", "1h")
	t.Setenv("SYFTBOX_AUTH_ACCESS_TOKEN_SECRET", "test-access-secret")
	t.Setenv("SYFTBOX_AUTH_ACCESS_TOKEN_EXPIRY", "1h")

	// Call loadConfig
	cfg, err := loadConfig(rootCmd)
	assert.NoError(t, err)

	assert.Equal(t, cfg.HTTP.Addr, ":8080")
	assert.Equal(t, cfg.HTTP.CertFile, "test-cert.pem")
	assert.Equal(t, cfg.HTTP.KeyFile, "test-key.pem")
	assert.Equal(t, cfg.Blob.BucketName, "test-bucket")
	assert.Equal(t, cfg.Blob.Region, "test-region")
	assert.Equal(t, cfg.Blob.Endpoint, "http://test-endpoint")
	assert.Equal(t, cfg.Blob.AccessKey, "test-access-key")
	assert.Equal(t, cfg.Blob.SecretKey, "test-secret-key")
	assert.Equal(t, cfg.Auth.Enabled, true)
	assert.Equal(t, cfg.Auth.TokenIssuer, "test-issuer")
	assert.Equal(t, cfg.Auth.EmailOTPLength, 6)
	assert.Equal(t, cfg.Auth.EmailOTPExpiry, 5*time.Minute)
	assert.Equal(t, cfg.Auth.RefreshTokenSecret, "test-refresh-secret")
	assert.Equal(t, cfg.Auth.RefreshTokenExpiry, 1*time.Hour)
	assert.Equal(t, cfg.Auth.AccessTokenSecret, "test-access-secret")
	assert.Equal(t, cfg.Auth.AccessTokenExpiry, 1*time.Hour)
}

func TestLoadConfigYAML(t *testing.T) {
	dummyConfig := `
http:
  cert_file: test-cert.pem
  key_file: test-key.pem

blob:
  bucket_name: test-bucket
  region: test-region
  endpoint: http://test-endpoint
  access_key: test-access-key
  secret_key: test-secret-key

auth:
  enabled: true
  token_issuer: test-issuer
  email_otp_length: 8
  email_otp_expiry: 5m
  refresh_token_secret: test-refresh-secret
  refresh_token_expiry: 1h
  access_token_secret: test-access-secret
  access_token_expiry: 1h
`
	dummyConfigFile := filepath.Join(os.TempDir(), "dummy.yaml")
	err := os.WriteFile(dummyConfigFile, []byte(dummyConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write dummy config file: %v", err)
	}
	defer os.Remove(dummyConfigFile) // Clean up after test

	// Call loadConfig
	cfg, err := loadConfig(rootCmd)
	assert.NoError(t, err)

	assert.Equal(t, cfg.HTTP.Addr, "localhost:8080")
	assert.Equal(t, cfg.HTTP.CertFile, "test-cert.pem")
	assert.Equal(t, cfg.HTTP.KeyFile, "test-key.pem")
	assert.Equal(t, cfg.Blob.BucketName, "test-bucket")
	assert.Equal(t, cfg.Blob.Region, "test-region")
	assert.Equal(t, cfg.Blob.Endpoint, "http://test-endpoint")
	assert.Equal(t, cfg.Blob.AccessKey, "test-access-key")
	assert.Equal(t, cfg.Blob.SecretKey, "test-secret-key")
	assert.Equal(t, cfg.Auth.Enabled, true)
	assert.Equal(t, cfg.Auth.TokenIssuer, "test-issuer")
	assert.Equal(t, cfg.Auth.EmailOTPLength, 8)
	assert.Equal(t, cfg.Auth.EmailOTPExpiry, 5*time.Minute)
	assert.Equal(t, cfg.Auth.RefreshTokenSecret, "test-refresh-secret")
	assert.Equal(t, cfg.Auth.RefreshTokenExpiry, 1*time.Hour)
	assert.Equal(t, cfg.Auth.AccessTokenSecret, "test-access-secret")
	assert.Equal(t, cfg.Auth.AccessTokenExpiry, 1*time.Hour)
}

func TestLoadConfigJSON(t *testing.T) {
	// Create a temporary JSON file with expected structure
	dummyConfig := `
{
	"http": {
		"addr": "localhost:38080",
		"cert_file": "path/to/test-cert.pem",
		"key_file": "path/to/test-key.pem"
	},
	"blob": {
		"bucket_name": "test-another-bucket",
		"region": "test-another-region",
		"access_key": "test-another-access-key",
		"secret_key": "test-another-secret-key"
	},
	"auth": {
		"enabled": true,
		"token_issuer": "test-issuer",
		"email_otp_length": 8,
		"email_otp_expiry": 0,
		"refresh_token_secret": "test-another-refresh-secret",
		"refresh_token_expiry": 0,
		"access_token_secret": "test-another-access-secret",
		"access_token_expiry": 0
	}
}
`
	dummyConfigFile := filepath.Join(os.TempDir(), "dummy.yaml")
	err := os.WriteFile(dummyConfigFile, []byte(dummyConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write dummy config file: %v", err)
	}
	defer os.Remove(dummyConfigFile) // Clean up after test

	// Call loadConfig
	cfg, err := loadConfig(rootCmd)
	assert.NoError(t, err)

	assert.Equal(t, cfg.HTTP.Addr, "localhost:38080")
	assert.Equal(t, cfg.HTTP.CertFile, "path/to/test-cert.pem")
	assert.Equal(t, cfg.HTTP.KeyFile, "path/to/test-key.pem")
	assert.Equal(t, cfg.Blob.BucketName, "test-another-bucket")
	assert.Equal(t, cfg.Blob.Region, "test-another-region")
	assert.Equal(t, cfg.Blob.Endpoint, "") // no endpoint in json
	assert.Equal(t, cfg.Blob.AccessKey, "test-another-access-key")
	assert.Equal(t, cfg.Blob.SecretKey, "test-another-secret-key")
	assert.Equal(t, cfg.Auth.Enabled, true)
	assert.Equal(t, cfg.Auth.TokenIssuer, "test-issuer")
	assert.Equal(t, cfg.Auth.EmailOTPLength, 8)
	assert.Equal(t, cfg.Auth.EmailOTPExpiry, 0*time.Second)
	assert.Equal(t, cfg.Auth.RefreshTokenSecret, "test-another-refresh-secret")
	assert.Equal(t, cfg.Auth.RefreshTokenExpiry, 0*time.Second)
	assert.Equal(t, cfg.Auth.AccessTokenSecret, "test-another-access-secret")
	assert.Equal(t, cfg.Auth.AccessTokenExpiry, 0*time.Second)
}
