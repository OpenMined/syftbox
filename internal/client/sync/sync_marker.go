package sync

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/openmined/syftbox/internal/utils"
)

// MarkerType defines the type of marker to be applied to a file.
// We use simple dot-suffixes for command-line friendliness (no special chars).
type MarkerType string

const (
	// LegacyRejected marks a file as rejected.
	LegacyRejected MarkerType = ".syftrejected"
	// LegacyConflict marks a file as having a conflict.
	LegacyConflict MarkerType = ".syftconflict"

	// Rejected marks a file as rejected.
	Rejected MarkerType = ".rejected"
	// Conflict marks a file as having a conflict.
	Conflict MarkerType = ".conflict"
)

// allMarkers is a central list of all known marker types.
// To add a new marker type, simply add it to this list.
var allMarkers = []MarkerType{Rejected, Conflict}

// timeFormat is the format used for timestamping rotated files (YYYY-MM-DD_HH-MM-SS).
// This format ensures that rotated files can be sorted lexicographically by time.
const (
	timeFormat       = "20060102150405"
	timestampPattern = `\d{14}`
)

// markerRegexes holds the pre-compiled regular expressions for each marker type.
// This avoids expensive recompilation on every function call.
var markerRegexes = make(map[MarkerType]*regexp.Regexp)

func init() {
	// The init function runs once when the package is first used.
	// We pre-compile all our regex patterns here for performance.
	for _, marker := range allMarkers {
		// Regex explanation:
		// %s          - The literal marker string (e.g., ".rejected"), with meta-characters escaped.
		// (\.\d{14})? - An optional group that matches a literal dot `\.` followed by exactly 14 digits `\d{14}`.
		pattern := fmt.Sprintf(`%s(\.%s)?`, regexp.QuoteMeta(string(marker)), timestampPattern)
		markerRegexes[marker] = regexp.MustCompile(pattern)
	}
}

// SetMarker applies a marker to a file at a given path.
// If a file with the same marker already exists, it is "rotated" by
// renaming it with its modification timestamp before the new file is marked.
// It returns the new path of the marked file.
func SetMarker(path string, mtype MarkerType) (string, error) {
	// First, ensure the source file actually exists.
	if !utils.FileExists(path) {
		return "", fmt.Errorf("cannot mark file: source file does not exist: %s", path)
	}

	markedPath := asMarkedPath(path, mtype)

	// Check if the target marked path already exists.
	if utils.FileExists(markedPath) {
		rotatedPath := asRotatedPath(markedPath, time.Now())
		if err := os.Rename(markedPath, rotatedPath); err != nil {
			return "", fmt.Errorf("failed to rotate existing marked file from %s to %s: %w", markedPath, rotatedPath, err)
		}
		slog.Debug("rotated marked file", "from", markedPath, "to", rotatedPath)
	}

	// Rename the original file to the marked path.
	if err := os.Rename(path, markedPath); err != nil {
		return "", fmt.Errorf("failed to mark file from %s to %s: %w", path, markedPath, err)
	}

	return markedPath, nil
}

// RemoveMarker renames a marked file to its original, unmarked name.
// It returns the original path.
func RemoveMarker(path string) (string, error) {
	if !IsMarkedPath(path) {
		// The file is not marked, so there's nothing to do.
		return path, nil
	}

	// First, ensure the marked file actually exists before trying to rename it.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("cannot unmark file: source file does not exist: %s", path)
	}

	originalPath := GetUnmarkedPath(path)

	// Check if the destination (original) path already exists.
	if _, err := os.Stat(originalPath); err == nil {
		return "", fmt.Errorf("cannot unmark file: destination file already exists: %s", originalPath)
	}

	if err := os.Rename(path, originalPath); err != nil {
		return "", fmt.Errorf("failed to unmark file from %s to %s: %w", path, originalPath, err)
	}

	return originalPath, nil
}

// IsMarkedPath checks if a given path string contains any known marker,
// including rotated (timestamped) variants.
func IsMarkedPath(path string) bool {
	return strings.Contains(path, string(Conflict)) || strings.Contains(path, string(Rejected))
}

func IsLegacyMarkedPath(path string) bool {
	return strings.Contains(path, string(LegacyConflict)) || strings.Contains(path, string(LegacyRejected))
}

// IsConflict checks if a path string is specifically marked as a conflict.
func IsConflictPath(path string) bool {
	return slices.Contains(GetMarkers(path), Conflict)
}

// IsRejected checks if a path string is specifically marked as rejected.
func IsRejectedPath(path string) bool {
	return slices.Contains(GetMarkers(path), Rejected)
}

// ConflictFileExists checks the filesystem to see if any .conflict file
// (including rotated versions) exists for the given base path.
func ConflictFileExists(basePath string) bool {
	return markerFileExists(basePath, Conflict)
}

// RejectedFileExists checks the filesystem to see if any .rejected file
// (including rotated versions) exists for the given base path.
func RejectedFileExists(basePath string) bool {
	return markerFileExists(basePath, Rejected)
}

// markerFileExists is a helper that checks the filesystem for any file
// matching a base path and a specific marker type, including rotations.
func markerFileExists(basePath string, mtype MarkerType) bool {
	// If the basePath itself is already marked, we should check against its original version
	if IsMarkedPath(basePath) {
		basePath = GetUnmarkedPath(basePath)
	}

	ext := filepath.Ext(basePath)
	base := strings.TrimSuffix(basePath, ext)

	// Create a glob pattern to find all files with this marker and any rotation.
	// e.g., /path/to/file.rejected*.txt
	globPattern := base + string(mtype) + "*" + ext
	matches, err := filepath.Glob(globPattern)
	if err != nil {
		slog.Error("failed to glob for existing marked files", "error", err, "globPattern", globPattern)
		return false
	}

	// If there's one or more matching file, a marked version exists.
	return len(matches) > 0
}

// GetUnmarkedPath strips all known markers and rotation timestamps from a
// path string to reveal the original, unmarked path.
func GetUnmarkedPath(path string) string {
	originalPath := path
	// We iterate through our known markers and use the pre-compiled regexes.
	for _, marker := range allMarkers {
		if re, ok := markerRegexes[marker]; ok {
			originalPath = re.ReplaceAllString(originalPath, "")
		}
	}
	return originalPath
}

// GetMarkers finds all known markers in a given path string.
func GetMarkers(path string) []MarkerType {
	var foundMarkers []MarkerType
	// We iterate through our known markers and use the pre-compiled regexes.
	for _, marker := range allMarkers {
		if re, ok := markerRegexes[marker]; ok && re.MatchString(path) {
			foundMarkers = append(foundMarkers, marker)
		}
	}
	return foundMarkers
}

// asMarkedPath constructs the marked path string without performing any file operations.
// e.g., "file.txt" -> "file.rejected.txt"
func asMarkedPath(path string, marker MarkerType) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	return base + string(marker) + ext
}

// asRotatedPath constructs the timestamped path for rotation.
// e.g., "file.rejected.txt" -> "file.rejected.20250712234500.txt"
func asRotatedPath(path string, t time.Time) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	timestamp := t.Format(timeFormat)
	return fmt.Sprintf("%s.%s%s", base, timestamp, ext)
}
