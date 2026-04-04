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
	// Try to kill the whole process group.
	_ = syscall.Kill(pgid, syscall.SIGKILL)
	// Fall back to killing the direct process.
	_ = cmd.Process.Kill()
}
