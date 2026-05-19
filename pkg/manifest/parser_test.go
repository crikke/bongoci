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

func TestParseContent_unknown_output_task(t *testing.T) {
	const toml = `
version = 1
[module]
name = "m"
image = "ubuntu:24.04"

[tasks.compile]
cmd = "compile"

[outputs.bin]
dest = "./out/bin"
[outputs.bin.from]
task = "nonexistent"
path = "/bin"
`
	_, err := manifest.ParseContent(toml, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown output task, got nil")
	}
}
