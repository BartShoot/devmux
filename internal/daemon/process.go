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
	"devmux/internal/protocol"
	"devmux/internal/terminal"
	"github.com/creack/pty"
)

type ProcessManager struct {
	processes map[string]*ManagedProcess
	mu        sync.Mutex
	verbose   bool    // if true, copy process output to stdout
	server    *Server // reference for notifications
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

func (pm *ProcessManager) SetServer(s *Server) {
	pm.server = s
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
			server := pm.server
			for _, p := range pm.processes {
				// Skip if already healthy - no need to keep polling
				if p.Status == StatusHealthy {
					continue
				}
				go func(proc *ManagedProcess) {
					status, _ := proc.HealthChecker.Check(ctx)
					pm.mu.Lock()
					oldStatus := proc.Status
					oldRunning := proc.Running
					if status == StatusHealthy || proc.Status == StatusChecking {
						proc.Status = status
					} else if !proc.Running {
						proc.Status = StatusUnhealthy
					}
					newStatus := proc.Status
					newRunning := proc.Running
					name := proc.Name
					pm.mu.Unlock()

					// Broadcast status change if changed
					if server != nil && (oldStatus != newStatus || oldRunning != newRunning) {
						paneID := server.getPaneID(name)
						if paneID != 0 {
							server.BroadcastPaneStatus(paneID, newRunning, string(newStatus))
						}
					}
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
	Terminal      *terminal.Terminal // Terminal emulator for this process
	PaneID        protocol.PaneID    // Numerical ID for streaming protocol
	updateSeq     uint64             // Sequence number for screen updates
}

func (pm *ProcessManager) RestartProcess(name string) error {
	pm.mu.Lock()
	p, exists := pm.processes[name]
	running := exists && p.Running
	pm.mu.Unlock()

	if !exists {
		return fmt.Errorf("process %s not found", name)
	}

	if running {
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

	// Create terminal emulator
	// Check if we have a stored size for this pane (from previous run)
	cols, rows := 80, 24
	if pm.server != nil {
		paneID := pm.server.getPaneID(name)
		if storedCols, storedRows := pm.server.getPaneSize(paneID); storedCols > 0 && storedRows > 0 {
			cols, rows = storedCols, storedRows
		}
	}
	term, termErr := terminal.New(cols, rows)
	if termErr != nil {
		fmt.Printf("Warning: failed to create terminal for %s: %v\n", name, termErr)
	}

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
		Terminal:      term,
	}

	if ptmx != nil {
		managed.PTY = ptmx
	}

	pm.processes[name] = managed

	go func() {
		// Cache server and paneID once discovered (avoids lock on every read)
		var cachedServer *Server
		var cachedPaneID protocol.PaneID

		defer func() {
			if ptmx != nil {
				ptmx.Close()
			}
			if stdin != nil {
				stdin.Close()
			}
			if term != nil {
				term.Close()
			}
			pm.mu.Lock()
			managed.Running = false
			pm.mu.Unlock()
		}()

		// Read output and feed to buffer, terminal, and subscribers
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				data := buf[:n]

				// Write to buffer (for backward compatibility / health checks)
				buffer.Write(data)

				// Write to verbose output if enabled
				if verbose {
					os.Stdout.Write(data)
				}

				// Feed to terminal emulator
				if term != nil {
					term.Write(data)

					// Lazily resolve server/paneID once available
					if cachedServer == nil {
						pm.mu.Lock()
						cachedServer = pm.server
						pm.mu.Unlock()
					}
					if cachedServer != nil && cachedPaneID == 0 {
						cachedPaneID = cachedServer.getPaneID(name)
						managed.PaneID = cachedPaneID
					}

					// Mark pane as dirty — actual screen materialization deferred to coalescer flush
					if cachedServer != nil && cachedPaneID != 0 {
						cachedServer.coalescer.MarkDirty(cachedPaneID)
					}
				}
			}
			if err != nil {
				break
			}
		}

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
	type procInfo struct {
		proc    *ManagedProcess
		running bool
	}
	procs := make([]procInfo, 0, len(pm.processes))
	for _, p := range pm.processes {
		procs = append(procs, procInfo{proc: p, running: p.Running})
	}
	pm.mu.Unlock()

	for _, info := range procs {
		if info.running {
			fmt.Printf("Stopping process %s...\n", info.proc.Name)
			pm.killProcessTree(info.proc)
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

