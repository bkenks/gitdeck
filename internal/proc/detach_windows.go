//go:build windows

package proc

import "os/exec"

// Detach is a no-op on Windows, which has no controlling terminal to escape.
func Detach(cmd *exec.Cmd) {}
