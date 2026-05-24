# Caching

bongoci uses two layers of caching, both inherited from BuildKit.

## In-process layer cache (default)

Every task step is an LLB exec. BuildKit hashes each step's inputs and reuses cached results across `ci run` invocations against the same buildkitd. To force a step to always re-run, set `CACHE FALSE`:

```bongo
INSTALL_DEPS:
    CMD "npm install"
    CACHE FALSE
```

This adds `llb.IgnoreCache` to the exec op (see `pkg/compiler/compiler.go`).

Note: in the default mode, `ci` starts a fresh buildkitd container per invocation. That means the in-process cache does **not** persist across runs unless you either:

- Use `--use-host-buildkit-daemon` so the buildkitd's state survives, **or**
- Configure `--cache-from` to push/pull cache from a registry (see below)

## Registry cache (`--cache-from`)

```sh
ci --cache-from localhost:5000/buildcache --cache-insecure run BUILD
```

When `--cache-from` is set, the runner configures both `CacheImports` and `CacheExports` against the same registry ref, with `mode=max` for the export side. This is BuildKit's standard [registry cache backend](https://github.com/moby/buildkit#registry-pushpull-backend).

Constraints:

- The ref must contain a `/`. A bare `localhost:5000` would be resolved to `docker.io/library/localhost:5000` by the reference parser, so the runner rejects it.
- `--cache-insecure` adds `registry.insecure=true` to both import and export attrs. Use this for plain-HTTP local registries.
- If the actual build solves but the cache export step fails (e.g. registry unreachable), the runner logs a warning and treats the build as successful. The artifact in `./out/` is still produced.

## Choosing a strategy

| Scenario | Recommendation |
| --- | --- |
| One-shot local builds | Defaults are fine; rebuilds are cheap |
| Iterative local development | `--use-host-buildkit-daemon` so the in-process cache persists across invocations |
| Distributed CI | `--cache-from <internal-registry>/buildcache` so workers share each other's layers |
