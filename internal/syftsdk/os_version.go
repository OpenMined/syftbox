package syftsdk

import (
	"bytes"
	"os/exec"
	"strings"
)

func getMacOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		version := strings.TrimSpace(out.String())
		return "macOS/" + version
	}
	return "macOS"
}

func getLinuxVersion() string {
	cmd := exec.Command("uname", "-r")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		kernel := strings.TrimSpace(out.String())
		
		// Try to get distribution info
		if data, err := exec.Command("lsb_release", "-si").Output(); err == nil {
			distro := strings.TrimSpace(string(data))
			if version, err := exec.Command("lsb_release", "-sr").Output(); err == nil {
				distroVersion := strings.TrimSpace(string(version))
				return distro + "/" + distroVersion + "; kernel/" + kernel
			}
			return distro + "; kernel/" + kernel
		}
		
		return "Linux; kernel/" + kernel
	}
	return "Linux"
}

func getWindowsVersion() string {
	cmd := exec.Command("cmd", "/c", "ver")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		version := strings.TrimSpace(out.String())
		// Extract version number from output like "Microsoft Windows [Version 10.0.19044.2604]"
		if strings.Contains(version, "[Version") {
			start := strings.Index(version, "[Version") + 9
			end := strings.Index(version[start:], "]")
			if end > 0 {
				return "Windows/" + version[start:start+end]
			}
		}
		return "Windows"
	}
	return "Windows"
}