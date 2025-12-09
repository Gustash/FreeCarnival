//go:build !windows

package launch

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to run in its own process group on Unix systems.
// This ensures child processes are killed when the parent is terminated.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup kills the entire process group on Unix systems.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Get the process group ID
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// If we can't get the pgid, just kill the process itself
		return cmd.Process.Kill()
	}

	// Kill the entire process group (negative PID kills the group)
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

