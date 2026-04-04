package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DynamoRIOBackend wraps the command with drrun then delegates rlimits to RlimitsBackend.
type DynamoRIOBackend struct{}

func (b *DynamoRIOBackend) Name() string { return "dynamorio" }

func (b *DynamoRIOBackend) WrapCmd(ctx context.Context, cmd string, args []string, cfg Config) (*exec.Cmd, error) {
	drrun, err := resolveDrrun()
	if err != nil {
		return nil, err
	}

	filterSO := os.Getenv("SHIMMY_SANDBOX_FILTER_SO")

	drArgs := []string{}
	if filterSO != "" {
		drArgs = append(drArgs, "-c", filterSO)
	}
	drArgs = append(drArgs, "--", cmd)
	drArgs = append(drArgs, args...)

	// Delegate the actual exec.Cmd construction to RlimitsBackend so that
	// rlimits are applied on the drrun process (and inherited by the child).
	rl := &RlimitsBackend{}
	return rl.WrapCmd(ctx, drrun, drArgs, cfg)
}

// resolveDrrun returns the path to the drrun binary.
func resolveDrrun() (string, error) {
	if home := os.Getenv("DYNAMORIO_HOME"); home != "" {
		p := filepath.Join(home, "bin64", "drrun")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("drrun not found: set DYNAMORIO_HOME env var")
}
