# Rootless BuildKit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `buildah` + `security.insecure` with `buildctl-daemonless.sh` inside `moby/buildkit:rootless`, eliminating the `sudo`/`--privileged` requirement for `DOCKERFILE` tasks.

**Architecture:** The outer BuildKit daemon container switches from privileged to rootless mode (security opts in `HostConfig`, correct socket bind path). The `DOCKERFILE` compiler step replaces `buildah` with an ephemeral nested `buildkitd` via `buildctl-daemonless.sh`, dropping `llb.Security(pb.SecurityMode_INSECURE)`.

**Tech Stack:** Go, `github.com/moby/moby/api/types/container`, `github.com/moby/buildkit/client/llb`, `github.com/moby/buildkit/solver/pb` (test only after this change).

---

## File Map

| File | Change |
|---|---|
| `pkg/buildenv/buildenv.go` | Fix malformed `ContainerCreate` — move security opts to `HostConfig.SecurityOpt`, fix bind path and `--addr` value, remove `strconv` import |
| `pkg/compiler/compiler.go` | Rename constant + function, replace buildah exec with `buildctl-daemonless.sh`, drop `pb` import |
| `pkg/compiler/compiler_test.go` | Add `TestCompile_docker_task_uses_rootless_buildkit` asserting image, args, env, and no `INSECURE` |
| `wiki/Getting-Started.md` | Remove privileged-container prerequisite, add docker-group note |

---

## Task 1: Fix `pkg/buildenv/buildenv.go`

**Files:**
- Modify: `pkg/buildenv/buildenv.go:3-14` (imports), `pkg/buildenv/buildenv.go:72-88` (ContainerCreate)

The current branch has a malformed `ContainerCreate` call: security opts are in `Cmd` (the container's command arguments to `buildkitd`) instead of `HostConfig.SecurityOpt`, `--addr` has no value, and the bind path is wrong. Fix all three.

- [ ] **Step 1: Remove `strconv` from imports**

Open `pkg/buildenv/buildenv.go`. The import block currently is:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)
```

Replace with (remove `"strconv"`):

```go
import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)
```

- [ ] **Step 2: Fix `ContainerCreate` call**

Replace the entire `ContainerCreate` call (lines 72–88) with:

```go
resp, err := cli.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
	Config: &container.Config{
		Image: bulder_image,
		Cmd:   []string{"--addr", "unix:///run/user/1000/buildkit/buildkitd.sock"},
	},
	HostConfig: &container.HostConfig{
		Binds:       []string{tmpDir + ":/run/user/1000/buildkit"},
		SecurityOpt: []string{"seccomp=unconfined", "apparmor=unconfined", "systempaths=unconfined"},
		ExtraHosts:  []string{"host.docker.internal:host-gateway"},
	},
})
```

Key changes:
- `Cmd`: was `["--group", <gid>, "--security-opt", ..., "--addr"]` (broken) → now `["--addr", "unix:///run/user/1000/buildkit/buildkitd.sock"]`
- `Binds`: was `tmpDir + ":/run/buildkit"` → `tmpDir + ":/run/user/1000/buildkit"` (matches UID 1000 baked into rootless image)
- `SecurityOpt` added to `HostConfig` (these are Docker host flags, not buildkitd args)

- [ ] **Step 3: Verify it compiles**

```bash
go build ./pkg/buildenv/...
```

Expected: no output (clean build). If you see `"strconv" imported and not used`, re-check Step 1.

- [ ] **Step 4: Commit**

```bash
git add pkg/buildenv/buildenv.go
git commit -m "fix(buildenv): correct rootless buildkitd container config"
```

---

## Task 2: Write failing test for rootless compiler step

**Files:**
- Modify: `pkg/compiler/compiler_test.go`

Add a test that asserts the compiled LLB for a `DOCKERFILE` task uses `moby/buildkit:rootless`, invokes `buildctl-daemonless.sh`, sets `BUILDKITD_FLAGS`, and has no `SecurityMode_INSECURE`. This test must **fail** before Task 3.

- [ ] **Step 1: Add `strings` to imports in `compiler_test.go`**

The current import block in `pkg/compiler/compiler_test.go`:

```go
import (
	"context"
	"testing"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/moby/buildkit/solver/pb"
)
```

Replace with:

```go
import (
	"context"
	"strings"
	"testing"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/moby/buildkit/solver/pb"
)
```

- [ ] **Step 2: Add `TestCompile_docker_task_uses_rootless_buildkit`**

Append this test to the end of `pkg/compiler/compiler_test.go`:

```go
func TestCompile_docker_task_uses_rootless_buildkit(t *testing.T) {
	r := makeRestore()
	dockerTask := &manifest.Task{
		Name:             "build-image",
		Cache:            true,
		Dockerfile:       strPtr("Dockerfile"),
		DockerfileOutput: strPtr("/out/image.tar"),
		Inputs: []manifest.Input{
			{Task: r, OutputName: "image", Dest: "/out"},
		},
	}
	m := &manifest.Manifest{
		AbsPath: "/test/module",
		Module:  manifest.Module{BaseImage: "ubuntu:24.04"},
		Tasks: map[string]*manifest.Task{
			"restore":     r,
			"build-image": dockerTask,
		},
	}

	result, err := compiler.Compile(m, "build-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def, err := result.State.Marshal(context.Background())
	if err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}

	var foundRootlessImage bool
	var foundBuildctlArg bool
	var foundBuildkitdFlags bool

	for _, raw := range def.Def {
		var op pb.Op
		if err := op.UnmarshalVT(raw); err != nil {
			t.Fatalf("UnmarshalVT: %v", err)
		}
		if src := op.GetSource(); src != nil {
			if strings.Contains(src.Identifier, "moby/buildkit:rootless") {
				foundRootlessImage = true
			}
		}
		if exec := op.GetExec(); exec != nil && exec.Meta != nil {
			if exec.Meta.SecurityMode == pb.SecurityMode_INSECURE {
				t.Error("exec op must not use SecurityMode_INSECURE")
			}
			for _, arg := range exec.Meta.Args {
				if arg == "buildctl-daemonless.sh" {
					foundBuildctlArg = true
				}
			}
			for _, env := range exec.Meta.Env {
				if env == "BUILDKITD_FLAGS=--oci-worker-no-process-sandbox" {
					foundBuildkitdFlags = true
				}
			}
		}
	}

	if !foundRootlessImage {
		t.Error("expected source op for docker-image://moby/buildkit:rootless")
	}
	if !foundBuildctlArg {
		t.Error("expected exec op with buildctl-daemonless.sh as first arg")
	}
	if !foundBuildkitdFlags {
		t.Error("expected exec env BUILDKITD_FLAGS=--oci-worker-no-process-sandbox")
	}
}
```

- [ ] **Step 3: Run the new test to confirm it fails**

```bash
go test ./pkg/compiler/... -run TestCompile_docker_task_uses_rootless_buildkit -v
```

Expected: `FAIL` — the test should report missing rootless image, missing `buildctl-daemonless.sh`, and missing `BUILDKITD_FLAGS` because the compiler still uses `buildah`.

---

## Task 3: Update `pkg/compiler/compiler.go` to pass the test

**Files:**
- Modify: `pkg/compiler/compiler.go:1-17` (imports + constant), `pkg/compiler/compiler.go:261-293` (function), `pkg/compiler/compiler.go:77` (call site)

- [ ] **Step 1: Replace the `buildahImage` constant and remove `pb` import**

Current top of file (lines 1–19):

```go
package compiler

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/manifest/types"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"github.com/moby/buildkit/solver/pb"
)

const buildahImage = "quay.io/buildah/stable"
```

Replace with:

```go
package compiler

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/manifest/types"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
)

const buildkitRootlessImage = "moby/buildkit:rootless"
```

- [ ] **Step 2: Replace `compileBuildahTask` with `compileDockerfileTask`**

Current function (lines 261–293):

```go
// compileBuildahTask compiles a docker task as a buildah exec, producing /out/image.tar.
// Downstream tasks can access the tarball at the declared inp.Dest path.
func compileBuildahTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string, env map[string]string) (llb.State, error) {
	base := llb.Image(buildahImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))
	base = base.With(envStateOptions(env)...)

	outFile := path.Join("/out", path.Base(*task.DockerfileOutput))
	cmd := fmt.Sprintf(
		"buildah --storage-driver=vfs build -f %s -t ci-build . && buildah --storage-driver=vfs push ci-build oci-archive://%s",
		*task.Dockerfile, outFile,
	)

	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", cmd}),
		llb.WithCustomNamef("Building image: %s", task.Name),
		llb.Security(pb.SecurityMode_INSECURE),
		llb.AddMount("/out", llb.Scratch()),
		contextMount,
		llb.Dir(absPath),
	}
	if !task.Cache {
		opts = append(opts, llb.IgnoreCache)
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}

	exec := st.Run(opts...)
	return exec.GetMount("/out"), nil
}
```

Replace with:

```go
// compileDockerfileTask compiles a DOCKERFILE task using buildctl-daemonless.sh inside
// moby/buildkit:rootless, producing an OCI archive at /out/<basename>.
// No security.insecure entitlement is required; nested user namespaces on the host suffice.
func compileDockerfileTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string, env map[string]string) (llb.State, error) {
	base := llb.Image(buildkitRootlessImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))
	base = base.With(envStateOptions(env)...)

	outFile := path.Join("/out", path.Base(*task.DockerfileOutput))

	opts := []llb.RunOption{
		llb.Args([]string{
			"buildctl-daemonless.sh", "build",
			"--frontend", "dockerfile.v0",
			"--local", "context=" + absPath,
			"--local", "dockerfile=" + absPath,
			"--opt", "filename=" + *task.Dockerfile,
			"--output", "type=oci,dest=" + outFile,
		}),
		llb.AddEnv("BUILDKITD_FLAGS", "--oci-worker-no-process-sandbox"),
		llb.WithCustomNamef("Building image: %s", task.Name),
		llb.AddMount("/out", llb.Scratch()),
		contextMount,
		llb.Dir(absPath),
	}
	if !task.Cache {
		opts = append(opts, llb.IgnoreCache)
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}

	exec := st.Run(opts...)
	return exec.GetMount("/out"), nil
}
```

- [ ] **Step 3: Update the call site in `Compile`**

In `pkg/compiler/compiler.go` at line 77, change:

```go
			st, err := compileBuildahTask(task, contextMount, depMounts, compiled, m.AbsPath, taskEnv)
```

to:

```go
			st, err := compileDockerfileTask(task, contextMount, depMounts, compiled, m.AbsPath, taskEnv)
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./pkg/compiler/...
```

Expected: no output. If you see `"github.com/moby/buildkit/solver/pb" imported and not used`, the `pb` import was not removed in Step 1.

- [ ] **Step 5: Run all compiler tests**

```bash
go test ./pkg/compiler/... -v
```

Expected: all tests `PASS`, including `TestCompile_docker_task_uses_rootless_buildkit` and the pre-existing `TestCompile_docker_task`.

- [ ] **Step 6: Commit**

```bash
git add pkg/compiler/compiler.go pkg/compiler/compiler_test.go
git commit -m "feat(compiler): replace buildah with rootless buildkit daemonless step"
```

---

## Task 4: Update `wiki/Getting-Started.md`

**Files:**
- Modify: `wiki/Getting-Started.md`

- [ ] **Step 1: Replace the privileged-containers prerequisite line**

In `wiki/Getting-Started.md`, find:

```markdown
- Permission to run privileged containers (BuildKit needs `--privileged` and `security.insecure` to run `buildah`-backed `DOCKERFILE` tasks).
```

Replace with:

```markdown
- Membership in the `docker` group (or equivalent socket access). No `sudo` or privileged containers required.
```

- [ ] **Step 2: Verify no other `buildah` or `security.insecure` references remain**

```bash
grep -r "buildah\|security\.insecure\|SecurityMode_INSECURE\|Privileged" \
  pkg/ wiki/
```

Expected: no output. If any matches appear, remove them.

- [ ] **Step 3: Commit**

```bash
git add wiki/Getting-Started.md
git commit -m "docs: remove privileged-container prerequisite, update for rootless buildkit"
```

---

## Final verification

- [ ] **Run all unit tests**

```bash
go test ./...
```

Expected: all packages pass. The `buildenv` integration test (tagged `//go:build integration`) is excluded by default and requires a live Docker socket — it is not run here.

- [ ] **Confirm no `buildah` or `security.insecure` in `pkg/`**

```bash
grep -r "buildah\|security\.insecure\|SecurityMode_INSECURE" pkg/
```

Expected: no output.
