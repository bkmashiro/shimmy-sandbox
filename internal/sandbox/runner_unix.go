//go:build linux || darwin || freebsd || openbsd || netbsd

package sandbox

import (
	"os/exec"
	"syscall"
)

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
