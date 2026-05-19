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
	if _, err = result.State.Marshal(context.Background()); err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
}
