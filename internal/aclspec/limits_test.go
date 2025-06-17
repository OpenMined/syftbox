package aclspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLimits(t *testing.T) {
	// Test that DefaultLimits creates a Limits object with expected default values
	// This is critical because these defaults affect security and functionality
	limits := DefaultLimits()

	// Verify the limits object is not nil
	assert.NotNil(t, limits, "DefaultLimits should return a non-nil Limits object")

	// Test file count limit - default should be 0 (unlimited)
	assert.Equal(t, uint32(0), limits.MaxFiles, 
		"Default MaxFiles should be 0 (unlimited) to not restrict file count by default")

	// Test file size limit - default should be 0 (unlimited)
	assert.Equal(t, int64(0), limits.MaxFileSize, 
		"Default MaxFileSize should be 0 (unlimited) to not restrict file size by default")

	// Test directory permission - default should allow directories
	assert.True(t, limits.AllowDirs, 
		"Default should allow directories since most use cases need directory creation")

	// Test symlink permission - default should NOT allow symlinks for security
	assert.False(t, limits.AllowSymlinks, 
		"Default should not allow symlinks for security reasons (prevent symlink attacks)")
}

func TestLimitsStruct(t *testing.T) {
	// Test creating Limits with custom values
	// This validates the struct can hold different configurations correctly
	customLimits := &Limits{
		MaxFiles:      100,
		MaxFileSize:   1024 * 1024, // 1MB
		AllowDirs:     false,
		AllowSymlinks: true,
	}

	assert.Equal(t, uint32(100), customLimits.MaxFiles, "Custom MaxFiles should be preserved")
	assert.Equal(t, int64(1024*1024), customLimits.MaxFileSize, "Custom MaxFileSize should be preserved")
	assert.False(t, customLimits.AllowDirs, "Custom AllowDirs should be preserved")
	assert.True(t, customLimits.AllowSymlinks, "Custom AllowSymlinks should be preserved")
}

func TestLimitsZeroValues(t *testing.T) {
	// Test that zero values in Limits struct behave as expected
	// This is important for understanding the semantic meaning of zero values
	var limits Limits

	// Zero values should represent the most restrictive settings
	assert.Equal(t, uint32(0), limits.MaxFiles, "Zero value MaxFiles should be 0")
	assert.Equal(t, int64(0), limits.MaxFileSize, "Zero value MaxFileSize should be 0")
	assert.False(t, limits.AllowDirs, "Zero value AllowDirs should be false (more restrictive)")
	assert.False(t, limits.AllowSymlinks, "Zero value AllowSymlinks should be false (more secure)")
}

func TestLimitsIndependence(t *testing.T) {
	// Test that multiple calls to DefaultLimits return independent objects
	// This ensures modifications to one instance don't affect others
	limits1 := DefaultLimits()
	limits2 := DefaultLimits()

	// Verify they start with the same values
	assert.Equal(t, limits1.MaxFiles, limits2.MaxFiles)
	assert.Equal(t, limits1.MaxFileSize, limits2.MaxFileSize)
	assert.Equal(t, limits1.AllowDirs, limits2.AllowDirs)
	assert.Equal(t, limits1.AllowSymlinks, limits2.AllowSymlinks)

	// Modify one instance
	limits1.MaxFiles = 50
	limits1.AllowDirs = false

	// Verify the other instance is unchanged
	assert.Equal(t, uint32(0), limits2.MaxFiles, "Modifying one instance should not affect another")
	assert.True(t, limits2.AllowDirs, "Modifying one instance should not affect another")
}

func TestLimitsExtremeValues(t *testing.T) {
	// Test Limits with extreme values to ensure robust handling
	// This validates the struct can handle boundary conditions
	extremeLimits := &Limits{
		MaxFiles:      ^uint32(0), // Maximum uint32 value
		MaxFileSize:   9223372036854775807, // Maximum int64 value
		AllowDirs:     true,
		AllowSymlinks: true,
	}

	// These extreme values should be preserved without overflow or corruption
	assert.Equal(t, ^uint32(0), extremeLimits.MaxFiles, "Maximum uint32 value should be preserved")
	assert.Equal(t, int64(9223372036854775807), extremeLimits.MaxFileSize, "Maximum int64 value should be preserved")
	assert.True(t, extremeLimits.AllowDirs, "Boolean values should be preserved")
	assert.True(t, extremeLimits.AllowSymlinks, "Boolean values should be preserved")
}

func TestLimitsSemantics(t *testing.T) {
	// Test the semantic meaning of limit values
	// This documents and validates the intended behavior of different settings
	
	// Test unlimited semantics (0 values)
	unlimited := &Limits{
		MaxFiles:    0,
		MaxFileSize: 0,
	}
	
	// Zero should mean "no limit" for numeric fields
	// This is a common convention in Unix systems
	assert.Equal(t, uint32(0), unlimited.MaxFiles, "Zero MaxFiles should mean unlimited")
	assert.Equal(t, int64(0), unlimited.MaxFileSize, "Zero MaxFileSize should mean unlimited")
	
	// Test specific limits
	limited := &Limits{
		MaxFiles:    10,
		MaxFileSize: 1000,
	}
	
	assert.True(t, limited.MaxFiles > 0, "Non-zero MaxFiles should impose a limit")
	assert.True(t, limited.MaxFileSize > 0, "Non-zero MaxFileSize should impose a limit")
}