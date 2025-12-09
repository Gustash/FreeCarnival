//go:build windows

package launch

import (
	"fmt"
	"os/exec"
)

// setupProcessGroup is a no-op on Windows.
// Process group management works differently on Windows.
func setupProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows
}

// killProcessGroup kills the process tree on Windows using taskkill.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Use taskkill to kill the process tree
	// /T kills the process tree, /F forces termination
	taskkill := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T", "/F")
	return taskkill.Run()
}

