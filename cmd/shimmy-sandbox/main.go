package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bkmashiro/shimmy-sandbox/internal/sandbox"
)

const dynamoRIOVersion = "10.0.0"
const dynamoRIOURL = "https://github.com/DynamoRIO/dynamorio/releases/download/release_10.0.0/DynamoRIO-Linux-10.0.0.tar.gz"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(125)
	}

	switch os.Args[1] {
	case "run":
		os.Exit(runCmd(os.Args[2:]))
	case "setup":
		os.Exit(setupCmd(os.Args[2:]))
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
	fmt.Fprintf(os.Stderr, `Usage: shimmy-sandbox <subcommand> [flags] [-- <command> [args...]]

Subcommands:
  run    Execute a command inside the sandbox
  setup  Download and install DynamoRIO to ~/.shimmy-sandbox/dynamorio/

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
  --json                   Emit result as JSON instead of raw stdout/stderr

Exit codes:
  0      Child exited 0
  1-123  Child exit code (pass-through)
  124    Timeout
  125    Sandbox internal error
  126    Blocked by sandbox limits
`)
}

// jsonOutput is the --json output format.
type jsonOutput struct {
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	ExitCode      int    `json:"exit_code"`
	WallTimeMs    int64  `json:"wall_time_ms"`
	TimedOut      bool   `json:"timed_out"`
	Blocked       bool   `json:"blocked"`
	BlockedReason string `json:"blocked_reason"`
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
	jsonMode := fs.Bool("json", false, "emit result as JSON")

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

	if *jsonMode {
		out := jsonOutput{
			Stdout:        string(result.Stdout),
			Stderr:        string(result.Stderr),
			ExitCode:      result.ExitCode,
			WallTimeMs:    result.WallTimeMs,
			TimedOut:      result.TimedOut,
			Blocked:       result.Blocked,
			BlockedReason: result.BlockedReason,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "shimmy-sandbox: json encode: %v\n", err)
			return 125
		}
		// In JSON mode always return the child exit code directly.
		return result.ExitCode
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

// setupCmd downloads DynamoRIO and extracts it to ~/.shimmy-sandbox/dynamorio/.
func setupCmd(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 125
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: cannot determine home dir: %v\n", err)
		return 125
	}

	installDir := filepath.Join(homeDir, ".shimmy-sandbox", "dynamorio")

	fmt.Printf("shimmy-sandbox setup: downloading DynamoRIO %s\n", dynamoRIOVersion)
	fmt.Printf("  URL: %s\n", dynamoRIOURL)
	fmt.Printf("  Install dir: %s\n", installDir)

	// Download tarball.
	resp, err := http.Get(dynamoRIOURL) //nolint:gosec // URL is a compile-time constant
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: download failed: %v\n", err)
		return 125
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: unexpected HTTP status %s\n", resp.Status)
		return 125
	}

	// Extract into a temp dir first, then move to final location.
	tmpDir, err := os.MkdirTemp("", "shimmy-setup-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: temp dir: %v\n", err)
		return 125
	}
	defer os.RemoveAll(tmpDir)

	fmt.Println("  Extracting...")
	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: extract: %v\n", err)
		return 125
	}

	// The tarball extracts to a subdirectory like DynamoRIO-Linux-10.0.0/.
	entries, err := os.ReadDir(tmpDir)
	if err != nil || len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: empty extract dir\n")
		return 125
	}

	extractedName := entries[0].Name()
	src := filepath.Join(tmpDir, extractedName)

	// Remove existing install if present.
	_ = os.RemoveAll(installDir)
	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: mkdir: %v\n", err)
		return 125
	}
	if err := os.Rename(src, installDir); err != nil {
		fmt.Fprintf(os.Stderr, "shimmy-sandbox setup: rename: %v\n", err)
		return 125
	}

	fmt.Printf("\nDynamoRIO installed to: %s\n", installDir)
	fmt.Printf("\nExport the following (add to your shell profile):\n")
	fmt.Printf("  export DYNAMORIO_HOME=%s\n", installDir)
	fmt.Printf("\nThen build the syscall filter:\n")
	fmt.Printf("  cd filter && DYNAMORIO_HOME=%s bash build.sh\n", installDir)
	fmt.Printf("\nQuick start:\n")
	fmt.Printf("  shimmy-sandbox run -- python3 your_script.py\n")
	return 0
}

// extractTarGz extracts a .tar.gz stream into destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		// Sanitise path — prevent directory traversal.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") {
			continue
		}
		target := filepath.Join(destDir, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			// Relative symlinks only.
			if filepath.IsAbs(hdr.Linkname) || strings.HasPrefix(hdr.Linkname, "..") {
				continue
			}
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}
