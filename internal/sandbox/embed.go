//go:build !no_embed

package sandbox

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed shimmy_filter.so
var embeddedFilterSO []byte

// extractEmbeddedFilter writes the embedded shimmy_filter.so to a temp file
// and returns its path. The caller is responsible for cleanup when done.
func extractEmbeddedFilter() (string, error) {
	if len(embeddedFilterSO) == 0 {
		return "", fmt.Errorf("embedded shimmy_filter.so is empty (placeholder build)")
	}

	f, err := os.CreateTemp("", "shimmy_filter_*.so")
	if err != nil {
		return "", fmt.Errorf("extractEmbeddedFilter: create temp: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(embeddedFilterSO); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("extractEmbeddedFilter: write: %w", err)
	}

	// Make it executable so the dynamic linker can load it.
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("extractEmbeddedFilter: chmod: %w", err)
	}

	return f.Name(), nil
}

// resolveFilterSO returns the path to the shimmy_filter.so to use.
// Priority:
//  1. SHIMMY_SANDBOX_FILTER_SO env var (explicit user override)
//  2. Extract the embedded .so to a temp file
//
// The second return value is a cleanup function; call it when the process exits.
func resolveFilterSO() (path string, cleanup func(), err error) {
	if p := os.Getenv("SHIMMY_SANDBOX_FILTER_SO"); p != "" {
		return p, func() {}, nil
	}

	extracted, err := extractEmbeddedFilter()
	if err != nil {
		return "", func() {}, err
	}
	return extracted, func() { os.Remove(extracted) }, nil
}
