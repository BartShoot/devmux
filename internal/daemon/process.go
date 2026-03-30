package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"devmux/internal/config"
	"github.com/creack/pty"
)

type ProcessManager struct {
	processes map[string]*ManagedProcess
	mu        sync.Mutex
	verbose   bool // if true, copy process output to stdout
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
		verbose:   false,
	}
}

func (pm *ProcessManager) SetVerbose(v bool) {
	pm.verbose = v
}

func (pm *ProcessManager) RunHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.mu.Lock()
			for _, p := range pm.processes {
				// Even if not running, we want to run the check one last time 
				// or keep the status if it's already healthy (for one-shots)
				go func(proc *ManagedProcess) {
					status, _ := proc.HealthChecker.Check(ctx)
					pm.mu.Lock()
					// For one-shots, if it WAS healthy, keep it healthy 
					// unless the check now says otherwise.
					if status == StatusHealthy || proc.Status == StatusChecking {
						proc.Status = status
					} else if !proc.Running {
						proc.Status = StatusUnhealthy
					}
					pm.mu.Unlock()
				}(p)
			}
			pm.mu.Unlock()
		}
	}
}

type ManagedProcess struct {
	Name          string
	Command       string
	Cwd           string
	PTY           *os.File
	Stdin         io.WriteCloser
	Cmd           *exec.Cmd
	Running       bool
	Buffer        *LogBuffer
	HealthChecker *HealthChecker
	Status        HealthStatus
	HCCfg         config.HealthCheck
}

func (pm *ProcessManager) RestartProcess(name string) error {
	pm.mu.Lock()
	p, exists := pm.processes[name]
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	if p.Running {
		fmt.Printf("Restarting %s: Killing process tree...\n", name)
		pm.killProcessTree(p)

		// Wait for it to be cleaned up by the goroutine
		for i := 0; i < 20; i++ {
			pm.mu.Lock()
			running := p.Running
			pm.mu.Unlock()
			if !running {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Capture config before deleting
	pm.mu.Lock()
	command := p.Command
	cwd := p.Cwd
	hcCfg := p.HCCfg
	buffer := p.Buffer
	delete(pm.processes, name)
	pm.mu.Unlock()

	// Clear the buffer and add a restart message
	buffer.Clear()
	buffer.Write([]byte(fmt.Sprintf("\n[yellow]----------------------------------------[-]\n")))
	buffer.Write([]byte(fmt.Sprintf("[yellow]  RESTARTING %s [-]\n", name)))
	buffer.Write([]byte(fmt.Sprintf("[yellow]----------------------------------------[-]\n\n")))

	return pm.StartProcessWithBuffer(name, command, cwd, hcCfg, buffer)
}

func (pm *ProcessManager) StartProcessWithBuffer(name, command, cwd string, hcCfg config.HealthCheck, buffer *LogBuffer) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.processes[name]; exists {
		return fmt.Errorf("process with name %s already exists", name)
	}

	// Use platform-appropriate shell
	cmd := shellCommand(command)

	// Set working directory if specified
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Try PTY first (don't set process group - PTY handles this differently)
	ptmx, ptyErr := pty.Start(cmd)

	var stdout io.ReadCloser
	var stdin io.WriteCloser

	// Capture verbose flag early for use in goroutines
	verbose := pm.verbose

	if ptyErr != nil {
		// PTY failed - log the reason and fall back to pipes
		fmt.Printf("PTY failed for %s: %v (falling back to pipes)\n", name, ptyErr)
		// Need to recreate cmd since pty.Start may have modified it
		cmd = shellCommand(command)
		if cwd != "" {
			cmd.Dir = cwd
		}
		setProcessGroup(cmd)

		var err error
		// Get stdout pipe
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		// Get stderr pipe separately
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", err)
		}
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdin pipe: %w", err)
		}
		if err = cmd.Start(); err != nil {
			return fmt.Errorf("failed to start process: %w", err)
		}

		// Copy stderr to buffer (and stdout if verbose)
		go func() {
			var writer io.Writer = buffer
			if verbose {
				writer = io.MultiWriter(os.Stdout, buffer)
			}
			io.Copy(writer, stderr)
		}()
	} else {
		stdout = ptmx
		stdin = ptmx
	}

	hc := NewHealthChecker(hcCfg, buffer)

	managed := &ManagedProcess{
		Name:          name,
		Command:       command,
		Cwd:           cwd,
		PTY:           nil,
		Stdin:         stdin,
		Cmd:           cmd,
		Running:       true,
		Buffer:        buffer,
		HealthChecker: hc,
		Status:        StatusChecking,
		HCCfg:         hcCfg,
	}

	if ptmx != nil {
		managed.PTY = ptmx
	}

	pm.processes[name] = managed

	go func() {
		defer func() {
			if ptmx != nil {
				ptmx.Close()
			}
			if stdin != nil {
				stdin.Close()
			}
			pm.mu.Lock()
			managed.Running = false
			pm.mu.Unlock()
		}()

		var writer io.Writer = buffer
		if verbose {
			writer = io.MultiWriter(os.Stdout, buffer)
		}
		io.Copy(writer, stdout)

		err := cmd.Wait()
		if verbose {
			if err != nil {
				fmt.Printf("Process %s exited with error: %v\n", name, err)
			} else {
				fmt.Printf("Process %s exited cleanly\n", name)
			}
		}
	}()

	return nil
}

func (pm *ProcessManager) StartProcess(name, command, cwd string, hcCfg config.HealthCheck) error {
	return pm.StartProcessWithBuffer(name, command, cwd, hcCfg, NewLogBuffer(1000))
}

func (pm *ProcessManager) StopAll() {
	pm.mu.Lock()
	procs := make([]*ManagedProcess, 0, len(pm.processes))
	for _, p := range pm.processes {
		procs = append(procs, p)
	}
	pm.mu.Unlock()

	for _, p := range procs {
		if p.Running {
			fmt.Printf("Stopping process %s...\n", p.Name)
			pm.killProcessTree(p)
		}
	}

	// Wait up to 2 seconds for ports to release
	fmt.Println("Waiting for processes to release ports...")
	time.Sleep(2 * time.Second)
}

// WriteInput writes data to a process's stdin
func (pm *ProcessManager) WriteInput(name, input string) error {
	pm.mu.Lock()
	p, exists := pm.processes[name]
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	if !p.Running {
		return fmt.Errorf("process %s is not running", name)
	}

	if p.Stdin == nil {
		return fmt.Errorf("process %s has no stdin", name)
	}

	_, err := p.Stdin.Write([]byte(input))
	return err
}

