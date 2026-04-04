# shimmy-sandbox

A sandbox executor that wraps arbitrary executables with resource limits.

## Usage

```
shimmy-sandbox run [flags] -- <command> [args...]
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `10s` | Maximum wall-clock time for the child process |
| `--memory-mb` | `256` | Virtual address space limit in MiB (`RLIMIT_AS`) |
| `--max-procs` | `32` | Maximum number of processes (`RLIMIT_NPROC`) |
| `--max-fsize-mb` | `64` | Maximum file size in MiB (`RLIMIT_FSIZE`) |
| `--max-fds` | `64` | Maximum open file descriptors (`RLIMIT_NOFILE`) |
| `--output-limit-kb` | `64` | Maximum combined stdout+stderr in KiB |
| `--work-dir` | `` | Working directory for child process |
| `--backend` | `auto` | Backend: `auto`, `rlimits`, `dynamorio` |
| `--drrun` | `` | Path to `drrun` binary |
| `--dr-tool` | `` | DynamoRIO client `.so` path |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Child exited 0 |
| 1–123 | Child exit code (pass-through) |
| 124 | Timeout |
| 125 | Sandbox internal error |
| 126 | Blocked by sandbox limits |

## Backend Selection (`auto`)

1. If `--drrun` is set, or `DYNAMORIO_HOME` is set and `$DYNAMORIO_HOME/bin64/drrun` exists → DynamoRIO backend
2. If `SHIMMY_SANDBOX_FILTER_SO` is set → DynamoRIO backend
3. Otherwise → rlimits-only backend

## Environment Variables

| Variable | Description |
|----------|-------------|
| `DYNAMORIO_HOME` | Root of a DynamoRIO installation (`bin64/drrun` must exist inside) |
| `SHIMMY_SANDBOX_FILTER_SO` | Path to a DynamoRIO client `.so` used as `drrun -c <so>` |

## DynamoRIO Backend

Wraps the command as:

```
$DYNAMORIO_HOME/bin64/drrun [-c $SHIMMY_SANDBOX_FILTER_SO] -- <cmd> [args...]
```

Resource limits are applied to the `drrun` process and inherited by the child.

## Build

```sh
# Native host build (for local testing)
make build-host

# Static linux/amd64 binary (for production / Lambda)
make build

# Run tests
make test

# Create Lambda layer zip
make lambda-layer
```

The `lambda-layer.zip` contains `bin/shimmy-sandbox` and can be added as an AWS Lambda layer. The binary is placed at `/opt/bin/shimmy-sandbox` after extraction.

## Lambda Usage

1. Add the `shimmy-sandbox` layer to your Lambda function.
2. Optionally add a second layer with DynamoRIO at `/opt/dynamorio/` and your filter `.so`.
3. Set environment variables:
   ```
   DYNAMORIO_HOME=/opt/dynamorio
   SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so
   ```
4. Invoke: `/opt/bin/shimmy-sandbox run --timeout 5s -- /path/to/program`

## Platform Notes

- The rlimits backend (`RLIMIT_AS`, `RLIMIT_NPROC`, `RLIMIT_FSIZE`, `RLIMIT_NOFILE`) is **Linux-only**.
- On other platforms the tool compiles and runs but resource limits are not enforced.
- The binary uses a re-exec pattern to apply rlimits in the child before exec'ing the target program, avoiding interference with the Go runtime in the parent process.
