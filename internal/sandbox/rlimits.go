//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const (
	// RLIMIT_NPROC is not exported by the syscall package on Linux.
	rlimitNPROC = 6

	envRlimitChild  = "_SHIMMY_SANDBOX_RLIMIT_CHILD"
	envMemLimit     = "_SHIMMY_SANDBOX_MEM_LIMIT"
	envProcLimit    = "_SHIMMY_SANDBOX_PROC_LIMIT"
	envFSizeLimit   = "_SHIMMY_SANDBOX_FSIZE_LIMIT"
	envFDLimit      = "_SHIMMY_SANDBOX_FD_LIMIT"
)

// RlimitsBackend applies Linux resource limits then execs the target.
type RlimitsBackend struct{}

func (b *RlimitsBackend) Run(ctx context.Context, cfg RunConfig) (Result, error) {
	cmd, err := buildRlimitsCmd(cfg)
	if err != nil {
		return Result{Kind: ExitInternal}, err
	}
	return baseRun(ctx, cmd, cfg), nil
}

// buildRlimitsCmd constructs an exec.Cmd that applies rlimits via re-exec.
func buildRlimitsCmd(cfg RunConfig) (*exec.Cmd, error) {
	if len(cfg.Args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	needsReexec := cfg.MemLimit > 0 || cfg.ProcLimit > 0 || cfg.FSizeLimit > 0 || cfg.FDLimit > 0

	var cmd *exec.Cmd
	if needsReexec {
		self, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("cannot determine own executable: %w", err)
		}

		// Build env with limit markers; re-exec child will read them.
		env := filterSandboxEnv(os.Environ())
		if cfg.MemLimit > 0 {
			env = append(env, fmt.Sprintf("%s=%d", envMemLimit, cfg.MemLimit))
		}
		if cfg.ProcLimit > 0 {
			env = append(env, fmt.Sprintf("%s=%d", envProcLimit, cfg.ProcLimit))
		}
		if cfg.FSizeLimit > 0 {
			env = append(env, fmt.Sprintf("%s=%d", envFSizeLimit, cfg.FSizeLimit))
		}
		if cfg.FDLimit > 0 {
			env = append(env, fmt.Sprintf("%s=%d", envFDLimit, cfg.FDLimit))
		}
		env = append(env, envRlimitChild+"=1")

		// argv: shimmy-sandbox <actual_cmd> [args...]
		argv := append([]string{"shimmy-sandbox"}, cfg.Args...)
		cmd = &exec.Cmd{
			Path: self,
			Args: argv,
			Env:  env,
		}
	} else {
		cmd = exec.Command(cfg.Args[0], cfg.Args[1:]...)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	return cmd, nil
}

// RunRlimitChild is called when the binary is re-exec'd as the rlimit wrapper.
// It reads limit env vars, applies them, then syscall.Exec's the real command.
func RunRlimitChild() error {
	applyLimit := func(envKey string, resource int) error {
		val := os.Getenv(envKey)
		if val == "" {
			return nil
		}
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return fmt.Errorf("parse %s=%q: %w", envKey, val, err)
		}
		lim := syscall.Rlimit{Cur: n, Max: n}
		if err := syscall.Setrlimit(resource, &lim); err != nil {
			return fmt.Errorf("setrlimit %s: %w", envKey, err)
		}
		return nil
	}

	if err := applyLimit(envMemLimit, syscall.RLIMIT_AS); err != nil {
		return err
	}
	if err := applyLimit(envProcLimit, rlimitNPROC); err != nil {
		return err
	}
	if err := applyLimit(envFSizeLimit, syscall.RLIMIT_FSIZE); err != nil {
		return err
	}
	if err := applyLimit(envFDLimit, syscall.RLIMIT_NOFILE); err != nil {
		return err
	}

	// os.Args[0] is the binary name; os.Args[1:] is the actual command.
	args := os.Args[1:]
	if len(args) == 0 {
		return fmt.Errorf("rlimit child: no command in args")
	}

	env := filterSandboxEnv(os.Environ())

	return syscall.Exec(args[0], args, env)
}

// filterSandboxEnv removes all _SHIMMY_SANDBOX_* variables from the environment.
func filterSandboxEnv(env []string) []string {
	out := env[:0:len(env)]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "_SHIMMY_SANDBOX_") {
			out = append(out, kv)
		}
	}
	return out
}
