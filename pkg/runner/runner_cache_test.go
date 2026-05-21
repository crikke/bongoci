package runner

import (
	"testing"

	bkclient "github.com/moby/buildkit/client"
)

func TestWithCacheOpt_empty(t *testing.T) {
	opt, err := withCacheOpt(bkclient.SolveOpt{}, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opt.CacheImports) != 0 {
		t.Errorf("expected no CacheImports, got %d", len(opt.CacheImports))
	}
}

func TestWithCacheOpt_withRef(t *testing.T) {
	opt, err := withCacheOpt(bkclient.SolveOpt{}, "myregistry/cache", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	opt, err := withCacheOpt(bkclient.SolveOpt{}, "localhost:5000/cache", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	opt, err := withCacheOpt(opt, "myregistry/cache", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

func TestWithCacheOpt_rejectsRefWithoutRepoPath(t *testing.T) {
	for _, ref := range []string{"localhost:5000", "registry.example.com", "myregistry:8080"} {
		_, err := withCacheOpt(bkclient.SolveOpt{}, ref, false)
		if err == nil {
			t.Errorf("ref %q: expected error for missing repository path, got nil", ref)
		}
	}
}

func TestWithCacheOpt_insecureSetsAttrOnBoth(t *testing.T) {
	opt, err := withCacheOpt(bkclient.SolveOpt{}, "host.docker.internal:5000/buildcache", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opt.CacheImports[0].Attrs["registry.insecure"] != "true" {
		t.Errorf("CacheImports[0].Attrs[registry.insecure]: got %q, want %q", opt.CacheImports[0].Attrs["registry.insecure"], "true")
	}
	if opt.CacheExports[0].Attrs["registry.insecure"] != "true" {
		t.Errorf("CacheExports[0].Attrs[registry.insecure]: got %q, want %q", opt.CacheExports[0].Attrs["registry.insecure"], "true")
	}
}

func TestWithCacheOpt_secureHasNoInsecureAttr(t *testing.T) {
	opt, err := withCacheOpt(bkclient.SolveOpt{}, "myregistry/cache", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := opt.CacheImports[0].Attrs["registry.insecure"]; ok {
		t.Error("expected no registry.insecure attr for secure registry on CacheImports")
	}
	if _, ok := opt.CacheExports[0].Attrs["registry.insecure"]; ok {
		t.Error("expected no registry.insecure attr for secure registry on CacheExports")
	}
}

func TestOnlyCacheVerticesFailed_cacheExportOnly(t *testing.T) {
	if !onlyCacheVerticesFailed([]string{"exporting cache to registry"}) {
		t.Error("expected true for cache-export-only failure")
	}
}

func TestOnlyCacheVerticesFailed_buildError(t *testing.T) {
	if onlyCacheVerticesFailed([]string{"running /bin/sh -c go build"}) {
		t.Error("expected false for build vertex failure")
	}
}

func TestOnlyCacheVerticesFailed_mixed(t *testing.T) {
	if onlyCacheVerticesFailed([]string{"exporting cache to registry", "running /bin/sh -c go build"}) {
		t.Error("expected false when build vertex also failed")
	}
}

func TestOnlyCacheVerticesFailed_empty(t *testing.T) {
	if onlyCacheVerticesFailed(nil) {
		t.Error("expected false for empty slice")
	}
}
