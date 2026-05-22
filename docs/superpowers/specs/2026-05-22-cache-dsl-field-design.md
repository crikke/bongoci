# CACHE DSL Field Design

## Overview

Add a `CACHE` boolean field to task definitions in the `.bongo` DSL. When not set it defaults to `true` (caching enabled). Setting `CACHE FALSE` instructs the compiler to pass `llb.IgnoreCache` to BuildKit, forcing that task step to always re-execute.

## DSL Syntax

```
TASK_NAME:
    CMD "some command"
    CACHE FALSE
```

- Value is an unquoted identifier: `TRUE` or `FALSE` (case-insensitive).
- Omitting `CACHE` is equivalent to `CACHE TRUE`.
- Only one `CACHE` directive per task is allowed; a duplicate is a parse error.

## Changes

### `pkg/manifest/types/types.go`

Add `Cache bool` to `Task`:

```go
type Task struct {
    Name             string
    Cache            bool
    Cmd              *string
    Dockerfile       *string
    DockerfileOutput *string
    Inputs           []Input
    Outputs          []Output
}
```

### `pkg/manifest/parser/parser.go`

In `parseTask()`, initialize `task.Cache = true` immediately after struct creation. Replace the broken stub with:

```go
case "CACHE":
    p.consume()
    tok, err := p.expect(IDENT)
    if err != nil {
        return nil, nil, err
    }
    switch strings.ToUpper(tok.Value) {
    case "TRUE":
        task.Cache = true
    case "FALSE":
        task.Cache = false
    default:
        return nil, nil, p.errorf(tok, "CACHE must be TRUE or FALSE, got %q", tok.Value)
    }
    if _, err := p.expect(NEWLINE); err != nil {
        return nil, nil, err
    }
```

### `pkg/compiler/compiler.go`

In `compileCmdTask` and `compileBuildahTask`, append `llb.IgnoreCache` when cache is disabled:

```go
if !task.Cache {
    opts = append(opts, llb.IgnoreCache)
}
```

### `cmd/bongo-ls/server.go`

Add to the task keyword completions map:

```go
"CACHE": "Whether to cache this task. Defaults to TRUE. Set to FALSE to always re-run.",
```

## Testing

- Parser test: `CACHE FALSE` sets `task.Cache = false`; omitting `CACHE` leaves it `true`; invalid value returns error.
- Compiler test: when `Cache = false`, `llb.IgnoreCache` appears in the compiled run options.
