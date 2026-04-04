package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "shimmy-sandbox-test-*")
	if err != nil {
		panic("failed to create temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "shimmy-sandbox")
	out, err := exec.Command("go", "build", "-o", binaryPath, "../../cmd/shimmy-sandbox").CombinedOutput()
	if err != nil {
		panic("failed to build binary: " + err.Error() + "\n" + string(out))
	}

	os.Exit(m.Run())
}

func runBinary(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command(binaryPath, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestCLIRunEcho(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runBinary("run", "--", "/bin/echo", "hi")
	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d", exitCode)
	}
	if stdout != "hi\n" {
		t.Errorf("expected stdout %q, got %q", "hi\n", stdout)
	}
}

func TestCLIRunTimeout(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runBinary("run", "--timeout", "100ms", "--", "/bin/sleep", "10")
	if exitCode != 124 {
		t.Errorf("expected exit 124 for timeout, got %d", exitCode)
	}
}

func TestCLIRunExitCode(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runBinary("run", "--", "/bin/sh", "-c", "exit 7")
	if exitCode != 7 {
		t.Errorf("expected exit 7, got %d", exitCode)
	}
}

func TestCLIRunOutputLimit(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runBinary("run", "--output-limit-kb", "1", "--", "/bin/sh", "-c", "yes | head -c 200000")
	if !strings.Contains(stdout, "[output truncated") {
		t.Errorf("expected truncation marker in stdout, got %q...", stdout[:min(64, len(stdout))])
	}
}

func TestCLIRunNoCmd(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runBinary("run")
	if exitCode != 125 {
		t.Errorf("expected exit 125 for missing command, got %d", exitCode)
	}
}

func TestCLIHelp(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runBinary("help")
	if exitCode != 0 {
		t.Errorf("expected exit 0 for help, got %d", exitCode)
	}
	combined := stderr
	if !strings.Contains(strings.ToLower(combined), "usage") {
		t.Errorf("expected 'Usage' in help output, got: %q", combined)
	}
}

func TestCLIUnknownSubcmd(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runBinary("foobar")
	if exitCode != 125 {
		t.Errorf("expected exit 125 for unknown subcommand, got %d", exitCode)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
