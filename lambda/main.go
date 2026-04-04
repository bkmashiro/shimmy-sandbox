package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
)

const shimmyBin = "/opt/shimmy-sandbox/bin/shimmy-sandbox"

type Request struct {
	Language  string `json:"language"`   // "python3", "c", "shell"
	Code      string `json:"code"`
	Stdin     string `json:"stdin"`
	TimeoutMs int    `json:"timeout_ms"` // default 5000
}

type Response struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimeMs   int64  `json:"time_ms"`
	Sandbox  string `json:"sandbox"`
	Verdict  string `json:"verdict"` // OK, TLE, RE, SB
}

func handler(ctx context.Context, req Request) (Response, error) {
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = 5000
	}

	reqID := requestID(ctx)
	sandboxDir := filepath.Join("/tmp", "sandbox-"+reqID)
	if err := os.MkdirAll(sandboxDir, 0700); err != nil {
		return Response{}, fmt.Errorf("mkdir sandbox dir: %w", err)
	}
	defer os.RemoveAll(sandboxDir)

	runCmd, runArgs, err := prepareExecution(req, sandboxDir)
	if err != nil {
		return Response{Stderr: err.Error(), ExitCode: 125, Verdict: "RE", Sandbox: "none"}, nil
	}

	timeoutSec := float64(req.TimeoutMs) / 1000.0
	args := []string{
		"run",
		fmt.Sprintf("--timeout=%.3fs", timeoutSec),
		"--memory-mb=256",
		"--max-procs=32",
		"--no-network",
		fmt.Sprintf("--work-dir=%s", sandboxDir),
		"--",
		runCmd,
	}
	args = append(args, runArgs...)

	cmd := exec.CommandContext(ctx, shimmyBin, args...)
	cmd.Stdin = strings.NewReader(req.Stdin)
	cmd.Env = sandboxEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Response{}, fmt.Errorf("exec shimmy-sandbox: %w", runErr)
		}
	}

	return Response{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		TimeMs:   elapsed,
		Sandbox:  "dynamorio",
		Verdict:  exitCodeToVerdict(exitCode),
	}, nil
}

// prepareExecution writes the source file and returns the command + args to execute.
func prepareExecution(req Request, dir string) (string, []string, error) {
	switch req.Language {
	case "python3":
		src := filepath.Join(dir, "submission.py")
		if err := os.WriteFile(src, []byte(req.Code), 0600); err != nil {
			return "", nil, fmt.Errorf("write python source: %w", err)
		}
		return "python3", []string{src}, nil

	case "c":
		src := filepath.Join(dir, "submission.c")
		if err := os.WriteFile(src, []byte(req.Code), 0600); err != nil {
			return "", nil, fmt.Errorf("write c source: %w", err)
		}
		binary := filepath.Join(dir, "submission")
		cc := exec.Command("gcc", "-O2", "-o", binary, src)
		var ccErr bytes.Buffer
		cc.Stderr = &ccErr
		if err := cc.Run(); err != nil {
			return "", nil, fmt.Errorf("compilation failed: %s", ccErr.String())
		}
		return binary, nil, nil

	case "shell":
		src := filepath.Join(dir, "submission.sh")
		if err := os.WriteFile(src, []byte(req.Code), 0700); err != nil {
			return "", nil, fmt.Errorf("write shell script: %w", err)
		}
		return "/bin/sh", []string{src}, nil

	default:
		return "", nil, fmt.Errorf("unsupported language: %q", req.Language)
	}
}

func sandboxEnv() []string {
	env := os.Environ()
	return append(env,
		"DYNAMORIO_HOME=/opt/dynamorio",
		"SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so",
	)
}

func exitCodeToVerdict(code int) string {
	switch code {
	case 0:
		return "OK"
	case 124:
		return "TLE"
	case 126:
		return "SB"
	default:
		return "RE"
	}
}

func requestID(ctx context.Context) string {
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		return lc.AwsRequestID
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func main() {
	lambda.Start(handler)
}
