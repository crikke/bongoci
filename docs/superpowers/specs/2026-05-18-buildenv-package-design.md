---
name: buildenv-package
description: Extract buildkitd startup into pkg/buildenv with an Environment struct that owns container + network lifecycle, extensible to mirror registries
metadata:
  type: project
---

# Design: `pkg/buildenv` — Build Environment Package

## Context

`startBuildkitd` and `waitForBuildkitd` currently live in `cmd/ci/main.go`. They are the first of several Docker containers the CLI will need to manage (mirror registries are planned). Extracting them now establishes a package with a clean lifecycle boundary before that growth happens.

## Goal

Move all Docker container and network management into `pkg/buildenv`, exposing a single `Environment` struct that `main.go` starts and defers-closes.

## Public API

```go
package buildenv

type Environment struct {
    BuildkitHost string
    // RegistryHost string  // added when mirror registries land
}

func Start(ctx context.Context) (*Environment, error)
func (e *Environment) Close()
```

`Start` provisions the full environment. `Close` tears it all down. No other functions are exported.

## Internal Structure

```go
type Environment struct {
    BuildkitHost string

    dockerClient  *dockerclient.Client
    networkID     string
    buildkitID    string
    tmpDir        string
}
```

All Docker-specific types are unexported fields. `main.go` has zero Docker imports after this change.

## Behaviour

### `Start(ctx)`

1. Create Docker client (`FromEnv` + `WithAPIVersionNegotiation`).
2. Create a per-run bridge network named `ci-build-<random-suffix>`.
3. Create and start the buildkitd container, attached to that network, with the socket tmpdir bind-mount.
4. Call the unexported `waitForBuildkitd` loop (100 ms poll, respects ctx deadline).
5. Return `*Environment` with `BuildkitHost` set to `unix://<tmpDir>/buildkitd.sock`.

If any step fails, all resources created so far are cleaned up before returning the error.

### `Close()`

1. Stop buildkitd container (5 s timeout).
2. Remove the Docker network.
3. Close the Docker client.
4. Remove the tmpDir.

Order matters: container must stop before network is removed.

### Network

A dedicated bridge network per run isolates containers. Future registry mirrors join the same network by name, avoiding host port conflicts.

## Callsite in `main.go`

Before:
```go
host, cleanup, startErr = startBuildkitd(startCtx)
if startErr != nil { return fmt.Errorf("start buildkitd: %w", startErr) }
defer cleanup()
```

After:
```go
env, err := buildenv.Start(startCtx)
if err != nil { return fmt.Errorf("start build environment: %w", err) }
defer env.Close()
host = env.BuildkitHost
```

The `--use-host-buildkit-daemon` branch remains in `main.go` unchanged — it never calls `buildenv`.

## Files Changed

| File | Change |
|------|--------|
| `pkg/buildenv/buildenv.go` | New file — entire package |
| `cmd/ci/main.go` | Remove `startBuildkitd`, `waitForBuildkitd`; add `buildenv.Start` callsite; remove Docker imports |

## Out of Scope

- Mirror registry containers (follow-on, adds fields to `Environment` and steps to `Start`/`Close`)
- Configuring which services start (YAGNI — `Start` always provisions the full environment)
