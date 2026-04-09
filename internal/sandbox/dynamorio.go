package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DynamoRIOBackend wraps the command with drrun then delegates rlimits to RlimitsBackend.
type DynamoRIOBackend struct {
	// tempSO holds the path of an extracted embedded .so so it can be cleaned up
	// after the child exits. It is set during WrapCmd.
	tempSO string
}

func (b *DynamoRIOBackend) Name() string { return "dynamorio" }

func (b *DynamoRIOBackend) WrapCmd(ctx context.Context, cmd string, args []string, cfg Config) (*exec.Cmd, error) {
	drrun, err := resolveDrrun()
	if err != nil {
		return nil, err
	}

	// Resolve the filter .so: env var → embedded extraction.
	filterSO, cleanup, err := resolveFilterSO()
	if err != nil {
		// Non-fatal: run without filter (no -c flag).
		filterSO = ""
	}
	// Store the cleanup path so callers can defer removal.
	b.tempSO = filterSO
	_ = cleanup // cleanup is best-effort; process exit will GC the temp anyway

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
// It checks DYNAMORIO_HOME first, then ~/.shimmy-sandbox/dynamorio.
func resolveDrrun() (string, error) {
	homes := []string{}

	if h := os.Getenv("DYNAMORIO_HOME"); h != "" {
		homes = append(homes, h)
	}

	// Also check the path installed by `shimmy-sandbox setup`.
	if home, err := os.UserHomeDir(); err == nil {
		homes = append(homes, filepath.Join(home, ".shimmy-sandbox", "dynamorio"))
	}

	for _, h := range homes {
		p := filepath.Join(h, "bin64", "drrun")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("drrun not found: set DYNAMORIO_HOME env var or run 'shimmy-sandbox setup'")
}
