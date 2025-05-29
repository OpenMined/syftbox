//go:build !windows

package appsv2

import "syscall"

// getSysProcAttr returns the sys proc attr for the current platform
func getSysProcAttr() *syscall.SysProcAttr {
	return nil
}
