package acl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAccessLevelString(t *testing.T) {
	// Test the String() method for all AccessLevel constants
	// This validates that each access level has the correct string representation
	// which is important for logging, debugging, and user-facing error messages
	testCases := []struct {
		level    AccessLevel
		expected string
		desc     string
	}{
		{
			level:    AccessRead,
			expected: "Read",
			desc:     "AccessRead should return 'Read'",
		},
		{
			level:    AccessWrite,
			expected: "Write",
			desc:     "AccessWrite should return 'Write'",
		},
		{
			level:    AccessAdmin,
			expected: "Admin",
			desc:     "AccessAdmin should return 'Admin'",
		},
		{
			level:    0,
			expected: "Unknown",
			desc:     "Zero value should return 'Unknown'",
		},
		{
			level:    AccessLevel(10),
			expected: "Unknown",
			desc:     "Undefined values should return 'Unknown'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.level.String()
			assert.Equal(t, tc.expected, result, tc.desc)
		})
	}
}

func TestAccessLevelStringUnknown(t *testing.T) {
	// Test String() method with invalid/unknown access level values
	// This ensures the system handles undefined access levels gracefully
	unknownLevel := AccessLevel(255) // Invalid access level
	result := unknownLevel.String()
	assert.Equal(t, "Unknown", result, "Unknown access levels should return 'Unknown'")
}

func TestAccessLevelValues(t *testing.T) {
	// Test that AccessLevel constants have the expected values
	// Since iota starts at 0 and we use iota + 1, values should be 1, 2, 3
	
	assert.Equal(t, AccessLevel(1), AccessRead, "AccessRead should be 1")
	assert.Equal(t, AccessLevel(2), AccessWrite, "AccessWrite should be 2")
	assert.Equal(t, AccessLevel(3), AccessAdmin, "AccessAdmin should be 3")
}

func TestAccessLevelUniqueness(t *testing.T) {
	// Test that all AccessLevel constants are unique
	// This prevents accidental duplicate values that could cause permission conflicts
	levels := []AccessLevel{
		AccessRead,
		AccessWrite,
		AccessAdmin,
	}

	// Check that no two levels have the same value
	for i, level1 := range levels {
		for j, level2 := range levels {
			if i != j {
				assert.NotEqual(t, level1, level2, 
					"AccessLevel constants should be unique: %s and %s have the same value", 
					level1.String(), level2.String())
			}
		}
	}
}

func TestAccessLevelHierarchy(t *testing.T) {
	// Test the logical hierarchy of access levels
	// This documents the intended permission hierarchy in the system
	
	// Verify the ordering based on iota values
	assert.True(t, AccessRead < AccessWrite, "Read should be lower than Write")
	assert.True(t, AccessWrite < AccessAdmin, "Write should be lower than Admin")
	assert.True(t, AccessRead < AccessAdmin, "Read should be lower than Admin")
}

func TestAccessLevelZeroValue(t *testing.T) {
	// Test the zero value of AccessLevel
	// This ensures the default/uninitialized value behaves correctly
	var zeroLevel AccessLevel
	
	assert.Equal(t, AccessLevel(0), zeroLevel, "Zero value should be 0")
	assert.Equal(t, "Unknown", zeroLevel.String(), "Zero value should return 'Unknown'")
	
	// Zero should not match any defined permission
	assert.NotEqual(t, AccessRead, zeroLevel, "Zero should not equal AccessRead")
	assert.NotEqual(t, AccessWrite, zeroLevel, "Zero should not equal AccessWrite")
	assert.NotEqual(t, AccessAdmin, zeroLevel, "Zero should not equal AccessAdmin")
}

func TestAccessLevelCasting(t *testing.T) {
	// Test that AccessLevel can be properly cast from and to uint8
	// This validates the underlying type compatibility
	
	// Test casting from uint8
	readLevel := AccessLevel(1)
	assert.Equal(t, AccessRead, readLevel, "Should be able to cast uint8 to AccessLevel")
	
	// Test casting to uint8
	readValue := uint8(AccessRead)
	assert.Equal(t, uint8(1), readValue, "Should be able to cast AccessLevel to uint8")
	
	// Test round-trip casting
	originalLevel := AccessAdmin
	castValue := uint8(originalLevel)
	backToLevel := AccessLevel(castValue)
	assert.Equal(t, originalLevel, backToLevel, "Round-trip casting should preserve value")
}

func TestAccessLevelStringConsistency(t *testing.T) {
	// Test that String() method is consistent across multiple calls
	// This ensures no side effects or state changes in the String() method
	level := AccessWrite
	
	firstCall := level.String()
	secondCall := level.String()
	thirdCall := level.String()
	
	assert.Equal(t, firstCall, secondCall, "String() should return consistent results")
	assert.Equal(t, secondCall, thirdCall, "String() should return consistent results")
	assert.Equal(t, "Write", firstCall, "String() should return correct value")
}

func TestAccessLevelEdgeCases(t *testing.T) {
	// Test edge cases and boundary values
	// This ensures robust handling of unusual but possible values
	
	// Test maximum uint8 value
	maxLevel := AccessLevel(255)
	assert.Equal(t, "Unknown", maxLevel.String(), "Maximum value should be handled as unknown")
	
	// Test values between defined constants
	betweenLevels := AccessLevel(4) // Just after AccessAdmin (3)
	assert.Equal(t, "Unknown", betweenLevels.String(), "Undefined values should be unknown")
	
	// Test that undefined values are handled correctly
	undefinedLevel := AccessLevel(10)
	assert.Equal(t, "Unknown", undefinedLevel.String(), "Higher undefined values should be unknown")
}