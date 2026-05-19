# Buildkitd Container Auto-Start Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `--use-host-buildkit-daemon` is not passed, `ci run` automatically starts an ephemeral `moby/buildkit` container, waits for it to be ready, and tears it down on exit.

**Architecture:** A temp dir is created on the host and bind-mounted into the container at `/run/buildkit`. Buildkitd writes its Unix socket there by default. The host connects at `unix:///tmp/ci-buildkitd-<rand>/buildkitd.sock`. A `cleanup` func stops the container and removes the temp dir; it is registered with `defer` in `run()`.

**Tech Stack:** `github.com/docker/docker/client` (Docker SDK), `github.com/docker/docker/api/types/container`, existing `github.com/moby/buildkit/client` for readiness polling.

---

### Task 1: Add Docker SDK dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `vendor/` (via `go mod vendor`)

- [ ] **Step 1: Add the dependency**

Run from the repo root (`identiq/platform/ci/`):

```bash
go get github.com/docker/docker/client
go mod tidy
```

- [ ] **Step 2: Vendor it**

```bash
go mod vendor
```

- [ ] **Step 3: Verify the build still compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum vendor/
git commit -m "chore: add github.com/docker/docker dependency"
```

---

### Task 2: Implement `startBuildkitd` (TDD)

**Files:**
- Create: `cmd/ci/main_test.go`
- Modify: `cmd/ci/main.go`

- [ ] **Step 1: Write the failing integration test**

Create `cmd/ci/main_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	bkclient "github.com/moby/buildkit/client"
)

func TestStartBuildkitd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	host, cleanup, err := startBuildkitd(ctx)
	if err != nil {
		t.Fatalf("startBuildkitd: %v", err)
	}
	defer cleanup()

	if !strings.HasPrefix(host, "unix://") {
		t.Fatalf("expected unix:// host, got %q", host)
	}

	c, err := bkclient.New(ctx, host)
	if err != nil {
		t.Fatalf("connect to buildkit: %v", err)
	}
	defer c.Close()

	if _, err := c.Info(ctx); err != nil {
		t.Fatalf("buildkit info: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test -tags integration -v ./cmd/ci/ -run TestStartBuildkitd
```

Expected: FAIL — `startBuildkitd` returns no host and no cleanup (it is an empty stub).

- [ ] **Step 3: Implement `startBuildkitd` and `waitForBuildkitd` in `main.go`**

Add the following imports to `cmd/ci/main.go` (merge with existing import block):

```go
import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/api/types/container"
	bkclient "github.com/moby/buildkit/client"
	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/runner"
)
```

Replace the empty `startBuildkitd` stub with:

```go
func startBuildkitd(ctx context.Context) (host string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "ci-buildkitd-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("docker client: %w", err)
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{Image: "moby/buildkit:latest"},
		&container.HostConfig{
			Privileged: true,
			Binds:      []string{tmpDir + ":/run/buildkit"},
			AutoRemove: true,
		},
		nil, nil, "",
	)
	if err != nil {
		cli.Close()
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("create buildkitd container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		cli.Close()
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("start buildkitd container: %w", err)
	}

	socketHost := "unix://" + tmpDir + "/buildkitd.sock"

	if err := waitForBuildkitd(ctx, socketHost); err != nil {
		stopTimeout := 5
		cli.ContainerStop(context.Background(), resp.ID, container.StopOptions{Timeout: &stopTimeout})
		cli.Close()
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("wait for buildkitd: %w", err)
	}

	cleanup = func() {
		stopTimeout := 5
		if err := cli.ContainerStop(context.Background(), resp.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			slog.Debug("stop buildkitd container", "error", err)
		}
		cli.Close()
		os.RemoveAll(tmpDir)
	}

	return socketHost, cleanup, nil
}

func waitForBuildkitd(ctx context.Context, host string) error {
	c, err := bkclient.New(ctx, host)
	if err != nil {
		return err
	}
	defer c.Close()

	for {
		_, err := c.Info(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test -tags integration -v ./cmd/ci/ -run TestStartBuildkitd
```

Expected: PASS. The test may take 10–30 seconds on first run if Docker pulls the `moby/buildkit:latest` image.

- [ ] **Step 5: Verify non-integration build is clean**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add cmd/ci/main.go cmd/ci/main_test.go
git commit -m "feat: implement startBuildkitd using Docker SDK with Unix socket"
```

---

### Task 3: Wire `startBuildkitd` into `run()`

**Files:**
- Modify: `cmd/ci/main.go:58-67`

- [ ] **Step 1: Replace the empty `if useHostBuildkitDaemon` block**

Current code in `run()` (around line 58):

```go
	// if this is disabled then create a container that runs buildkitd
	if useHostBuildkitDaemon {

	}

	host := os.Getenv("BUILDKIT_HOST")
	if host == "" {
		host = "unix:///run/buildkit/buildkitd.sock"
	}
```

Replace with:

```go
	host := os.Getenv("BUILDKIT_HOST")
	if host == "" {
		host = "unix:///run/buildkit/buildkitd.sock"
	}

	if !*useHostBuildkitDaemon {
		var cleanup func()
		var startErr error
		host, cleanup, startErr = startBuildkitd(ctx)
		if startErr != nil {
			return fmt.Errorf("start buildkitd: %w", startErr)
		}
		defer cleanup()
	}
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Run the integration test end-to-end**

```bash
go test -tags integration -v ./cmd/ci/ -run TestStartBuildkitd
```

Expected: PASS.

- [ ] **Step 4: Smoke-test the flag wiring manually**

Run `ci` without the flag (auto-starts buildkitd) against a real `build.toml`. If you don't have one handy, verify that the error message when no `build.toml` is found is unchanged:

```bash
go run ./cmd/ci/ run sometask 2>&1 || true
```

Expected: `error: build.toml not found (searched up from current directory)` — the buildkitd start only happens after a valid manifest is found, so this path does not start a container.

- [ ] **Step 5: Commit**

```bash
git add cmd/ci/main.go
git commit -m "feat: wire startBuildkitd into run() when --use-host-buildkit-daemon is not set"
```
