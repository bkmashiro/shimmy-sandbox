package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DynamoRIOBackend wraps the command with drrun before applying rlimits.
type DynamoRIOBackend struct{}

func (b *DynamoRIOBackend) Run(ctx context.Context, cfg RunConfig) (Result, error) {
	if len(cfg.Args) == 0 {
		return Result{Kind: ExitInternal}, fmt.Errorf("no command specified")
	}

	drrun, err := resolveDrrun(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: %v\n", err)
		return Result{Kind: ExitInternal}, err
	}

	wrapped := buildDrrunArgs(drrun, cfg)

	newCfg := cfg
	newCfg.Args = wrapped

	rl := &RlimitsBackend{}
	return rl.Run(ctx, newCfg)
}

// resolveDrrun returns the path to the drrun binary.
func resolveDrrun(cfg RunConfig) (string, error) {
	if cfg.DrrunPath != "" {
		return cfg.DrrunPath, nil
	}
	if home := os.Getenv("DYNAMORIO_HOME"); home != "" {
		p := filepath.Join(home, "bin64", "drrun")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("drrun not found: set DYNAMORIO_HOME or --drrun flag")
}

// buildDrrunArgs assembles the argv slice for drrun invocation.
func buildDrrunArgs(drrun string, cfg RunConfig) []string {
	args := []string{drrun}
	if cfg.DrTool != "" {
		// Resolve SHIMMY_SANDBOX_FILTER_SO if DrTool not explicitly set.
		args = append(args, "-c", cfg.DrTool)
	}
	args = append(args, "--")
	args = append(args, cfg.Args...)
	return args
}
