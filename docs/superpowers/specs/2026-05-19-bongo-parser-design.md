# .bongo DSL Parser Design

**Date:** 2026-05-19  
**Scope:** `pkg/manifest/` and `pkg/manifest/parser/` only. Breakage in `pkg/runner/` and `pkg/compiler/` from the data model change is out of scope and will be fixed separately.

---

## Goal

Replace the existing TOML-based manifest parser with a hand-written lexer + recursive descent parser for the `.bongo` DSL. The data model is redesigned to match the DSL's semantics — task-level named outputs replace the old top-level `[outputs]` map, and `EXPORT:` declares which task outputs are written back to the host.

---

## DSL Reference

```
# comment
BONGOVER = 1

MODULE:
    NAME =       "ci"
    BASE_IMAGE = "golang:1.25"

    INCLUDE
        "../another_module/"
        "../and_another_module/"

    EXPORT:
        INPUT DOCKER_BUILD DOCKERFILE
        INPUT TEST RESULT

INSTALL_DEPS:
    CMD "npm install"
    OUTPUT MODULES "./node_modules"

BUILD:
    INPUT INSTALL_DEPS MODULES "./node_modules"
    CMD "npm build"

TEST:
    INPUT INSTALL_DEPS MODULES "./node_modules"
    OUTPUT RESULT "./test-result.xml"
    CMD "npm test -o ./test-result.xml"

DOCKER_BUILD:
    DOCKERFILE "./test.Dockerfile"
    OUTPUT DOCKERFILE DOCKERFILE

BLACKDUCK_IMAGE_SCAN:
    INPUT DOCKER_BUILD DOCKERFILE "/mnt/dockerfile.tar"
    CMD "blackduck run ..."
```

### Grammar (informal)

```
file          → "BONGOVER" "=" INT  module_block  task_block*
module_block  → "MODULE" ":" INDENT module_stmt* DEDENT
module_stmt   → name_stmt | image_stmt | include_block | export_block
name_stmt     → "NAME" "=" STRING NEWLINE
image_stmt    → "BASE_IMAGE" "=" STRING NEWLINE
include_block → "INCLUDE" NEWLINE INDENT STRING+ DEDENT
export_block  → "EXPORT" ":" INDENT ("INPUT" IDENT IDENT NEWLINE)+ DEDENT
task_block    → IDENT ":" INDENT task_stmt* DEDENT
task_stmt     → cmd_stmt | output_stmt | input_stmt | dockerfile_stmt
cmd_stmt      → "CMD" STRING NEWLINE
output_stmt   → "OUTPUT" IDENT (STRING | IDENT) NEWLINE
input_stmt    → "INPUT" IDENT IDENT STRING? NEWLINE
dockerfile_stmt → "DOCKERFILE" STRING NEWLINE
```

---

## Data Model

Replaces all types in `pkg/manifest/manifest.go`. The `BurntSushi/toml` dependency and all `toml*` wire types are removed.

```go
// Manifest is the parsed representation of a build.bongo file.
type Manifest struct {
    AbsPath string
    Version int
    Module  Module
    Tasks   map[string]*Task
}

// Module holds module-level metadata.
type Module struct {
    Name      string
    BaseImage string
    Include   []string // dependency paths, resolved to absolute at parse time
    Exports   []Export // task outputs to be written back to the host after the build
}

// Export references a named output from a task that should be
// materialized on the host filesystem after the build completes.
type Export struct {
    TaskName   string
    OutputName string
}

// Task is a single build step.
type Task struct {
    Name       string
    Cmd        string
    Dockerfile string
    Inputs     []Input
    Outputs    []Output
}

// Input wires a named output from an upstream task into this task.
type Input struct {
    Task       *Task  // resolved pointer (nil until pass 2)
    OutputName string
    Dest       string // mount destination inside the container
}

// Output is a named artifact produced by a task.
type Output struct {
    Name string
    Path string
}
```

---

## Architecture

### Package layout

```
pkg/manifest/
  manifest.go        — data model (Manifest, Module, Task, Input, Output, Export)
  parse.go           — Parse(path string) and ParseContent(src, dir string) entry points
  parse_test.go      — integration tests for full .bongo documents
  parser/
    lexer.go         — Lexer, Token, TokenType
    parser.go        — Parse(r io.Reader, dir string) (*manifest.Manifest, error)
    lexer_test.go    — lexer unit tests
    parser_test.go   — parser unit tests
```

The old `parser.go` (TOML) and `parser_test.go` are replaced. `pkg/manifest/parser/bongo_parser.go` (currently a stub) is replaced by the real implementation files.

### Lexer (`parser/lexer.go`)

Converts raw bytes into a flat token stream. Key decisions:

- **Indentation as tokens.** The lexer maintains a stack of indent levels. When a line's leading whitespace depth increases, it emits one `INDENT`. When it decreases, it emits one `DEDENT` per level popped. The parser never counts spaces.
- **Blank lines and comments skipped.** Lines that are empty or start with `#` (after stripping leading whitespace) produce no tokens.
- **Token types:**

| Type    | Example                   |
|---------|---------------------------|
| IDENT   | `MODULE`, `CMD`, `BUILD`  |
| STRING  | `"npm install"`           |
| INT     | `1`                       |
| EQUALS  | `=`                       |
| COLON   | `:`                       |
| NEWLINE | end of logical line       |
| INDENT  | synthetic                 |
| DEDENT  | synthetic                 |
| EOF     | end of file               |

- Every token carries `Line` and `Col` for error messages.
- The lexer exposes `Next() Token` (consumes) and is driven entirely by the parser.

### Parser (`parser/parser.go`)

Recursive descent. Maintains one token of lookahead (`peek`). Two helpers:

- `consume() Token` — advance and return current token
- `expect(typ TokenType) Token` — consume or return a parse error

**Two-pass resolution** (same pattern as the old TOML parser):

1. Parse all task blocks into `map[string]*Task` with `Input.Task == nil`.
2. Walk all inputs, look up `Task` by name, set the pointer; error on unknown task names.
3. Run cycle detection (reuse existing `checkCycles` logic verbatim).

**Exports are validated** in pass 2: each `Export` must reference a task name that exists and an output name that exists on that task.

**Error format:**
```
build.bongo:14:5: expected CMD, INPUT, OUTPUT, or DOCKERFILE; got "FOOBAZ"
```

### Entry points (`parse.go`)

```go
// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error)

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error)
```

These replace the existing `Parse` / `ParseContent` in the old `parser.go`.

---

## Testing

- `parser/lexer_test.go` — unit tests for individual token sequences (indent/dedent stack, string escapes, comment skipping)
- `parser/parser_test.go` — unit tests for individual grammar rules
- `pkg/manifest/parse_test.go` — integration tests for full `.bongo` documents, covering:
  - Basic module + single task
  - Multiple tasks with INPUT/OUTPUT wiring
  - INCLUDE (relative and absolute paths)
  - EXPORT block (host output materialization)
  - DOCKERFILE task
  - Error cases: missing NAME, missing BASE_IMAGE, unknown input task, unknown export task/output, cycle, bad indent

---

## What is explicitly out of scope

- `pkg/runner/` and `pkg/compiler/` — will break due to data model change; fixed separately by the developer
- Cross-module input resolution (linking inputs across module boundaries) — deferred
- DSL features beyond what is in `build.bongo` today (env vars, conditionals, new task types)
- Keeping `build.toml` / TOML support
