# Cobra CLI Design

**Date:** 2026-05-27  
**Issue:** [#34 — Add spf13/cobra](https://github.com/crikke/bongoci/issues/34)

## Summary

Migrate `cmd/ci` from the standard `flag` package to [spf13/cobra](https://github.com/spf13/cobra) and add three stub subcommands (`list`, `validate`, `init`) that will be implemented later. The existing `run` command behaviour is preserved exactly.

## Approach

Option A: Cobra wrapper in `cmd/ci`. Single binary, single package. The existing core logic (compile → buildenv → runner) is untouched; only the flag-parsing wiring changes.

## File Structure

```
cmd/ci/
  main.go       — entry point; builds root command, registers subcommands, executes
  run.go        — run subcommand; flags + core logic (extracted from current main.go)
  list.go       — list stub
  validate.go   — validate stub
  init.go       — init stub
```

All files share `package main`.

## Commands

### Root: `ci`

- Shows help when called with no subcommand (Cobra default).
- Persistent flag: `--verbose` / `-v` (bool, default false) — sets slog level to debug. Moved from `run` to root so all future subcommands inherit it.

### `ci run <task> [<task>...]`

Identical behaviour to the current CLI. Flags move from `flag.FlagSet` to `cobra.Command.Flags()`:

| Flag | Default | Notes |
|------|---------|-------|
| `--use-host-buildkit-daemon` | false | Connect to host buildkitd instead of starting one |
| `--cache-from <ref>` | `""` | Registry ref for BuildKit cache import/export |
| `--cache-insecure` | false | Allow plain-HTTP registry for `--cache-from` |
| `--buildkit-image <image>` | `moby/buildkit:v0.29.0-ubuntu` | Override buildkit image |
| `--buildah-image <image>` | `quay.io/buildah/stable:v1.43.1` | Override buildah image |

Helper functions `findBuildToml` and `mustCwd` move to `run.go`.

### `ci list`

Stub. Prints `"not implemented"` and exits 0. No flags, no args.

### `ci validate`

Stub. Prints `"not implemented"` and exits 0. No flags, no args.

### `ci init`

Stub. Prints `"not implemented"` and exits 0. No flags, no args.

## Dependencies

Add `github.com/spf13/cobra` to `go.mod` and `vendor/`.

## Slog Initialization

`--verbose` is a persistent flag on the root command. Slog level is set inside a `PersistentPreRunE` on the root command, which runs before every subcommand. This replaces the current per-command slog setup in `run()`.

## Error Handling

Cobra's default error handling (`SilenceUsage: true` on the root command is recommended so usage isn't printed on runtime errors) combined with the existing `fmt.Fprintln(os.Stderr, err)` + `os.Exit(1)` pattern in `main.go`.

## Testing

No new tests required for this change. The existing tests in `pkg/` are not affected. The `run` command logic is unchanged, so behaviour is preserved.
