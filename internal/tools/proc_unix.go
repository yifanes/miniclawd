//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child process into its own process group and
// makes context cancellation kill the whole group. Without this, `bash -c`
// wrappers leave orphaned grandchildren behind when the timeout fires.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID targets the whole process group.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
