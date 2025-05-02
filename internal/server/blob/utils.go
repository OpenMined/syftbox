package blob

import (
	"regexp"
	"unicode/utf8"
)

// Match: starts with one or more / OR contains \ OR contains ..
var regexForbiddenPatterns = regexp.MustCompile(`^/+|\\+|\.\.`)

// Validate a key for S3 and local file system compatibility
func ValidateKey(key string) bool {
	// S3 keys must be between 1 and 1024 bytes long
	if len(key) == 0 || len(key) > 1024 {
		return false
	} else if key == "." || key == ".." {
		return false
	}

	// Check for forbidden patterns using regex
	if regexForbiddenPatterns.MatchString(key) {
		return false
	}

	// S3 keys must be valid UTF-8 strings
	return utf8.ValidString(key)
}
