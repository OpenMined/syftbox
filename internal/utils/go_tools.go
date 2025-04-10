package utils

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
)

// InstallGoTool installs a Go tool using 'go install'
// toolPath should be the full import path of the tool (e.g., "github.com/swaggo/swag/cmd/swag")
// version is the version to install (e.g., "latest" or "v1.2.3")
// force will reinstall the tool even if it's already installed
func InstallGoTool(toolPath, version string, force bool) error {
	// Get the tool name from the path
	toolName := filepath.Base(toolPath)

	// Check if tool is already installed
	if !force {
		if _, err := exec.LookPath(toolName); err == nil {
			log.Printf("Go tool %s is already installed", toolName)
			return nil
		}
		log.Printf("Go tool %s not found, installing...", toolName)
	}

	cmd := exec.Command("go", "install", fmt.Sprintf("%s@%s", toolPath, version))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install %s: %w", toolPath, err)
	}
	log.Printf("Go tool %s installed successfully", toolName)
	return nil
}
