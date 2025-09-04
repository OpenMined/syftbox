package syftsdk

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOSVersion(t *testing.T) {
	// Test that getOSVersion returns something meaningful for current OS
	version := getOSVersion()
	assert.NotEmpty(t, version)
	
	// Check it contains expected OS identifier
	switch runtime.GOOS {
	case "darwin":
		assert.True(t, strings.Contains(version, "macOS") || version == "macOS",
			"Darwin should return macOS identifier")
	case "linux":
		assert.True(t, strings.Contains(version, "Linux") || strings.Contains(version, "kernel"),
			"Linux should contain Linux or kernel identifier")
	case "windows":
		assert.True(t, strings.Contains(version, "Windows") || version == "Windows",
			"Windows should return Windows identifier")
	}
}

func TestGetUserAgent(t *testing.T) {
	// Test that getUserAgent returns properly formatted string
	ua := getUserAgent()
	
	// Should not be empty
	assert.NotEmpty(t, ua)
	
	// Should start with SyftBox/
	assert.True(t, strings.HasPrefix(ua, "SyftBox/"), "User agent should start with SyftBox/")
	
	// Should contain version info
	assert.Contains(t, ua, "(")
	assert.Contains(t, ua, ")")
	
	// Should contain OS and architecture
	assert.Contains(t, ua, runtime.GOOS)
	assert.Contains(t, ua, runtime.GOARCH)
	
	// Should contain Go version
	assert.Contains(t, ua, "Go/")
	assert.Contains(t, ua, runtime.Version())
	
	// Should have proper format: SyftBox/version (revision; os/arch; Go/version; osdetails)
	parts := strings.Split(ua, " ")
	assert.GreaterOrEqual(t, len(parts), 2, "User agent should have at least 2 parts")
	
	// First part should be SyftBox/version
	assert.True(t, strings.HasPrefix(parts[0], "SyftBox/"))
	
	// Rest should be in parentheses
	remainder := strings.Join(parts[1:], " ")
	assert.True(t, strings.HasPrefix(remainder, "("))
	assert.True(t, strings.HasSuffix(remainder, ")"))
}

func TestSyftBoxUserAgent(t *testing.T) {
	// Test the exported variable
	assert.NotEmpty(t, SyftBoxUserAgent)
	
	// Should contain all expected components
	assert.Contains(t, SyftBoxUserAgent, "SyftBox/")
	assert.Contains(t, SyftBoxUserAgent, runtime.GOOS)
	assert.Contains(t, SyftBoxUserAgent, runtime.GOARCH)
	assert.Contains(t, SyftBoxUserAgent, "Go/")
	
	t.Logf("Current SyftBox User-Agent: %s", SyftBoxUserAgent)
}

func TestOSVersionFunctions(t *testing.T) {
	// Test OS-specific functions based on current platform
	switch runtime.GOOS {
	case "darwin":
		t.Run("macOS", func(t *testing.T) {
			version := getMacOSVersion()
			assert.NotEmpty(t, version)
			// Should be "macOS" or "macOS/version"
			assert.True(t, strings.HasPrefix(version, "macOS"))
		})
	case "linux":
		t.Run("Linux", func(t *testing.T) {
			version := getLinuxVersion()
			assert.NotEmpty(t, version)
			// Should contain "Linux" or distribution info
			assert.True(t, strings.Contains(version, "Linux") || strings.Contains(version, "kernel"))
		})
	case "windows":
		t.Run("Windows", func(t *testing.T) {
			version := getWindowsVersion()
			assert.NotEmpty(t, version)
			// Should be "Windows" or "Windows/version"
			assert.True(t, strings.HasPrefix(version, "Windows"))
		})
	}
}