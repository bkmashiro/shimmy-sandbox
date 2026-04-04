//go:build linux

package sandbox

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// RlimitsBackend applies Linux resource limits via unix.Prlimit after child start.
type RlimitsBackend struct{}

func (b *RlimitsBackend) Name() string { return "rlimits" }

func (b *RlimitsBackend) WrapCmd(ctx context.Context, cmd string, args []string, cfg Config) (*exec.Cmd, error) {
	c := exec.CommandContext(ctx, cmd, args...)
	c.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	return c, nil
}

// ApplyRlimits sets resource limits on a running child process via prlimit(2).
func ApplyRlimits(pid int, cfg Config) {
	if cfg.MemoryBytes > 0 {
		_ = unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{Cur: cfg.MemoryBytes, Max: cfg.MemoryBytes}, nil)
	}
	if cfg.MaxProcs > 0 {
		_ = unix.Prlimit(pid, unix.RLIMIT_NPROC, &unix.Rlimit{Cur: cfg.MaxProcs, Max: cfg.MaxProcs}, nil)
	}
	if cfg.MaxFsizeBytes > 0 {
		_ = unix.Prlimit(pid, unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: cfg.MaxFsizeBytes, Max: cfg.MaxFsizeBytes}, nil)
	}
	if cfg.MaxFDs > 0 {
		_ = unix.Prlimit(pid, unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: cfg.MaxFDs, Max: cfg.MaxFDs}, nil)
	}
	// Always disable core dumps in the child.
	_ = unix.Prlimit(pid, unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}, nil)
}
