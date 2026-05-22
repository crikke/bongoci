package compiler_test

import (
	"context"
	"testing"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
)

func strPtr(s string) *string { return &s }

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

func testManifest() *manifest.Manifest {
	r := makeRestore()
	c := makeCompile(r)
	return &manifest.Manifest{
		AbsPath: "/test/module",
		Module: manifest.Module{
			BaseImage: "ubuntu:24.04",
		},
		Tasks: map[string]*manifest.Task{
			"restore": r,
			"compile": c,
		},
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
	m.Module.Include = []string{"/other/module"}
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
		Module: manifest.Module{
			BaseImage: "ubuntu:24.04",
		},
		Tasks: map[string]*manifest.Task{
			"restore":     r,
			"build-image": dockerTask,
		},
	}

	result, err := compiler.Compile(m, "build-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err = result.State.Marshal(context.Background()); err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
}
