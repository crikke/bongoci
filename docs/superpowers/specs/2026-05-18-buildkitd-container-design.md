# Buildkitd Container Auto-Start Design

**Date:** 2026-05-18
**Status:** Approved

## Overview

When `--use-host-buildkit-daemon` is not passed, `ci run` automatically starts an ephemeral `moby/buildkit` container, uses it for the duration of the run, then stops and removes it on exit. This removes the requirement for users to have a buildkitd already running on their host.

## Mechanism

**Connection:** Unix socket via bind mount. A temp directory is created on the host and mounted into the container at `/run/buildkit`. Buildkitd writes its socket there by default. The host-side address becomes `unix:///tmp/ci-buildkitd-<rand>/buildkitd.sock`.

**Lifecycle:** Created fresh on every `ci run` invocation, torn down via `defer cleanup()` when `run()` returns (normal exit or error).

**Implementation:** Docker SDK (`github.com/docker/docker/client`), not `os/exec`. Gives structured error handling and avoids parsing CLI output.

## `startBuildkitd` Function

Signature: `func startBuildkitd(ctx context.Context) (host string, cleanup func(), err error)`

Steps:
1. `os.MkdirTemp("", "ci-buildkitd-*")` → e.g. `/tmp/ci-buildkitd-3827649`
2. `client.NewClientWithOpts(client.FromEnv)` — connects to the local Docker daemon
3. `cli.ContainerCreate` with:
   - Image: `moby/buildkit:latest`
   - `HostConfig.Privileged = true` (buildkitd requires it)
   - `HostConfig.Binds = ["/tmp/ci-buildkitd-<rand>:/run/buildkit"]`
   - `HostConfig.AutoRemove = true`
4. `cli.ContainerStart`
5. Readiness loop: retry `bkclient.New(ctx, host)` with short sleeps until the client connects or `ctx` is cancelled. This naturally waits for the socket file to appear and buildkitd to accept connections.
6. Return `host`, and a `cleanup` func that calls `cli.ContainerStop` then `os.RemoveAll(tmpDir)`.

## Wiring in `run()`

```go
host := os.Getenv("BUILDKIT_HOST")
if host == "" {
    host = "unix:///run/buildkit/buildkitd.sock"
}

if !*useHostBuildkitDaemon {
    var cleanup func()
    var err error
    host, cleanup, err = startBuildkitd(ctx)
    if err != nil {
        return fmt.Errorf("start buildkitd: %w", err)
    }
    defer cleanup()
}
```

`startBuildkitd` sets `host` directly, so the `BUILDKIT_HOST` env var / default socket is only used in the host-daemon path.

## New Dependency

`github.com/docker/docker` — specifically:
- `github.com/docker/docker/client`
- `github.com/docker/docker/api/types/container`

Added to `go.mod` and vendored.

## Error Handling

- Docker daemon not reachable → `NewClientWithOpts` returns an error; surfaced as `"start buildkitd: ..."` with exit 1
- Image not present locally → `ContainerCreate` fails with a clear error; caller sees it
- Readiness timeout → context cancellation causes the retry loop to exit with an error
- Cleanup failures (stop/remove) → logged at debug level, not returned (best-effort teardown)

## Out of Scope

- Pulling the image if not present (Docker will pull automatically on `ContainerCreate`)
- Reusing a running buildkitd container across invocations
- Docker-in-Docker support
