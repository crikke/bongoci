# Structured Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `log/slog` structured logging across `main.go`, `compiler.go`, and `runner.go`, with a `-v`/`--verbose` flag to opt into debug output.

**Architecture:** Set the slog default logger once in `main.go` based on the `-v` flag; all packages call `slog.Info`/`slog.Debug`/`slog.Error` directly via the global default. No signature changes to `Compile` or `Run`. Raw BuildKit build output (`log.Data` bytes) stays on stdout; all log messages go to stderr.

**Tech Stack:** Go stdlib `log/slog`, `flag.FlagSet`

---

## Files

- Modify: `cmd/ci/main.go` — flag parsing, slog init, replace/add log calls
- Modify: `pkg/compiler/compiler.go` — replace `fmt.Printf`, add topo sort debug log
- Modify: `pkg/runner/runner.go` — add connect/copy debug calls, migrate `printStatus` to slog

---

### Task 1: Logger init and `-v` flag in `main.go`

**Files:**
- Modify: `cmd/ci/main.go`

- [ ] **Step 1: Verify existing tests pass before touching anything**

```bash
cd /home/elstoffo/dev/elstoffo/ci2
go test ./...
```

Expected: all tests pass (PASS lines, no FAIL).

- [ ] **Step 2: Update `cmd/ci/main.go`**

Replace the entire file content with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/runner"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ci", flag.ContinueOnError)
	verbose := fs.Bool("v", false, "enable debug logging")
	fs.BoolVar(verbose, "verbose", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	args = fs.Args()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if len(args) < 2 || args[0] != "run" {
		return fmt.Errorf("usage: ci [-v] run <task> [<task>...]")
	}
	taskNames := args[1:]

	tomlPath, found := findBuildToml(mustCwd())
	if !found {
		return fmt.Errorf("build.toml not found (searched up from current directory)")
	}

	m, err := manifest.Parse(tomlPath)
	if err != nil {
		return err
	}
	slog.Debug("manifest parsed", "path", tomlPath, "tasks", len(m.Tasks))

	host := os.Getenv("BUILDKIT_HOST")
	if host == "" {
		host = "unix:///run/buildkit/buildkitd.sock"
	}
	slog.Debug("buildkit host", "host", host)

	ctx := context.Background()

	for _, taskName := range taskNames {
		if _, ok := m.Tasks[taskName]; !ok {
			names := make([]string, 0, len(m.Tasks))
			for n := range m.Tasks {
				names = append(names, n)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown task %q; available: %v", taskName, names)
		}

		result, err := compiler.Compile(m, taskName)
		if err != nil {
			return fmt.Errorf("compile %q: %w", taskName, err)
		}

		var taskOutputs []manifest.Output
		for _, out := range m.Outputs {
			if out.TaskName == taskName {
				taskOutputs = append(taskOutputs, out)
			}
		}

		slog.Info("running task", "task", taskName)
		if err := runner.Run(ctx, host, result, taskOutputs); err != nil {
			return fmt.Errorf("task %q failed: %w", taskName, err)
		}
	}
	return nil
}

func findBuildToml(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, "build.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func mustCwd() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not determine working directory:", err)
		os.Exit(1)
	}
	return dir
}
```

- [ ] **Step 3: Build to confirm no compile errors**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 4: Run tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/ci/main.go
git commit -m "feat: add -v flag and slog logger init in main"
```

---

### Task 2: Structured logging in `compiler.go`

**Files:**
- Modify: `pkg/compiler/compiler.go`

- [ ] **Step 1: Add `log/slog` import and replace `fmt.Printf` with `slog.Debug`**

In `pkg/compiler/compiler.go`:

1. Add `"log/slog"` to the import block (keep `"fmt"` — it's still used for `Errorf`, `Sprintf`).

2. After the `topoSort` call (currently lines 34–37), add a debug log:

```go
sorted, err := topoSort(m.Tasks, targetTaskName)
if err != nil {
    return nil, err
}
slog.Debug("topo sort", "order", sorted)
```

3. Replace the `fmt.Printf` inside the compile loop (currently line 66):

```go
// before:
fmt.Printf("Compiling task %s \n", task.Name)

// after:
slog.Debug("compiling task", "task", task.Name)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 3: Run tests**

```bash
go test ./...
```

Expected: all tests pass. The `compiler_test.go` tests call `Compile` directly — `slog.Debug` calls are silently dropped at the default Info level, so output is unchanged.

- [ ] **Step 4: Commit**

```bash
git add pkg/compiler/compiler.go
git commit -m "feat: replace fmt.Printf with slog.Debug in compiler"
```

---

### Task 3: Structured logging in `runner.go`

**Files:**
- Modify: `pkg/runner/runner.go`

- [ ] **Step 1: Add `log/slog` import**

In `pkg/runner/runner.go`, add `"log/slog"` to the import block. Keep all existing imports — `"fmt"` and `"os"` are still used throughout.

- [ ] **Step 2: Add connect log in `Run`**

After the `bkclient.New` call (currently lines 44–47), add:

```go
c, err := bkclient.New(ctx, host)
if err != nil {
    return fmt.Errorf("connect to buildkit at %q: %w\nhint: set BUILDKIT_HOST or ensure buildkitd is running", host, err)
}
slog.Debug("connected to buildkit", "host", host)
defer c.Close()
```

- [ ] **Step 3: Add copy log in `CopyOutputs`**

In `CopyOutputs`, add a debug log at the top of the loop (currently line 184):

```go
for _, out := range outputs {
    slog.Debug("copying output", "src", out.SrcPath, "dest", out.DestPath)
    src := filepath.Join(exportDir, strings.TrimPrefix(out.SrcPath, "/"))
    ...
```

- [ ] **Step 4: Migrate `printStatus` to slog**

Replace the entire `printStatus` function (currently lines 247–263):

```go
func printStatus(ch chan *bkclient.SolveStatus) {
	for status := range ch {
		for _, v := range status.Vertexes {
			switch {
			case v.Error != "":
				slog.Error("vertex failed", "name", v.Name, "error", v.Error)
			case v.Completed != nil:
				slog.Info("vertex done", "name", v.Name)
			case v.Started != nil:
				slog.Debug("vertex started", "name", v.Name)
			}
		}
		for _, log := range status.Logs {
			os.Stdout.Write(log.Data)
		}
	}
}
```

- [ ] **Step 5: Build**

```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 6: Run tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/runner/runner.go
git commit -m "feat: add slog logging to runner (connect, copy, vertex events)"
```
