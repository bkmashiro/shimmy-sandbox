package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ExitKind classifies how a child process terminated.
type ExitKind int

const (
	ExitSuccess     ExitKind = iota // process exited 0
	ExitPassthrough                 // process exited 1-123 (raw code in Result.RawCode)
	ExitTimeout                     // killed by timeout → caller maps to 124
	ExitInternal                    // shimmy-sandbox internal failure → 125
	ExitBlocked                     // blocked/killed by sandbox limit → 126
)

// Result is the outcome of a sandbox run.
type Result struct {
	Kind    ExitKind
	RawCode int // valid when Kind == ExitPassthrough
}

// RunConfig describes what to run and which limits to enforce.
type RunConfig struct {
	Args       []string      // Args[0] is the executable path
	Timeout    time.Duration // 0 = no timeout
	MemLimit   int64         // RLIMIT_AS bytes, 0 = no limit
	ProcLimit  int64         // RLIMIT_NPROC count, 0 = no limit
	FSizeLimit int64         // RLIMIT_FSIZE bytes, 0 = no limit
	FDLimit    int64         // RLIMIT_NOFILE count, 0 = no limit
	OutLimit   int64         // combined stdout+stderr byte cap (bytes), 0 = no limit
	WorkDir    string        // working directory for child; "" = inherit
	DrrunPath  string        // path to drrun binary (DynamoRIO backend)
	DrTool     string        // DynamoRIO client .so path
}

// outputLimiter is a thread-safe writer that caps total bytes forwarded.
// Once the limit is reached it writes a truncation notice and discards further data.
type outputLimiter struct {
	mu       sync.Mutex
	dst      io.Writer
	limit    int64
	written  int64
	notified bool
}

func newOutputLimiter(dst io.Writer, limit int64) *outputLimiter {
	return &outputLimiter{dst: dst, limit: limit}
}

func (o *outputLimiter) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.limit <= 0 {
		n, err := o.dst.Write(p)
		return n, err
	}

	remaining := o.limit - o.written
	if remaining <= 0 {
		if !o.notified {
			o.notified = true
			fmt.Fprintf(o.dst, "\n[output truncated at %d bytes]\n", o.limit)
		}
		return len(p), nil
	}

	chunk := p
	if int64(len(chunk)) > remaining {
		chunk = p[:remaining]
	}
	n, err := o.dst.Write(chunk)
	o.written += int64(n)

	if o.written >= o.limit && !o.notified {
		o.notified = true
		fmt.Fprintf(o.dst, "\n[output truncated at %d bytes]\n", o.limit)
	}

	return len(p), err
}

// baseRun starts cmd (already configured with SysProcAttr etc.) and waits for
// it to finish, enforcing the timeout and output limit from cfg.
func baseRun(ctx context.Context, cmd *exec.Cmd, cfg RunConfig) Result {
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	stdoutLim := io.Writer(os.Stdout)
	stderrLim := io.Writer(os.Stderr)
	if cfg.OutLimit > 0 {
		lim := newOutputLimiter(os.Stdout, cfg.OutLimit)
		stdoutLim = lim
		// stderr shares the same counter so the combined cap is respected
		stderrLim = lim
		// stderr should also be visible; wrap a secondary writer that tees to
		// real stderr but counts against the shared limiter
		stderrLim = &stderrTee{shared: lim, raw: os.Stderr}
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = stdoutLim
	cmd.Stderr = stderrLim

	runCtx := ctx
	var cancel context.CancelFunc
	if cfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: failed to start process: %v\n", err)
		return Result{Kind: ExitInternal}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-runCtx.Done():
		// Timeout fired — kill the whole process group.
		killGroup(cmd)
		<-done
		return Result{Kind: ExitTimeout}

	case err := <-done:
		if err == nil {
			return Result{Kind: ExitSuccess}
		}
		return classifyExitError(err)
	}
}

// stderrTee writes to raw stderr but counts bytes against the shared outputLimiter.
type stderrTee struct {
	shared *outputLimiter
	raw    io.Writer
}

func (t *stderrTee) Write(p []byte) (int, error) {
	// Count against shared limit (writes to shared.dst which is stdout — we
	// don't want stderr going to stdout, so we track via the limiter but
	// write to raw stderr).
	t.shared.mu.Lock()
	remaining := int64(0)
	if t.shared.limit > 0 {
		remaining = t.shared.limit - t.shared.written
	}
	t.shared.mu.Unlock()

	if t.shared.limit > 0 && remaining <= 0 {
		// Update shared notified state and return.
		t.shared.mu.Lock()
		if !t.shared.notified {
			t.shared.notified = true
			fmt.Fprintf(t.raw, "\n[output truncated at %d bytes]\n", t.shared.limit)
		}
		t.shared.mu.Unlock()
		return len(p), nil
	}

	chunk := p
	if t.shared.limit > 0 && int64(len(chunk)) > remaining {
		chunk = p[:remaining]
	}

	n, err := t.raw.Write(chunk)

	t.shared.mu.Lock()
	t.shared.written += int64(n)
	if t.shared.limit > 0 && t.shared.written >= t.shared.limit && !t.shared.notified {
		t.shared.notified = true
		fmt.Fprintf(t.raw, "\n[output truncated at %d bytes]\n", t.shared.limit)
	}
	t.shared.mu.Unlock()

	return len(p), err
}

func classifyExitError(err error) Result {
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: wait error: %v\n", err)
		return Result{Kind: ExitInternal}
	}

	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return Result{Kind: ExitInternal}
	}

	if ws.Signaled() {
		sig := ws.Signal()
		switch sig {
		case syscall.SIGKILL, syscall.SIGXFSZ, syscall.SIGXCPU:
			return Result{Kind: ExitBlocked}
		default:
			return Result{Kind: ExitBlocked}
		}
	}

	code := ws.ExitStatus()
	switch {
	case code == 0:
		return Result{Kind: ExitSuccess}
	case code >= 1 && code <= 123:
		return Result{Kind: ExitPassthrough, RawCode: code}
	case code == 124:
		// Child itself exited 124 — pass through (ambiguous but child's fault).
		return Result{Kind: ExitPassthrough, RawCode: code}
	default:
		return Result{Kind: ExitPassthrough, RawCode: code}
	}
}
