# `ci` CLI Reference

```
ci [flags] run <task> [<task>...]
```

`ci` searches upward from the current directory for `build.bongo` and runs the named tasks one after another against a buildkitd. Source: `cmd/ci/main.go`.

## Flags

| Flag | Default | Effect |
| --- | --- | --- |
| `-v`, `--verbose` | `false` | Sets the slog level to `debug` |
| `--use-host-buildkit-daemon` | `false` | Skip launching a containerised buildkitd; connect to `$BUILDKIT_HOST` (or `unix:///run/buildkit/buildkitd.sock` if unset) |
| `--cache-from <ref>` | `""` | Registry ref to import/export the BuildKit cache. Must contain a `/` (e.g. `localhost:5000/buildcache`). See [[Caching]] |
| `--cache-insecure` | `false` | Allow plain HTTP for `--cache-from` (needed for local insecure registries) |

## Positional arguments

`run` is required and must be followed by at least one task name. Unknown task names are rejected with the list of available tasks.

```sh
ci run BUILD TEST DOCKER_BUILD
```

## Exit behaviour

- `0` on success
- `1` and a message on `stderr` for any parse, compile, or solve error
- Errors from buildkitd's cache-export step are downgraded to a warning when the actual build solved successfully (the artifact is still produced)

## Build environment

Unless `--use-host-buildkit-daemon` is set, `ci`:

1. Creates a Docker bridge network (internal, no outbound)
2. Starts a `moby/buildkit:latest` container with a shared `tmpdir` mount for the socket
3. Waits up to 2 minutes for buildkitd to respond to `Info`
4. Tears the container and network down on exit (or any failure during start)

See `pkg/buildenv/buildenv.go`.

## Output layout

For each task in `MODULE.EXPORT`, the matching task output gets copied to:

```
./out/<task>/<output_name>/<basename(path)>
```

The runner first writes the exporter contents to a temp directory, then copies into place under `./out/`.
