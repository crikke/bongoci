# Rootless BuildKit — Replace buildah with rootless BuildKit (Issue #31)

## Background

`bongoci` currently requires `sudo` (or privileged container access) because:

1. `pkg/buildenv/buildenv.go` starts the outer BuildKit daemon with `Privileged: true`.
2. `pkg/compiler/compiler.go` compiles `DOCKERFILE` tasks via `buildah --storage-driver=vfs build` inside an LLB step guarded by `llb.Security(pb.SecurityMode_INSECURE)`.

The `security.insecure` entitlement is the load-bearing reason for the privileged daemon. Eliminating it allows a switch to `moby/buildkit:rootless` with only Docker socket access (no `sudo`).

## Constraint

`DOCKERFILE` tasks remain containerized in their own LLB step. The shape — one `llb.Run` step that execs a builder tool inside its own image and produces an OCI tarball at `/out` — is intentional and does not change.

## Chosen approach — `buildctl-daemonless.sh`

Use `moby/buildkit:rootless` as the step image and invoke the bundled `buildctl-daemonless.sh` script. It starts an ephemeral `buildkitd` inside the LLB run step, runs one `buildctl build`, and exits. No `security.insecure` needed; requires `--oci-worker-no-process-sandbox` via `BUILDKITD_FLAGS` and unprivileged user namespaces on the host.

Host requirement: `kernel.unprivileged_userns_clone=1` (default on Fedora, Ubuntu ≥ 20.04, most modern distros).

## Changes

### `pkg/buildenv/buildenv.go`

The outer BuildKit daemon container switches from privileged mode to rootless mode.

- Image stays `moby/buildkit:rootless` (already set on branch).
- `Privileged: true` removed from `HostConfig` (already done on branch).
- `--allow-insecure-entitlement security.insecure` removed from `Cmd` (already done on branch).
- **Fix malformed `ContainerCreate`**: security opts (`seccomp=unconfined`, `apparmor=unconfined`, `systempaths=unconfined`) move from `Cmd` to `HostConfig.SecurityOpt`.
- `Cmd` corrected to: `["--addr", "unix:///run/user/1000/buildkit/buildkitd.sock"]`.
- Bind path corrected from `:/run/buildkit` to `:/run/user/1000/buildkit` (UID baked into rootless image).
- Remove unused `strconv` import (no longer calling `os.Getgid()`).

Socket on host remains `unix://` + tmpDir + `/buildkitd.sock` — no change to `socketHost` or `waitForBuildkitd`.

### `pkg/compiler/compiler.go`

The `DOCKERFILE` task compiler step replaces `buildah` with `buildctl-daemonless.sh`.

- Rename constant: `buildahImage` → `buildkitRootlessImage = "moby/buildkit:rootless"`.
- Rename function: `compileBuildahTask` → `compileDockerfileTask`.
- Drop `llb.Security(pb.SecurityMode_INSECURE)`.
- Add `llb.AddEnv("BUILDKITD_FLAGS", "--oci-worker-no-process-sandbox")` to run options.
- Replace buildah shell command with:
  ```
  buildctl-daemonless.sh build
    --frontend dockerfile.v0
    --local context=<absPath>
    --local dockerfile=<absPath>
    --opt filename=<task.Dockerfile>
    --output type=oci,dest=<outFile>
  ```
- Remove `github.com/moby/buildkit/solver/pb` import (was only used for `SecurityMode_INSECURE`).

Output format stays OCI archive — no downstream changes required.

### `pkg/compiler/compiler_test.go`

- Update `TestCompile_docker_task`: assert the marshalled LLB ops use `buildkitRootlessImage` and contain no `SecurityMode_INSECURE` exec security entry.

### `wiki/Getting-Started.md`

- Remove: "Permission to run privileged containers (BuildKit needs `--privileged` and `security.insecure`...)".
- Add: "Membership in the `docker` group (or equivalent socket access). No `sudo` or privileged containers required."

## Acceptance criteria (from issue)

- `ci run` against a manifest with a `DOCKERFILE` task succeeds without `sudo` on a host where the user is in the `docker` group.
- No reference to `buildah` or `security.insecure` remains in `pkg/`.
- Existing tests under `pkg/compiler` and `pkg/buildenv` pass; updated test covers the `DOCKERFILE` step shape.
- Getting Started docs updated.

## Out of scope

- Kaniko fallback (separate issue if nested userns proves problematic on a target host).
- Translating Dockerfiles into native LLB ops.
- Multi-arch builds.
