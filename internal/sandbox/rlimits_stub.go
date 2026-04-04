//go:build !linux

package sandbox

import (
	"context"
	"os/exec"
)

// RlimitsBackend is a no-op stub on non-Linux platforms.
type RlimitsBackend struct{}

func (b *RlimitsBackend) Name() string { return "rlimits" }

func (b *RlimitsBackend) WrapCmd(ctx context.Context, cmd string, args []string, cfg Config) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, cmd, args...), nil
}

// ApplyRlimits is a no-op on non-Linux systems.
func ApplyRlimits(pid int, cfg Config) {}
