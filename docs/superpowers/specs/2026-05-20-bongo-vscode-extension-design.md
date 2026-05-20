# VS Code Extension for .bongo DSL — Design Spec

**Date:** 2026-05-20
**Status:** Approved

---

## Overview

A VS Code extension that provides first-class editor support for `.bongo` build manifest files. Features: syntax highlighting, snippets, inline diagnostics, and hover documentation.

The extension is a thin TypeScript client that delegates language intelligence to a Go language server (`bongo-ls`) which re-uses the existing `pkg/manifest/parser` package.

---

## Repository Layout

```
bongoci/
├── cmd/
│   ├── ci/               (existing)
│   └── bongo-ls/         (new — Go language server)
│       └── main.go
├── pkg/
│   └── manifest/parser/  (existing — reused as-is)
└── vscode-bongo/         (new — VS Code extension)
    ├── package.json
    ├── language-configuration.json
    ├── syntaxes/
    │   └── bongo.tmLanguage.json
    ├── snippets/
    │   └── bongo.json
    └── src/
        └── extension.ts
```

---

## TextMate Grammar

File: `vscode-bongo/syntaxes/bongo.tmLanguage.json`

| Scope | Pattern |
|---|---|
| `keyword.control.bongo` | `BONGOVER`, `MODULE`, `NAME`, `BASE_IMAGE`, `INCLUDE`, `EXPORT`, `CMD`, `DOCKERFILE`, `INPUT`, `OUTPUT` (word-boundary match) |
| `entity.name.function.bongo` | Any identifier immediately followed by `:` (task and block names, e.g. `BUILD:`, `TEST:`) |
| `string.quoted.double.bongo` | `"..."` with `\"` escape support |
| `constant.numeric.bongo` | Integer literals `[0-9]+` |
| `comment.line.bongo` | `#` to end of line |
| `variable.other.bongo` | Bare identifiers used as references (task names in `INPUT` lines, output names) — matched after keyword rules so known keywords are not captured by this scope |

---

## Language Configuration

File: `vscode-bongo/language-configuration.json`

- Line comment character: `#` (enables `Ctrl+/` toggle)
- Auto-closing pairs: `"` → `"`
- Word pattern: `[A-Za-z_][A-Za-z0-9_-]*`

---

## Snippets

File: `vscode-bongo/snippets/bongo.json`

| Prefix | Description |
|---|---|
| `bongo` | Full file skeleton: `BONGOVER = 1`, `MODULE:` block, one starter task |
| `module` | `MODULE:` block with `NAME`, `BASE_IMAGE`, `EXPORT:` sub-block |
| `task` | Named task block with `CMD` line |
| `input` | `INPUT <TASK> <OUTPUT> "dest"` line |
| `output` | `OUTPUT "name" "path"` line |

Tab stops navigate between placeholders in natural fill-in order.

---

## Go Language Server (`bongo-ls`)

### Transport

Stdio-based JSON-RPC 2.0 using [`github.com/tliron/glsp`](https://github.com/tliron/glsp).

### LSP Methods

| Method | Behaviour |
|---|---|
| `initialize` | Declares capabilities: `textDocumentSync` (incremental), `hoverProvider: true`, `diagnosticProvider` (via push) |
| `textDocument/didOpen` | Stores document content, parses it, publishes diagnostics |
| `textDocument/didChange` | Updates stored content, re-parses, republishes diagnostics |
| `textDocument/hover` | Identifies the word under the cursor, looks it up in a static keyword→markdown map, returns a `MarkupContent` response |
| `shutdown` / `exit` | Clean process shutdown |

### Diagnostics

`parser.Parse()` returns errors formatted as `"file:line:col: message"`. The server parses this string into an LSP `Diagnostic` with `severity: Error` and the exact line/column range, then sends `textDocument/publishDiagnostics` to the client.

On a successful parse (no error) the server sends an empty diagnostics array to clear any previous errors.

### Hover Documentation

A static `map[string]string` in the server source maps each keyword to a short markdown description:

| Keyword | Description |
|---|---|
| `BONGOVER` | Schema version declaration. Must be the first non-comment line. Current version: `1`. |
| `MODULE` | Top-level block declaring module metadata. Required exactly once. |
| `NAME` | Module name (string). Used as the build artifact identifier. |
| `BASE_IMAGE` | Docker image used as the execution environment for all tasks. |
| `INCLUDE` | List of additional file paths to include in the build context. |
| `EXPORT` | Declares which task outputs are exported from this module. |
| `CMD` | Shell command to run inside the task container. |
| `DOCKERFILE` | Builds a Docker image from a Dockerfile. Arguments: `"path/to/Dockerfile" "output/image.tar"`. |
| `INPUT` | Declares a dependency on another task's named output. Arguments: `TASK_NAME OUTPUT_NAME ["dest"]`. |
| `OUTPUT` | Declares a named output artifact. Arguments: `"name" "path"`. |

The server identifies the hovered word by scanning the raw text of the hovered line at the given character offset.

### In-memory State

The server maintains `map[documentURI]string` of open document text, updated on each `didChange` notification.

---

## TypeScript VS Code Extension Client

### Activation

Activates on `onLanguage:bongo` (triggered when any `*.bongo` file is opened).

### `extension.ts` behaviour

1. Resolves the `bongo-ls` binary path: checks the `bongo.serverPath` user setting first, then falls back to `which bongo-ls` / PATH lookup.
2. Spawns `bongo-ls` as a child process with stdio transport using `vscode-languageclient/node`.
3. Registers `deactivate()` to stop the language client and kill the server process.

### `package.json` contributions

| Key | Value |
|---|---|
| `languages` | `bongo`, file extension `*.bongo` |
| `grammars` | TextMate grammar for `bongo` |
| `snippets` | Snippets for `bongo` |
| `configuration` | `bongo.serverPath` — string setting, default `""`, description: "Path to the bongo-ls binary. Leave empty to use PATH." |

---

## Build & Development Workflow

- `bongo-ls` is built with `go build ./cmd/bongo-ls` and placed on PATH (or pointed to via `bongo.serverPath`).
- The VS Code extension is developed with `npm install && npm run compile` inside `vscode-bongo/`.
- During development, open `vscode-bongo/` as the workspace root and press `F5` to launch an Extension Development Host with the extension loaded.
- For distribution, `vsce package` produces a `.vsix` installable via `code --install-extension bongo.vsix`.

---

## Out of Scope

- Marketplace publishing
- Go-to-definition / find-references
- Auto-completion (snippets handle the common cases)
- Semantic tokens
- Formatting
