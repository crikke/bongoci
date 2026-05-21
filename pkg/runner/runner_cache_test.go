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
