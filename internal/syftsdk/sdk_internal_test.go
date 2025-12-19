package syftsdk

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDevURL(t *testing.T) {
	assert.True(t, isDevURL("http://localhost:8080"))
	assert.True(t, isDevURL("http://127.0.0.1:8080"))
	assert.True(t, isDevURL("http://0.0.0.0:8080"))
	assert.False(t, isDevURL("https://syftbox.net"))
}

func TestIsAuthDisabled_EnvOverride(t *testing.T) {
	t.Setenv("SYFTBOX_AUTH_ENABLED", "false")
	assert.True(t, isAuthDisabled("https://syftbox.net"))

	t.Setenv("SYFTBOX_AUTH_ENABLED", "true")
	assert.False(t, isAuthDisabled("https://syftbox.net"))

	// When unset, dev URLs disable auth.
	_ = os.Unsetenv("SYFTBOX_AUTH_ENABLED")
	assert.True(t, isAuthDisabled("http://127.0.0.1:8080"))
	assert.False(t, isAuthDisabled("https://syftbox.net"))
}

