package apps

import (
	"fmt"
	"slices"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type ProcessStats struct {
	// Process Name
	ProcessName string `json:"processName"`
	// Process ID
	PID int32 `json:"pid"`
	// Status of the process
	Status []string `json:"status"`
	// Command line arguments for this app's process
	Cmdline []string `json:"cmdline"`
	// Current working directory of this app's process
	CWD string `json:"cwd"`
	// Environment variables for this app's process
	Environ []string `json:"environ"`
	// Executable path of this app's process
	Exe string `json:"exe"`
	// List of groups this app is a member of
	Gids []uint32 `json:"gids"`
	// List of user IDs this app is a member of
	Uids []uint32 `json:"uids"`
	// Nice value of this app's process
	Nice int32 `json:"nice"`
	// Username of the user this app is running as
	Username string `json:"username"`
	// All connections this app is listening on
	Connections []net.ConnectionStat `json:"connections"`
	// Percentage of total CPU this app is using
	CPUPercent float64 `json:"cpuPercent"`
	// CPU times breakdown
	CPUTimes *cpu.TimesStat `json:"cpuTimes"`
	// Number of threads this app is using
	NumThreads int32 `json:"numThreads"`
	// Percentage of total RAM this app is using
	MemoryPercent float32 `json:"memoryPercent"`
	// Memory info
	MemoryInfo *process.MemoryInfoStat `json:"memoryInfo"`
	// How long the app has been running in milliseconds
	Uptime int64 `json:"uptime"`
	// Children processes
	Children []*ProcessStats `json:"children"`
}

func NewProcessStats(p *process.Process) (*ProcessStats, error) {
	// Get process name
	processName, err := p.Name()
	if err != nil {
		return nil, fmt.Errorf("failed to get process name: %w", err)
	}

	status, err := p.Status()
	if err != nil {
		status = []string{}
	}

	// Get command line
	cmdline, err := p.CmdlineSlice()
	if err != nil {
		cmdline = []string{} // Empty slice if we can't get cmdline
	}

	// Get working directory
	cwd, err := p.Cwd()
	if err != nil {
		cwd = "" // Empty string if we can't get cwd
	}

	// Get environment variables
	environ, err := p.Environ()
	if err != nil {
		environ = []string{} // Empty slice if we can't get environ
	}

	// Get executable path
	exe, err := p.Exe()
	if err != nil {
		exe = "" // Empty string if we can't get exe
	}

	// Get group IDs
	gids, err := p.Gids()
	if err != nil {
		gids = []uint32{} // Empty slice if we can't get gids
	}

	// Get user IDs
	uids, err := p.Uids()
	if err != nil {
		uids = []uint32{} // Empty slice if we can't get uids
	}

	// Get nice value
	nice, err := p.Nice()
	if err != nil {
		nice = 0 // Default nice value if we can't get it
	}

	// Get username
	username, err := p.Username()
	if err != nil {
		username = "" // Empty string if we can't get username
	}

	// Get connections
	connections, err := p.Connections()
	if err != nil || len(connections) == 0 {
		connections = []net.ConnectionStat{}
	}

	cpuPercent, err := p.CPUPercent()
	if err != nil {
		cpuPercent = 0
	}

	// Get CPU times
	cpuTimes, err := p.Times()
	if err != nil {
		cpuTimes = nil // Nil if we can't get CPU times
	}

	// Get number of threads
	numThreads, err := p.NumThreads()
	if err != nil {
		numThreads = 0 // Default to 0 if we can't get num threads
	}

	memoryPercent, err := p.MemoryPercent()
	if err != nil {
		memoryPercent = 0
	}

	// Get memory info
	memoryInfo, err := p.MemoryInfo()
	if err != nil {
		memoryInfo = nil // Nil if we can't get memory info
	}

	createTime, err := p.CreateTime()
	var uptime int64
	if err != nil {
		uptime = 0
	} else {
		now := time.Now().UnixMilli()
		uptime = now - createTime
	}

	childProcesses, err := p.Children()
	if err != nil {
		childProcesses = []*process.Process{}
	}
	children := make([]*ProcessStats, len(childProcesses))
	for i, child := range childProcesses {
		childStats, err := NewProcessStats(child)
		if err != nil {
			continue
		}
		children[i] = childStats
	}

	return &ProcessStats{
		ProcessName:   processName,
		PID:           p.Pid,
		Status:        status,
		Cmdline:       cmdline,
		CWD:           cwd,
		Environ:       environ,
		Exe:           exe,
		Gids:          gids,
		Uids:          uids,
		Nice:          nice,
		Username:      username,
		Connections:   connections,
		CPUPercent:    cpuPercent,
		CPUTimes:      cpuTimes,
		NumThreads:    numThreads,
		MemoryPercent: memoryPercent,
		MemoryInfo:    memoryInfo,
		Uptime:        uptime,
		Children:      children,
	}, nil
}

// ProcessListenPorts returns a list of ports that the process is listening on
func ProcessListenPorts(process *process.Process) []uint32 {
	ports := make([]uint32, 0)

	if process == nil {
		return ports
	}

	// Recursively travel down the process tree and return the port of all connections that is not 0
	connections, _ := process.Connections()
	for _, connection := range connections {
		if connection.Laddr.Port != 0 && connection.Status == "LISTEN" {
			ports = append(ports, connection.Laddr.Port)
		}
	}
	children, _ := process.Children()
	for _, child := range children {
		childPorts := ProcessListenPorts(child)
		ports = append(ports, childPorts...)
	}

	slices.Sort(ports)
	ports = slices.Compact(ports)
	return ports
}
