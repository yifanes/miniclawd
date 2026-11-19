//go:build windows

package tools

import "os/exec"

// configureProcessGroup is a no-op on Windows (no process groups support in
// the same form); context cancellation kills the direct child only.
func configureProcessGroup(cmd *exec.Cmd) {}
