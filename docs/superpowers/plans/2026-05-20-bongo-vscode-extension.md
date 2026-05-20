# Bongo VS Code Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a VS Code extension for `.bongo` files with syntax highlighting, snippets, inline diagnostics, and hover docs powered by a Go stdio language server (`bongo-ls`).

**Architecture:** Two components — (1) `cmd/bongo-ls`: a Go LSP server using `github.com/tliron/glsp` that re-uses the existing `pkg/manifest/parser` package for diagnostics and serves hover docs from a static keyword map; (2) `vscode-bongo`: a TypeScript VS Code extension that spawns `bongo-ls` and connects via stdio using `vscode-languageclient`. The server uses full text sync (simpler than incremental; correct for small `.bongo` files).

**Tech Stack:** Go + `github.com/tliron/glsp` (LSP server), TypeScript + `vscode-languageclient` v9 (VS Code client), TextMate grammar (syntax highlighting)

---

## File Map

| File | Status | Responsibility |
|---|---|---|
| `cmd/bongo-ls/server.go` | Create | In-memory doc store, helper functions, handlers |
| `cmd/bongo-ls/main.go` | Create | Entry point, glsp server wiring |
| `cmd/bongo-ls/server_test.go` | Create | Unit tests for helpers |
| `go.mod` / `go.sum` / `vendor/` | Modify | Add glsp dependency |
| `vscode-bongo/syntaxes/bongo.tmLanguage.json` | Create | TextMate grammar |
| `vscode-bongo/language-configuration.json` | Create | Comment toggle, word pattern, auto-close |
| `vscode-bongo/snippets/bongo.json` | Create | File/block/task/input/output snippets |
| `vscode-bongo/package.json` | Create | Extension manifest + contributions |
| `vscode-bongo/tsconfig.json` | Create | TypeScript compiler config |
| `vscode-bongo/src/extension.ts` | Create | Extension activate/deactivate, LSP client |

---

## Task 1: Add glsp dependency

**Files:**
- Modify: `go.mod`, `go.sum`, `vendor/`

- [ ] **Step 1: Fetch glsp and re-vendor**

```bash
go get -mod=mod github.com/tliron/glsp@latest && go mod tidy && go mod vendor
```

Expected: `go.mod` gains a `github.com/tliron/glsp` require line; `vendor/github.com/tliron/glsp/` directory appears.

- [ ] **Step 2: Verify existing code still builds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum vendor/
git commit -m "chore: add github.com/tliron/glsp for bongo-ls language server"
```

---

## Task 2: Write failing tests for server helpers

**Files:**
- Create: `cmd/bongo-ls/server_test.go`

- [ ] **Step 1: Create the test file**

```go
package main

import (
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestParseErrorToDiagnostic_wellFormed(t *testing.T) {
	d := parseErrorToDiagnostic(`build.bongo:3:5: unexpected token "FOO"`)
	if d == nil {
		t.Fatal("expected non-nil diagnostic")
	}
	if d.Range.Start.Line != 2 {
		t.Errorf("line: got %d want 2", d.Range.Start.Line)
	}
	if d.Range.Start.Character != 4 {
		t.Errorf("col: got %d want 4", d.Range.Start.Character)
	}
	if d.Message != `unexpected token "FOO"` {
		t.Errorf("message: got %q", d.Message)
	}
	if *d.Severity != protocol.DiagnosticSeverityError {
		t.Error("expected error severity")
	}
}

func TestParseErrorToDiagnostic_malformed(t *testing.T) {
	d := parseErrorToDiagnostic("something went wrong")
	if d == nil {
		t.Fatal("expected non-nil fallback diagnostic")
	}
	if d.Range.Start.Line != 0 {
		t.Errorf("expected line 0, got %d", d.Range.Start.Line)
	}
	if d.Message != "something went wrong" {
		t.Errorf("message: got %q", d.Message)
	}
}

func TestParseErrorToDiagnostic_messageWithColon(t *testing.T) {
	d := parseErrorToDiagnostic(`build.bongo:1:1: expected ":", got "="`)
	if d == nil {
		t.Fatal("expected non-nil diagnostic")
	}
	if d.Message != `expected ":", got "="` {
		t.Errorf("message: got %q", d.Message)
	}
}

func TestWordAtPosition_keyword(t *testing.T) {
	text := "BONGOVER = 1\nMODULE:\n\tNAME = \"foo\"\n"
	word := wordAtPosition(text, 0, 3)
	if word != "BONGOVER" {
		t.Errorf("got %q want %q", word, "BONGOVER")
	}
}

func TestWordAtPosition_midWord(t *testing.T) {
	text := "BASE_IMAGE = \"ubuntu\"\n"
	word := wordAtPosition(text, 0, 5)
	if word != "BASE_IMAGE" {
		t.Errorf("got %q want %q", word, "BASE_IMAGE")
	}
}

func TestWordAtPosition_outOfBounds(t *testing.T) {
	word := wordAtPosition("hello\n", 99, 0)
	if word != "" {
		t.Errorf("expected empty string, got %q", word)
	}
}

func TestWordAtPosition_emptyText(t *testing.T) {
	word := wordAtPosition("", 0, 0)
	if word != "" {
		t.Errorf("expected empty string, got %q", word)
	}
}

func TestWordAtPosition_colAtEnd(t *testing.T) {
	word := wordAtPosition("CMD\n", 0, 3) // col == len(line), past the word
	if word != "" {
		t.Errorf("expected empty string, got %q", word)
	}
}
```

- [ ] **Step 2: Run — expect compile failure**

```bash
go test ./cmd/bongo-ls/...
```

Expected: compilation error — `parseErrorToDiagnostic` and `wordAtPosition` undefined.

---

## Task 3: Implement server helpers

**Files:**
- Create: `cmd/bongo-ls/server.go`

- [ ] **Step 1: Create `cmd/bongo-ls/server.go`**

```go
package main

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/crikke/ci/pkg/manifest/parser"
)

var (
	docs   = make(map[protocol.DocumentUri]string)
	docsMu sync.Mutex
)

var keywords = map[string]string{
	"BONGOVER":   "Schema version declaration. Must be the first non-comment line. Current version: `1`.",
	"MODULE":     "Top-level block declaring module metadata. Required exactly once.",
	"NAME":       "Module name (string). Used as the build artifact identifier.",
	"BASE_IMAGE": "Docker image used as the execution environment for all tasks.",
	"INCLUDE":    "List of additional file paths to include in the build context.",
	"EXPORT":     "Declares which task outputs are exported from this module.",
	"CMD":        "Shell command to run inside the task container.",
	"DOCKERFILE": "Builds a Docker image from a Dockerfile. Arguments: `\"path/to/Dockerfile\" \"output/image.tar\"`.",
	"INPUT":      "Declares a dependency on another task's named output. Arguments: `TASK_NAME OUTPUT_NAME [\"dest\"]`.",
	"OUTPUT":     "Declares a named output artifact. Arguments: `\"name\" \"path\"`.",
}

// parseErrorToDiagnostic converts "filename:line:col: message" to an LSP Diagnostic.
// Falls back to line 0, col 0 if the string does not match the expected format.
func parseErrorToDiagnostic(errMsg string) *protocol.Diagnostic {
	parts := strings.SplitN(errMsg, ":", 4)
	sev := protocol.DiagnosticSeverityError
	fallback := &protocol.Diagnostic{
		Range:    protocol.Range{},
		Severity: &sev,
		Message:  errMsg,
	}
	if len(parts) < 4 {
		return fallback
	}
	lineNum, err1 := strconv.Atoi(strings.TrimSpace(parts[1]))
	colNum, err2 := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err1 != nil || err2 != nil {
		return fallback
	}
	line := lineNum - 1 // parser is 1-based; LSP is 0-based
	col := colNum - 1
	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}
	return &protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line), Character: uint32(col)},
			End:   protocol.Position{Line: uint32(line), Character: uint32(col + 1)},
		},
		Severity: &sev,
		Message:  strings.TrimSpace(parts[3]),
	}
}

// wordAtPosition returns the contiguous identifier word at (line, col) in text.
// line and col are 0-based.
func wordAtPosition(text string, line, col int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	lineText := lines[line]
	if col < 0 || col >= len(lineText) {
		return ""
	}
	start := col
	for start > 0 && isWordChar(rune(lineText[start-1])) {
		start--
	}
	end := col
	for end < len(lineText) && isWordChar(rune(lineText[end])) {
		end++
	}
	return lineText[start:end]
}

func isWordChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}

func uriToDir(uri protocol.DocumentUri) string {
	u, err := url.Parse(string(uri))
	if err != nil {
		return "."
	}
	return filepath.Dir(u.Path)
}

func publishDiagnostics(context *glsp.Context, uri protocol.DocumentUri, text string) {
	dir := uriToDir(uri)
	var diags []protocol.Diagnostic
	if _, err := parser.Parse(text, dir); err != nil {
		if d := parseErrorToDiagnostic(err.Error()); d != nil {
			diags = append(diags, *d)
		}
	}
	if diags == nil {
		diags = []protocol.Diagnostic{} // empty slice clears previous squiggles
	}
	_ = context.Notify("textDocument/publishDiagnostics", &protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}
```

- [ ] **Step 2: Run tests — expect pass**

```bash
go test ./cmd/bongo-ls/...
```

Expected: `PASS`

---

## Task 4: Implement main entry point and LSP handlers

**Files:**
- Create: `cmd/bongo-ls/main.go`

- [ ] **Step 1: Create `cmd/bongo-ls/main.go`**

```go
package main

import (
	"fmt"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspServer "github.com/tliron/glsp/server"
)

func main() {
	runServer()
}

func runServer() {
	handler := protocol.Handler{
		Initialize:            initialize,
		Initialized:           func(_ *glsp.Context, _ *protocol.InitializedParams) error { return nil },
		Shutdown:              func(_ *glsp.Context) error { return nil },
		TextDocumentDidOpen:   textDocumentDidOpen,
		TextDocumentDidChange: textDocumentDidChange,
		TextDocumentHover:     textDocumentHover,
	}
	server := glspServer.NewStdioServer(&handler)
	server.RunStdio()
}

func initialize(_ *glsp.Context, _ *protocol.InitializeParams) (any, error) {
	syncFull := protocol.TextDocumentSyncKindFull
	return protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: boolPtr(true),
				Change:    &syncFull,
			},
			HoverProvider: true,
		},
	}, nil
}

func boolPtr(b bool) *bool { return &b }

func textDocumentDidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	docsMu.Lock()
	docs[params.TextDocument.URI] = params.TextDocument.Text
	docsMu.Unlock()
	publishDiagnostics(context, params.TextDocument.URI, params.TextDocument.Text)
	return nil
}

func textDocumentDidChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// Full sync: each change contains the complete document text; last wins.
	change := params.ContentChanges[len(params.ContentChanges)-1]
	docsMu.Lock()
	docs[params.TextDocument.URI] = change.Text
	docsMu.Unlock()
	publishDiagnostics(context, params.TextDocument.URI, change.Text)
	return nil
}

func textDocumentHover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	docsMu.Lock()
	text, ok := docs[params.TextDocument.URI]
	docsMu.Unlock()
	if !ok {
		return nil, nil
	}
	word := wordAtPosition(text, int(params.Position.Line), int(params.Position.Character))
	desc, found := keywords[word]
	if !found {
		return nil, nil
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: fmt.Sprintf("**%s**\n\n%s", word, desc),
		},
	}, nil
}
```

> **Note on glsp API:** After running `go get`, verify: (1) `glspServer.NewStdioServer` accepts `*protocol.Handler` — if the signature differs, check `vendor/github.com/tliron/glsp/server/`. (2) `change.Text` — if `ContentChanges` is `[]interface{}`, type-assert each element as `protocol.TextDocumentContentChangeEvent`.

- [ ] **Step 2: Build the binary**

```bash
go build -o /tmp/bongo-ls ./cmd/bongo-ls/
```

Expected: binary at `/tmp/bongo-ls`, no errors.

- [ ] **Step 3: Run all tests**

```bash
go test ./cmd/bongo-ls/...
```

Expected: `PASS`

- [ ] **Step 4: Commit**

```bash
git add cmd/bongo-ls/
git commit -m "feat(bongo-ls): Go language server — diagnostics and hover"
```

---

## Task 5: TextMate grammar

**Files:**
- Create: `vscode-bongo/syntaxes/bongo.tmLanguage.json`

- [ ] **Step 1: Create the grammar file**

```json
{
    "$schema": "https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json",
    "name": "Bongo",
    "scopeName": "source.bongo",
    "patterns": [
        { "include": "#comment" },
        { "include": "#keyword" },
        { "include": "#string" },
        { "include": "#number" },
        { "include": "#task-name" },
        { "include": "#variable" }
    ],
    "repository": {
        "comment": {
            "name": "comment.line.bongo",
            "match": "#.*$"
        },
        "keyword": {
            "name": "keyword.control.bongo",
            "match": "\\b(BONGOVER|MODULE|NAME|BASE_IMAGE|INCLUDE|EXPORT|CMD|DOCKERFILE|INPUT|OUTPUT)\\b"
        },
        "string": {
            "name": "string.quoted.double.bongo",
            "begin": "\"",
            "end": "\"",
            "patterns": [
                { "name": "constant.character.escape.bongo", "match": "\\\\\"" }
            ]
        },
        "number": {
            "name": "constant.numeric.bongo",
            "match": "\\b[0-9]+\\b"
        },
        "task-name": {
            "name": "entity.name.function.bongo",
            "match": "^[A-Za-z_][A-Za-z0-9_-]*(?=\\s*:)"
        },
        "variable": {
            "name": "variable.other.bongo",
            "match": "\\b[A-Za-z_][A-Za-z0-9_-]*\\b"
        }
    }
}
```

The `task-name` rule is anchored to `^` so it only fires at the start of a line, avoiding false matches on `INPUT TASK output` where `TASK` is followed by a space, not a colon. `keyword` fires before `variable`, so known keywords are never captured by the variable scope.

---

## Task 6: Language configuration

**Files:**
- Create: `vscode-bongo/language-configuration.json`

- [ ] **Step 1: Create the configuration file**

```json
{
    "comments": {
        "lineComment": "#"
    },
    "brackets": [],
    "autoClosingPairs": [
        { "open": "\"", "close": "\"", "notIn": ["string"] }
    ],
    "wordPattern": "[A-Za-z_][A-Za-z0-9_-]*"
}
```

---

## Task 7: Snippets

**Files:**
- Create: `vscode-bongo/snippets/bongo.json`

- [ ] **Step 1: Create the snippets file**

```json
{
    "Bongo file skeleton": {
        "prefix": "bongo",
        "body": [
            "BONGOVER = 1",
            "",
            "MODULE:",
            "\tNAME = \"${1:module-name}\"",
            "\tBASE_IMAGE = \"${2:ubuntu:22.04}\"",
            "",
            "${3:BUILD}:",
            "\tCMD \"${4:echo hello}\""
        ],
        "description": "Full .bongo file skeleton"
    },
    "MODULE block": {
        "prefix": "module",
        "body": [
            "MODULE:",
            "\tNAME = \"${1:module-name}\"",
            "\tBASE_IMAGE = \"${2:ubuntu:22.04}\"",
            "\tEXPORT:",
            "\t\tINPUT ${3:TASK} ${4:output}"
        ],
        "description": "MODULE block with NAME, BASE_IMAGE, EXPORT"
    },
    "Task block": {
        "prefix": "task",
        "body": [
            "${1:TASK_NAME}:",
            "\tCMD \"${2:command}\""
        ],
        "description": "Named task block with CMD"
    },
    "INPUT line": {
        "prefix": "input",
        "body": "INPUT ${1:TASK} ${2:output} \"${3:dest}\"",
        "description": "INPUT dependency declaration"
    },
    "OUTPUT line": {
        "prefix": "output",
        "body": "OUTPUT \"${1:name}\" \"${2:path}\"",
        "description": "OUTPUT artifact declaration"
    }
}
```

---

## Task 8: VS Code extension client

**Files:**
- Create: `vscode-bongo/package.json`
- Create: `vscode-bongo/tsconfig.json`
- Create: `vscode-bongo/src/extension.ts`

- [ ] **Step 1: Create `vscode-bongo/package.json`**

```json
{
    "name": "vscode-bongo",
    "displayName": "Bongo",
    "description": "Language support for .bongo build files",
    "version": "0.0.1",
    "publisher": "bongoci",
    "engines": { "vscode": "^1.75.0" },
    "activationEvents": [],
    "main": "./out/extension.js",
    "contributes": {
        "languages": [
            {
                "id": "bongo",
                "extensions": [".bongo"],
                "configuration": "./language-configuration.json"
            }
        ],
        "grammars": [
            {
                "language": "bongo",
                "scopeName": "source.bongo",
                "path": "./syntaxes/bongo.tmLanguage.json"
            }
        ],
        "snippets": [
            {
                "language": "bongo",
                "path": "./snippets/bongo.json"
            }
        ],
        "configuration": {
            "title": "Bongo",
            "properties": {
                "bongo.serverPath": {
                    "type": "string",
                    "default": "",
                    "description": "Path to the bongo-ls binary. Leave empty to use PATH."
                }
            }
        }
    },
    "dependencies": {
        "vscode-languageclient": "^9.0.1"
    },
    "devDependencies": {
        "@types/vscode": "^1.75.0",
        "typescript": "^5.3.0"
    },
    "scripts": {
        "compile": "tsc -p ./",
        "watch": "tsc -watch -p ./"
    }
}
```

`activationEvents: []` — from VS Code 1.74+, `onLanguage:bongo` is inferred automatically from `contributes.languages`.

- [ ] **Step 2: Create `vscode-bongo/tsconfig.json`**

```json
{
    "compilerOptions": {
        "module": "commonjs",
        "target": "ES2020",
        "outDir": "out",
        "lib": ["ES2020"],
        "sourceMap": true,
        "rootDir": "src",
        "strict": true
    },
    "exclude": ["node_modules", ".vscode-test"]
}
```

- [ ] **Step 3: Create `vscode-bongo/src/extension.ts`**

```typescript
import { workspace, ExtensionContext } from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
} from 'vscode-languageclient/node';

let client: LanguageClient;

export function activate(context: ExtensionContext): void {
    const config = workspace.getConfiguration('bongo');
    let serverPath = config.get<string>('serverPath', '');
    if (!serverPath) {
        serverPath = 'bongo-ls';
    }

    const serverOptions: ServerOptions = {
        command: serverPath,
        args: [],
    };

    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: 'file', language: 'bongo' }],
    };

    client = new LanguageClient('bongo', 'Bongo Language Server', serverOptions, clientOptions);
    context.subscriptions.push(client);
    client.start();
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}
```

- [ ] **Step 4: Install dependencies and compile**

Run inside `vscode-bongo/`:

```bash
cd vscode-bongo && npm install && npm run compile
```

Expected: `out/extension.js` created, no TypeScript errors.

- [ ] **Step 5: Commit**

```bash
git add vscode-bongo/
git commit -m "feat(vscode-bongo): VS Code extension — syntax highlighting, snippets, LSP client"
```

---

## Task 9: End-to-end smoke test

- [ ] **Step 1: Install bongo-ls to PATH**

```bash
go build -o "$HOME/go/bin/bongo-ls" ./cmd/bongo-ls/
```

Expected: `~/go/bin/bongo-ls` created (ensure `~/go/bin` is on `$PATH`).

- [ ] **Step 2: Open Extension Development Host**

In VS Code, open the `vscode-bongo/` folder. Press `F5` (Run > Start Debugging). A new VS Code window opens with the extension loaded.

- [ ] **Step 3: Verify features in the Extension Development Host**

Open `build.bongo` from the repo root. Check:
- Keywords (`BONGOVER`, `MODULE`, `CMD`, etc.) are highlighted in the keyword colour.
- Strings are highlighted.
- `# this is a comment` is greyed out.
- `Ctrl+/` toggles `#` line comments.
- Hovering over `BONGOVER` shows a documentation popup with the keyword description.
- Typing `task` then `Tab` expands the task snippet.
- Introducing a syntax error (e.g. remove `:` after `BUILD`) shows a red squiggle on the affected line.

- [ ] **Step 4: Commit if cleanup was needed**

```bash
git add -u && git commit -m "chore: bongo VS Code extension smoke-tested"
```
