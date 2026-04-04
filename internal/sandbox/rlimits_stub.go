//go:build !linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

// RlimitsBackend is a no-op stub on non-Linux platforms.
type RlimitsBackend struct{}

func (b *RlimitsBackend) Run(ctx context.Context, cfg RunConfig) (Result, error) {
	if len(cfg.Args) == 0 {
		return Result{Kind: ExitInternal}, fmt.Errorf("no command specified")
	}
	cmd := exec.Command(cfg.Args[0], cfg.Args[1:]...)
	return baseRun(ctx, cmd, cfg), nil
}

// RunRlimitChild is a stub on non-Linux systems.
func RunRlimitChild() error {
	return fmt.Errorf("rlimits not supported on this platform")
}
