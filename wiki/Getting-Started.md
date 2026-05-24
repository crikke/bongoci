# Getting Started

## Prerequisites

- **Go 1.25** (matches `go.mod`)
- **Docker daemon** reachable through the standard env (`DOCKER_HOST` or the default socket). `ci` uses the Docker API to spin up a `moby/buildkit` container for each invocation, unless `--use-host-buildkit-daemon` is passed.
- Permission to run privileged containers (BuildKit needs `--privileged` and `security.insecure` to run `buildah`-backed `DOCKERFILE` tasks).

## Build the binaries

```sh
go build -o ./out/ci        ./cmd/ci
go build -o ./out/bongo-ls  ./cmd/bongo-ls
```

The release pipeline (see [[Releases]]) ships prebuilt Linux amd64/arm64 tarballs.

## Run your first task

Create a `build.bongo` in your repo root:

```bongo
BONGOVER = 1

MODULE:
    NAME = "demo"
    BASE_IMAGE = "alpine:3.20"

HELLO:
    CMD "echo hi > /out/greeting.txt"
    OUTPUT GREETING "/out/greeting.txt"
```

Then:

```sh
ci run HELLO
```

`ci` walks up from the current directory looking for `build.bongo`, parses it, starts a buildkitd container, compiles the task graph, and copies declared outputs to `./out/<task>/<output_name>/<basename>` on the host.

Useful flags:

| Flag | Effect |
| --- | --- |
| `-v` / `--verbose` | Debug-level slog output |
| `--use-host-buildkit-daemon` | Skip the managed container; use `BUILDKIT_HOST` (or `unix:///run/buildkit/buildkitd.sock`) |
| `--cache-from <ref>` | Import/export BuildKit cache via a registry. See [[Caching]] |
| `--cache-insecure` | Allow plain-HTTP for `--cache-from` (local registries) |

## `.bongoignore`

Files matching patterns in `.bongoignore` (or `.dockerignore` as a fallback) are excluded from the build context that gets mounted into tasks. Same syntax as `.dockerignore`.
