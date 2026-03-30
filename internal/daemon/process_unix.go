//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup sets the process to run in its own process group on Unix
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the entire process tree on Unix using process groups
func (pm *ProcessManager) killProcessTree(p *ManagedProcess) {
	if p.Cmd.Process == nil {
		return
	}

	// On Unix, send SIGTERM to the process group (negative PID)
	pgid, err := syscall.Getpgid(p.Cmd.Process.Pid)
	if err == nil {
		// Send SIGTERM to entire process group
		syscall.Kill(-pgid, syscall.SIGTERM)

		// Give processes time to gracefully shutdown
		time.Sleep(500 * time.Millisecond)

		// Force kill if still running
		syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		// Fallback to killing just the main process
		p.Cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		p.Cmd.Process.Kill()
	}
}

// shellCommand returns the shell command for Unix
func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

// GetShellInfo returns shell path and arg format for logging
func GetShellInfo() (shell string, arg string) {
	return "/bin/sh", "-c"
}
