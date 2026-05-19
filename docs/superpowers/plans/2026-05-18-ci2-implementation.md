# ci2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `ci run <task>` — a CLI that parses `build.toml`, compiles tasks into a BuildKit LLB DAG, and executes them via buildkitd, exporting artifacts to `./out`.

**Architecture:** Four focused packages: `manifest` parses TOML into a domain model, `compiler` converts the model into an `llb.State` DAG, `runner` connects to buildkitd and solves it via the gateway API, and `cmd/ci` wires them into the CLI. BuildKit's Go client handles session protocol and local export transparently.

**Tech Stack:** Go 1.25, `moby/buildkit` v0.30 (`client`, `client/llb`, `frontend/gateway/client`), `github.com/BurntSushi/toml`

---

## File Map

| File | Responsibility |
|------|---------------|
| `cmd/ci/main.go` | CLI: parse args, find `build.toml`, wire manifest → compiler → runner |
| `pkg/manifest/manifest.go` | Domain types: `Manifest`, `Task`, `TaskInput`, `Output` |
| `pkg/manifest/parser.go` | TOML decode → domain model; two-pass input wiring; cycle detection |
| `pkg/manifest/parser_test.go` | Unit tests for parser |
| `pkg/compiler/compiler.go` | Domain model → `llb.State` DAG; topo-sort; dep/local-dir map |
| `pkg/compiler/compiler_test.go` | Unit tests for compiler |
| `pkg/runner/runner.go` | BuildKit client; gateway solve; status streaming; local/OCI export |
| `pkg/runner/runner_test.go` | Unit tests for file-copy helpers |

---

### Task 1: Project bootstrap

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Initialize git and commit skeleton**

```bash
cd /home/elstoffo/dev/elstoffo/ci2
git init
git add go.mod go.sum build.toml
git commit -m "chore: initial go project skeleton"
```

- [ ] **Add TOML library**

```bash
go get github.com/BurntSushi/toml@latest
```

- [ ] **Promote buildkit to direct dependency**

```bash
go get github.com/moby/buildkit@v0.30.0
go mod tidy
```

- [ ] **Create package directories**

```bash
mkdir -p pkg/manifest pkg/compiler pkg/runner cmd/ci
```

- [ ] **Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add toml and buildkit as direct dependencies"
```

---

### Task 2: Domain model

**Files:**
- Create: `pkg/manifest/manifest.go`

- [ ] **Write `pkg/manifest/manifest.go`**

```go
package manifest

// Manifest is the parsed representation of build.toml.
type Manifest struct {
	AbsPath      string            // absolute path to the module directory
	Name         string
	Image        string
	Dependencies []string          // sibling module paths, resolved to absolute at parse time
	Tasks        map[string]*Task
	Outputs      map[string]Output
}

// Task represents one build step.
type Task struct {
	ID         string
	Name       string
	Cmd        string
	Type       string   // "exec" (default) or "docker"
	Dockerfile string   // docker tasks only
	Inputs     []TaskInput
}

// TaskInput wires a path from an upstream task's /out scratch into this task's container.
type TaskInput struct {
	Task *Task
	Path string // path within the upstream task's /out scratch (e.g. "/packages")
	Dest string // mount destination in this container (e.g. "/packages")
}

// Output declares what gets copied from a task's /out scratch back to the host.
type Output struct {
	TaskName string
	SrcPath  string // path within task's /out scratch (e.g. "/payments-service")
	DestPath string // host destination (e.g. "./out/payments-service")
}
```

- [ ] **Verify it compiles**

```bash
go build ./pkg/manifest/...
```

Expected: no output (success).

- [ ] **Commit**

```bash
git add pkg/manifest/manifest.go
git commit -m "feat: add manifest domain model types"
```

---

### Task 3: TOML parser

**Files:**
- Create: `pkg/manifest/parser_test.go`
- Create: `pkg/manifest/parser.go`

- [ ] **Write the failing tests in `pkg/manifest/parser_test.go`**

```go
package manifest_test

import (
	"path/filepath"
	"testing"

	"github.com/crikke/ci/pkg/manifest"
)

func TestParseContent_basic(t *testing.T) {
	const toml = `
version = 1
[module]
name = "test-module"
image = "ubuntu:24.04"

[tasks.build]
cmd = "make build"
`
	m, err := manifest.ParseContent(toml, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "test-module" {
		t.Errorf("Name: got %q, want %q", m.Name, "test-module")
	}
	if m.Image != "ubuntu:24.04" {
		t.Errorf("Image: got %q, want %q", m.Image, "ubuntu:24.04")
	}
	if m.AbsPath != "/some/dir" {
		t.Errorf("AbsPath: got %q, want %q", m.AbsPath, "/some/dir")
	}
	if len(m.Tasks) != 1 {
		t.Fatalf("Tasks: got %d, want 1", len(m.Tasks))
	}
	task, ok := m.Tasks["build"]
	if !ok {
		t.Fatal("task 'build' not found")
	}
	if task.Name != "build" {
		t.Errorf("task.Name: got %q, want %q", task.Name, "build")
	}
	if task.Cmd != "make build" {
		t.Errorf("task.Cmd: got %q, want %q", task.Cmd, "make build")
	}
	if task.Type != "exec" {
		t.Errorf("task.Type: got %q, want %q (default)", task.Type, "exec")
	}
}

func TestParseContent_inputs(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.restore]
cmd = "dotnet restore"

[tasks.compile]
cmd = "dotnet publish"
inputs = [
    { task = "restore", path = "/packages", dest = "/packages" }
]
`
	m, err := manifest.ParseContent(toml, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	compile := m.Tasks["compile"]
	if len(compile.Inputs) != 1 {
		t.Fatalf("compile.Inputs: got %d, want 1", len(compile.Inputs))
	}
	inp := compile.Inputs[0]
	if inp.Task.Name != "restore" {
		t.Errorf("inp.Task.Name: got %q, want %q", inp.Task.Name, "restore")
	}
	if inp.Path != "/packages" {
		t.Errorf("inp.Path: got %q, want %q", inp.Path, "/packages")
	}
	if inp.Dest != "/packages" {
		t.Errorf("inp.Dest: got %q, want %q", inp.Dest, "/packages")
	}
}

func TestParseContent_missing_name(t *testing.T) {
	const toml = `
version = 1
[module]
image = "ubuntu:24.04"
`
	_, err := manifest.ParseContent(toml, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing module.name, got nil")
	}
}

func TestParseContent_missing_image(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
`
	_, err := manifest.ParseContent(toml, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing module.image, got nil")
	}
}

func TestParseContent_unknown_input_task(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.compile]
cmd = "compile"
inputs = [
    { task = "nonexistent", path = "/out", dest = "/out" }
]
`
	_, err := manifest.ParseContent(toml, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown input task, got nil")
	}
}

func TestParseContent_cycle(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.a]
cmd = "a"
inputs = [{ task = "b", path = "/out", dest = "/out" }]

[tasks.b]
cmd = "b"
inputs = [{ task = "a", path = "/out", dest = "/out" }]
`
	_, err := manifest.ParseContent(toml, "/some/dir")
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
}

func TestParseContent_dependencies_absolute(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"
dependencies = ["../other"]
`
	m, err := manifest.ParseContent(toml, "/home/user/module")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Dependencies) != 1 {
		t.Fatalf("Dependencies: got %d, want 1", len(m.Dependencies))
	}
	want := filepath.Clean("/home/user/other")
	if m.Dependencies[0] != want {
		t.Errorf("dep path: got %q, want %q", m.Dependencies[0], want)
	}
}

func TestParseContent_outputs(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.compile]
cmd = "compile"

[outputs.binary]
dest = "./out/binary"
[outputs.binary.from]
task = "compile"
path = "/binary"
`
	m, err := manifest.ParseContent(toml, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := m.Outputs["binary"]
	if !ok {
		t.Fatal("output 'binary' not found")
	}
	if out.TaskName != "compile" {
		t.Errorf("out.TaskName: got %q, want %q", out.TaskName, "compile")
	}
	if out.SrcPath != "/binary" {
		t.Errorf("out.SrcPath: got %q, want %q", out.SrcPath, "/binary")
	}
	if out.DestPath != "./out/binary" {
		t.Errorf("out.DestPath: got %q, want %q", out.DestPath, "./out/binary")
	}
}

func TestParseContent_docker_task(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.build-image]
type = "docker"
dockerfile = "Dockerfile"
`
	m, err := manifest.ParseContent(toml, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task, ok := m.Tasks["build-image"]
	if !ok {
		t.Fatal("task 'build-image' not found")
	}
	if task.Type != "docker" {
		t.Errorf("task.Type: got %q, want %q", task.Type, "docker")
	}
	if task.Dockerfile != "Dockerfile" {
		t.Errorf("task.Dockerfile: got %q, want %q", task.Dockerfile, "Dockerfile")
	}
}
```

- [ ] **Run tests to confirm they fail**

```bash
go test ./pkg/manifest/...
```

Expected: compilation error — `manifest.ParseContent` undefined.

- [ ] **Implement `pkg/manifest/parser.go`**

```go
package manifest

import (
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Parse reads build.toml from filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	var raw tomlManifest
	if _, err := toml.DecodeFile(filePath, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	return build(&raw, filepath.Dir(filePath))
}

// ParseContent parses TOML content with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	var raw tomlManifest
	if _, err := toml.Decode(content, &raw); err != nil {
		return nil, fmt.Errorf("parse toml: %w", err)
	}
	return build(&raw, dir)
}

func build(raw *tomlManifest, dir string) (*Manifest, error) {
	if raw.Module.Name == "" {
		return nil, fmt.Errorf("manifest is missing module.name")
	}
	if raw.Module.Image == "" {
		return nil, fmt.Errorf("manifest is missing module.image")
	}

	deps := make([]string, len(raw.Module.Dependencies))
	for i, d := range raw.Module.Dependencies {
		if filepath.IsAbs(d) {
			deps[i] = d
		} else {
			deps[i] = filepath.Clean(filepath.Join(dir, d))
		}
	}

	// Pass 1: create Task instances without inputs.
	taskMap := make(map[string]*Task, len(raw.Tasks))
	for name, rt := range raw.Tasks {
		typ := rt.Type
		if typ == "" {
			typ = "exec"
		}
		taskMap[name] = &Task{
			ID:         name,
			Name:       name,
			Cmd:        rt.Cmd,
			Type:       typ,
			Dockerfile: rt.Dockerfile,
		}
	}

	// Pass 2: wire inputs.
	for name, rt := range raw.Tasks {
		if len(rt.Inputs) == 0 {
			continue
		}
		inputs := make([]TaskInput, 0, len(rt.Inputs))
		for _, ri := range rt.Inputs {
			dep, ok := taskMap[ri.Task]
			if !ok {
				return nil, fmt.Errorf("task %q: unknown input task %q", name, ri.Task)
			}
			inputs = append(inputs, TaskInput{
				Task: dep,
				Path: ri.Path,
				Dest: ri.Dest,
			})
		}
		taskMap[name].Inputs = inputs
	}

	if err := checkCycles(taskMap); err != nil {
		return nil, err
	}

	outputMap := make(map[string]Output, len(raw.Outputs))
	for name, ro := range raw.Outputs {
		if _, ok := taskMap[ro.From.Task]; !ok {
			return nil, fmt.Errorf("output %q: unknown task %q", name, ro.From.Task)
		}
		outputMap[name] = Output{
			TaskName: ro.From.Task,
			SrcPath:  ro.From.Path,
			DestPath: ro.Dest,
		}
	}

	return &Manifest{
		AbsPath:      dir,
		Name:         raw.Module.Name,
		Image:        raw.Module.Image,
		Dependencies: deps,
		Tasks:        taskMap,
		Outputs:      outputMap,
	}, nil
}

func checkCycles(tasks map[string]*Task) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle detected at task %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, inp := range tasks[name].Inputs {
			if err := dfs(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		return nil
	}

	for name := range tasks {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}

// TOML wire types

type tomlManifest struct {
	Version int                    `toml:"version"`
	Module  tomlModule             `toml:"module"`
	Tasks   map[string]tomlTask   `toml:"tasks"`
	Outputs map[string]tomlOutput `toml:"outputs"`
}

type tomlModule struct {
	Name         string   `toml:"name"`
	Image        string   `toml:"image"`
	Dependencies []string `toml:"dependencies"`
}

type tomlTask struct {
	Cmd        string      `toml:"cmd"`
	Type       string      `toml:"type"`
	Dockerfile string      `toml:"dockerfile"`
	Inputs     []tomlInput `toml:"inputs"`
}

type tomlInput struct {
	Task string `toml:"task"`
	Path string `toml:"path"`
	Dest string `toml:"dest"`
}

type tomlOutput struct {
	From tomlOutputFrom `toml:"from"`
	Dest string         `toml:"dest"`
}

type tomlOutputFrom struct {
	Task string `toml:"task"`
	Path string `toml:"path"`
}
```

- [ ] **Run tests to confirm they pass**

```bash
go test ./pkg/manifest/...
```

Expected:
```
ok      github.com/crikke/ci/pkg/manifest       0.XXXs
```

- [ ] **Commit**

```bash
git add pkg/manifest/
git commit -m "feat: implement manifest TOML parser with two-pass input wiring and cycle detection"
```

---

### Task 4: LLB compiler

**Files:**
- Create: `pkg/compiler/compiler_test.go`
- Create: `pkg/compiler/compiler.go`

- [ ] **Write the failing tests in `pkg/compiler/compiler_test.go`**

```go
package compiler_test

import (
	"context"
	"testing"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
)

func makeRestore() *manifest.Task {
	return &manifest.Task{
		ID:   "restore",
		Name: "restore",
		Cmd:  "dotnet restore",
		Type: "exec",
	}
}

func makeCompile(restore *manifest.Task) *manifest.Task {
	return &manifest.Task{
		ID:   "compile",
		Name: "compile",
		Cmd:  "dotnet publish",
		Type: "exec",
		Inputs: []manifest.TaskInput{
			{Task: restore, Path: "/packages", Dest: "/packages"},
		},
	}
}

func testManifest() *manifest.Manifest {
	r := makeRestore()
	c := makeCompile(r)
	return &manifest.Manifest{
		Name:    "test",
		Image:   "ubuntu:24.04",
		AbsPath: "/test/module",
		Tasks: map[string]*manifest.Task{
			"restore": r,
			"compile": c,
		},
		Outputs: map[string]manifest.Output{},
	}
}

func TestCompile_unknown_task(t *testing.T) {
	m := testManifest()
	_, err := compiler.Compile(m, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
}

func TestCompile_single_exec_task(t *testing.T) {
	m := testManifest()
	result, err := compiler.Compile(m, "restore")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "exec" {
		t.Errorf("TaskType: got %q, want %q", result.TaskType, "exec")
	}
	if result.LocalDirs["context"] != "/test/module" {
		t.Errorf("LocalDirs[context]: got %q, want %q", result.LocalDirs["context"], "/test/module")
	}
	if _, err = result.State.Marshal(context.Background()); err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
}

func TestCompile_task_with_inputs(t *testing.T) {
	m := testManifest()
	result, err := compiler.Compile(m, "compile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err = result.State.Marshal(context.Background()); err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
}

func TestCompile_local_dirs_includes_deps(t *testing.T) {
	m := testManifest()
	m.Dependencies = []string{"/other/module"}
	result, err := compiler.Compile(m, "restore")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.LocalDirs) < 2 {
		t.Errorf("expected >= 2 LocalDirs (context + dep), got %d: %v", len(result.LocalDirs), result.LocalDirs)
	}
	found := false
	for _, v := range result.LocalDirs {
		if v == "/other/module" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("LocalDirs does not contain dep path /other/module: %v", result.LocalDirs)
	}
}
```

- [ ] **Run tests to confirm they fail**

```bash
go test ./pkg/compiler/...
```

Expected: compilation error — `compiler.Compile` undefined.

- [ ] **Implement `pkg/compiler/compiler.go`**

```go
package compiler

import (
	"fmt"
	"strings"

	"github.com/crikke/ci/pkg/manifest"
	"github.com/moby/buildkit/client/llb"
)

// Result holds the compiled LLB state and metadata needed by the runner.
type Result struct {
	State     llb.State
	TaskType  string            // "exec" or "docker"
	LocalDirs map[string]string // buildkit local name → absolute host path
}

// Compile converts targetTaskName (and its transitive inputs) into an LLB state.
// Each exec task runs in the base image with:
//   - context + dependencies mounted read-only at their absolute host paths
//   - upstream task outputs mounted read-only at the declared Dest paths
//   - a scratch volume at /out as the sole writable output
func Compile(m *manifest.Manifest, targetTaskName string) (*Result, error) {
	if _, ok := m.Tasks[targetTaskName]; !ok {
		return nil, fmt.Errorf("unknown task %q", targetTaskName)
	}

	sorted, err := topoSort(m.Tasks, targetTaskName)
	if err != nil {
		return nil, err
	}

	localDirs := map[string]string{"context": m.AbsPath}
	depNames := make([]string, len(m.Dependencies))
	for i, dep := range m.Dependencies {
		name := fmt.Sprintf("dep-%d", i)
		depNames[i] = name
		localDirs[name] = dep
	}

	base := llb.Image(m.Image)
	contextMount := llb.AddMount(m.AbsPath, llb.Local("context"), llb.Readonly)

	depMounts := make([]llb.RunOption, len(m.Dependencies))
	for i, dep := range m.Dependencies {
		depMounts[i] = llb.AddMount(dep, llb.Local(depNames[i]), llb.Readonly)
	}

	compiled := make(map[string]llb.State) // task name → /out scratch state

	for _, name := range sorted {
		task := m.Tasks[name]

		opts := []llb.RunOption{
			llb.Args([]string{"/bin/sh", "-c", task.Cmd}),
			contextMount,
		}
		opts = append(opts, depMounts...)

		for _, inp := range task.Inputs {
			srcPath := strings.TrimPrefix(inp.Path, "/")
			upstreamOut := compiled[inp.Task.Name]
			mountOpts := []llb.MountOption{llb.Readonly}
			if srcPath != "" {
				mountOpts = append([]llb.MountOption{llb.SourcePath(srcPath)}, mountOpts...)
			}
			opts = append(opts, llb.AddMount(inp.Dest, upstreamOut, mountOpts...))
		}

		opts = append(opts, llb.AddMount("/out", llb.Scratch()))
		exec := base.Run(opts...)
		compiled[name] = exec.GetMount("/out")
	}

	return &Result{
		State:     compiled[targetTaskName],
		TaskType:  m.Tasks[targetTaskName].Type,
		LocalDirs: localDirs,
	}, nil
}

// topoSort returns task names in dependency-first order for targetTask's subgraph.
func topoSort(tasks map[string]*manifest.Task, target string) ([]string, error) {
	var result []string
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle at task %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, inp := range tasks[name].Inputs {
			if err := visit(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		result = append(result, name)
		return nil
	}

	return result, visit(target)
}
```

- [ ] **Run tests to confirm they pass**

```bash
go test ./pkg/compiler/...
```

Expected:
```
ok      github.com/crikke/ci/pkg/compiler       0.XXXs
```

- [ ] **Commit**

```bash
git add pkg/compiler/
git commit -m "feat: implement LLB compiler with topo-sort and scratch /out output"
```

---

### Task 5: BuildKit runner

**Files:**
- Create: `pkg/runner/runner_test.go`
- Create: `pkg/runner/runner.go`

- [ ] **Write the failing tests in `pkg/runner/runner_test.go`**

```go
package runner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crikke/ci/pkg/runner"
)

func TestCopyOutputs_file(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Write a file into src simulating /out/binary
	if err := os.WriteFile(filepath.Join(src, "binary"), []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	outputs := []runner.ExportedOutput{
		{SrcPath: "/binary", DestPath: filepath.Join(dst, "binary")},
	}

	if err := runner.CopyOutputs(src, outputs); err != nil {
		t.Fatalf("CopyOutputs: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "binary"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content: got %q, want %q", string(got), "hello")
	}
}

func TestCopyOutputs_directory(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Write files into src/packages/ simulating /out/packages/
	pkgDir := filepath.Join(src, "packages")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "a.dll"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputs := []runner.ExportedOutput{
		{SrcPath: "/packages", DestPath: filepath.Join(dst, "packages")},
	}

	if err := runner.CopyOutputs(src, outputs); err != nil {
		t.Fatalf("CopyOutputs: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "packages", "a.dll"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "a" {
		t.Errorf("content: got %q, want %q", string(got), "a")
	}
}

func TestCopyOutputs_missing_src(t *testing.T) {
	dst := t.TempDir()
	outputs := []runner.ExportedOutput{
		{SrcPath: "/does-not-exist", DestPath: filepath.Join(dst, "out")},
	}
	if err := runner.CopyOutputs("/nonexistent-base", outputs); err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}
```

- [ ] **Run tests to confirm they fail**

```bash
go test ./pkg/runner/...
```

Expected: compilation error — `runner.CopyOutputs` and `runner.ExportedOutput` undefined.

- [ ] **Implement `pkg/runner/runner.go`**

```go
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	bkclient "github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
)

// ExportedOutput maps a path inside the /out scratch to a host destination.
// Exported for use in tests.
type ExportedOutput struct {
	SrcPath  string // path within the exported tmpDir (e.g. "/binary")
	DestPath string // host destination (e.g. "./out/binary")
}

// Run solves the compiled LLB graph via buildkitd and copies declared outputs to the host.
func Run(ctx context.Context, host string, result *compiler.Result, outputs []manifest.Output) error {
	c, err := bkclient.New(ctx, host)
	if err != nil {
		return fmt.Errorf("connect to buildkit at %q: %w\nhint: set BUILDKIT_HOST or ensure buildkitd is running", host, err)
	}
	defer c.Close()

	exported := make([]ExportedOutput, len(outputs))
	for i, out := range outputs {
		exported[i] = ExportedOutput{SrcPath: out.SrcPath, DestPath: out.DestPath}
	}

	if result.TaskType == "docker" {
		return solveDocker(ctx, c, result, exported)
	}
	return solveExec(ctx, c, result, exported)
}

func solveExec(ctx context.Context, c *bkclient.Client, result *compiler.Result, outputs []ExportedOutput) error {
	tmpDir, err := os.MkdirTemp("", "ci-export-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	solveOpt := bkclient.SolveOpt{
		Exports: []bkclient.ExportEntry{
			{Type: bkclient.ExporterLocal, OutputDir: tmpDir},
		},
		LocalDirs: result.LocalDirs,
	}

	if err := solve(ctx, c, result, solveOpt); err != nil {
		return err
	}

	return CopyOutputs(tmpDir, outputs)
}

func solveDocker(ctx context.Context, c *bkclient.Client, result *compiler.Result, outputs []ExportedOutput) error {
	if len(outputs) == 0 {
		return solve(ctx, c, result, bkclient.SolveOpt{LocalDirs: result.LocalDirs})
	}

	out := outputs[0]
	if err := os.MkdirAll(filepath.Dir(out.DestPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(out.DestPath), err)
	}
	f, err := os.Create(out.DestPath)
	if err != nil {
		return fmt.Errorf("create %q: %w", out.DestPath, err)
	}

	solveOpt := bkclient.SolveOpt{
		Exports: []bkclient.ExportEntry{
			{
				Type: bkclient.ExporterOCI,
				Output: func(_ map[string]string) (io.WriteCloser, error) {
					return f, nil
				},
			},
		},
		LocalDirs: result.LocalDirs,
	}
	return solve(ctx, c, result, solveOpt)
}

func solve(ctx context.Context, c *bkclient.Client, result *compiler.Result, solveOpt bkclient.SolveOpt) error {
	ch := make(chan *bkclient.SolveStatus)
	done := make(chan struct{})
	go func() {
		defer close(done)
		printStatus(ch)
	}()

	_, err := c.Build(ctx, solveOpt, "", func(ctx context.Context, gwc gateway.Client) (*gateway.Result, error) {
		def, err := result.State.Marshal(ctx)
		if err != nil {
			return nil, fmt.Errorf("marshal LLB: %w", err)
		}
		return gwc.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
	}, ch)

	<-done
	return err
}

// CopyOutputs copies each declared output from exportDir to its host DestPath.
// Exported for use in tests.
func CopyOutputs(exportDir string, outputs []ExportedOutput) error {
	for _, out := range outputs {
		src := filepath.Join(exportDir, strings.TrimPrefix(out.SrcPath, "/"))
		if err := os.MkdirAll(filepath.Dir(out.DestPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(out.DestPath), err)
		}
		if err := copyPath(src, out.DestPath); err != nil {
			return fmt.Errorf("copy %q → %q: %w", out.SrcPath, out.DestPath, err)
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func printStatus(ch chan *bkclient.SolveStatus) {
	for status := range ch {
		for _, v := range status.Vertexes {
			switch {
			case v.Error != "":
				fmt.Printf("ERROR %s: %s\n", v.Name, v.Error)
			case v.Completed != nil:
				fmt.Printf("DONE  %s\n", v.Name)
			case v.Started != nil:
				fmt.Printf("=>    %s\n", v.Name)
			}
		}
		for _, log := range status.Logs {
			fmt.Printf("%s", log.Msg)
		}
	}
}
```

- [ ] **Run tests to confirm they pass**

```bash
go test ./pkg/runner/...
```

Expected:
```
ok      github.com/crikke/ci/pkg/runner         0.XXXs
```

- [ ] **Commit**

```bash
git add pkg/runner/
git commit -m "feat: implement buildkit runner with gateway solve and local/OCI export"
```

---

### Task 6: CLI entrypoint

**Files:**
- Create: `cmd/ci/main.go`

- [ ] **Write `cmd/ci/main.go`**

```go
package main

import (
	"context"
	"fmt"
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
	if len(args) < 2 || args[0] != "run" {
		return fmt.Errorf("usage: ci run <task> [<task>...]")
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

	host := os.Getenv("BUILDKIT_HOST")
	if host == "" {
		host = "unix:///run/buildkit/buildkitd.sock"
	}

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

		fmt.Printf("=> running task %q\n", taskName)
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

- [ ] **Build to confirm it compiles**

```bash
go build ./cmd/ci/...
```

Expected: no output (success). Binary at `./ci` or `$GOPATH/bin/ci`.

- [ ] **Run all tests**

```bash
go test ./...
```

Expected:
```
ok      github.com/crikke/ci/pkg/manifest       0.XXXs
ok      github.com/crikke/ci/pkg/compiler       0.XXXs
ok      github.com/crikke/ci/pkg/runner         0.XXXs
?       github.com/crikke/ci/cmd/ci             [no test files]
```

- [ ] **Commit**

```bash
git add cmd/ci/main.go
git commit -m "feat: add CLI entrypoint with task validation and output wiring"
```

---

### Task 7: Docker task support in compiler

The compiler currently only handles `exec` tasks. Docker tasks use BuildKit's `dockerfile.v0` frontend — they produce an OCI image, not a filesystem state. The runner already handles `TaskType == "docker"` via `solveDocker`.

What's missing: `Compile` needs to return a different kind of `Result` for docker tasks that tells the runner to use the dockerfile frontend with the compiled input states passed as named contexts.

**Files:**
- Modify: `pkg/compiler/compiler.go`
- Modify: `pkg/runner/runner.go`
- Modify: `pkg/compiler/compiler_test.go`

- [ ] **Write a failing test for docker task compilation in `pkg/compiler/compiler_test.go`**

Add to the existing test file:

```go
func TestCompile_docker_task(t *testing.T) {
	r := makeRestore()
	dockerTask := &manifest.Task{
		ID:         "build-image",
		Name:       "build-image",
		Type:       "docker",
		Dockerfile: "Dockerfile",
		Inputs: []manifest.TaskInput{
			{Task: r, Path: "/out", Dest: "/out"},
		},
	}
	m := &manifest.Manifest{
		Name:    "test",
		Image:   "ubuntu:24.04",
		AbsPath: "/test/module",
		Tasks: map[string]*manifest.Task{
			"restore":     r,
			"build-image": dockerTask,
		},
		Outputs: map[string]manifest.Output{},
	}

	result, err := compiler.Compile(m, "build-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskType != "docker" {
		t.Errorf("TaskType: got %q, want %q", result.TaskType, "docker")
	}
	if result.Dockerfile != "Dockerfile" {
		t.Errorf("Dockerfile: got %q, want %q", result.Dockerfile, "Dockerfile")
	}
	if len(result.DockerInputs) != 1 {
		t.Errorf("DockerInputs: got %d, want 1", len(result.DockerInputs))
	}
}
```

- [ ] **Run to confirm it fails**

```bash
go test ./pkg/compiler/...
```

Expected: compilation error — `result.Dockerfile` and `result.DockerInputs` undefined.

- [ ] **Extend `Result` in `pkg/compiler/compiler.go`**

Replace the `Result` struct and add docker handling in `Compile`:

```go
// Result holds the compiled LLB state and metadata needed by the runner.
type Result struct {
	// Exec tasks
	State    llb.State
	TaskType string            // "exec" or "docker"
	LocalDirs map[string]string

	// Docker tasks only
	Dockerfile   string
	DockerInputs []llb.State // compiled input states, passed as named gateway contexts
}
```

In `Compile`, after the topo-sort loop, handle the docker case:

```go
target := m.Tasks[targetTaskName]

if target.Type == "docker" {
	dockerInputs := make([]llb.State, len(target.Inputs))
	for i, inp := range target.Inputs {
		dockerInputs[i] = compiled[inp.Task.Name]
	}
	return &Result{
		TaskType:     "docker",
		Dockerfile:   target.Dockerfile,
		DockerInputs: dockerInputs,
		LocalDirs:    localDirs,
	}, nil
}

return &Result{
	State:     compiled[targetTaskName],
	TaskType:  "exec",
	LocalDirs: localDirs,
}, nil
```

- [ ] **Update `solveDocker` in `pkg/runner/runner.go` to pass docker inputs as named contexts**

Replace `solveDocker`:

```go
func solveDocker(ctx context.Context, c *bkclient.Client, result *compiler.Result, outputs []ExportedOutput) error {
	var ociOut io.WriteCloser
	exports := []bkclient.ExportEntry{}

	if len(outputs) > 0 {
		out := outputs[0]
		if err := os.MkdirAll(filepath.Dir(out.DestPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(out.DestPath), err)
		}
		f, err := os.Create(out.DestPath)
		if err != nil {
			return fmt.Errorf("create %q: %w", out.DestPath, err)
		}
		ociOut = f
		exports = append(exports, bkclient.ExportEntry{
			Type: bkclient.ExporterOCI,
			Output: func(_ map[string]string) (io.WriteCloser, error) {
				return ociOut, nil
			},
		})
	}

	solveOpt := bkclient.SolveOpt{
		Exports:   exports,
		LocalDirs: result.LocalDirs,
	}

	ch := make(chan *bkclient.SolveStatus)
	done := make(chan struct{})
	go func() {
		defer close(done)
		printStatus(ch)
	}()

	_, err := c.Build(ctx, solveOpt, "", func(ctx context.Context, gwc gateway.Client) (*gateway.Result, error) {
		frontendInputs := make(map[string]*pb.Definition, len(result.DockerInputs))
		for i, st := range result.DockerInputs {
			def, err := st.Marshal(ctx)
			if err != nil {
				return nil, fmt.Errorf("marshal docker input %d: %w", i, err)
			}
			frontendInputs[fmt.Sprintf("context%d", i)] = def.ToPB()
		}
		return gwc.Solve(ctx, gateway.SolveRequest{
			Frontend:       "dockerfile.v0",
			FrontendOpt:    map[string]string{"filename": result.Dockerfile},
			FrontendInputs: frontendInputs,
		})
	}, ch)

	<-done
	return err
}
```

Add the `pb` import in runner.go:
```go
pb "github.com/moby/buildkit/solver/pb"
```

- [ ] **Run all tests**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Commit**

```bash
git add pkg/compiler/ pkg/runner/
git commit -m "feat: add docker task support with dockerfile.v0 frontend and named context inputs"
```
