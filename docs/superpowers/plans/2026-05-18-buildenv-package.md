# buildenv Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract `startBuildkitd` and `waitForBuildkitd` from `cmd/ci/main.go` into a new `pkg/buildenv` package that owns the full Docker container + network lifecycle for the build environment.

**Architecture:** A single `Environment` struct is returned by `Start(ctx)` and exposes `BuildkitHost`; `Close()` tears down all resources in reverse order. `main.go` retains the `--use-host-buildkit-daemon` flag branch and calls `buildenv.Start` for the managed path. All Docker imports are removed from `main.go`.

**Tech Stack:** Go 1.25, `github.com/moby/moby/client` (Docker SDK), `github.com/moby/buildkit/client` (buildkitd readiness check), `log/slog`.

---

### Task 1: Write the failing integration test for `pkg/buildenv`

**Files:**
- Create: `pkg/buildenv/buildenv_test.go`

- [ ] **Step 1: Create the test file**

```go
//go:build integration

package buildenv_test

import (
	"context"
	"strings"
	"testing"
	"time"

	bkclient "github.com/moby/buildkit/client"

	"github.com/crikke/ci/pkg/buildenv"
)

func TestStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	env, err := buildenv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer env.Close()

	if !strings.HasPrefix(env.BuildkitHost, "unix://") {
		t.Fatalf("expected unix:// host, got %q", env.BuildkitHost)
	}

	c, err := bkclient.New(ctx, env.BuildkitHost)
	if err != nil {
		t.Fatalf("connect to buildkit: %v", err)
	}
	defer c.Close()

	if _, err := c.Info(ctx); err != nil {
		t.Fatalf("buildkit info: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails (package does not exist yet)**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
go test -tags integration ./pkg/buildenv/... 2>&1
```

Expected: compile error — `cannot find package "github.com/crikke/ci/pkg/buildenv"`

---

### Task 2: Implement `pkg/buildenv/buildenv.go`

**Files:**
- Create: `pkg/buildenv/buildenv.go`

- [ ] **Step 1: Create the package file**

```go
package buildenv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// Environment holds the Docker resources that form the managed build environment.
// Call Close when done to release all resources.
type Environment struct {
	BuildkitHost string

	dockerClient *dockerclient.Client
	networkID    string
	buildkitID   string
	tmpDir       string
}

// Start provisions a Docker network and a buildkitd container, waits for
// buildkitd to be ready, and returns the environment. If any step fails,
// all resources created so far are cleaned up before the error is returned.
func Start(ctx context.Context) (*Environment, error) {
	tmpDir, err := os.MkdirTemp("", "ci-buildkitd-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("docker client: %w", err)
	}

	networkName := "ci-build-" + filepath.Base(tmpDir)
	netResult, err := cli.NetworkCreate(ctx, networkName, dockerclient.NetworkCreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		cli.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create docker network: %w", err)
	}
	networkID := netResult.ID

	resp, err := cli.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &container.Config{
			Image: "moby/buildkit:latest",
			Cmd:   []string{"--group", fmt.Sprintf("%d", os.Getgid())},
		},
		HostConfig: &container.HostConfig{
			Privileged: true,
			Binds:      []string{tmpDir + ":/run/buildkit"},
			AutoRemove: true,
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkID: {},
			},
		},
	})
	if err != nil {
		_, _ = cli.NetworkRemove(context.Background(), networkID, dockerclient.NetworkRemoveOptions{})
		cli.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create buildkitd container: %w", err)
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, dockerclient.ContainerStartOptions{}); err != nil {
		_, _ = cli.ContainerRemove(context.Background(), resp.ID, dockerclient.ContainerRemoveOptions{Force: true})
		_, _ = cli.NetworkRemove(context.Background(), networkID, dockerclient.NetworkRemoveOptions{})
		cli.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("start buildkitd container: %w", err)
	}

	socketHost := "unix://" + tmpDir + "/buildkitd.sock"
	slog.Debug("waiting for buildkitd", "host", socketHost)

	if err := waitForBuildkitd(ctx, socketHost); err != nil {
		stopTimeout := 5
		_, _ = cli.ContainerStop(context.Background(), resp.ID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout})
		_, _ = cli.NetworkRemove(context.Background(), networkID, dockerclient.NetworkRemoveOptions{})
		cli.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("wait for buildkitd: %w", err)
	}
	slog.Debug("buildkitd ready", "host", socketHost)

	return &Environment{
		BuildkitHost: socketHost,
		dockerClient: cli,
		networkID:    networkID,
		buildkitID:   resp.ID,
		tmpDir:       tmpDir,
	}, nil
}

// Close stops the buildkitd container, removes the Docker network, closes
// the Docker client, and removes the socket tmpdir.
func (e *Environment) Close() {
	stopTimeout := 5
	if _, err := e.dockerClient.ContainerStop(context.Background(), e.buildkitID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout}); err != nil {
		slog.Debug("stop buildkitd container", "error", err)
	}
	if _, err := e.dockerClient.NetworkRemove(context.Background(), e.networkID, dockerclient.NetworkRemoveOptions{}); err != nil {
		slog.Debug("remove docker network", "error", err)
	}
	e.dockerClient.Close()
	os.RemoveAll(e.tmpDir)
}

func waitForBuildkitd(ctx context.Context, host string) error {
	c, err := bkclient.New(ctx, host)
	if err != nil {
		return err
	}
	defer c.Close()

	for {
		if _, err := c.Info(ctx); err == nil {
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

- [ ] **Step 2: Run the integration test to verify it passes**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
go test -tags integration -v ./pkg/buildenv/...
```

Expected: `PASS` — `TestStart` should start buildkitd, verify the unix socket prefix, connect, and call Info successfully.

- [ ] **Step 3: Verify the package compiles cleanly without the integration tag**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
go build ./pkg/buildenv/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
git add pkg/buildenv/buildenv.go pkg/buildenv/buildenv_test.go
git commit -m "feat: add pkg/buildenv with Environment, Start, and Close"
```

---

### Task 3: Update `cmd/ci/main.go` to use `buildenv.Start`

**Files:**
- Modify: `cmd/ci/main.go` — replace `startBuildkitd`/`waitForBuildkitd` callsite with `buildenv.Start`; remove both functions and Docker imports
- Modify: `cmd/ci/main_test.go` — remove `TestStartBuildkitd` (covered by `pkg/buildenv` test)

- [ ] **Step 1: Replace the buildkitd block in `run()` and remove old functions**

In `cmd/ci/main.go`, replace the entire `var host string` block (lines 67–83) with:

```go
var host string
if !*useHostBuildkitDaemon {
    startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
    defer startCancel()
    env, startErr := buildenv.Start(startCtx)
    if startErr != nil {
        return fmt.Errorf("start build environment: %w", startErr)
    }
    defer env.Close()
    host = env.BuildkitHost
} else {
    host = os.Getenv("BUILDKIT_HOST")
    if host == "" {
        host = "unix:///run/buildkit/buildkitd.sock"
    }
}
```

Then delete the `startBuildkitd` function (lines 140–205) and the `waitForBuildkitd` function (lines 207–225).

Update the import block to remove `bkclient` and Docker SDK imports and add `buildenv`:

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

    "github.com/crikke/ci/pkg/buildenv"
    "github.com/crikke/ci/pkg/compiler"
    "github.com/crikke/ci/pkg/manifest"
    "github.com/crikke/ci/pkg/runner"
)
```

Also remove the unused `app` struct (lines 29–31) if it has no other references.

- [ ] **Step 2: Remove `TestStartBuildkitd` from `cmd/ci/main_test.go`**

The entire contents of `cmd/ci/main_test.go` test `startBuildkitd`, which no longer exists in the `main` package. Delete the file:

```bash
rm /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci/cmd/ci/main_test.go
```

- [ ] **Step 3: Build to confirm no compile errors**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Run the full non-integration test suite**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
go test ./...
```

Expected: all tests pass (compiler, manifest, runner packages).

- [ ] **Step 5: Commit**

```bash
cd /home/elstoffo/dev/analytics/an-asp-algorithms/identiq/platform/ci
git add cmd/ci/main.go
git rm cmd/ci/main_test.go
git commit -m "refactor: use pkg/buildenv in main, remove startBuildkitd"
```
