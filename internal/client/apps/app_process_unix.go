//go:build !windows

package apps

import "syscall"

// getSysProcAttr returns the sys proc attr for the current platform
func getSysProcAttr() *syscall.SysProcAttr {
	return nil
}
