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
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
	}
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
				if !p.Running {
					p.Status = StatusUnhealthy
					continue
				}
				go func(proc *ManagedProcess) {
					status, _ := proc.HealthChecker.Check(ctx)
					pm.mu.Lock()
					proc.Status = status
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
	PTY           *os.File
	Cmd           *exec.Cmd
	Running       bool
	Buffer        *LogBuffer
	HealthChecker *HealthChecker
	Status        HealthStatus
	HCCfg         config.HealthCheck
}

func (pm *ProcessManager) StartProcess(name, command string, hcCfg config.HealthCheck) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.processes[name]; exists {
		return fmt.Errorf("process with name %s already exists", name)
	}

	cmd := exec.Command("cmd.exe", "/c", command)
	ptmx, err := pty.Start(cmd)
	
	var stdout io.ReadCloser
	var stdin io.WriteCloser

	if err != nil && err.Error() == "unsupported" {
		// Fallback for Windows or other unsupported PTY systems
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		cmd.Stderr = cmd.Stdout // Redirect stderr to stdout
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdin pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start process: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to start process in PTY: %w", err)
	} else {
		stdout = ptmx
		stdin = ptmx
	}

	buffer := NewLogBuffer(1000)
	hc := NewHealthChecker(hcCfg, buffer)

	managed := &ManagedProcess{
		Name:          name,
		Command:       command,
		PTY:           nil, // Only set if ptmx worked
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

		// Capture process output in the buffer and also mirror to stdout
		io.Copy(io.MultiWriter(os.Stdout, buffer), stdout)
		err := cmd.Wait()
		if err != nil {
			fmt.Printf("Process %s exited with error: %v\n", name, err)
		} else {
			fmt.Printf("Process %s exited cleanly\n", name)
		}
	}()

	return nil
}

func (pm *ProcessManager) RestartProcess(name string) error {
	pm.mu.Lock()
	p, exists := pm.processes[name]
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	if p.Running {
		if err := p.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process %s: %w", name, err)
		}
	}

	// Wait a bit for the old process to exit
	time.Sleep(500 * time.Millisecond)

	pm.mu.Lock()
	command := p.Command
	hcCfg := p.HCCfg
	delete(pm.processes, name)
	pm.mu.Unlock()

	return pm.StartProcess(name, command, hcCfg)
}
