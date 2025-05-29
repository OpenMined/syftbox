package appsv2

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/openmined/syftbox/internal/utils"
	"github.com/shirou/gopsutil/v4/process"
)

var (
	ErrAlreadyRunning = errors.New("process already running")
	ErrNotRunning     = errors.New("process not running")
)

type AppProcessStatus string

const (
	StatusNew     AppProcessStatus = "new"
	StatusRunning AppProcessStatus = "running"
	StatusStopped AppProcessStatus = "stopped"
)

type AppExit struct {
	Code  int
	Error error
}

// AppProcess manages a subprocess and its children
type AppProcess struct {
	ID         string // not PID!
	procName   string
	procArgs   []string
	procEnvs   []string
	procDir    string
	procStdout io.Writer
	procStderr io.Writer

	proc     *exec.Cmd
	procInfo *process.Process
	procMu   sync.RWMutex

	state   AppProcessStatus
	stateMu sync.RWMutex

	// Channel to signal when process completes
	exit chan AppExit
	done chan struct{}
}

// NewAppProcess creates a new AppProcess instance
func NewAppProcess(name string, args ...string) *AppProcess {
	return &AppProcess{
		ID:       utils.TokenHex(3),
		procName: name,
		state:    StatusNew,
		procArgs: args,
		// procStdout: os.Stdout,
		// procStderr: os.Stderr,
	}
}

func (p *AppProcess) SetID(id string) *AppProcess {
	p.ID = id
	return p
}

func (p *AppProcess) SetEnvs(envs map[string]string) *AppProcess {
	for key, value := range envs {
		p.procEnvs = append(p.procEnvs, fmt.Sprintf("%s=%s", key, value))
	}
	return p
}

func (p *AppProcess) SetWorkingDir(path string) *AppProcess {
	p.procDir = path
	return p
}

func (p *AppProcess) SetStdout(w io.Writer) *AppProcess {
	p.procStdout = w
	return p
}

func (p *AppProcess) SetStderr(w io.Writer) *AppProcess {
	p.procStderr = w
	return p
}

// Start starts the subprocess
func (p *AppProcess) Start() error {
	if p.GetStatus() == StatusRunning {
		return ErrAlreadyRunning
	}

	p.procMu.Lock()
	defer p.procMu.Unlock()

	p.proc = exec.Command(p.procName, p.procArgs...)
	// set working directory
	if p.procDir != "" {
		p.proc.Dir = p.procDir
	}

	// Set up process group for proper tree killing
	p.proc.SysProcAttr = getSysProcAttr()

	// Inherit environment and set up I/O
	p.proc.Env = append(os.Environ(), p.procEnvs...)
	p.proc.Stdin = nil
	p.proc.Stdout = p.procStdout
	p.proc.Stderr = p.procStderr

	// Start the process
	if err := p.proc.Start(); err != nil {
		p.setStatusStopped()
		return fmt.Errorf("failed to start process: %w", err)
	}

	p.setStatusRunning()
	p.exit = make(chan AppExit)
	p.done = make(chan struct{})

	procInfo, err := process.NewProcess(int32(p.proc.Process.Pid))
	if err != nil {
		p.setStatusStopped()
		return fmt.Errorf("failed to get process info: %w", err)
	}
	p.procInfo = procInfo

	// Monitor process completion
	go p.monitor()

	return nil
}

// Stop terminates the process and all its children
func (p *AppProcess) Stop() error {
	state := p.GetStatus()

	if state != StatusRunning {
		return ErrNotRunning
	}

	// Kill the entire process group
	if err := p.killProcessGroup(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	// set the process to stopped
	p.setStatusStopped()

	p.procMu.Lock()
	p.proc = nil
	p.procInfo = nil
	p.procMu.Unlock()

	return nil
}

// Wait blocks until the process exits and returns the exit code or error
func (p *AppProcess) Wait() (int, error) {
	if p.GetStatus() != StatusRunning {
		return -1, ErrNotRunning
	}

	// wait for the process to exit
	exitVal := <-p.exit

	return exitVal.Code, exitVal.Error
}

func (p *AppProcess) Process() *process.Process {
	p.procMu.RLock()
	defer p.procMu.RUnlock()
	return p.procInfo
}

func (p *AppProcess) GetStatus() AppProcessStatus {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.state
}

func (p *AppProcess) setStatusStopped() {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.state = StatusStopped
}

func (p *AppProcess) setStatusRunning() {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.state = StatusRunning
}

// monitor watches for process completion
func (p *AppProcess) monitor() {
	// wait for the process to exit
	err := p.proc.Wait()
	exitCode := p.proc.ProcessState.ExitCode()

	p.setStatusStopped()

	if err != nil {
		var sysErr *exec.ExitError
		if errors.As(err, &sysErr) {
			exitCode = sysErr.ExitCode()
		}
	}

	// send the exit code and error to the exit channel
	p.exit <- AppExit{
		Code:  exitCode,
		Error: err,
	}

	// close the done and exit channels
	close(p.done)
	close(p.exit)
}

// killProcessGroup kills the process and all its children
func (p *AppProcess) killProcessGroup() error {
	// lock & copy the process and process info
	p.procMu.RLock()
	proc := p.proc
	procInfo := p.procInfo

	p.procMu.RUnlock()

	if proc == nil || proc.Process == nil {
		return fmt.Errorf("process is nil")
	}

	if procInfo == nil {
		return fmt.Errorf("process info is nil")
	}

	// get the process ID
	pid := proc.Process.Pid

	// Get all descendants in a bottom-up order.
	descendants, err := getProcessTreeBottomUp(procInfo)
	if err != nil {
		// Fallback to killing the main process if we can't get children
		descendants = []*process.Process{procInfo}
	}

	// if there are no descendants, so we're done
	if len(descendants) == 0 {
		return nil
	}

	// send SIGTERM to all descendants
	slog.Debug("kill process group: SIGTERM", "id", p.ID, "pid", pid, "subprocs", len(descendants))
	for _, child := range descendants {
		if err := child.Terminate(); err != nil {
			slog.Debug("kill process group: SIGTERM", "id", p.ID, "pid", child.Pid, "ppid", pid, "err", err)
		}
	}

	// give some time for cleanup
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()

	select {
	case <-p.done:
		slog.Debug("kill process group: process completed", "id", p.ID, "pid", pid)
		return nil
	case <-timeout.C:
		slog.Debug("kill process group: timed out", "id", p.ID, "pid", pid)
	}

	// nuke the process group with SIGKILL
	slog.Debug("kill process group: SIGKILL", "id", p.ID, "pid", pid, "subprocs", len(descendants))
	for _, child := range descendants {
		// skip if the process doesn't exist
		exists, err := process.PidExists(child.Pid)
		if err != nil || !exists {
			continue
		}
		// kill the process
		if err := child.Kill(); err != nil {
			slog.Warn("kill process group: SIGKILL", "id", p.ID, "pid", child.Pid, "ppid", pid, "err", err)
		}
	}

	return nil
}

// getProcessTreeBottomUp recursively traverses the process tree starting from a given process
// and returns a flattened slice of all descendant processes in a bottom-up order.
func getProcessTreeBottomUp(proc *process.Process) ([]*process.Process, error) {
	var tree []*process.Process
	children, err := proc.Children()
	if err != nil {
		// If we can't list children, we can't go deeper.
		return nil, fmt.Errorf("failed to list children for pid %d: %w", proc.Pid, err)
	}

	// Recursively call for each child.
	for _, child := range children {
		// We ignore errors from sub-trees to ensure we kill as much of the tree as possible.
		subtree, _ := getProcessTreeBottomUp(child)
		tree = append(tree, subtree...)
	}

	// Add the parent process to the list *after* all its children and their descendants.
	tree = append(tree, proc)
	return tree, nil
}
