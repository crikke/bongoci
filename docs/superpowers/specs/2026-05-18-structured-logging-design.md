# Structured Logging Design

**Date:** 2026-05-18  
**Scope:** `cmd/ci/main.go`, `pkg/compiler/compiler.go`, `pkg/runner/runner.go`

## Overview

Replace bare `fmt.Printf` log calls with `log/slog` structured logging. Add a `-v` / `--verbose` CLI flag to opt into debug-level output. No new dependencies — `log/slog` is stdlib since Go 1.21.

## Logger Setup (`main.go`)

Parse a `-v` / `--verbose` boolean flag with `flag.FlagSet` before the subcommand. Set the slog default logger once at startup:

```
ci [-v] run <task> [<task>...]
```

- Default level: `slog.LevelInfo`
- Verbose (`-v`): `slog.LevelDebug`
- Handler: `slog.NewTextHandler(os.Stderr, ...)`
- All log output goes to **stderr**; build output (`log.Data` from BuildKit) stays on **stdout**

## Log Sites

### `main.go`

| Level | Message | Fields |
|-------|---------|--------|
| Debug | manifest parsed | `path`, `tasks` (count) |
| Debug | buildkit host | `host` |
| Info  | running task (replaces existing `fmt.Printf`) | `task` |

### `compiler.go`

| Level | Message | Fields |
|-------|---------|--------|
| Debug | compiling task (replaces existing `fmt.Printf`) | `task` |
| Debug | topo sort order | `order` ([]string) |

### `runner.go`

| Level | Message | Fields |
|-------|---------|--------|
| Debug | connecting to buildkit | `host` |
| Debug | copying output | `src`, `dest` |
| Debug | vertex started (in `printStatus`) | `name` |
| Info  | vertex completed (in `printStatus`) | `name` |
| Error | vertex error (in `printStatus`) | `name`, `error` |

Raw build output (`log.Data` bytes) continues to write directly to stdout — it is container stdout/stderr, not a log message.

## Error Handling

No changes to error handling. Errors continue to propagate via return values. `slog.Error` is used only for BuildKit vertex errors surfaced through `printStatus`.

## Testing

No new tests required. Existing tests do not assert on log output. The change is purely additive (new log calls) plus a flag.
