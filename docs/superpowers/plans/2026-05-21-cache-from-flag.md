# cache-from flag Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `--cache-from=<ref>` CLI flag that imports a BuildKit registry cache into the solve, threaded through a new `RunOptions` struct.

**Architecture:** `withCacheOpt` is fixed to actually apply cache imports when a non-empty `cacheFrom` string is provided. A new `RunOptions` struct in the `runner` package replaces the bare `host string` parameter on `Run`, carrying both `Host` and `CacheFrom`. `main.go` parses the new flag and populates `RunOptions`.

**Tech Stack:** Go, `github.com/moby/buildkit/client` (`bkclient.SolveOpt`, `bkclient.CacheOptionsEntry`)

---

### Task 1: Fix `withCacheOpt` — test then implement

**Files:**
- Create: `pkg/runner/runner_cache_test.go`
- Modify: `pkg/runner/runner.go` (lines 84–94)

The build is currently broken because `cacheOpt` in `withCacheOpt` is declared but never used. This task fixes that by giving the function a `cacheFrom string` parameter, adding the import entry when non-empty, and removing the dead variable.

- [ ] **Step 1: Write the failing tests**

Create `pkg/runner/runner_cache_test.go`:

```go
package runner

import (
	"testing"

	bkclient "github.com/moby/buildkit/client"
)

func TestWithCacheOpt_empty(t *testing.T) {
	opt := withCacheOpt(bkclient.SolveOpt{}, "")
	if len(opt.CacheImports) != 0 {
		t.Errorf("expected no CacheImports, got %d", len(opt.CacheImports))
	}
}

func TestWithCacheOpt_withRef(t *testing.T) {
	opt := withCacheOpt(bkclient.SolveOpt{}, "myregistry/cache")
	if len(opt.CacheImports) != 1 {
		t.Fatalf("expected 1 CacheImport, got %d", len(opt.CacheImports))
	}
	entry := opt.CacheImports[0]
	if entry.Type != "registry" {
		t.Errorf("Type: got %q, want %q", entry.Type, "registry")
	}
	if entry.Attrs["ref"] != "myregistry/cache" {
		t.Errorf("Attrs[ref]: got %q, want %q", entry.Attrs["ref"], "myregistry/cache")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```
go test ./pkg/runner/... -run TestWithCacheOpt -v
```

Expected: compile error — `withCacheOpt` currently takes only one argument.

- [ ] **Step 3: Update `withCacheOpt` in `pkg/runner/runner.go`**

Replace the current `withCacheOpt` function (lines 84–94):

```go
func withCacheOpt(opt bkclient.SolveOpt, cacheFrom string) bkclient.SolveOpt {
	if cacheFrom == "" {
		return opt
	}
	opt.CacheImports = append(opt.CacheImports, bkclient.CacheOptionsEntry{
		Type:  "registry",
		Attrs: map[string]string{"ref": cacheFrom},
	})
	return opt
}
```

Also update the call site in `solveExec` (line 74) — temporarily pass an empty string to keep it compiling:

```go
solveOpt = withCacheOpt(solveOpt, "")
```

- [ ] **Step 4: Run tests to verify they pass**

```
go test ./pkg/runner/... -run TestWithCacheOpt -v
```

Expected:
```
--- PASS: TestWithCacheOpt_empty
--- PASS: TestWithCacheOpt_withRef
PASS
```

- [ ] **Step 5: Verify build**

```
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/runner/runner.go pkg/runner/runner_cache_test.go
git commit -m "fix(runner): implement withCacheOpt registry cache import"
```

---

### Task 2: Add `RunOptions`, update `Run`/`solveExec`, wire `main.go`

**Files:**
- Modify: `pkg/runner/runner.go` (add struct, update `Run` + `solveExec`)
- Modify: `cmd/ci/main.go` (add flag, pass `RunOptions`)

No new tests needed here — the logic change is in `withCacheOpt` (already tested). This task is purely signature plumbing.

- [ ] **Step 1: Add `RunOptions` struct to `pkg/runner/runner.go`**

Add after the `ExportedOutput` type (after line 24):

```go
// RunOptions configures a Run call.
type RunOptions struct {
	Host      string
	CacheFrom string // registry ref for cache import; empty = disabled
}
```

- [ ] **Step 2: Update `Run` and `solveExec` in `pkg/runner/runner.go`**

Replace the `Run` function signature and body (lines 41–50):

```go
func Run(ctx context.Context, opts RunOptions, result *compiler.Result, outputs []ExportedOutput) error {
	c, err := bkclient.New(ctx, opts.Host)
	if err != nil {
		return fmt.Errorf("connect to buildkit at %q: %w\nhint: set BUILDKIT_HOST or ensure buildkitd is running", opts.Host, err)
	}
	slog.Debug("connected to buildkit", "host", opts.Host)
	defer c.Close()

	return solveExec(ctx, c, opts, result, outputs)
}
```

Replace the `solveExec` signature and the `withCacheOpt` call (lines 52–81):

```go
func solveExec(ctx context.Context, c *bkclient.Client, opts RunOptions, result *compiler.Result, outputs []ExportedOutput) error {
	tmpDir, err := os.MkdirTemp("", "ci-export-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	mounts, err := localMounts(result.LocalDirs)
	if err != nil {
		return err
	}

	solveOpt := bkclient.SolveOpt{
		Exports: []bkclient.ExportEntry{
			{Type: bkclient.ExporterLocal, OutputDir: tmpDir},
		},

		LocalMounts:         mounts,
		AllowedEntitlements: []string{"security.insecure"},
	}

	solveOpt = withCacheOpt(solveOpt, opts.CacheFrom)

	if err := solve(ctx, c, result, solveOpt); err != nil {
		return err
	}

	return CopyOutputs(tmpDir, outputs)
}
```

- [ ] **Step 3: Update `cmd/ci/main.go`**

Add the `--cache-from` flag after the existing flag declarations (after line 31):

```go
cacheFrom := fs.String("cache-from", "", "registry ref to import build cache from (e.g. myregistry/cache)")
```

Replace the `runner.Run` call (line 112):

```go
opts := runner.RunOptions{Host: host, CacheFrom: *cacheFrom}
if err := runner.Run(ctx, opts, result, taskOutputs); err != nil {
    return fmt.Errorf("task %q failed: %w", taskName, err)
}
```

- [ ] **Step 4: Verify build and all tests pass**

```
go build ./...
go test ./...
```

Expected:
```
ok  github.com/crikke/ci/pkg/runner
ok  github.com/crikke/ci/pkg/manifest
ok  github.com/crikke/ci/pkg/manifest/parser
```

- [ ] **Step 5: Commit**

```bash
git add pkg/runner/runner.go cmd/ci/main.go
git commit -m "feat(runner): add --cache-from flag via RunOptions"
```
