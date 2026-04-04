package sandbox

import (
	"context"
	"os/exec"
)

// Config holds the sandbox resource limits and execution settings.
type Config struct {
	MemoryBytes   uint64
	MaxProcs      uint64
	MaxFsizeBytes uint64
	MaxFDs        uint64
	WorkDir       string
	Env           []string
	NoNetwork     bool
}

// Backend wraps a command for sandboxed execution.
type Backend interface {
	Name() string
	// WrapCmd returns the final exec.Cmd to run (may wrap with drrun).
	WrapCmd(ctx context.Context, cmd string, args []string, cfg Config) (*exec.Cmd, error)
}
