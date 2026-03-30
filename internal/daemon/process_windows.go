//go:build windows

package daemon

import (
	"fmt"
	"os/exec"
)

// setProcessGroup is a no-op on Windows (process groups work differently)
func setProcessGroup(cmd *exec.Cmd) {
	// Windows handles process groups differently
}

// killProcessTree kills the entire process tree on Windows using taskkill
func (pm *ProcessManager) killProcessTree(p *ManagedProcess) {
	if p.Cmd.Process == nil {
		return
	}

	// On Windows, taskkill /F /T is necessary for process trees
	exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", p.Cmd.Process.Pid)).Run()
	p.Cmd.Process.Kill() // Fallback
}

// shellCommand returns the shell command for Windows
func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd.exe", "/c", command)
}

// GetShellInfo returns shell path and arg format for logging
func GetShellInfo() (shell string, arg string) {
	return "cmd.exe", "/c"
}
