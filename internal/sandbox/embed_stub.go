//go:build no_embed

package sandbox

import (
	"fmt"
	"os"
)

// resolveFilterSO returns the path to the filter .so.
// In no_embed builds the .so is not compiled in, so only the env var is honoured.
func resolveFilterSO() (path string, cleanup func(), err error) {
	if p := os.Getenv("SHIMMY_SANDBOX_FILTER_SO"); p != "" {
		return p, func() {}, nil
	}
	return "", func() {}, fmt.Errorf(
		"no shimmy_filter.so embedded (built with -tags no_embed); " +
			"set SHIMMY_SANDBOX_FILTER_SO or rebuild without -tags no_embed")
}
