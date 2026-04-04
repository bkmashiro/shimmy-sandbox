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
	// Re-exec entry point: when this binary is used as the rlimit wrapper.
	if os.Getenv("_SHIMMY_SANDBOX_RLIMIT_CHILD") == "1" {
		if err := sandbox.RunRlimitChild(); err != nil {
			fmt.Fprintf(os.Stderr, "shimmy-sandbox: rlimit child: %v\n", err)
			os.Exit(125)
		}
		// RunRlimitChild calls syscall.Exec and never returns on success.
		os.Exit(125)
	}

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
  --output-limit-kb int    Max combined stdout+stderr in KiB (default 64)
  --work-dir string        Working directory for child process
  --no-network             Block network syscalls (requires seccomp/DynamoRIO)
  --backend string         Backend: auto, rlimits, dynamorio (default auto)
  --drrun string           Path to drrun binary
  --dr-tool string         DynamoRIO client .so path

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
	outputLimitKB := fs.Int("output-limit-kb", 64, "max combined stdout+stderr in KiB (0 = no limit)")
	workDir := fs.String("work-dir", "", "working directory for child process")
	_ = fs.Bool("no-network", false, "block network syscalls (requires seccomp/DynamoRIO)")
	backend := fs.String("backend", "auto", `backend: "auto", "rlimits", or "dynamorio"`)
	drrun := fs.String("drrun", "", "path to drrun binary")
	drTool := fs.String("dr-tool", "", "DynamoRIO client .so path")

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

	cfg := sandbox.RunConfig{
		Args:       cmdArgs,
		Timeout:    *timeout,
		MemLimit:   int64(*memoryMB) * 1024 * 1024,
		ProcLimit:  int64(*maxProcs),
		FSizeLimit: int64(*maxFsizeMB) * 1024 * 1024,
		FDLimit:    int64(*maxFDs),
		OutLimit:   int64(*outputLimitKB) * 1024,
		WorkDir:    *workDir,
		DrrunPath:  *drrun,
		DrTool:     *drTool,
	}

	bt := resolveBackend(*backend, cfg)

	b, err := sandbox.NewBackend(bt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox: %v\n", err)
		return 125
	}

	result, err := b.Run(context.Background(), cfg)
	if err != nil && result.Kind == sandbox.ExitInternal {
		return 125
	}

	return exitCodeFor(result)
}

// resolveBackend applies "auto" backend selection logic.
func resolveBackend(backend string, cfg sandbox.RunConfig) sandbox.BackendType {
	if backend != "auto" {
		return sandbox.BackendType(backend)
	}

	// Auto: prefer DynamoRIO if available.
	if cfg.DrrunPath != "" {
		return sandbox.BackendDynamoRIO
	}
	if home := os.Getenv("DYNAMORIO_HOME"); home != "" {
		return sandbox.BackendDynamoRIO
	}
	if cfg.DrTool == "" {
		if t := os.Getenv("SHIMMY_SANDBOX_FILTER_SO"); t != "" {
			return sandbox.BackendDynamoRIO
		}
	}
	return sandbox.BackendRlimits
}

// exitCodeFor maps a Result to a process exit code.
func exitCodeFor(r sandbox.Result) int {
	switch r.Kind {
	case sandbox.ExitSuccess:
		return 0
	case sandbox.ExitPassthrough:
		return r.RawCode
	case sandbox.ExitTimeout:
		return 124
	case sandbox.ExitInternal:
		return 125
	case sandbox.ExitBlocked:
		return 126
	default:
		return 125
	}
}
