package uibridge

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/yashgorana/syftbox-go/internal/utils"
)

// GenerateSwagger generates Swagger documentation and compares it with existing docs.
// It only updates the documentation files if they have changed, preventing unnecessary
// hot-reloading of the server.
func GenerateSwagger() {
	// Find project root
	projectRoot, err := utils.FindProjectRoot()
	if err != nil {
		log.Fatalf("Failed to find project root: %v", err)
	}

	// Install swag if needed
	if err := utils.InstallGoTool("github.com/swaggo/swag/cmd/swag", "latest", false); err != nil {
		log.Fatalf("Failed to install swag: %v", err)
	}

	// Create a temp directory for generating the new docs
	tempDir := filepath.Join(projectRoot, "tmp", "docs")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		log.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory when done

	// Run swag init to generate docs in temp directory
	fmt.Println("Generating Swagger documentation in temp directory...")
	cmd := exec.Command("swag", "init",
		"-g", "server.go",
		"-d", filepath.Join(projectRoot, "internal", "uibridge"),
		"-o", tempDir,
	)
	// Print command errors to console
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to generate Swagger docs: %v", err)
	}

	// Define the target docs directory
	docsDir := filepath.Join(projectRoot, "internal", "uibridge", "docs")

	// Ensure the target docs directory exists
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		log.Fatalf("Failed to create docs directory: %v", err)
	}

	// Compare and copy files only if they have changed
	updated, fileCount, err := utils.CompareAndCopyFiles(tempDir, docsDir)
	if err != nil {
		log.Fatalf("Error while processing Swagger docs: %v", err)
	}

	if updated {
		fmt.Println("Swagger documentation updated successfully!")
	} else {
		fmt.Println("Swagger documentation is already up to date. No files were changed.")
	}

	if fileCount == 0 {
		log.Printf("Warning: No swagger files were generated. Check the configuration.")
	}
}
