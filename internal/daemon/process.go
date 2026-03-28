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
	hcCfg := p.HCCfg
	buffer := p.Buffer
	delete(pm.processes, name)
	pm.mu.Unlock()

	// Clear the buffer and add a restart message
	buffer.Clear()
	buffer.Write([]byte(fmt.Sprintf("\n[yellow]----------------------------------------[-]\n")))
	buffer.Write([]byte(fmt.Sprintf("[yellow]  RESTARTING %s [-]\n", name)))
	buffer.Write([]byte(fmt.Sprintf("[yellow]----------------------------------------[-]\n\n")))

	return pm.StartProcessWithBuffer(name, command, hcCfg, buffer)
}

func (pm *ProcessManager) StartProcessWithBuffer(name, command string, hcCfg config.HealthCheck, buffer *LogBuffer) error {
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
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		cmd.Stderr = cmd.Stdout
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

	hc := NewHealthChecker(hcCfg, buffer)

	managed := &ManagedProcess{
		Name:          name,
		Command:       command,
		PTY:           nil,
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

func (pm *ProcessManager) StartProcess(name, command string, hcCfg config.HealthCheck) error {
	return pm.StartProcessWithBuffer(name, command, hcCfg, NewLogBuffer(1000))
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

func (pm *ProcessManager) killProcessTree(p *ManagedProcess) {
	if p.Cmd.Process == nil {
		return
	}
	// On Windows, taskkill /F /T is necessary for go run trees
	exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", p.Cmd.Process.Pid)).Run()
	p.Cmd.Process.Kill() // Fallback
}
