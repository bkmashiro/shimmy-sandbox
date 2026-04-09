package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RunConfig describes what to run and which limits to enforce.
type RunConfig struct {
	Cmd      string
	Args     []string
	Timeout  time.Duration
	Config   Config
	OutLimit int64 // combined stdout+stderr byte cap in bytes, 0 = no limit
}

// RunResult is the outcome of a sandbox run.
type RunResult struct {
	Stdout        []byte
	Stderr        []byte
	ExitCode      int
	TimedOut      bool
	WallTimeMs    int64
	Blocked       bool
	BlockedReason string
}

// shimmy block prefix emitted by shimmy_filter.c
const blockPrefix = "[shimmy] BLOCKED:"

// parseBlocked scans stderr for block lines emitted by shimmy_filter.c.
func parseBlocked(stderr []byte) (blocked bool, reason string) {
	for _, line := range bytes.Split(stderr, []byte("\n")) {
		s := string(bytes.TrimSpace(line))
		if strings.HasPrefix(s, blockPrefix) {
			reason = strings.TrimSpace(s[len(blockPrefix):])
			blocked = true
			return
		}
	}
	return
}

// Run executes the sandboxed command using the given backend and returns the result.
func Run(ctx context.Context, b Backend, rcfg RunConfig) (RunResult, error) {
	runCtx := ctx
	var cancel context.CancelFunc
	if rcfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, rcfg.Timeout)
		defer cancel()
	}

	cmd, err := b.WrapCmd(runCtx, rcfg.Cmd, rcfg.Args, rcfg.Config)
	if err != nil {
		return RunResult{ExitCode: 125}, err
	}

	if rcfg.Config.WorkDir != "" {
		cmd.Dir = rcfg.Config.WorkDir
	}

	cmd.Stdin = os.Stdin

	var outBuf, errBuf syncBuf
	if rcfg.OutLimit > 0 {
		lim := newOutputLimiter(&outBuf, rcfg.OutLimit)
		cmd.Stdout = lim
		// stderr shares the same counter so the combined cap is respected
		cmd.Stderr = &stderrTee{shared: lim, raw: &errBuf}
	} else {
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
	}

	start := time.Now()

	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: 125}, fmt.Errorf("failed to start process: %w", err)
	}

	// Apply resource limits on the child process before it can exec further.
	ApplyRlimits(cmd.Process.Pid, rcfg.Config)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	timedOut := false

	select {
	case <-runCtx.Done():
		timedOut = true
		killGroup(cmd)
		<-done
	case waitErr = <-done:
	}

	wallMs := time.Since(start).Milliseconds()

	result := RunResult{
		Stdout:     outBuf.bytes(),
		Stderr:     errBuf.bytes(),
		TimedOut:   timedOut,
		WallTimeMs: wallMs,
	}

	// Check for shimmy filter blocks in stderr.
	result.Blocked, result.BlockedReason = parseBlocked(result.Stderr)

	if timedOut {
		result.ExitCode = 124
		return result, nil
	}

	if waitErr == nil {
		result.ExitCode = 0
		return result, nil
	}

	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return RunResult{ExitCode: 125, WallTimeMs: wallMs}, fmt.Errorf("wait: %w", waitErr)
	}

	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		result.ExitCode = 125
		return result, nil
	}

	if ws.Signaled() {
		result.ExitCode = 126
		return result, nil
	}

	result.ExitCode = ws.ExitStatus()
	return result, nil
}

// syncBuf is a goroutine-safe byte buffer.
type syncBuf struct {
	mu   sync.Mutex
	data []byte
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.data = append(b.data, p...)
	b.mu.Unlock()
	return len(p), nil
}

func (b *syncBuf) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.data))
	copy(out, b.data)
	return out
}

// outputLimiter is a thread-safe writer that caps total bytes written.
// Once the limit is reached it writes a truncation notice and discards further data.
type outputLimiter struct {
	mu       sync.Mutex
	dst      *syncBuf
	limit    int64
	written  int64
	notified bool
}

func newOutputLimiter(dst *syncBuf, limit int64) *outputLimiter {
	return &outputLimiter{dst: dst, limit: limit}
}

func (o *outputLimiter) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	remaining := o.limit - o.written
	if remaining <= 0 {
		if !o.notified {
			o.notified = true
			notice := fmt.Sprintf("\n[output truncated at %d bytes]\n", o.limit)
			o.dst.Write([]byte(notice)) //nolint:errcheck
		}
		return len(p), nil
	}

	chunk := p
	if int64(len(chunk)) > remaining {
		chunk = p[:remaining]
	}
	n, _ := o.dst.Write(chunk)
	o.written += int64(n)

	if o.written >= o.limit && !o.notified {
		o.notified = true
		notice := fmt.Sprintf("\n[output truncated at %d bytes]\n", o.limit)
		o.dst.Write([]byte(notice)) //nolint:errcheck
	}

	return len(p), nil
}

// stderrTee counts bytes against the shared outputLimiter but writes to raw stderr buf.
type stderrTee struct {
	shared *outputLimiter
	raw    *syncBuf
}

func (t *stderrTee) Write(p []byte) (int, error) {
	t.shared.mu.Lock()
	remaining := t.shared.limit - t.shared.written
	t.shared.mu.Unlock()

	if remaining <= 0 {
		t.shared.mu.Lock()
		if !t.shared.notified {
			t.shared.notified = true
			notice := fmt.Sprintf("\n[output truncated at %d bytes]\n", t.shared.limit)
			t.raw.Write([]byte(notice)) //nolint:errcheck
		}
		t.shared.mu.Unlock()
		return len(p), nil
	}

	chunk := p
	if int64(len(chunk)) > remaining {
		chunk = p[:remaining]
	}

	n, err := t.raw.Write(chunk)

	t.shared.mu.Lock()
	t.shared.written += int64(n)
	if t.shared.written >= t.shared.limit && !t.shared.notified {
		t.shared.notified = true
		notice := fmt.Sprintf("\n[output truncated at %d bytes]\n", t.shared.limit)
		t.raw.Write([]byte(notice)) //nolint:errcheck
	}
	t.shared.mu.Unlock()

	return len(p), err
}
