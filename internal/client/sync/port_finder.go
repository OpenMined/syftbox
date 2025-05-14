package main

import (
	"fmt"
	"log"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func getPortsForRunningCmd(cmd *exec.Cmd) ([]int, error) {
	// Check if the process has started and has a valid PID
	if cmd.Process == nil {
		return nil, fmt.Errorf("process has not been started")
	}

	pid := cmd.Process.Pid
	slog.Info("PID", "pid", pid)

	// Now use the PID to get port information
	// This example uses the Unix/Linux approach with lsof
	lsofCmd := exec.Command("lsof", "-i", "-P", "-n", "-a", "-p", strconv.Itoa(pid))
	output, err := lsofCmd.CombinedOutput()
	if err != nil {
		// lsof might return an error if there are no open ports
		// Check if the output contains useful information anyway
		if len(output) == 0 {
			return nil, fmt.Errorf("no port information found or error running lsof: %v", err)
		}
	}

	slog.Info("Output", "output", string(output))

	// Parse the output to extract port numbers
	portRegex := regexp.MustCompile(`:([\d]+)`)
	lines := strings.Split(string(output), "\n")

	var ports []int
	portMap := make(map[int]bool) // To avoid duplicates

	for _, line := range lines {
		if strings.Contains(line, "LISTEN") || strings.Contains(line, "ESTABLISHED") {
			matches := portRegex.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) > 1 {
					port, err := strconv.Atoi(match[1])
					if err == nil && !portMap[port] {
						ports = append(ports, port)
						portMap[port] = true
					}
				}
			}
		}
	}

	return ports, nil
}

func main() {
	// Example: Start a process that listens on a port

	slog.Info("Starting pingpong server")
	dir_path := "examples/pingpong/run.sh"

	// resolve the dir_path to an absolute path
	abs_path, err := filepath.Abs(dir_path)
	if err != nil {
		slog.Error("failed to resolve directory path", "error", err)
		return
	}

	slog.Info("Resolved directory path", "abs_path", abs_path)

	cmd := exec.Command(abs_path)

	// Start the process
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start process: %v", err)
	}

	// Wait a moment for the process to open ports if necessary
	// time.Sleep(time.Second)

	// Get the ports
	ports, err := getPortsForRunningCmd(cmd)
	if err != nil {
		log.Printf("Warning: %v", err)
	}

	fmt.Printf("Process %d is using ports: %v\n", cmd.Process.Pid, ports)

	// Don't forget to properly clean up the process when done
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill process: %v", err)
		}
	}()

	// Wait for the process to finish if needed
	// cmd.Wait()
}
