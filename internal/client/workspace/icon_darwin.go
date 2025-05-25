//go:build darwin

package workspace

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/openmined/syftbox/internal/utils"
)

//go:embed icon.icns
var syftboxIcon []byte

func setFolderIcon(dirPath string) error {
	targetDir, err := utils.ResolvePath(dirPath)
	if err != nil {
		return fmt.Errorf("set folder icon: failed to resolve dir '%s': %w", dirPath, err)
	}

	if !utils.DirExists(targetDir) {
		return fmt.Errorf("set folder icon: dir does not exist: %s", targetDir)
	}

	// create a temp file with the icon
	iconPath, err := os.CreateTemp("", "workspace.icns")
	if err != nil {
		return fmt.Errorf("set folder icon: failed to create temp file: %w", err)
	}
	defer os.Remove(iconPath.Name())

	if _, err := iconPath.Write(syftboxIcon); err != nil {
		return fmt.Errorf("set folder icon: failed to write icon to temp file: %w", err)
	}

	// amazing solution by https://github.com/mklement0/fileicon/blob/master/bin/fileicon
	appleScript := fmt.Sprintf(`
    use framework "Cocoa"

    set sourcePath to "%s"
    set destPath to "%s"

    set imageData to (current application's NSImage's alloc()'s initWithContentsOfFile:sourcePath)
    (current application's NSWorkspace's sharedWorkspace()'s setIcon:imageData forFile:destPath options:2)
	`, iconPath.Name(), targetDir)

	cmd := exec.Command("osascript", "-e", appleScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// AppleScript errors often include specific error messages and numbers in the output
		return fmt.Errorf("set folder icon: osascript error %q: %w", string(output), err)
	}

	return nil
}

func iconExists(dirPath string) bool {
	iconPath := filepath.Join(dirPath, "Icon\r")
	return utils.FileExists(iconPath)
}
