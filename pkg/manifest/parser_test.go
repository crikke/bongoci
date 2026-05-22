package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crikke/ci/pkg/manifest"
)

func TestParseContent_basic(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "test-module"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "make build"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Module.Name != "test-module" {
		t.Errorf("Name: got %q, want %q", m.Module.Name, "test-module")
	}
	if m.Module.BaseImage != "ubuntu:24.04" {
		t.Errorf("BaseImage: got %q, want %q", m.Module.BaseImage, "ubuntu:24.04")
	}
	if m.AbsPath != "/some/dir" {
		t.Errorf("AbsPath: got %q, want %q", m.AbsPath, "/some/dir")
	}
	if len(m.Tasks) != 1 {
		t.Fatalf("Tasks: got %d, want 1", len(m.Tasks))
	}
	task, ok := m.Tasks["BUILD"]
	if !ok {
		t.Fatal("task 'BUILD' not found")
	}
	if *task.Cmd != "make build" {
		t.Errorf("task.Cmd: got %q, want %q", *task.Cmd, "make build")
	}
}

func TestParseContent_inputs_outputs(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

RESTORE:
    CMD "dotnet restore"
    OUTPUT "PACKAGES" "./packages"

COMPILE:
    INPUT RESTORE PACKAGES "/packages"
    CMD "dotnet publish"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	restore, ok := m.Tasks["RESTORE"]
	if !ok {
		t.Fatal("task 'RESTORE' not found")
	}
	if len(restore.Outputs) != 1 || restore.Outputs[0].Name != "PACKAGES" || restore.Outputs[0].Path != "./packages" {
		t.Errorf("RESTORE.Outputs: got %+v", restore.Outputs)
	}
	compile, ok := m.Tasks["COMPILE"]
	if !ok {
		t.Fatal("task 'COMPILE' not found")
	}
	if len(compile.Inputs) != 1 {
		t.Fatalf("COMPILE.Inputs: got %d, want 1", len(compile.Inputs))
	}
	inp := compile.Inputs[0]
	if inp.Task.Name != "RESTORE" {
		t.Errorf("inp.Task.Name: got %q, want %q", inp.Task.Name, "RESTORE")
	}
	if inp.OutputName != "PACKAGES" {
		t.Errorf("inp.OutputName: got %q, want %q", inp.OutputName, "PACKAGES")
	}
	if inp.Dest != "/packages" {
		t.Errorf("inp.Dest: got %q, want %q", inp.Dest, "/packages")
	}
}

func TestParseContent_include(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    INCLUDE
        "../other"
`
	m, err := manifest.ParseContent(src, "/home/user/module")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Module.Include) != 1 {
		t.Fatalf("Include: got %d, want 1", len(m.Module.Include))
	}
	want := filepath.Clean("/home/user/other")
	if m.Module.Include[0] != want {
		t.Errorf("Include[0]: got %q, want %q", m.Module.Include[0], want)
	}
}

func TestParseContent_export(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT BUILD ARTIFACT

BUILD:
    CMD "make"
    OUTPUT "ARTIFACT" "./bin/app"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Module.Exports) != 1 {
		t.Fatalf("Exports: got %d, want 1", len(m.Module.Exports))
	}
	exp := m.Module.Exports[0]
	if exp.TaskName != "BUILD" || exp.OutputName != "ARTIFACT" {
		t.Errorf("Export: got %+v", exp)
	}
}

func TestParseContent_dockerfile_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD_IMAGE:
    DOCKERFILE "./Dockerfile" "/out/image.tar"
    OUTPUT "IMAGE" "/out/image.tar"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task, ok := m.Tasks["BUILD_IMAGE"]
	if !ok {
		t.Fatal("task 'BUILD_IMAGE' not found")
	}
	if task.Dockerfile == nil || *task.Dockerfile != "./Dockerfile" {
		t.Errorf("Dockerfile: got %q, want %q", *task.Dockerfile, "./Dockerfile")
	}
	if task.DockerfileOutput == nil || *task.DockerfileOutput != "/out/image.tar" {
		t.Errorf("DockerfileOutput: got %v, want %q", task.DockerfileOutput, "/out/image.tar")
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Name != "IMAGE" || task.Outputs[0].Path != "/out/image.tar" {
		t.Errorf("Outputs: got %+v", task.Outputs)
	}
}

func TestParseContent_missing_name(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    BASE_IMAGE = "ubuntu:24.04"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing MODULE.NAME, got nil")
	}
}

func TestParseContent_missing_base_image(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing MODULE.BASE_IMAGE, got nil")
	}
}

func TestParseContent_unknown_input_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

COMPILE:
    INPUT NONEXISTENT PACKAGES "/packages"
    CMD "compile"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown input task, got nil")
	}
}

func TestParseContent_cycle(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

A:
    OUTPUT "OUT" "./out"
    INPUT B OUT "/out"
    CMD "a"

B:
    OUTPUT "OUT" "./out"
    INPUT A OUT "/out"
    CMD "b"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
}

func TestParseContent_unknown_export_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT NONEXISTENT ARTIFACT

BUILD:
    CMD "make"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown export task, got nil")
	}
}

func TestParseContent_unknown_export_output(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT BUILD NONEXISTENT

BUILD:
    CMD "make"
    OUTPUT "ARTIFACT" "./bin"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown export output name, got nil")
	}
}

func TestParseContent_comment_ignored(t *testing.T) {
	const src = `
# top-level comment
BONGOVER = 1
MODULE:
    NAME = "m" # inline comment
    BASE_IMAGE = "ubuntu:24.04"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseContent_version(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("Version: got %d, want 1", m.Version)
	}
}

func TestParseContent_duplicate_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "first"

BUILD:
    CMD "second"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for duplicate task name, got nil")
	}
}

// ensure ParseContent error message contains line info
func TestParseContent_error_has_line(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    BADKEYWORD "x"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), ":") {
		t.Errorf("error message has no line info: %v", err)
	}
}

func toPtr(str string) *string {
	return &str
}

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
