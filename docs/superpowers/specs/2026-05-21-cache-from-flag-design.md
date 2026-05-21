# Design: `--cache-from` flag for build cache import

**Date:** 2026-05-21

## Summary

Add a `--cache-from=<ref>` CLI flag that, when provided, imports a BuildKit registry cache into the solve. The flag value flows through a new `RunOptions` struct in the `runner` package, which replaces the bare `host string` parameter on `runner.Run`.

## Changes

### `pkg/runner/runner.go`

Add a `RunOptions` struct that bundles runner configuration:

```go
type RunOptions struct {
    Host      string // buildkitd address
    CacheFrom string // registry ref for cache import; empty = disabled
}
```

Update `Run` signature:
```go
func Run(ctx context.Context, opts RunOptions, result *compiler.Result, outputs []ExportedOutput) error
```

Update `solveExec` to accept and thread `RunOptions` down to `withCacheOpt`.

Update `withCacheOpt` signature to `withCacheOpt(opt bkclient.SolveOpt, cacheFrom string) bkclient.SolveOpt`:
- If `cacheFrom` is non-empty, append `bkclient.CacheOptionsEntry{Type: "registry", Attrs: map[string]string{"ref": cacheFrom}}` to `opt.CacheImports`.
- Return `opt` in all cases (removes the current dead-code path where `cacheOpt` is built but never applied).

### `cmd/ci/main.go`

Add flag:
```go
cacheFrom := fs.String("cache-from", "", "registry ref to import build cache from")
```

Build options and pass to runner:
```go
opts := runner.RunOptions{Host: host, CacheFrom: *cacheFrom}
runner.Run(ctx, opts, result, taskOutputs)
```

## Behaviour

- `--cache-from` is optional; omitting it leaves `CacheImports` empty (current behaviour preserved).
- Only registry-type cache import is supported. The `ref` attribute maps directly to the BuildKit registry cache importer.
- No cache export is added by this change; the existing (unused) export code is removed.

## Out of scope

- Cache export (`--cache-to`)
- Multiple `--cache-from` values
