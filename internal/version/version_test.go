package version

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionStrings_NonEmptyAndContainParts(t *testing.T) {
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, Revision)
	assert.NotEmpty(t, AppName)

	short := Short()
	assert.Contains(t, short, Version)
	assert.Contains(t, short, Revision)

	shortApp := ShortWithApp()
	assert.True(t, strings.HasPrefix(shortApp, AppName+" "))

	detailed := Detailed()
	assert.Contains(t, detailed, Version)
	assert.Contains(t, detailed, Revision)
	assert.Contains(t, detailed, "/") // GOOS/GOARCH part

	detailedApp := DetailedWithApp()
	assert.True(t, strings.HasPrefix(detailedApp, AppName+" "))
}

