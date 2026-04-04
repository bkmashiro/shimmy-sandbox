package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRlimitsBackendName(t *testing.T) {
	b := &RlimitsBackend{}
	if b.Name() != "rlimits" {
		t.Errorf("expected name 'rlimits', got %q", b.Name())
	}
}

func TestDynamoRIOBackendMissingHome(t *testing.T) {
	t.Setenv("DYNAMORIO_HOME", "")
	t.Setenv("SHIMMY_SANDBOX_FILTER_SO", "")

	b := &DynamoRIOBackend{}
	_, err := b.WrapCmd(context.Background(), "/bin/echo", []string{"hi"}, Config{})
	if err == nil {
		t.Fatal("expected error when DYNAMORIO_HOME is unset")
	}
	if !strings.Contains(err.Error(), "DYNAMORIO_HOME") {
		t.Errorf("expected error to mention DYNAMORIO_HOME, got: %v", err)
	}
}

func TestDynamoRIOBackendWithHome(t *testing.T) {
	// Create a fake drrun binary
	tmp := t.TempDir()
	bin64 := filepath.Join(tmp, "bin64")
	if err := os.MkdirAll(bin64, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDrrun := filepath.Join(bin64, "drrun")
	if err := os.WriteFile(fakeDrrun, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DYNAMORIO_HOME", tmp)
	t.Setenv("SHIMMY_SANDBOX_FILTER_SO", "/path/to/filter.so")

	b := &DynamoRIOBackend{}
	cmd, err := b.WrapCmd(context.Background(), "/bin/echo", []string{"arg1"}, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The first argument should be the drrun binary
	if cmd.Path != fakeDrrun {
		t.Errorf("expected cmd.Path=%q, got %q", fakeDrrun, cmd.Path)
	}

	// Args should contain: drrun -c filter.so -- /bin/echo arg1
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "-c") {
		t.Errorf("expected -c flag in args, got: %v", cmd.Args)
	}
	if !strings.Contains(joined, "filter.so") {
		t.Errorf("expected filter.so in args, got: %v", cmd.Args)
	}
	if !strings.Contains(joined, "--") {
		t.Errorf("expected -- separator in args, got: %v", cmd.Args)
	}
	if !strings.Contains(joined, "/bin/echo") {
		t.Errorf("expected /bin/echo in args, got: %v", cmd.Args)
	}
	if !strings.Contains(joined, "arg1") {
		t.Errorf("expected arg1 in args, got: %v", cmd.Args)
	}
}

func TestDynamoRIOBackendNoFilter(t *testing.T) {
	// Create a fake drrun binary
	tmp := t.TempDir()
	bin64 := filepath.Join(tmp, "bin64")
	if err := os.MkdirAll(bin64, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeDrrun := filepath.Join(bin64, "drrun")
	if err := os.WriteFile(fakeDrrun, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DYNAMORIO_HOME", tmp)
	t.Setenv("SHIMMY_SANDBOX_FILTER_SO", "")

	b := &DynamoRIOBackend{}
	cmd, err := b.WrapCmd(context.Background(), "/bin/echo", []string{"hi"}, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "-c") {
		t.Errorf("expected no -c flag when SHIMMY_SANDBOX_FILTER_SO is unset, got: %v", cmd.Args)
	}
	if !strings.Contains(joined, "/bin/echo") {
		t.Errorf("expected /bin/echo in args, got: %v", cmd.Args)
	}
}
