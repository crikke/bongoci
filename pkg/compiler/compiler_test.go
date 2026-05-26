package compiler_test

import (
	"context"
	"testing"

	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/moby/buildkit/solver/pb"
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
	_, err := compiler.Compile(m, "nonexistent","")
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
}

func TestCompile_single_exec_task(t *testing.T) {
	m := testManifest()
	result, err := compiler.Compile(m, "restore","")
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
	result, err := compiler.Compile(m, "compile","")
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
	result, err := compiler.Compile(m, "restore","")
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
	result, err := compiler.Compile(m, "build","")
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

func TestCompile_task_env_overrides_module_env(t *testing.T) {
	task := &manifest.Task{
		Name:  "build",
		Cache: true,
		Cmd:   strPtr("make"),
		Env:   map[string]string{"LOG_LEVEL": "debug", "TASK_ONLY": "1"},
	}
	m := &manifest.Manifest{
		AbsPath: "/test/module",
		Module: manifest.Module{
			BaseImage: "ubuntu:24.04",
			Env:       map[string]string{"LOG_LEVEL": "info", "MODULE_ONLY": "yes"},
		},
		Tasks: map[string]*manifest.Task{"build": task},
	}
	result, err := compiler.Compile(m, "build","")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def, err := result.State.Marshal(context.Background())
	if err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}

	var execEnv []string
	for _, raw := range def.Def {
		var op pb.Op
		if err := op.UnmarshalVT(raw); err != nil {
			t.Fatalf("UnmarshalVT op: %v", err)
		}
		if exec := op.GetExec(); exec != nil && exec.Meta != nil {
			execEnv = append(execEnv, exec.Meta.Env...)
		}
	}
	want := map[string]string{
		"LOG_LEVEL":   "debug", // task overrides module
		"TASK_ONLY":   "1",
		"MODULE_ONLY": "yes",
	}
	got := envSliceToMap(execEnv)
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			t.Errorf("env[%q]: got %q (present=%v), want %q", k, gv, ok, v)
		}
	}
}

func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string)
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
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

	result, err := compiler.Compile(m, "build-image","")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err = result.State.Marshal(context.Background()); err != nil {
		t.Fatalf("State.Marshal: %v", err)
	}
}
