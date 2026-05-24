# Architecture

A `ci run <task>` invocation moves through four packages:

```
build.bongo ──► pkg/manifest ──► pkg/compiler ──► pkg/runner ──► buildkitd
                  (parser)         (LLB graph)     (solve+export)
                                                       │
                                          pkg/buildenv │ (managed buildkitd container)
                                                       ▼
                                                  ./out/<task>/<output>/...
```

## 1. `pkg/manifest` – parsing

`manifest.Parse(path)` reads the file and hands it to `parser.Parse(src, dir)`. The parser is a small lexer (`lexer.go`) feeding a recursive-descent parser (`parser.go`):

- The lexer is indentation-aware: it tracks a stack of indent levels and emits synthetic `INDENT` / `DEDENT` tokens (Python-style).
- The parser runs two passes. Pass 1 builds tasks; `INPUT` references are stored as a raw `(task, output, dest)` triple. Pass 2 resolves those to `*Task` pointers so cycle detection and topological sort can walk the graph by pointer.
- Validation: required module fields, duplicate task/env keys, unknown input/export references, and DAG acyclicity.

The data model lives in `pkg/manifest/types/types.go` and is aliased by `pkg/manifest/manifest.go` so consumers can keep using `manifest.Task` etc.

## 2. `pkg/compiler` – manifest → LLB

`compiler.Compile(m, targetTaskName)`:

1. **Topo-sort** the subgraph reachable from `targetTaskName`.
2. **Mount the context**: the manifest's directory is added as `llb.Local("context")` and mounted at its absolute host path. Patterns from `.bongoignore` (falling back to `.dockerignore`) are passed as `ExcludePatterns`.
3. **Mount `INCLUDE` deps**: each dep gets a `dep-N` local and is mounted at its absolute host path inside the container.
4. **Per task**, in topological order:
   - For `CMD` tasks: start from `MODULE.BASE_IMAGE`, layer in upstream `INPUT` artifacts via `llb.Copy(...)`, apply env vars, then `Run("/bin/sh", "-c", task.Cmd)`. `CACHE FALSE` adds `llb.IgnoreCache`.
   - For `DOCKERFILE` tasks: start from `quay.io/buildah/stable`, run `buildah build` then `buildah push oci-archive://...` with `security.insecure`, and treat `/out` as the task's artifact mount. The downstream `INPUT` reads from this `/out` state.
5. **Build the export state**: a `Scratch` accumulating all `MODULE.EXPORT` outputs. If the target task isn't in the export list, the full target state is copied in as well so the runner still has something to export.

Result is `{ State llb.State, LocalDirs map[string]string }`.

## 3. `pkg/buildenv` – buildkitd container

`buildenv.Start(ctx)` creates an isolated environment for the build:

- A dedicated internal Docker bridge network
- A privileged `moby/buildkit:latest` container with `--allow-insecure-entitlement security.insecure` and `--group $(id -g)`
- The buildkit socket is bind-mounted out via a temp directory; the host points at `unix://<tmp>/buildkitd.sock`
- `waitForBuildkitd` polls `client.Info` until ready
- `Close()` stops the container, removes the network, and cleans the tmpdir

If `--use-host-buildkit-daemon` is set, this step is skipped entirely.

## 4. `pkg/runner` – solve and export

`runner.Run(ctx, opts, result, outputs)`:

- Connects to buildkitd at `opts.Host`
- Builds `SolveOpt` with `ExporterLocal` writing to a temp dir, plus any registry cache import/export (see [[Caching]])
- Streams `*bkclient.SolveStatus` updates to `progressui` for the human-readable progress display, while also recording any failed vertex names
- After `Build` returns, `CopyOutputs(tmpDir, outputs)` walks each `ExportedOutput` and copies it to its `./out/...` destination

A subtlety in error handling: if every failed vertex name contains "exporting cache", the build artifact was produced even though cache export to the registry failed. The runner downgrades the error to a warning. See `onlyCacheVerticesFailed` in `pkg/runner/runner.go`.
