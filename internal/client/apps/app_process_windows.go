//go:build windows

package appsv2

import (
	"syscall"
)

// getSysProcAttr returns the sys proc attr for the current platform
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow: true,
	}
}

// killProcessGroup kills the process and all its children
// func (p *AppProcess) killProcessGroup() error {
// 	slog.Debug("kill process group: started", "pid", p.cmd.Process.Pid)

// 	pid := p.cmd.Process.Pid

// 	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
// }
