# CACHE DSL Field Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `CACHE` boolean DSL field to task definitions that defaults to `true` and passes `llb.IgnoreCache` to BuildKit when set to `FALSE`.

**Architecture:** Add `Cache bool` to the `Task` type; the parser initializes it to `true` and overrides it when `CACHE FALSE` is present; the compiler appends `llb.IgnoreCache` to run options when `!task.Cache`; the LSP completion map gets a `CACHE` entry.

**Tech Stack:** Go, BuildKit LLB (`github.com/moby/buildkit/client/llb`), recursive-descent parser in `pkg/manifest/parser/`

---

## File Map

| File | Change |
|------|--------|
| `pkg/manifest/types/types.go` | Add `Cache bool` to `Task` |
| `pkg/compiler/compiler_test.go` | Set `Cache: true` on existing test helpers; add new IgnoreCache test |
| `pkg/manifest/parser/parser.go` | Fix `CACHE` case; add `strings` import |
| `pkg/manifest/parser_test.go` | Add `CACHE` parser tests |
| `pkg/compiler/compiler.go` | Append `llb.IgnoreCache` in `compileCmdTask` and `compileBuildahTask` |
| `cmd/bongo-ls/server.go` | Add `CACHE` to keyword completions map |

---

### Task 1: Add `Cache bool` to Task type and update test helpers

**Files:**
- Modify: `pkg/manifest/types/types.go`
- Modify: `pkg/compiler/compiler_test.go`

Go's zero value for `bool` is `false`, but unset `Cache` must behave as `true`. The parser will initialize it to `true`; manually constructed test tasks need `Cache: true` set explicitly to avoid silently disabling cache in all existing compiler tests.

- [ ] **Step 1: Add `Cache bool` to `Task`**

In `pkg/manifest/types/types.go`, add the field after `Name`:

```go
type Task struct {
	Name             string
	Cache            bool
	Cmd              *string
	Dockerfile       *string
	DockerfileOutput *string
	Inputs           []Input
	Outputs          []Output
}
```

- [ ] **Step 2: Update compiler test helpers to set `Cache: true`**

In `pkg/compiler/compiler_test.go`, update `makeRestore` and `makeCompile`:

```go
func makeRestore() *manifest.Task {
	return &manifest.Task{
		Name:  "restore",
		Cache: true,
		Cmd:   strPtr("dotnet restore"),
	}
}

func makeCompile(restore *manifest.Task) *manifest.Task {
	return &manifest.Task{
		Name:  "compile",
		Cache: true,
		Cmd:   strPtr("dotnet publish"),
		Inputs: []manifest.Input{
			{Task: restore, OutputName: "packages", Dest: "/packages"},
		},
	}
}
```

Also update `TestCompile_docker_task` which constructs `dockerTask` directly:

```go
dockerTask := &manifest.Task{
	Name:             "build-image",
	Cache:            true,
	Dockerfile:       strPtr("Dockerfile"),
	DockerfileOutput: strPtr("/out/image.tar"),
	Inputs: []manifest.Input{
		{Task: r, OutputName: "image", Dest: "/out"},
	},
}
```

- [ ] **Step 3: Run existing tests to verify they still pass**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/compiler/... -v
```

Expected: all existing tests PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/manifest/types/types.go pkg/compiler/compiler_test.go
git commit -m "feat: add Cache bool field to Task type"
```

---

### Task 2: Add parser support for `CACHE TRUE/FALSE`

**Files:**
- Modify: `pkg/manifest/parser/parser.go`
- Modify: `pkg/manifest/parser_test.go`

The lexer emits `CACHE`, `TRUE`/`FALSE` as IDENT tokens (unquoted). The parser must initialize `task.Cache = true` before the parsing loop, then override it when the `CACHE` directive is present.

- [ ] **Step 1: Write failing parser tests**

Add to `pkg/manifest/parser_test.go` (after the last existing test):

```go
func TestParseContent_cache_false(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

INSTALL_DEPS:
    CMD "npm install"
    CACHE FALSE
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task, ok := m.Tasks["INSTALL_DEPS"]
	if !ok {
		t.Fatal("task 'INSTALL_DEPS' not found")
	}
	if task.Cache {
		t.Error("expected Cache=false for CACHE FALSE, got true")
	}
}

func TestParseContent_cache_true_explicit(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "make"
    CACHE TRUE
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task := m.Tasks["BUILD"]
	if !task.Cache {
		t.Error("expected Cache=true for CACHE TRUE, got false")
	}
}

func TestParseContent_cache_defaults_true(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "make"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task := m.Tasks["BUILD"]
	if !task.Cache {
		t.Error("expected Cache=true when CACHE is omitted, got false")
	}
}

func TestParseContent_cache_invalid_value(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "make"
    CACHE MAYBE
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for invalid CACHE value, got nil")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/manifest/... -run TestParseContent_cache -v
```

Expected: FAIL — `TestParseContent_cache_defaults_true` fails because `Cache` is `false` (zero value); the `CACHE FALSE` test likely panics or errors due to the broken stub.

- [ ] **Step 3: Fix the parser**

In `pkg/manifest/parser/parser.go`, add `"strings"` to the import block:

```go
import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crikke/ci/pkg/manifest/types"
)
```

In `parseTask()`, set `Cache = true` immediately after creating the task struct:

```go
task := &types.Task{Name: nameTok.Value, Cache: true}
```

Replace the broken `CACHE` stub (currently lines 292-294) with:

```go
case "CACHE":
	p.consume()
	tok, err := p.expect(IDENT)
	if err != nil {
		return nil, nil, err
	}
	switch strings.ToUpper(tok.Value) {
	case "TRUE":
		task.Cache = true
	case "FALSE":
		task.Cache = false
	default:
		return nil, nil, p.errorf(tok, "CACHE must be TRUE or FALSE, got %q", tok.Value)
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, nil, err
	}
```

- [ ] **Step 4: Run tests to confirm they pass**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/manifest/... -v
```

Expected: all tests PASS including the four new `TestParseContent_cache_*` tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/manifest/parser/parser.go pkg/manifest/parser_test.go
git commit -m "feat(parser): add CACHE TRUE/FALSE directive to task DSL"
```

---

### Task 3: Apply `llb.IgnoreCache` in the compiler when `Cache = false`

**Files:**
- Modify: `pkg/compiler/compiler.go`
- Modify: `pkg/compiler/compiler_test.go`

`llb.IgnoreCache` is a `constraintsOptFunc` in `github.com/moby/buildkit/client/llb` that sets `IgnoreCache = true` in the op's metadata. After marshaling, this is visible in `llb.Definition.Metadata` (a `map[digest.Digest]llb.OpMetadata`).

- [ ] **Step 1: Write a failing compiler test for `Cache = false`**

Add to `pkg/compiler/compiler_test.go`:

```go
func TestCompile_cache_false_sets_ignore_cache(t *testing.T) {
	task := &manifest.Task{
		Name:  "build",
		Cache: false,
		Cmd:   strPtr("make build"),
	}
	m := &manifest.Manifest{
		AbsPath: "/test/module",
		Module:  manifest.Module{BaseImage: "ubuntu:24.04"},
		Tasks:   map[string]*manifest.Task{"build": task},
	}
	result, err := compiler.Compile(m, "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def, err := result.State.Marshal(context.Background())
	if err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
	found := false
	for _, meta := range def.Metadata {
		if meta.IgnoreCache {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected IgnoreCache=true in LLB metadata for task with Cache=false")
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/compiler/... -run TestCompile_cache_false -v
```

Expected: FAIL — `IgnoreCache` is not set because the compiler doesn't handle it yet.

- [ ] **Step 3: Add `llb.IgnoreCache` to `compileCmdTask`**

In `pkg/compiler/compiler.go`, update `compileCmdTask`:

```go
func compileCmdTask(base llb.State, task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string) (llb.State, error) {
	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", *task.Cmd}),
		llb.WithCustomNamef("Running task: %s", task.Name),
		llb.WithDescription(map[string]string{"foo": "bar"}),
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

	return st.Run(opts...).State, nil
}
```

- [ ] **Step 4: Add `llb.IgnoreCache` to `compileBuildahTask`**

In `pkg/compiler/compiler.go`, update `compileBuildahTask`:

```go
func compileBuildahTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string) (llb.State, error) {
	base := llb.Image(buildahImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))

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

- [ ] **Step 5: Run all compiler tests**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/compiler/... -v
```

Expected: all tests PASS including `TestCompile_cache_false_sets_ignore_cache`.

- [ ] **Step 6: Commit**

```bash
git add pkg/compiler/compiler.go pkg/compiler/compiler_test.go
git commit -m "feat(compiler): pass llb.IgnoreCache for tasks with CACHE FALSE"
```

---

### Task 4: Add `CACHE` to LSP completions

**Files:**
- Modify: `cmd/bongo-ls/server.go`

- [ ] **Step 1: Add `CACHE` to the task keyword map**

In `cmd/bongo-ls/server.go`, add `"CACHE"` to the existing completions map alongside `CMD`, `DOCKERFILE`, `INPUT`, and `OUTPUT`:

```go
"CACHE":      "Whether BuildKit caches this step. Defaults to TRUE. Set to FALSE to always re-run.",
```

- [ ] **Step 2: Run the bongo-ls tests**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./cmd/bongo-ls/... -v
```

Expected: all tests PASS.

- [ ] **Step 3: Confirm `build.bongo` parses cleanly**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go run ./cmd/ci/... --help 2>&1 || true
go test ./pkg/manifest/... -v -run TestParseContent_cache
```

Expected: no errors; cache tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/bongo-ls/server.go
git commit -m "feat(lsp): add CACHE keyword to bongo-ls completions"
```

---

### Task 5: Run full test suite

- [ ] **Step 1: Run all tests**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./... 2>&1
```

Expected: all packages PASS, no compilation errors.

- [ ] **Step 2: Verify `build.bongo` parses with `CACHE FALSE`**

```
cd /home/elstoffo/dev/elstoffo/bongoci && go test ./pkg/manifest/... -v -run TestParseContent
```

Expected: all manifest parser tests PASS.
