# shimmy-sandbox

A standalone sandbox executor for running untrusted student code, built for the [Lambda Feedback / Shimmy](https://lambdafeedback.com/) platform. Enforces resource limits (rlimits) and optional DynamoRIO-based syscall filtering.

**Linux only.** macOS/Windows are not supported.

---

## Quick Start

```sh
# 1. Install DynamoRIO (downloads ~150 MB, one-time)
shimmy-sandbox setup

# 2. Build the syscall filter (requires cmake + gcc)
export DYNAMORIO_HOME=~/.shimmy-sandbox/dynamorio
cd filter && bash build.sh && cp shimmy_filter.so ../internal/sandbox/

# 3. Run student code safely
shimmy-sandbox run -- python3 script.py

# 4. With full options
shimmy-sandbox run \
  --timeout 10s \
  --memory-mb 128 \
  --max-procs 16 \
  --output-limit-kb 64 \
  --no-network \
  --json \
  -- python3 solution.py
```

---

## Architecture

```
shimmy-sandbox CLI
       │
       ▼
  selectBackend()
  ┌─────────────┬──────────────────┐
  │ RlimitsBackend │ DynamoRIOBackend  │
  │  (default)    │                   │
  │               │  drrun             │
  │               │    -c filter.so   │
  │               │    -- <cmd>       │
  └─────────────┴──────────────────┘
         │
         ▼
    exec.Cmd (child process)
         │
         ├── RLIMIT_AS     (memory)
         ├── RLIMIT_NPROC  (processes)
         ├── RLIMIT_FSIZE  (file size)
         ├── RLIMIT_NOFILE (file descriptors)
         ├── RLIMIT_CORE=0 (no core dumps)
         │
         └── [DynamoRIO filter, if backend=dynamorio]
               ├── Block: socket / connect / bind / listen
               ├── Block: sendto / sendmsg / recvfrom / recvmsg
               ├── Block: fork / vfork / clone (non-thread) / execve / execveat
               ├── Block: ptrace / mount / chroot / pivot_root / unshare / setns
               ├── Block: mmap(PROT_WRITE|PROT_EXEC)
               └── Block: open/openat outside allowed paths
```

---

## Subcommands

### `run`

Execute a command inside the sandbox.

```
shimmy-sandbox run [flags] -- <command> [args...]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `10s` | Maximum wall-clock time (0 = no timeout) |
| `--memory-mb` | `256` | Virtual address space limit in MiB (`RLIMIT_AS`) |
| `--max-procs` | `32` | Maximum number of processes (`RLIMIT_NPROC`) |
| `--max-fsize-mb` | `64` | Maximum file size in MiB (`RLIMIT_FSIZE`) |
| `--max-fds` | `64` | Maximum open file descriptors (`RLIMIT_NOFILE`) |
| `--no-network` | `false` | Block network (requires DynamoRIO filter) |
| `--work-dir` | `` | Working directory for child process |
| `--output-limit-kb` | `64` | Max combined stdout+stderr in KiB (0 = no limit) |
| `--backend` | `auto` | Backend: `auto`, `rlimits`, `dynamorio` |
| `--json` | `false` | Emit result as JSON to stdout |

#### JSON output format (`--json`)

```json
{
  "stdout": "hello\n",
  "stderr": "",
  "exit_code": 0,
  "wall_time_ms": 12,
  "timed_out": false,
  "blocked": false,
  "blocked_reason": ""
}
```

### `setup`

Download and install DynamoRIO 10.0.0 to `~/.shimmy-sandbox/dynamorio/`.

```
shimmy-sandbox setup
```

After setup, build the syscall filter (requires `cmake` and `gcc`):

```sh
export DYNAMORIO_HOME=~/.shimmy-sandbox/dynamorio
cd filter && bash build.sh
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0–123 | Child exit code (pass-through) |
| 124 | Timeout (SIGKILL) |
| 125 | Sandbox internal error |
| 126 | Child killed by signal |

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `DYNAMORIO_HOME` | Root of a DynamoRIO installation (`bin64/drrun` must exist inside) |
| `SHIMMY_SANDBOX_FILTER_SO` | Path to a DynamoRIO client `.so` (overrides embedded or auto-resolved) |

The `auto` backend selects DynamoRIO if `DYNAMORIO_HOME` or `SHIMMY_SANDBOX_FILTER_SO` is set; otherwise falls back to rlimits.

---

## Build

### Go binary

```sh
# Normal build (embeds shimmy_filter.so — requires it to exist)
make build

# CI / no-.so build
go build -tags no_embed -o bin/shimmy-sandbox ./cmd/shimmy-sandbox

# Lambda (static linux/amd64)
make lambda-layer
```

### shimmy_filter.so (DynamoRIO client)

```sh
# Requires: cmake, gcc, and DynamoRIO installed
export DYNAMORIO_HOME=~/.shimmy-sandbox/dynamorio
cd filter && bash build.sh
```

The built `.so` is copied to `filter/shimmy_filter.so`. Copy it to `internal/sandbox/shimmy_filter.so` before building without `-tags no_embed`.

> **Note:** The repository ships a minimal placeholder `internal/sandbox/shimmy_filter.so` (valid ELF, no symbols) so the `//go:embed` directive compiles without a real DynamoRIO build. Run `filter/build.sh` to replace it with the real filter.

---

## Tests

```sh
# Fast tests (no DynamoRIO required)
go test -tags no_embed -v ./...

# Full tests (requires DYNAMORIO_HOME + built filter.so)
go test -v ./...
```

DynamoRIO-specific tests automatically skip when `DYNAMORIO_HOME` is not set.

---

## Lambda Deploy

1. Run `make lambda-layer` to produce `lambda-layer.zip`.
2. Upload as an AWS Lambda layer.
3. Attach the layer (binary lands at `/opt/bin/shimmy-sandbox`).
4. Optionally add a second layer with DynamoRIO at `/opt/dynamorio/` and filter `.so`.
5. Set environment variables:
   ```
   DYNAMORIO_HOME=/opt/dynamorio
   SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/shimmy_filter.so
   ```

---

## Shimmy Integration Example

```go
cmd := exec.Command(
    "/opt/bin/shimmy-sandbox", "run",
    "--timeout", "10s",
    "--memory-mb", "256",
    "--max-procs", "32",
    "--output-limit-kb", "64",
    "--json",
    "--",
    userBinary,
)
var stdout bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = os.Stderr
if err := cmd.Run(); err != nil {
    // check cmd.ProcessState.ExitCode() for 124/125/126
}
// Parse stdout as JSON
```

---

## Notes

- Linux only. The binary compiles on other platforms but rlimits are not enforced.
- Network blocking (`--no-network`) requires a DynamoRIO filter `.so`; the flag alone has no effect with the rlimits backend.
- Core dumps are always disabled in the child (`RLIMIT_CORE=0`).
- `--json` mode always emits JSON to stdout and returns the child's exit code directly.
