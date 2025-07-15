package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"

	"github.com/openmined/syftbox/internal/utils"
)

// writeFileWithIntegrityCheck writes the body to the file at path and verifies the integrity of the file
func writeFileWithIntegrityCheck(path string, body []byte, expectedETag string) error {
	if err := utils.EnsureParent(path); err != nil {
		return fmt.Errorf("ensure parent error: %w", err)
	}

	hasher := md5.New()

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file error: %w", err)
	}
	defer file.Close()

	writer := io.MultiWriter(file, hasher)

	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf("write error: %w", err)
	}

	computedETag := fmt.Sprintf("%x", hasher.Sum(nil))

	if expectedETag != computedETag {
		return fmt.Errorf("integrity check failed expected %q got %q", expectedETag, computedETag)
	}

	return nil
}
