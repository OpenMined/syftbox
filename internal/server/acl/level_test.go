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
			level:    AccessCreate,
			expected: "Create",
			desc:     "AccessCreate should return 'Create'",
		},
		{
			level:    AccessWrite,
			expected: "Write",
			desc:     "AccessWrite should return 'Write'",
		},
		{
			level:    AccessReadACL,
			expected: "ReadACL",
			desc:     "AccessReadACL should return 'ReadACL'",
		},
		{
			level:    AccessWriteACL,
			expected: "WriteACL",
			desc:     "AccessWriteACL should return 'WriteACL'",
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

func TestAccessLevelBitFlags(t *testing.T) {
	// Test that AccessLevel constants are properly defined as bit flags
	// This validates the bit flag implementation which allows for efficient permission checking
	
	// Verify each level has a unique bit pattern
	assert.Equal(t, AccessLevel(1), AccessRead, "AccessRead should be bit 0 (value 1)")
	assert.Equal(t, AccessLevel(2), AccessCreate, "AccessCreate should be bit 1 (value 2)")
	assert.Equal(t, AccessLevel(4), AccessWrite, "AccessWrite should be bit 2 (value 4)")
	assert.Equal(t, AccessLevel(8), AccessReadACL, "AccessReadACL should be bit 3 (value 8)")
	assert.Equal(t, AccessLevel(16), AccessWriteACL, "AccessWriteACL should be bit 4 (value 16)")
}

func TestAccessLevelUniqueness(t *testing.T) {
	// Test that all AccessLevel constants are unique
	// This prevents accidental duplicate values that could cause permission conflicts
	levels := []AccessLevel{
		AccessRead,
		AccessCreate,
		AccessWrite,
		AccessReadACL,
		AccessWriteACL,
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

func TestAccessLevelBitOperations(t *testing.T) {
	// Test that bit operations work correctly with AccessLevel flags
	// This validates that the bit flag design allows for combining permissions
	
	// Test combining permissions with OR
	combined := AccessRead | AccessWrite
	assert.NotEqual(t, AccessRead, combined, "Combined permissions should differ from individual permissions")
	assert.NotEqual(t, AccessWrite, combined, "Combined permissions should differ from individual permissions")
	
	// Test checking individual permissions with AND
	assert.Equal(t, AccessRead, combined&AccessRead, "Should be able to check for read permission in combined flags")
	assert.Equal(t, AccessWrite, combined&AccessWrite, "Should be able to check for write permission in combined flags")
	assert.Equal(t, AccessLevel(0), combined&AccessCreate, "Should not find create permission in read+write combination")
}

func TestAccessLevelHierarchy(t *testing.T) {
	// Test the logical hierarchy of access levels
	// This documents the intended permission hierarchy in the system
	
	// Basic file operations should have lower bit values than ACL operations
	assert.True(t, AccessRead < AccessReadACL, "Read should have lower value than ReadACL")
	assert.True(t, AccessWrite < AccessWriteACL, "Write should have lower value than WriteACL")
	
	// Within basic operations, read should be the lowest level
	assert.True(t, AccessRead < AccessCreate, "Read should be the most basic permission")
	assert.True(t, AccessRead < AccessWrite, "Read should be lower than write")
	
	// ACL operations should be the highest levels
	assert.True(t, AccessReadACL > AccessWrite, "ReadACL should be higher than basic write")
	assert.True(t, AccessWriteACL > AccessReadACL, "WriteACL should be the highest permission")
}

func TestAccessLevelZeroValue(t *testing.T) {
	// Test the zero value of AccessLevel
	// This ensures the default/uninitialized value behaves correctly
	var zeroLevel AccessLevel
	
	assert.Equal(t, AccessLevel(0), zeroLevel, "Zero value should be 0")
	assert.Equal(t, "Unknown", zeroLevel.String(), "Zero value should be treated as unknown")
	
	// Zero should not match any defined permission
	assert.NotEqual(t, AccessRead, zeroLevel, "Zero should not equal AccessRead")
	assert.NotEqual(t, AccessCreate, zeroLevel, "Zero should not equal AccessCreate")
	assert.NotEqual(t, AccessWrite, zeroLevel, "Zero should not equal AccessWrite")
	assert.NotEqual(t, AccessReadACL, zeroLevel, "Zero should not equal AccessReadACL")
	assert.NotEqual(t, AccessWriteACL, zeroLevel, "Zero should not equal AccessWriteACL")
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
	originalLevel := AccessWriteACL
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
	betweenLevels := AccessLevel(3) // Between AccessCreate (2) and AccessWrite (4)
	assert.Equal(t, "Unknown", betweenLevels.String(), "Undefined intermediate values should be unknown")
	
	// Test that the bit flag pattern continues to work with undefined values
	undefinedLevel := AccessLevel(32) // Next bit after AccessWriteACL (16)
	assert.Equal(t, "Unknown", undefinedLevel.String(), "Higher undefined bits should be unknown")
}