package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openmined/syftbox/internal/syftsdk"
	"github.com/stretchr/testify/require"
)

func newTestRefreshToken(t *testing.T) string {
	t.Helper()
	claims := &syftsdk.AuthClaims{
		Type: syftsdk.RefreshToken,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "alice@example.com",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		},
	}
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("k"))
	require.NoError(t, err)
	return tokenStr
}

func TestLogin_AlreadyLoggedIn_PrintsConfig(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "SyftBox")
	cfgPath := filepath.Join(tmp, "config.json")
	writeTestConfig(t, cfgPath, "alice@example.com", dataDir, newTestRefreshToken(t))

	out, code := runCLI(t, "--config", cfgPath, "login")
	require.Equal(t, 0, code, out)

	plain := stripANSI(out)
	require.Contains(t, plain, "**Already logged in**")
	require.Contains(t, plain, "SYFTBOX DATASITE CONFIG")
	require.Contains(t, plain, "alice@example.com")
	require.Contains(t, plain, dataDir)
}

func TestLogin_AlreadyLoggedIn_QuietHasNoOutput(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "SyftBox")
	cfgPath := filepath.Join(tmp, "config.json")
	writeTestConfig(t, cfgPath, "alice@example.com", dataDir, newTestRefreshToken(t))

	out, code := runCLI(t, "--config", cfgPath, "login", "--quiet")
	require.Equal(t, 0, code, out)
	require.Equal(t, "", strings.TrimSpace(stripANSI(out)))
}

