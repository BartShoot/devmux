//go:build windows

package daemon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobObject windows.Handle
)

func init() {
	// Create a Job Object to ensure all child processes are killed when the daemon exits
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		fmt.Printf("Warning: failed to create Job Object: %v\n", err)
		return
	}
	jobObject = h

	// Set the Job Object to kill all processes on close
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		jobObject,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		fmt.Printf("Warning: failed to set Job Object information: %v\n", err)
		windows.CloseHandle(jobObject)
		jobObject = 0
		return
	}

	// Assign the current process (the daemon) to the Job Object.
	// All future children will automatically inherit the job.
	err = windows.AssignProcessToJobObject(jobObject, windows.CurrentProcess())
	if err != nil {
		// This can fail if the process is already part of a job (e.g. run via some IDEs)
		// We log it but continue as it's not always fatal.
		fmt.Printf("Note: failed to assign daemon to Job Object: %v\n", err)
	}
}

// setProcessGroup is a no-op on Windows (process groups work differently)
func setProcessGroup(cmd *exec.Cmd) {
	// Job Object handles this automatically for us on Windows
}

// killProcessTree kills the entire process tree on Windows using taskkill
func (pm *ProcessManager) killProcessTree(p *ManagedProcess) {
	if p.Cmd.Process == nil {
		return
	}

	// On Windows, taskkill /F /T is necessary for manual kills
	exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", p.Cmd.Process.Pid)).Run()
	p.Cmd.Process.Kill() // Fallback
}

// shellCommand returns the shell command for Windows.
func shellCommand(command string) *exec.Cmd {
	normalized := filepath.FromSlash(command)

	if !strings.ContainsAny(normalized, " <>&|") {
		cmd := exec.Command(normalized)
		// Ensure it uses the Job Object even if inheritance was somehow blocked
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}
		return cmd
	}

	cmd := exec.Command("cmd.exe", "/c", normalized)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return cmd
}

// GetShellInfo returns shell path and arg format for logging
func GetShellInfo() (shell string, arg string) {
	return "cmd.exe", "/c"
}
