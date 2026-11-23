//go:build windows

package git

import "os/exec"

func Detach(cmd *exec.Cmd) {}
