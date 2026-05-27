# `ci` CLI Reference

```
ci [global flags] <subcommand> [flags] [args]
```

`ci` searches upward from the current directory for `build.bongo` and runs the named tasks one after another against a buildkitd. Source: `cmd/ci/main.go`.

## Subcommands

| Subcommand | Description |
| --- | --- |
| `run <task> [<task>...]` | Run one or more tasks from `build.bongo` |
| `list` | List available tasks (stub, not yet implemented) |
| `validate` | Validate `build.bongo` without running (stub, not yet implemented) |
| `init` | Initialise a new `build.bongo` file (stub, not yet implemented) |

## Global flags

Available to all subcommands.

| Flag | Default | Effect |
| --- | --- | --- |
| `-v`, `--verbose` | `false` | Sets the slog level to `debug` |

## `run` subcommand

```
ci run [flags] <task> [<task>...]
```

At least one task name is required. Unknown task names are rejected with the list of available tasks.

```sh
ci run BUILD TEST DOCKER_BUILD
```

### `run` flags

| Flag | Default | Effect |
| --- | --- | --- |
| `--use-host-buildkit-daemon` | `false` | Skip launching a containerised buildkitd; connect to `$BUILDKIT_HOST` (or `unix:///run/buildkit/buildkitd.sock` if unset) |
| `--cache-from <ref>` | `""` | Registry ref to import/export the BuildKit cache. Must contain a `/` (e.g. `localhost:5000/buildcache`). See [[Caching]] |
| `--cache-insecure` | `false` | Allow plain HTTP for `--cache-from` (needed for local insecure registries) |
| `--buildkit-image <image>` | `moby/buildkit:v0.29.0-ubuntu` | Use a different buildkit image |
| `--buildah-image <image>` | `quay.io/buildah/stable:v1.43.1` | Use a different buildah image |

## Exit behaviour

- `0` on success
- `1` and a message on `stderr` for any parse, compile, or solve error
- Errors from buildkitd's cache-export step are downgraded to a warning when the actual build solved successfully (the artifact is still produced)

## Build environment

Unless `--use-host-buildkit-daemon` is set, `ci`:

1. Creates a Docker bridge network (internal, no outbound)
2. Starts a `moby/buildkit:latest` container, privileged, with `security.insecure` allowed and a shared `tmpdir` mount for the socket
3. Waits up to 2 minutes for buildkitd to respond to `Info`
4. Tears the container and network down on exit (or any failure during start)

See `pkg/buildenv/buildenv.go`.

## Output layout

For each task in `MODULE.EXPORT`, the matching task output gets copied to:

```
./out/<task>/<output_name>/<basename(path)>
```

The runner first writes the exporter contents to a temp directory, then copies into place under `./out/`.
