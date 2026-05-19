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
