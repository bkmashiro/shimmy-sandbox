# shimmy-sandbox

A standalone sandbox executor that wraps arbitrary executables with resource limits and optional DynamoRIO instrumentation. Designed for AWS Lambda but works anywhere Linux runs.

## What it does

`shimmy-sandbox` wraps a child process with:

- **Wall-clock timeout** — kill the process after a configurable duration
- **Memory limit** (`RLIMIT_AS`) — cap total virtual address space
- **Process limit** (`RLIMIT_NPROC`) — prevent fork bombs
- **File-size limit** (`RLIMIT_FSIZE`) — cap files the process can write
- **File-descriptor limit** (`RLIMIT_NOFILE`) — cap open file descriptors
- **Output limiting** — truncate combined stdout+stderr after a byte cap
- **DynamoRIO integration** — wrap with `drrun` for syscall filtering/network blocking

## Quick start

```bash
# Build (linux/amd64 static binary)
make build

# Run echo with defaults
./bin/shimmy-sandbox run -- echo hello

# Run with a 5-second timeout and 128 MiB memory cap
./bin/shimmy-sandbox run --timeout 5s --memory-mb 128 -- python3 script.py

# Run with DynamoRIO
DYNAMORIO_HOME=/opt/dynamorio \
SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so \
  ./bin/shimmy-sandbox run --timeout 10s -- ./untrusted-binary
```

## Usage

```
shimmy-sandbox run [flags] -- <command> [args...]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout duration` | `10s` | Wall-clock timeout (0 = no timeout) |
| `--memory-mb int` | `256` | `RLIMIT_AS` in MiB |
| `--max-procs int` | `32` | `RLIMIT_NPROC` |
| `--max-fsize-mb int` | `64` | `RLIMIT_FSIZE` in MiB |
| `--max-fds int` | `64` | `RLIMIT_NOFILE` |
| `--output-limit-kb int` | `64` | Combined stdout+stderr cap in KiB (0 = no limit) |
| `--work-dir string` | `""` | Working directory for child (empty = inherit) |
| `--no-network` | `false` | Block network syscalls (requires DynamoRIO backend) |
| `--backend string` | `auto` | `auto`, `rlimits`, or `dynamorio` |
| `--drrun string` | `""` | Path to `drrun` binary (overrides auto-detection) |
| `--dr-tool string` | `""` | DynamoRIO client `.so` path |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Child exited 0 |
| `1`–`123` | Child exit code (pass-through) |
| `124` | Timeout |
| `125` | Sandbox internal error |
| `126` | Blocked by sandbox limits (SIGKILL from rlimit, SIGXFSZ, etc.) |

## Environment variables

| Variable | Description |
|----------|-------------|
| `DYNAMORIO_HOME` | Root of a DynamoRIO installation; enables DynamoRIO backend in `auto` mode |
| `SHIMMY_SANDBOX_FILTER_SO` | Path to a DynamoRIO client `.so`; used when `DYNAMORIO_HOME` is set |

## Backend selection (`--backend auto`)

1. If `--drrun` is set → DynamoRIO backend
2. Else if `DYNAMORIO_HOME` is set and `$DYNAMORIO_HOME/bin64/drrun` exists → DynamoRIO backend
3. Else if `SHIMMY_SANDBOX_FILTER_SO` is set → DynamoRIO backend
4. Else → rlimits-only backend

## Backends

### `rlimits` (default fallback)

Applies Linux resource limits via `setrlimit(2)`, then execs the target directly. Uses a re-exec trick to apply limits inside the child process address space before the target binary runs (no CGO, no external dependencies).

### `dynamorio`

Wraps the command with `drrun`:

```
$DYNAMORIO_HOME/bin64/drrun [-c <filter.so>] -- <cmd> [args...]
```

Resource limits (rlimits) are also applied to the `drrun` process group, so they cascade to the instrumented child.

## Lambda deployment

### Step 1 — build the layer

```bash
make lambda-layer
# Produces lambda-layer.zip containing bin/shimmy-sandbox
```

### Step 2 — upload to AWS

```bash
aws lambda publish-layer-version \
  --layer-name shimmy-sandbox \
  --zip-file fileb://lambda-layer.zip \
  --compatible-runtimes provided.al2 provided.al2023
```

### Step 3 — add a second layer for DynamoRIO (optional)

Build a second zip with:
```
/opt/dynamorio/          ← full DynamoRIO distribution
/opt/sandbox/syscall_filter.so  ← your DynamoRIO client
```

Set Lambda environment variables:
```
DYNAMORIO_HOME=/opt/dynamorio
SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so
```

### Step 4 — invoke from your function

```go
// In your Lambda handler (Go example):
cmd := exec.Command("/opt/bin/shimmy-sandbox",
    "run",
    "--timeout", "10s",
    "--memory-mb", "256",
    "--",
    "/path/to/untrusted-binary", "arg1", "arg2",
)
```

Or via shell:
```bash
/opt/bin/shimmy-sandbox run --timeout 10s -- node index.js
```

## Integration with shimmy

Set the executor in your shimmy config:

```json
{
  "cmd": "shimmy-sandbox",
  "args": ["run", "--timeout", "10s", "--memory-mb", "256", "--", "originalCmd"]
}
```

## Notes

- Linux only for rlimits; the binary stubs out gracefully on non-Linux hosts for development.
- No external Go dependencies — stdlib only.
- The re-exec pattern is used to apply `setrlimit` in the child's address space without CGO or `golang.org/x/sys`.
