//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// Detach starts cmd in its own session, with no controlling terminal. An ssh
// or gpg prompt then fails cleanly instead of scribbling over the TUI.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
