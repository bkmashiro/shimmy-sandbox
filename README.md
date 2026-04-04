# shimmy-sandbox

A standalone sandbox executor for running untrusted commands with resource limits (rlimits) and optional DynamoRIO-based syscall filtering.

## Usage

```
shimmy-sandbox run [flags] -- <command> [args...]
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | `10s` | Maximum wall-clock time |
| `--memory-mb` | `256` | Virtual address space limit in MiB (`RLIMIT_AS`) |
| `--max-procs` | `32` | Maximum number of processes (`RLIMIT_NPROC`) |
| `--max-fsize-mb` | `64` | Maximum file size in MiB (`RLIMIT_FSIZE`) |
| `--max-fds` | `64` | Maximum open file descriptors (`RLIMIT_NOFILE`) |
| `--no-network` | `false` | Block network (enforcement via DynamoRIO filter) |
| `--work-dir` | `` | Working directory for child process |
| `--output-limit-kb` | `64` | Max combined stdout+stderr in KiB |
| `--backend` | `auto` | Backend: `auto`, `rlimits`, `dynamorio` |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0–123 | Child exit code (pass-through) |
| 124 | Timeout (SIGKILL) |
| 125 | Sandbox internal error |
| 126 | Blocked by sandbox |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `DYNAMORIO_HOME` | Root of a DynamoRIO installation (`bin64/drrun` must exist inside) |
| `SHIMMY_SANDBOX_FILTER_SO` | Path to a DynamoRIO client `.so` used as `drrun -c <so>` |

The `auto` backend selects DynamoRIO if `DYNAMORIO_HOME` or `SHIMMY_SANDBOX_FILTER_SO` is set; otherwise falls back to rlimits.

## Build

```sh
make build           # host binary → bin/shimmy-sandbox
make test            # run tests
make lambda-layer    # cross-compile linux/amd64 and zip → lambda-layer.zip
```

## Lambda Deploy

1. Run `make lambda-layer` to produce `lambda-layer.zip`.
2. Upload `lambda-layer.zip` as an AWS Lambda layer.
3. Attach the layer to your function (binary lands at `/opt/bin/shimmy-sandbox`).
4. Optionally add a second layer with DynamoRIO at `/opt/dynamorio/` and your filter `.so`.
5. Set environment variables on the function:
   ```
   DYNAMORIO_HOME=/opt/dynamorio
   SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so
   ```
6. Invoke the sandbox from your handler:
   ```sh
   /opt/bin/shimmy-sandbox run --timeout 5s --memory-mb 128 -- /path/to/program arg1
   ```

## Shimmy Integration Example

```go
cmd := exec.Command(
    "/opt/bin/shimmy-sandbox", "run",
    "--timeout", "10s",
    "--memory-mb", "256",
    "--max-procs", "32",
    "--output-limit-kb", "64",
    "--",
    userBinary,
)
cmd.Stdout = &stdout
cmd.Stderr = &stderr
if err := cmd.Run(); err != nil {
    // check cmd.ProcessState.ExitCode() for 124/125/126
}
```

## Notes

- The rlimits backend is Linux-only; the binary compiles on other platforms but limits are not enforced.
- Network blocking (`--no-network`) requires a DynamoRIO filter `.so`; the flag alone has no effect with the rlimits backend.
- Core dumps are always disabled in the child (`RLIMIT_CORE=0`).
