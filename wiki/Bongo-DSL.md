# The .bongo DSL

`build.bongo` files are parsed by `pkg/manifest/parser`. The language is indentation-sensitive (like Python), with `#` line comments and double-quoted strings.

## File shape

```bongo
BONGOVER = 1

MODULE:
    NAME = "ci"
    BASE_IMAGE = "golang:1.25"
    INCLUDE
        "../shared/"
    EXPORT:
        INPUT DOCKER_BUILD TARBALL
        INPUT TEST RESULT

INSTALL_DEPS:
    CMD "npm install"
    CACHE FALSE
    OUTPUT "MODULES" "./node_modules"

BUILD:
    INPUT INSTALL_DEPS MODULES "./node_modules"
    CMD "npm run build"

DOCKER_BUILD:
    DOCKERFILE "./test.Dockerfile" "./image.tar"
    OUTPUT "TARBALL" "./image.tar"
```

## Lexical rules

| | |
| --- | --- |
| Comments | `# to end of line` |
| Strings | `"..."` with `\\` and `\"` escapes |
| Indentation | Spaces or tabs (tab = 4 spaces); blocks open after `:` / `INCLUDE` |
| Identifiers | `[A-Za-z_][A-Za-z0-9_-]*` |
| Statements end | Newline |

## `BONGOVER`

Must be the first non-comment line.

```bongo
BONGOVER = 1
```

## `MODULE:`

Required exactly once. Allowed children:

- `NAME = "..."` – required, module name
- `BASE_IMAGE = "..."` – required, image used as the base for every `CMD` task
- `INCLUDE` – followed by an indented block of string paths; each is resolved relative to the manifest's directory and mounted read-only into every task at its absolute host path
- `ENV KEY "value"` – module-wide environment variable; tasks can override per key
- `EXPORT:` – block of `INPUT <TASK> <OUTPUT>` lines naming the artifacts to copy back to the host after a build

## Tasks

A task is `TASK_NAME:` followed by an indented body. Allowed statements:

| Statement | Notes |
| --- | --- |
| `CMD "..."` | Shell command run as `/bin/sh -c "..."` in the base image |
| `DOCKERFILE "path" "out.tar"` | Build a Docker image and emit an OCI archive at `/out/<basename(out.tar)>`. Mutually exclusive with `CMD` |
| `OUTPUT "NAME" "path"` | Name an artifact path inside the container so downstream tasks (and `EXPORT`) can refer to it |
| `INPUT UPSTREAM OUTPUT "dest"` | Mount/copy `UPSTREAM`'s `OUTPUT` to `dest` in this task. `dest` is optional |
| `CACHE TRUE` / `CACHE FALSE` | Defaults to `TRUE`. `FALSE` adds `llb.IgnoreCache` so the step always re-runs |
| `ENV KEY "value"` | Per-task env var; overrides the module-level `KEY` if set |

Either `CMD` or `DOCKERFILE` is required. A task must not declare both.

## `EXPORT`

```bongo
EXPORT:
    INPUT DOCKER_BUILD TARBALL
    INPUT TEST RESULT
```

Each `INPUT <task> <output>` declares an output to materialise on the host after the build. The runner writes them to `./out/<task>/<output>/<basename(path)>` relative to the working directory.

## `INCLUDE`

```bongo
INCLUDE
    "../another_module/"
```

Each path is made absolute relative to the manifest's directory, then mounted read-only at that same absolute path inside every task. Useful for sibling modules referenced by build scripts.

## Validation done at parse time

- `MODULE.NAME` and `MODULE.BASE_IMAGE` must be present
- Duplicate task names are rejected
- `INPUT` references must resolve to a task that exists
- `EXPORT` references must resolve to a task and one of its declared `OUTPUT` names
- The task graph must be acyclic
- Indentation must be consistent
- Duplicate `ENV` keys within a single module or task are rejected

Parse errors are formatted `build.bongo:<line>:<col>: <message>`; the LSP turns these into squiggles (see [[VS Code Extension]]).
