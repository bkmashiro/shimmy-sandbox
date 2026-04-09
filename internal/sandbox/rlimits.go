//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
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
// Errors are logged to stderr; they are non-fatal so the run still proceeds.
func ApplyRlimits(pid int, cfg Config) {
	if cfg.MemoryBytes > 0 {
		lim := unix.Rlimit{Cur: cfg.MemoryBytes, Max: cfg.MemoryBytes}
		if err := unix.Prlimit(pid, unix.RLIMIT_AS, &lim, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[shimmy] warning: RLIMIT_AS: %v\n", err)
		}
	}
	if cfg.MaxProcs > 0 {
		lim := unix.Rlimit{Cur: cfg.MaxProcs, Max: cfg.MaxProcs}
		if err := unix.Prlimit(pid, unix.RLIMIT_NPROC, &lim, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[shimmy] warning: RLIMIT_NPROC: %v\n", err)
		}
	}
	if cfg.MaxFsizeBytes > 0 {
		lim := unix.Rlimit{Cur: cfg.MaxFsizeBytes, Max: cfg.MaxFsizeBytes}
		if err := unix.Prlimit(pid, unix.RLIMIT_FSIZE, &lim, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[shimmy] warning: RLIMIT_FSIZE: %v\n", err)
		}
	}
	if cfg.MaxFDs > 0 {
		lim := unix.Rlimit{Cur: cfg.MaxFDs, Max: cfg.MaxFDs}
		if err := unix.Prlimit(pid, unix.RLIMIT_NOFILE, &lim, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[shimmy] warning: RLIMIT_NOFILE: %v\n", err)
		}
	}
	// Always disable core dumps in the child.
	coreLim := unix.Rlimit{Cur: 0, Max: 0}
	if err := unix.Prlimit(pid, unix.RLIMIT_CORE, &coreLim, nil); err != nil {
		fmt.Fprintf(os.Stderr, "[shimmy] warning: RLIMIT_CORE: %v\n", err)
	}
}
