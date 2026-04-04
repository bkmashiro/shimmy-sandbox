package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bkmashiro/shimmy-sandbox/internal/sandbox"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(125)
	}

	switch os.Args[1] {
	case "run":
		os.Exit(runCmd(os.Args[2:]))
	case "help", "--help", "-h":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: unknown subcommand %q\n", os.Args[1])
		printUsage()
		os.Exit(125)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: shimmy-sandbox run [flags] -- <command> [args...]

Subcommands:
  run    Execute a command inside the sandbox

Run flags:
  --timeout duration       Timeout (default 10s; 0 = no timeout)
  --memory-mb int          RLIMIT_AS in MiB (default 256)
  --max-procs int          RLIMIT_NPROC (default 32)
  --max-fsize-mb int       RLIMIT_FSIZE in MiB (default 64)
  --max-fds int            RLIMIT_NOFILE (default 64)
  --no-network             Block network (enforcement via DynamoRIO filter)
  --work-dir string        Working directory for child process
  --output-limit-kb int    Max combined stdout+stderr in KiB (default 64)
  --backend string         Backend: auto, rlimits, dynamorio (default auto)

Exit codes:
  0      Child exited 0
  1-123  Child exit code (pass-through)
  124    Timeout
  125    Sandbox internal error
  126    Blocked by sandbox limits
`)
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	timeout := fs.Duration("timeout", 10*time.Second, "timeout duration (0 = no timeout)")
	memoryMB := fs.Int("memory-mb", 256, "RLIMIT_AS in MiB")
	maxProcs := fs.Int("max-procs", 32, "RLIMIT_NPROC")
	maxFsizeMB := fs.Int("max-fsize-mb", 64, "RLIMIT_FSIZE in MiB")
	maxFDs := fs.Int("max-fds", 64, "RLIMIT_NOFILE")
	noNetwork := fs.Bool("no-network", false, "block network (requires DynamoRIO filter)")
	workDir := fs.String("work-dir", "", "working directory for child process")
	outputLimitKB := fs.Int("output-limit-kb", 64, "max combined stdout+stderr in KiB (0 = no limit)")
	backend := fs.String("backend", "auto", `backend: "auto", "rlimits", or "dynamorio"`)

	if err := fs.Parse(args); err != nil {
		return 125
	}

	cmdArgs := fs.Args()
	// Strip leading "--" separator if present.
	if len(cmdArgs) > 0 && cmdArgs[0] == "--" {
		cmdArgs = cmdArgs[1:]
	}
	if len(cmdArgs) == 0 {
		fmt.Fprintln(os.Stderr, "shimmy-sandbox: no command specified after --")
		fs.Usage()
		return 125
	}

	cfg := sandbox.Config{
		MemoryBytes:   uint64(*memoryMB) * 1024 * 1024,
		MaxProcs:      uint64(*maxProcs),
		MaxFsizeBytes: uint64(*maxFsizeMB) * 1024 * 1024,
		MaxFDs:        uint64(*maxFDs),
		WorkDir:       *workDir,
		NoNetwork:     *noNetwork,
	}

	rcfg := sandbox.RunConfig{
		Cmd:      cmdArgs[0],
		Args:     cmdArgs[1:],
		Timeout:  *timeout,
		Config:   cfg,
		OutLimit: int64(*outputLimitKB) * 1024,
	}

	b := selectBackend(*backend)

	result, err := sandbox.Run(context.Background(), b, rcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: %v\n", err)
	}

	// Write buffered output.
	if len(result.Stdout) > 0 {
		os.Stdout.Write(result.Stdout) //nolint:errcheck
	}
	if len(result.Stderr) > 0 {
		os.Stderr.Write(result.Stderr) //nolint:errcheck
	}

	if result.TimedOut {
		return 124
	}

	switch result.ExitCode {
	case 125:
		return 125
	case 126:
		return 126
	default:
		return result.ExitCode
	}
}

// selectBackend applies "auto" backend selection logic.
func selectBackend(backend string) sandbox.Backend {
	switch backend {
	case "dynamorio":
		return &sandbox.DynamoRIOBackend{}
	case "rlimits":
		return &sandbox.RlimitsBackend{}
	default: // "auto"
		if os.Getenv("DYNAMORIO_HOME") != "" || os.Getenv("SHIMMY_SANDBOX_FILTER_SO") != "" {
			return &sandbox.DynamoRIOBackend{}
		}
		return &sandbox.RlimitsBackend{}
	}
}
