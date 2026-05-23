# Signal Handling — Graceful Shutdown Design

**Date:** 2026-05-24
**Issue:** #22
**Branch:** 22-cli-handle-sigintsigterm-for-graceful-shutdown

## Problem

`cmd/ci/main.go` does not propagate SIGINT/SIGTERM to in-flight work. The current code has a partial, incomplete implementation:

- A manual `sigc` channel and `signal.Notify` goroutine exist inside the `if !*useHostBuildkitDaemon` block only.
- The goroutine calls `startCancel()`, which only cancels the 2-minute `buildenv.Start` timeout context — not the main context.
- `runner.Run` receives the raw `context.Background()`, so it is never cancelled by a signal.
- The goroutine has a `// ... do something ...` placeholder, confirming it is unfinished.
- If the user presses Ctrl+C mid-build, `env.Close()` is reached only if the process exits cleanly — a forced kill leaves Docker networks and buildkitd containers orphaned.

## Decision

Use `signal.NotifyContext` (stdlib, Go 1.16+) to create a single root context cancelled on SIGINT or SIGTERM. All downstream operations derive from this context.

## Context Lifecycle

```
signal.NotifyContext(ctx, SIGINT, SIGTERM)   ← root ctx, cancelled on signal
│
├── context.WithTimeout(ctx, 2min)           ← startCtx, for buildenv.Start only
│
└── runner.Run(ctx, ...)                     ← root ctx, cancelled by signal mid-build
```

- `defer stop()` releases the signal goroutine when `run()` returns normally.
- `defer env.Close()` uses `context.Background()` internally (existing behaviour) and runs regardless of context state — cleanup is guaranteed.
- Signal handling is unconditional: it covers both the managed-daemon path and `--use-host-buildkit-daemon`.

## Behaviour on Signal

| Phase | Signal arrives | Result |
|---|---|---|
| During `buildenv.Start` | SIGINT/SIGTERM | `startCtx` cancelled (root ctx cancelled too); `Start` returns error; `env.Close()` skipped (env was never returned); process exits |
| During `runner.Run` | SIGINT/SIGTERM | Root `ctx` cancelled; BuildKit aborts solve; `env.Close()` runs via defer; Docker network and buildkitd container removed |
| Between tasks (multiple tasks queued) | SIGINT/SIGTERM | Root `ctx` cancelled; next `runner.Run` call returns immediately with context error; `env.Close()` runs via defer |

## Changes Required

**`cmd/ci/main.go` only.** No changes to `buildenv` or `runner` — both already accept and respect `context.Context`.

1. Replace `ctx := context.Background()` with `signal.NotifyContext`.
2. Add `defer stop()`.
3. Remove the manual `sigc` channel, `signal.Notify` call, and goroutine.
4. Keep `startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)` and `defer startCancel()`, but now derive from the signal-aware root ctx.
5. Replace `runner.Run(ctx, ...)` argument — `ctx` is already the right variable name; the fix is that it now carries cancellation.
6. Remove the now-unused `time` import if applicable (it's still used for `2*time.Minute`, so it stays).

## Out of Scope

- Timeout for graceful shutdown (BuildKit cancellation is near-instant).
- Logging the signal source (not necessary for the fix).
- Changes to `buildenv.Close` or `runner.Run` signatures.
