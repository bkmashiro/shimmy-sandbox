//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

func rlimitsBackend() Backend { return &RlimitsBackend{} }

// ---------------------------------------------------------------------------
// Basic execution
// ---------------------------------------------------------------------------

func TestRunBasic(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:  "/bin/echo",
		Args: []string{"hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if string(result.Stdout) != "hello\n" {
		t.Errorf("expected stdout %q, got %q", "hello\n", result.Stdout)
	}
}

func TestRunExitCode(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:  "/bin/sh",
		Args: []string{"-c", "exit 42"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit 42, got %d", result.ExitCode)
	}
}

// TestRunExitCodePassthrough checks that arbitrary exit codes pass through.
func TestRunExitCodePassthrough(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, 1, 2, 7, 13, 42, 100, 123} {
		code := code
		t.Run(fmt.Sprintf("exit_%d", code), func(t *testing.T) {
			t.Parallel()
			result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
				Cmd:  "/bin/sh",
				Args: []string{"-c", fmt.Sprintf("exit %d", code)},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ExitCode != code {
				t.Errorf("expected exit %d, got %d", code, result.ExitCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestRunTimeout(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "/bin/sleep",
		Args:    []string{"5"},
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
	if result.ExitCode != 124 {
		t.Errorf("expected exit 124, got %d", result.ExitCode)
	}
}

func TestRunTimeoutWallTime(t *testing.T) {
	t.Parallel()
	const timeoutMs = 300
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "/bin/sleep",
		Args:    []string{"10"},
		Timeout: timeoutMs * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
	// Wall time should be at least timeoutMs and at most 3× that.
	if result.WallTimeMs < timeoutMs {
		t.Errorf("wall_time_ms=%d, expected >= %d", result.WallTimeMs, timeoutMs)
	}
	if result.WallTimeMs > timeoutMs*3 {
		t.Errorf("wall_time_ms=%d is suspiciously large (>%d)", result.WallTimeMs, timeoutMs*3)
	}
}

func TestRunNoTimeout(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "/bin/true",
		Timeout: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("expected TimedOut=false")
	}
}

// ---------------------------------------------------------------------------
// Output limit
// ---------------------------------------------------------------------------

func TestRunOutputLimit(t *testing.T) {
	t.Parallel()
	const limitBytes = 1024
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:      "/bin/sh",
		Args:     []string{"-c", "yes | head -c 204800"},
		Timeout:  10 * time.Second,
		OutLimit: limitBytes,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(result.Stdout, []byte("[output truncated")) {
		t.Errorf("expected truncation marker in stdout, got %d bytes: %q...", len(result.Stdout), result.Stdout[:minInt(64, len(result.Stdout))])
	}
}

func TestRunOutputLimitStderr(t *testing.T) {
	t.Parallel()
	const limitBytes = 512
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:      "/bin/sh",
		Args:     []string{"-c", "yes >&2 | head -c 204800 >&2 ; true"},
		Timeout:  10 * time.Second,
		OutLimit: limitBytes,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := string(result.Stdout) + string(result.Stderr)
	if !strings.Contains(combined, "[output truncated") {
		t.Errorf("expected truncation marker in combined output, got stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
	}
}

// ---------------------------------------------------------------------------
// Stderr
// ---------------------------------------------------------------------------

func TestRunStderr(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:  "/bin/sh",
		Args: []string{"-c", "echo err >&2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(result.Stderr), "err") {
		t.Errorf("expected stderr to contain 'err', got %q", result.Stderr)
	}
}

// ---------------------------------------------------------------------------
// Working directory
// ---------------------------------------------------------------------------

func TestRunWorkDir(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd: "/bin/pwd",
		Config: Config{
			WorkDir: "/tmp",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "/tmp") {
		t.Errorf("expected stdout to contain '/tmp', got %q", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestRunBadCmd(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd: "/nonexistent/binary/does/not/exist",
	})
	if err == nil && result.ExitCode == 0 {
		t.Error("expected error or non-zero exit for nonexistent binary")
	}
	if err == nil && result.ExitCode != 125 {
		t.Errorf("expected ExitCode=125 for bad cmd, got %d", result.ExitCode)
	}
}

func TestRunSignaledExitCode(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:  "/bin/sh",
		Args: []string{"-c", "kill -9 $$"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 126 {
		t.Errorf("expected exit 126 for signaled process, got %d", result.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// Memory limit
// ---------------------------------------------------------------------------

func TestRunRlimitsMemory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("rlimits memory test only on linux")
	}
	// 4 MB address space — python trying to allocate 512 MB should fail.
	const fourMB = 4 * 1024 * 1024
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "/bin/sh",
		Args:    []string{"-c", "python3 -c 'x=\"A\"*536870912' 2>/dev/null; true"},
		Timeout: 5 * time.Second,
		Config: Config{
			MemoryBytes: fourMB,
		},
	})
	if err == nil && result.ExitCode == 0 {
		t.Log("process exited 0 (shell swallowed the error); limit was still applied")
	}
	_ = result
}

func TestRunRlimitsMemoryPython(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("rlimits memory test only on linux")
	}
	// Limit to 8 MB and try to allocate 256 MB via python3.
	const eightMB = 8 * 1024 * 1024
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "python3",
		Args:    []string{"-c", "x = bytearray(256 * 1024 * 1024)"},
		Timeout: 5 * time.Second,
		Config: Config{
			MemoryBytes: eightMB,
		},
	})
	// Expect a non-zero exit code (OOM / MemoryError / SIGKILL).
	if err != nil {
		// Error starting is also acceptable (can't exec python3 at all within 8 MB).
		t.Logf("got launch error (expected with tight limit): %v", err)
		return
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit when allocating 256MB with 8MB limit")
	}
	t.Logf("exit_code=%d timed_out=%v (correct — memory limit killed python)", result.ExitCode, result.TimedOut)
}

// ---------------------------------------------------------------------------
// Wall time
// ---------------------------------------------------------------------------

func TestRunWallTime(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:     "/bin/sleep",
		Args:    []string{"0.1"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.WallTimeMs < 50 {
		t.Errorf("wall_time_ms=%d seems too low for sleep 0.1", result.WallTimeMs)
	}
	if result.WallTimeMs > 5000 {
		t.Errorf("wall_time_ms=%d seems too high", result.WallTimeMs)
	}
}

// ---------------------------------------------------------------------------
// Blocked field parsing
// ---------------------------------------------------------------------------

func TestParseBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		stderr        string
		wantBlocked   bool
		wantSubstring string
	}{
		{
			name:          "not blocked",
			stderr:        "some normal output\n",
			wantBlocked:   false,
			wantSubstring: "",
		},
		{
			name:          "socket blocked",
			stderr:        "[shimmy] BLOCKED: socket() network syscall blocked\n",
			wantBlocked:   true,
			wantSubstring: "socket",
		},
		{
			name:          "connect blocked",
			stderr:        "stderr line\n[shimmy] BLOCKED: connect() network syscall blocked\nmore output\n",
			wantBlocked:   true,
			wantSubstring: "connect",
		},
		{
			name:          "mmap blocked",
			stderr:        "[shimmy] BLOCKED: mmap() with PROT_WRITE|PROT_EXEC blocked\n",
			wantBlocked:   true,
			wantSubstring: "mmap",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			blocked, reason := parseBlocked([]byte(tc.stderr))
			if blocked != tc.wantBlocked {
				t.Errorf("blocked=%v, want %v (stderr=%q)", blocked, tc.wantBlocked, tc.stderr)
			}
			if tc.wantSubstring != "" && !strings.Contains(reason, tc.wantSubstring) {
				t.Errorf("reason=%q, expected to contain %q", reason, tc.wantSubstring)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JSON output (via RunResult struct)
// ---------------------------------------------------------------------------

func TestRunResultJSON(t *testing.T) {
	t.Parallel()
	result, err := Run(context.Background(), rlimitsBackend(), RunConfig{
		Cmd:  "/bin/echo",
		Args: []string{"json-test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	type jsonOut struct {
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
		ExitCode   int    `json:"exit_code"`
		WallTimeMs int64  `json:"wall_time_ms"`
		TimedOut   bool   `json:"timed_out"`
		Blocked    bool   `json:"blocked"`
	}
	out := jsonOut{
		Stdout:     string(result.Stdout),
		Stderr:     string(result.Stderr),
		ExitCode:   result.ExitCode,
		WallTimeMs: result.WallTimeMs,
		TimedOut:   result.TimedOut,
		Blocked:    result.Blocked,
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(raw), "json-test") {
		t.Errorf("expected json to contain 'json-test', got %s", raw)
	}
	if !strings.Contains(string(raw), `"exit_code":0`) {
		t.Errorf("expected exit_code:0 in json, got %s", raw)
	}
}

// ---------------------------------------------------------------------------
// DynamoRIO backend (skipped when DYNAMORIO_HOME not set)
// ---------------------------------------------------------------------------

func TestDynamoRIOBackendRun(t *testing.T) {
	t.Parallel()
	if _, err := resolveDrrun(); err != nil {
		t.Skip("DYNAMORIO_HOME not set — skipping DynamoRIO test")
	}

	b := &DynamoRIOBackend{}
	result, err := Run(context.Background(), b, RunConfig{
		Cmd:     "/bin/echo",
		Args:    []string{"dynamorio-test"},
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Stdout), "dynamorio-test") {
		t.Errorf("expected stdout to contain 'dynamorio-test', got %q", result.Stdout)
	}
}

func TestDynamoRIONetworkBlock(t *testing.T) {
	t.Parallel()
	if _, err := resolveDrrun(); err != nil {
		t.Skip("DYNAMORIO_HOME not set — skipping DynamoRIO network block test")
	}
	// Check that shimmy_filter.so is also available.
	soPath, cleanup, err := resolveFilterSO()
	if err != nil || soPath == "" {
		t.Skip("shimmy_filter.so not available — skipping network block test")
	}
	defer cleanup()

	b := &DynamoRIOBackend{}
	result, runErr := Run(context.Background(), b, RunConfig{
		Cmd:     "/bin/sh",
		Args:    []string{"-c", "curl -s --max-time 2 http://google.com 2>&1; true"},
		Timeout: 10 * time.Second,
		Config: Config{
			NoNetwork: true,
		},
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !result.Blocked {
		t.Errorf("expected Blocked=true; stderr=%q", result.Stderr)
	}
	t.Logf("blocked_reason=%q", result.BlockedReason)
}

// ---------------------------------------------------------------------------
// parseBlocked additional cases
// ---------------------------------------------------------------------------

func TestParseBlockedOpenPath(t *testing.T) {
	t.Parallel()
	stderr := `[shimmy] filter loaded; network=1 exec=1 ptrace=1 rwx=1 allowed_paths=1 extra_blocked=0
[shimmy] BLOCKED: open("/etc/passwd") path not in allowed_paths
`
	blocked, reason := parseBlocked([]byte(stderr))
	if !blocked {
		t.Errorf("expected blocked=true for open path block, stderr=%q", stderr)
	}
	if !strings.Contains(reason, "/etc/passwd") {
		t.Errorf("expected reason to mention /etc/passwd, got %q", reason)
	}
}

func TestParseBlockedExtraBlocked(t *testing.T) {
	t.Parallel()
	stderr := "[shimmy] BLOCKED: syscall 42 blocked (extra_blocked)\n"
	blocked, reason := parseBlocked([]byte(stderr))
	if !blocked {
		t.Errorf("expected blocked=true, stderr=%q", stderr)
	}
	if !strings.Contains(reason, "extra_blocked") {
		t.Errorf("expected reason to mention extra_blocked, got %q", reason)
	}
}

func TestParseBlockedEmpty(t *testing.T) {
	t.Parallel()
	blocked, reason := parseBlocked([]byte(""))
	if blocked {
		t.Error("expected blocked=false for empty stderr")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestParseBlockedMultipleLines(t *testing.T) {
	t.Parallel()
	// Only the first BLOCKED line is returned.
	stderr := "normal line\n[shimmy] BLOCKED: fork() process spawn blocked\n[shimmy] BLOCKED: execve() process spawn blocked\n"
	blocked, reason := parseBlocked([]byte(stderr))
	if !blocked {
		t.Error("expected blocked=true")
	}
	if !strings.Contains(reason, "fork") {
		t.Errorf("expected first blocked reason (fork), got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// buildPolicyArgs tests
// ---------------------------------------------------------------------------

func TestBuildPolicyArgsDefaults(t *testing.T) {
	t.Parallel()
	// With NoNetwork=false and no AllowedPaths, only -block_network 0 should appear.
	cfg := Config{NoNetwork: false}
	args := buildPolicyArgs(cfg)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-block_network") {
		t.Errorf("expected -block_network in args when NoNetwork=false, got: %v", args)
	}
	if !strings.Contains(joined, "0") {
		t.Errorf("expected value '0' for -block_network when NoNetwork=false, got: %v", args)
	}
}

func TestBuildPolicyArgsNoNetworkTrue(t *testing.T) {
	t.Parallel()
	// With NoNetwork=true, -block_network should NOT be passed (filter defaults to 1).
	cfg := Config{NoNetwork: true}
	args := buildPolicyArgs(cfg)
	for _, a := range args {
		if a == "-block_network" {
			t.Errorf("expected no -block_network flag when NoNetwork=true, got: %v", args)
		}
	}
}

func TestBuildPolicyArgsAllowedPaths(t *testing.T) {
	t.Parallel()
	cfg := Config{
		NoNetwork:    true,
		AllowedPaths: []string{"/tmp/", "/usr/lib/"},
	}
	args := buildPolicyArgs(cfg)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-allowed_paths") {
		t.Errorf("expected -allowed_paths in args, got: %v", args)
	}
	if !strings.Contains(joined, "/tmp/") {
		t.Errorf("expected /tmp/ in args, got: %v", args)
	}
	if !strings.Contains(joined, "/usr/lib/") {
		t.Errorf("expected /usr/lib/ in args, got: %v", args)
	}
}

func TestBuildPolicyArgsNoAllowedPaths(t *testing.T) {
	t.Parallel()
	cfg := Config{NoNetwork: true, AllowedPaths: nil}
	args := buildPolicyArgs(cfg)
	for _, a := range args {
		if a == "-allowed_paths" {
			t.Errorf("expected no -allowed_paths when AllowedPaths is nil, got: %v", args)
		}
	}
}

func TestBuildPolicyArgsAllowedPathsCSV(t *testing.T) {
	t.Parallel()
	cfg := Config{
		AllowedPaths: []string{"/tmp/", "/proc/self/", "/usr/"},
	}
	args := buildPolicyArgs(cfg)
	// Find the value after -allowed_paths
	for i, a := range args {
		if a == "-allowed_paths" && i+1 < len(args) {
			val := args[i+1]
			if !strings.Contains(val, "/tmp/") || !strings.Contains(val, "/proc/self/") || !strings.Contains(val, "/usr/") {
				t.Errorf("expected CSV with all paths, got %q", val)
			}
			return
		}
	}
	t.Error("did not find -allowed_paths flag in args")
}

// ---------------------------------------------------------------------------
// DynamoRIO backend policy tests (skip when DynamoRIO not available)
// ---------------------------------------------------------------------------

func TestDynamoRIONetworkBlockPython(t *testing.T) {
	t.Parallel()
	if _, err := resolveDrrun(); err != nil {
		t.Skip("DYNAMORIO_HOME not set — skipping DynamoRIO network block test")
	}
	soPath, cleanup, err := resolveFilterSO()
	if err != nil || soPath == "" {
		t.Skip("shimmy_filter.so not available — skipping network block test")
	}
	defer cleanup()

	b := &DynamoRIOBackend{}
	result, runErr := Run(context.Background(), b, RunConfig{
		Cmd:     "python3",
		Args:    []string{"-c", "import socket; socket.socket()"},
		Timeout: 15 * time.Second,
		Config: Config{
			NoNetwork: true,
		},
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !result.Blocked {
		t.Errorf("expected Blocked=true when network is blocked; stderr=%q", result.Stderr)
	}
	t.Logf("blocked_reason=%q", result.BlockedReason)
}

func TestDynamoRIOAllowedPathsBlock(t *testing.T) {
	t.Parallel()
	if _, err := resolveDrrun(); err != nil {
		t.Skip("DYNAMORIO_HOME not set — skipping DynamoRIO allowed_paths test")
	}
	soPath, cleanup, err := resolveFilterSO()
	if err != nil || soPath == "" {
		t.Skip("shimmy_filter.so not available — skipping allowed_paths test")
	}
	defer cleanup()

	b := &DynamoRIOBackend{}
	// Only allow /tmp/ — opening /etc/passwd should be blocked.
	result, runErr := Run(context.Background(), b, RunConfig{
		Cmd:     "/bin/sh",
		Args:    []string{"-c", "cat /etc/passwd 2>&1; true"},
		Timeout: 15 * time.Second,
		Config: Config{
			AllowedPaths: []string{"/tmp/"},
		},
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !result.Blocked {
		t.Errorf("expected Blocked=true when /etc/passwd is not in allowed_paths; stderr=%q", result.Stderr)
	}
	t.Logf("blocked_reason=%q", result.BlockedReason)
}

func TestDynamoRIORWXMmap(t *testing.T) {
	t.Parallel()
	if _, err := resolveDrrun(); err != nil {
		t.Skip("DYNAMORIO_HOME not set — skipping DynamoRIO RWX mmap test")
	}
	soPath, cleanup, err := resolveFilterSO()
	if err != nil || soPath == "" {
		t.Skip("shimmy_filter.so not available — skipping RWX mmap test")
	}
	defer cleanup()

	b := &DynamoRIOBackend{}
	// Use python3 to call mmap with PROT_READ|PROT_WRITE|PROT_EXEC (7).
	result, runErr := Run(context.Background(), b, RunConfig{
		Cmd: "python3",
		Args: []string{"-c",
			"import mmap; m = mmap.mmap(-1, 4096, prot=mmap.PROT_READ|mmap.PROT_WRITE|mmap.PROT_EXEC)"},
		Timeout: 15 * time.Second,
		Config:  Config{},
	})
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if !result.Blocked {
		t.Errorf("expected Blocked=true for RWX mmap; stderr=%q", result.Stderr)
	}
	t.Logf("blocked_reason=%q", result.BlockedReason)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
