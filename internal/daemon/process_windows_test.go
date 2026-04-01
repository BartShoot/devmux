//go:build windows

package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"devmux/internal/config"
	"golang.org/x/sys/windows"
)

func TestShellCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // partial match for Args
	}{
		{
			name:     "Simple command",
			input:    "ls",
			expected: "ls",
		},
		{
			name:     "Command with forward slashes",
			input:    "bin/app",
			expected: "bin\\app",
		},
		{
			name:     "Command with spaces",
			input:    "echo 'hello world'",
			expected: "cmd.exe /c echo 'hello world'",
		},
		{
			name:     "Command with pipes",
			input:    "ls | grep go",
			expected: "cmd.exe /c ls | grep go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := shellCommand(tt.input)
			fullCmd := strings.Join(cmd.Args, " ")
			
			// For cmd.exe /c cases, don't use FromSlash for the whole expectation
			// because /c must remain /c
			if !strings.Contains(fullCmd, tt.expected) && !strings.Contains(fullCmd, filepath.FromSlash(tt.expected)) {
				t.Errorf("expected %q to contain %q, got %q", fullCmd, tt.expected, fullCmd)
			}
			
			if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
				t.Error("expected HideWindow to be true")
			}
		})
	}
}

func TestJobObjectAssignment(t *testing.T) {
	// Start a long-running process
	cmd := shellCommand("waitfor /t 10 something")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	defer cmd.Process.Kill()

	// Use Windows API to check if process is in a job
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatalf("OpenProcess failed: %v", err)
	}
	defer windows.CloseHandle(h)

	var inJob int32
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	isProcessInJob := kernel32.NewProc("IsProcessInJob")
	
	r1, _, err := isProcessInJob.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&inJob)))
	if r1 == 0 {
		t.Fatalf("IsProcessInJob failed: %v", err)
	}

	if inJob == 0 {
		t.Error("expected process to be in a job, but it is not")
	}
}

func TestKillProcessTree(t *testing.T) {
	pm := NewProcessManager()

	// Create a command that spawns a child process and keeps it alive
	// On Windows, cmd.exe /c start /b waitfor /t 60 child-wait-123 & waitfor /t 60 parent-wait-123
	// This will spawn a child process.
	// We'll use a simpler approach: cmd /c "start /b ping 127.0.0.1 -t & ping 127.0.0.1 -t"
	// But wait, ping -t stays alive.

	// Use a more reliable way: cmd /c "echo hello & pause"
	// We'll use start /b to spawn a child.
	cmdStr := "cmd.exe /c \"start /b ping localhost -t & ping localhost -t\""
	err := pm.StartProcess("tree-proc", cmdStr, "", config.HealthCheck{})
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	pm.mu.Lock()
	p := pm.processes["tree-proc"]
	pm.mu.Unlock()

	if !p.Running {
		t.Fatal("process should be running")
	}

	// Capture PID before killing
	pid := p.Cmd.Process.Pid

	// Kill it
	pm.killProcessTree(p)

	// Give it time to stop
	time.Sleep(1 * time.Second)

	// Check if PID still exists
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(h)
		var exitCode uint32
		err = windows.GetExitCodeProcess(h, &exitCode)
		if err == nil && exitCode == 259 { // 259 is STILL_ACTIVE
			t.Errorf("Process %d should have been killed, but it is still active", pid)
		}
	}
}
