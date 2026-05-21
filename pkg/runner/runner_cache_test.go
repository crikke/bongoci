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

func TestWithCacheOpt_frontendAttrsNotNil(t *testing.T) {
	// buildkit's solve.go does maps.Clone(opt.FrontendAttrs) then maps.Copy into it;
	// if FrontendAttrs is nil the clone is nil and Copy panics when CacheImports are set.
	opt := withCacheOpt(bkclient.SolveOpt{}, "localhost:5000/cache")
	if opt.FrontendAttrs == nil {
		t.Error("FrontendAttrs must be non-nil when CacheImports are set")
	}
}

func TestWithCacheOpt_appendsToExisting(t *testing.T) {
	existing := bkclient.CacheOptionsEntry{
		Type:  "registry",
		Attrs: map[string]string{"ref": "existing/cache"},
	}
	opt := bkclient.SolveOpt{
		CacheImports: []bkclient.CacheOptionsEntry{existing},
	}
	opt = withCacheOpt(opt, "myregistry/cache")
	if len(opt.CacheImports) != 2 {
		t.Fatalf("expected 2 CacheImports, got %d", len(opt.CacheImports))
	}
	if opt.CacheImports[0].Attrs["ref"] != "existing/cache" {
		t.Errorf("first entry: got %q, want %q", opt.CacheImports[0].Attrs["ref"], "existing/cache")
	}
	if opt.CacheImports[1].Attrs["ref"] != "myregistry/cache" {
		t.Errorf("second entry: got %q, want %q", opt.CacheImports[1].Attrs["ref"], "myregistry/cache")
	}
}
