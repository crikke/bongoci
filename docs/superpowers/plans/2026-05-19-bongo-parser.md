# .bongo DSL Parser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the TOML-based manifest parser with a hand-written lexer + recursive descent parser for the `.bongo` DSL, with a redesigned data model.

**Architecture:** A `Tokenize` function in `pkg/manifest/parser/lexer.go` converts `.bongo` source into a flat `[]Token` slice with synthetic `INDENT`/`DEDENT` tokens for indentation. A recursive descent `Parse` function in `pkg/manifest/parser/parser.go` walks the token slice and builds a `*manifest.Manifest` in two passes (create tasks, then wire inputs). Thin entry points in `pkg/manifest/parse.go` expose `Parse`/`ParseContent` to the rest of the codebase.

**Tech Stack:** Go standard library only — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-19-bongo-parser-design.md`

**Scope:** `pkg/manifest/` and `pkg/manifest/parser/` only. `pkg/runner/` and `pkg/compiler/` will break due to data model changes; that is out of scope and will be fixed separately.

---

### Task 1: New data model, stub entry points, failing integration tests

**Files:**
- Modify: `pkg/manifest/manifest.go`
- Delete: `pkg/manifest/parser.go`
- Create: `pkg/manifest/parse.go`
- Modify: `pkg/manifest/parser_test.go`

- [ ] **Step 1: Replace `pkg/manifest/manifest.go` with the new data model**

```go
package manifest

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
	Task       *Task
	OutputName string
	Dest       string // mount destination inside the container
}

// Output is a named artifact produced by a task.
type Output struct {
	Name string
	Path string
}
```

- [ ] **Step 2: Delete `pkg/manifest/parser.go`**

```bash
rm pkg/manifest/parser.go
```

- [ ] **Step 3: Create stub `pkg/manifest/parse.go`**

```go
package manifest

import "fmt"

// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	return nil, fmt.Errorf("not implemented")
}

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Verify the package compiles**

```bash
go build ./pkg/manifest/...
```

Expected: no errors (the stub satisfies the interface, the parser/ sub-package still has bongo_parser.go declaring `package parser`).

- [ ] **Step 5: Replace `pkg/manifest/parser_test.go` with bongo-syntax integration tests**

```go
package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crikke/ci/pkg/manifest"
)

func TestParseContent_basic(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "test-module"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    CMD "make build"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Module.Name != "test-module" {
		t.Errorf("Name: got %q, want %q", m.Module.Name, "test-module")
	}
	if m.Module.BaseImage != "ubuntu:24.04" {
		t.Errorf("BaseImage: got %q, want %q", m.Module.BaseImage, "ubuntu:24.04")
	}
	if m.AbsPath != "/some/dir" {
		t.Errorf("AbsPath: got %q, want %q", m.AbsPath, "/some/dir")
	}
	if len(m.Tasks) != 1 {
		t.Fatalf("Tasks: got %d, want 1", len(m.Tasks))
	}
	task, ok := m.Tasks["BUILD"]
	if !ok {
		t.Fatal("task 'BUILD' not found")
	}
	if task.Cmd != "make build" {
		t.Errorf("task.Cmd: got %q, want %q", task.Cmd, "make build")
	}
}

func TestParseContent_inputs_outputs(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

RESTORE:
    CMD "dotnet restore"
    OUTPUT PACKAGES "./packages"

COMPILE:
    INPUT RESTORE PACKAGES "/packages"
    CMD "dotnet publish"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	restore, ok := m.Tasks["RESTORE"]
	if !ok {
		t.Fatal("task 'RESTORE' not found")
	}
	if len(restore.Outputs) != 1 || restore.Outputs[0].Name != "PACKAGES" || restore.Outputs[0].Path != "./packages" {
		t.Errorf("RESTORE.Outputs: got %+v", restore.Outputs)
	}
	compile, ok := m.Tasks["COMPILE"]
	if !ok {
		t.Fatal("task 'COMPILE' not found")
	}
	if len(compile.Inputs) != 1 {
		t.Fatalf("COMPILE.Inputs: got %d, want 1", len(compile.Inputs))
	}
	inp := compile.Inputs[0]
	if inp.Task.Name != "RESTORE" {
		t.Errorf("inp.Task.Name: got %q, want %q", inp.Task.Name, "RESTORE")
	}
	if inp.OutputName != "PACKAGES" {
		t.Errorf("inp.OutputName: got %q, want %q", inp.OutputName, "PACKAGES")
	}
	if inp.Dest != "/packages" {
		t.Errorf("inp.Dest: got %q, want %q", inp.Dest, "/packages")
	}
}

func TestParseContent_include(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    INCLUDE
        "../other"
`
	m, err := manifest.ParseContent(src, "/home/user/module")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Module.Include) != 1 {
		t.Fatalf("Include: got %d, want 1", len(m.Module.Include))
	}
	want := filepath.Clean("/home/user/other")
	if m.Module.Include[0] != want {
		t.Errorf("Include[0]: got %q, want %q", m.Module.Include[0], want)
	}
}

func TestParseContent_export(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT BUILD ARTIFACT

BUILD:
    CMD "make"
    OUTPUT ARTIFACT "./bin/app"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Module.Exports) != 1 {
		t.Fatalf("Exports: got %d, want 1", len(m.Module.Exports))
	}
	exp := m.Module.Exports[0]
	if exp.TaskName != "BUILD" || exp.OutputName != "ARTIFACT" {
		t.Errorf("Export: got %+v", exp)
	}
}

func TestParseContent_dockerfile_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD_IMAGE:
    DOCKERFILE "./Dockerfile"
    OUTPUT IMAGE IMAGE
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task, ok := m.Tasks["BUILD_IMAGE"]
	if !ok {
		t.Fatal("task 'BUILD_IMAGE' not found")
	}
	if task.Dockerfile != "./Dockerfile" {
		t.Errorf("Dockerfile: got %q, want %q", task.Dockerfile, "./Dockerfile")
	}
	if len(task.Outputs) != 1 || task.Outputs[0].Name != "IMAGE" {
		t.Errorf("Outputs: got %+v", task.Outputs)
	}
}

func TestParseContent_missing_name(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    BASE_IMAGE = "ubuntu:24.04"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing MODULE.NAME, got nil")
	}
}

func TestParseContent_missing_base_image(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for missing MODULE.BASE_IMAGE, got nil")
	}
}

func TestParseContent_unknown_input_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

COMPILE:
    INPUT NONEXISTENT PACKAGES "/packages"
    CMD "compile"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown input task, got nil")
	}
}

func TestParseContent_cycle(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

A:
    OUTPUT OUT "./out"
    INPUT B OUT "/out"
    CMD "a"

B:
    OUTPUT OUT "./out"
    INPUT A OUT "/out"
    CMD "b"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
}

func TestParseContent_unknown_export_task(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT NONEXISTENT ARTIFACT

BUILD:
    CMD "make"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown export task, got nil")
	}
}

func TestParseContent_unknown_export_output(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
    EXPORT:
        INPUT BUILD NONEXISTENT

BUILD:
    CMD "make"
    OUTPUT ARTIFACT "./bin"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected error for unknown export output name, got nil")
	}
}

func TestParseContent_comment_ignored(t *testing.T) {
	const src = `
# top-level comment
BONGOVER = 1
MODULE:
    NAME = "m" # inline comment
    BASE_IMAGE = "ubuntu:24.04"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseContent_version(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"
`
	m, err := manifest.ParseContent(src, "/some/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("Version: got %d, want 1", m.Version)
	}
}

// ensure ParseContent error message contains line info
func TestParseContent_error_has_line(t *testing.T) {
	const src = `
BONGOVER = 1
MODULE:
    NAME = "m"
    BASE_IMAGE = "ubuntu:24.04"

BUILD:
    BADKEYWORD "x"
`
	_, err := manifest.ParseContent(src, "/some/dir")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), ":") {
		t.Errorf("error message has no line info: %v", err)
	}
}
```

- [ ] **Step 6: Run the tests and confirm they all fail with "not implemented"**

```bash
go test ./pkg/manifest/... -v 2>&1 | head -40
```

Expected: all tests fail, error message contains "not implemented".

- [ ] **Step 7: Commit**

```bash
git add pkg/manifest/manifest.go pkg/manifest/parse.go pkg/manifest/parser_test.go
git rm pkg/manifest/parser.go
git commit -m "feat: new manifest data model, stub parse entry points, failing integration tests"
```

---

### Task 2: Implement lexer (TDD)

**Files:**
- Create: `pkg/manifest/parser/lexer_test.go`
- Create: `pkg/manifest/parser/lexer.go`

- [ ] **Step 1: Create `pkg/manifest/parser/lexer_test.go`**

```go
package parser

import (
	"testing"
)

func tokTypes(tokens []Token) []TokenType {
	types := make([]TokenType, len(tokens))
	for i, t := range tokens {
		types[i] = t.Type
	}
	return types
}

func TestTokenize_basic(t *testing.T) {
	tokens, err := Tokenize("BONGOVER = 1\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Token{
		{Type: IDENT, Value: "BONGOVER", Line: 1, Col: 1},
		{Type: EQUALS, Value: "=", Line: 1, Col: 10},
		{Type: INT, Value: "1", Line: 1, Col: 12},
		{Type: NEWLINE, Line: 1},
		{Type: EOF},
	}
	if len(tokens) != len(want) {
		t.Fatalf("token count: got %d, want %d\ngot:  %v\nwant: %v", len(tokens), len(want), tokens, want)
	}
	for i, tok := range tokens {
		if tok != want[i] {
			t.Errorf("token[%d]: got %+v, want %+v", i, tok, want[i])
		}
	}
}

func TestTokenize_indent_dedent(t *testing.T) {
	src := "MODULE:\n    NAME = \"ci\"\n"
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTypes := []TokenType{
		IDENT, COLON, NEWLINE, // MODULE:
		INDENT,                // indent increase
		IDENT, EQUALS, STRING, NEWLINE, // NAME = "ci"
		DEDENT, // indent decrease
		EOF,
	}
	got := tokTypes(tokens)
	if len(got) != len(wantTypes) {
		t.Fatalf("token count: got %d (%v), want %d (%v)", len(got), got, len(wantTypes), wantTypes)
	}
	for i := range got {
		if got[i] != wantTypes[i] {
			t.Errorf("token[%d].Type: got %s, want %s", i, got[i], wantTypes[i])
		}
	}
}

func TestTokenize_nested_indent(t *testing.T) {
	src := "MODULE:\n    EXPORT:\n        INPUT A B\n"
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTypes := []TokenType{
		IDENT, COLON, NEWLINE,          // MODULE:
		INDENT,                          // depth 1
		IDENT, COLON, NEWLINE,          // EXPORT:
		INDENT,                          // depth 2
		IDENT, IDENT, IDENT, NEWLINE,   // INPUT A B
		DEDENT,                          // back to depth 1
		DEDENT,                          // back to depth 0
		EOF,
	}
	got := tokTypes(tokens)
	if len(got) != len(wantTypes) {
		t.Fatalf("token count: got %d (%v), want %d (%v)", len(got), got, len(wantTypes), wantTypes)
	}
	for i := range got {
		if got[i] != wantTypes[i] {
			t.Errorf("token[%d].Type: got %s, want %s", i, got[i], wantTypes[i])
		}
	}
}

func TestTokenize_comment_skipped(t *testing.T) {
	tokens, err := Tokenize("# comment\nBONGOVER = 1\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens[0].Type != IDENT || tokens[0].Value != "BONGOVER" {
		t.Errorf("first token after comment: got %+v, want IDENT BONGOVER", tokens[0])
	}
}

func TestTokenize_inline_comment_skipped(t *testing.T) {
	tokens, err := Tokenize("NAME = \"ci\" # inline\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// expect: IDENT EQUALS STRING NEWLINE EOF (no comment token)
	wantTypes := []TokenType{IDENT, EQUALS, STRING, NEWLINE, EOF}
	got := tokTypes(tokens)
	if len(got) != len(wantTypes) {
		t.Fatalf("token count: got %d (%v), want %d (%v)", len(got), got, len(wantTypes), wantTypes)
	}
}

func TestTokenize_blank_lines_skipped(t *testing.T) {
	src := "A:\n\n    CMD \"x\"\n"
	tokens, err := Tokenize(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// blank line between A: and CMD should produce no tokens
	wantTypes := []TokenType{IDENT, COLON, NEWLINE, INDENT, IDENT, STRING, NEWLINE, DEDENT, EOF}
	got := tokTypes(tokens)
	if len(got) != len(wantTypes) {
		t.Fatalf("token count: got %d (%v), want %d (%v)", len(got), got, len(wantTypes), wantTypes)
	}
}

func TestTokenize_string_value(t *testing.T) {
	tokens, err := Tokenize(`CMD "npm install"` + "\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) < 2 || tokens[1].Type != STRING || tokens[1].Value != "npm install" {
		t.Errorf("string token: got %+v", tokens)
	}
}

func TestTokenize_string_with_escape(t *testing.T) {
	tokens, err := Tokenize(`CMD "say \"hi\""` + "\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) < 2 || tokens[1].Value != `say "hi"` {
		t.Errorf("escaped string: got %q", tokens[1].Value)
	}
}

func TestTokenize_unterminated_string(t *testing.T) {
	_, err := Tokenize(`CMD "oops` + "\n")
	if err == nil {
		t.Fatal("expected error for unterminated string, got nil")
	}
}

func TestTokenize_colon(t *testing.T) {
	tokens, err := Tokenize("MODULE:\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) < 2 || tokens[1].Type != COLON {
		t.Errorf("expected COLON, got %+v", tokens)
	}
}

func TestTokenize_ident_with_underscore_and_digits(t *testing.T) {
	tokens, err := Tokenize("INSTALL_DEPS_2:\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens[0].Type != IDENT || tokens[0].Value != "INSTALL_DEPS_2" {
		t.Errorf("ident: got %+v", tokens[0])
	}
}

func TestTokenize_empty(t *testing.T) {
	tokens, err := Tokenize("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Type != EOF {
		t.Errorf("empty input: got %v", tokens)
	}
}
```

- [ ] **Step 2: Run lexer tests — expect compile failure**

```bash
go test ./pkg/manifest/parser/... -v 2>&1 | head -20
```

Expected: compile error — `Tokenize`, `Token`, `IDENT`, etc. are undefined.

- [ ] **Step 3: Create `pkg/manifest/parser/lexer.go`**

```go
package parser

import (
	"fmt"
	"strings"
)

// TokenType identifies the category of a lexed token.
type TokenType int

const (
	IDENT   TokenType = iota // uppercase/lowercase identifier or keyword
	STRING                   // "quoted value" (quotes stripped)
	INT                      // integer literal
	EQUALS                   // =
	COLON                    // :
	NEWLINE                  // end of logical line
	INDENT                   // synthetic: indentation increased
	DEDENT                   // synthetic: indentation decreased
	EOF                      // end of input
)

func (t TokenType) String() string {
	names := [...]string{"IDENT", "STRING", "INT", "EQUALS", "COLON", "NEWLINE", "INDENT", "DEDENT", "EOF"}
	if int(t) < len(names) {
		return names[t]
	}
	return fmt.Sprintf("TokenType(%d)", int(t))
}

// Token is a single lexical unit with source position.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int // 1-indexed
}

// Tokenize converts .bongo source into a flat token slice.
// Blank lines and # comments are skipped.
// Indentation changes emit synthetic INDENT/DEDENT tokens.
func Tokenize(src string) ([]Token, error) {
	var out []Token
	lines := strings.Split(src, "\n")
	stack := []int{0} // indentation level stack

	for lineNum, line := range lines {
		lineNo := lineNum + 1

		// measure leading whitespace
		indent := 0
		for _, ch := range line {
			if ch == ' ' {
				indent++
			} else if ch == '\t' {
				indent += 4
			} else {
				break
			}
		}
		rest := strings.TrimLeft(line, " \t")

		// skip blank lines and full-line comments
		if rest == "" || strings.HasPrefix(rest, "#") {
			continue
		}

		// emit INDENT / DEDENT
		top := stack[len(stack)-1]
		switch {
		case indent > top:
			stack = append(stack, indent)
			out = append(out, Token{Type: INDENT, Line: lineNo, Col: 1})
		case indent < top:
			for len(stack) > 1 && stack[len(stack)-1] > indent {
				stack = stack[:len(stack)-1]
				out = append(out, Token{Type: DEDENT, Line: lineNo, Col: 1})
			}
			if stack[len(stack)-1] != indent {
				return nil, fmt.Errorf("line %d: inconsistent indentation", lineNo)
			}
		}

		// tokenize the rest of the line; col is 0-indexed offset from line start
		col := indent
		for i := 0; i < len(rest); {
			ch := rest[i]
			switch {
			case ch == ' ' || ch == '\t':
				i++
				col++
			case ch == '#':
				i = len(rest) // rest of line is a comment
			case ch == '=':
				out = append(out, Token{Type: EQUALS, Value: "=", Line: lineNo, Col: col + 1})
				i++
				col++
			case ch == ':':
				out = append(out, Token{Type: COLON, Value: ":", Line: lineNo, Col: col + 1})
				i++
				col++
			case ch == '"':
				j := i + 1
				for j < len(rest) {
					if rest[j] == '"' && (j == 0 || rest[j-1] != '\\') {
						break
					}
					j++
				}
				if j >= len(rest) {
					return nil, fmt.Errorf("line %d, col %d: unterminated string", lineNo, col+1)
				}
				val := rest[i+1 : j]
				val = strings.ReplaceAll(val, `\"`, `"`)
				out = append(out, Token{Type: STRING, Value: val, Line: lineNo, Col: col + 1})
				advance := j - i + 1
				col += advance
				i += advance
			case ch >= '0' && ch <= '9':
				j := i
				for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
					j++
				}
				out = append(out, Token{Type: INT, Value: rest[i:j], Line: lineNo, Col: col + 1})
				col += j - i
				i = j
			case isIdentStart(ch):
				j := i
				for j < len(rest) && isIdentPart(rest[j]) {
					j++
				}
				out = append(out, Token{Type: IDENT, Value: rest[i:j], Line: lineNo, Col: col + 1})
				col += j - i
				i = j
			default:
				return nil, fmt.Errorf("line %d, col %d: unexpected character %q", lineNo, col+1, ch)
			}
		}
		out = append(out, Token{Type: NEWLINE, Line: lineNo})
	}

	// flush remaining DEDENTs
	for len(stack) > 1 {
		stack = stack[:len(stack)-1]
		out = append(out, Token{Type: DEDENT})
	}
	out = append(out, Token{Type: EOF})
	return out, nil
}

func isIdentStart(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || ch >= '0' && ch <= '9' || ch == '-'
}
```

- [ ] **Step 4: Run lexer tests and confirm they pass**

```bash
go test ./pkg/manifest/parser/... -v -run TestTokenize
```

Expected: all `TestTokenize_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/manifest/parser/lexer.go pkg/manifest/parser/lexer_test.go
git commit -m "feat: implement .bongo lexer with INDENT/DEDENT token emission"
```

---

### Task 3: Implement parser, wire entry points, verify all tests pass

**Files:**
- Create: `pkg/manifest/parser/parser.go`
- Delete: `pkg/manifest/parser/bongo_parser.go`
- Modify: `pkg/manifest/parse.go`

- [ ] **Step 1: Create `pkg/manifest/parser/parser.go`**

```go
package parser

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/crikke/ci/pkg/manifest"
)

// rawInput holds an unresolved task input during parse pass 1.
type rawInput struct {
	taskName   string
	outputName string
	dest       string
}

type parseState struct {
	tokens []Token
	pos    int
	src    string // used in error messages
}

// Parse converts .bongo source into a *manifest.Manifest.
// dir is the absolute path to the module directory.
func Parse(src, dir string) (*manifest.Manifest, error) {
	tokens, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	ps := &parseState{tokens: tokens, src: "build.bongo"}
	return ps.parseFile(dir)
}

func (p *parseState) peek() Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return Token{Type: EOF}
}

func (p *parseState) consume() Token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parseState) expect(typ TokenType) (Token, error) {
	t := p.consume()
	if t.Type != typ {
		return t, p.errorf(t, "expected %s, got %q", typ, t.Value)
	}
	return t, nil
}

func (p *parseState) expectIdent(val string) (Token, error) {
	t := p.consume()
	if t.Type != IDENT || t.Value != val {
		return t, p.errorf(t, "expected %q, got %q", val, t.Value)
	}
	return t, nil
}

func (p *parseState) errorf(t Token, format string, args ...interface{}) error {
	return fmt.Errorf("%s:%d:%d: %s", p.src, t.Line, t.Col, fmt.Sprintf(format, args...))
}

func (p *parseState) parseFile(dir string) (*manifest.Manifest, error) {
	// BONGOVER = INT NEWLINE
	if _, err := p.expectIdent("BONGOVER"); err != nil {
		return nil, err
	}
	if _, err := p.expect(EQUALS); err != nil {
		return nil, err
	}
	verTok, err := p.expect(INT)
	if err != nil {
		return nil, err
	}
	version, _ := strconv.Atoi(verTok.Value)
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, err
	}

	mod, err := p.parseModule(dir)
	if err != nil {
		return nil, err
	}

	taskMap := make(map[string]*manifest.Task)
	rawInputsMap := make(map[string][]rawInput)

	for p.peek().Type == IDENT {
		task, raws, err := p.parseTask()
		if err != nil {
			return nil, err
		}
		taskMap[task.Name] = task
		rawInputsMap[task.Name] = raws
	}

	if tok := p.peek(); tok.Type != EOF {
		return nil, p.errorf(tok, "unexpected token %q", tok.Value)
	}

	// Pass 2: resolve Input.Task pointers
	for taskName, raws := range rawInputsMap {
		for _, ri := range raws {
			dep, ok := taskMap[ri.taskName]
			if !ok {
				return nil, fmt.Errorf("task %q: unknown input task %q", taskName, ri.taskName)
			}
			taskMap[taskName].Inputs = append(taskMap[taskName].Inputs, manifest.Input{
				Task:       dep,
				OutputName: ri.outputName,
				Dest:       ri.dest,
			})
		}
	}

	// Validate exports
	for _, exp := range mod.Exports {
		task, ok := taskMap[exp.TaskName]
		if !ok {
			return nil, fmt.Errorf("export: unknown task %q", exp.TaskName)
		}
		found := false
		for _, out := range task.Outputs {
			if out.Name == exp.OutputName {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("export: task %q has no output named %q", exp.TaskName, exp.OutputName)
		}
	}

	if err := checkCycles(taskMap); err != nil {
		return nil, err
	}

	return &manifest.Manifest{
		AbsPath: dir,
		Version: version,
		Module:  *mod,
		Tasks:   taskMap,
	}, nil
}

func (p *parseState) parseModule(dir string) (*manifest.Module, error) {
	if _, err := p.expectIdent("MODULE"); err != nil {
		return nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, err
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, err
	}
	if _, err := p.expect(INDENT); err != nil {
		return nil, err
	}

	mod := &manifest.Module{}
	for p.peek().Type == IDENT {
		switch p.peek().Value {
		case "NAME":
			p.consume()
			if _, err := p.expect(EQUALS); err != nil {
				return nil, err
			}
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, err
			}
			mod.Name = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
		case "BASE_IMAGE":
			p.consume()
			if _, err := p.expect(EQUALS); err != nil {
				return nil, err
			}
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, err
			}
			mod.BaseImage = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
		case "INCLUDE":
			p.consume()
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
			if _, err := p.expect(INDENT); err != nil {
				return nil, err
			}
			for p.peek().Type == STRING {
				tok := p.consume()
				path := tok.Value
				if !filepath.IsAbs(path) {
					path = filepath.Clean(filepath.Join(dir, path))
				}
				mod.Include = append(mod.Include, path)
				if _, err := p.expect(NEWLINE); err != nil {
					return nil, err
				}
			}
			if _, err := p.expect(DEDENT); err != nil {
				return nil, err
			}
		case "EXPORT":
			p.consume()
			if _, err := p.expect(COLON); err != nil {
				return nil, err
			}
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, err
			}
			if _, err := p.expect(INDENT); err != nil {
				return nil, err
			}
			for p.peek().Type == IDENT && p.peek().Value == "INPUT" {
				p.consume()
				taskTok, err := p.expect(IDENT)
				if err != nil {
					return nil, err
				}
				outTok, err := p.expect(IDENT)
				if err != nil {
					return nil, err
				}
				mod.Exports = append(mod.Exports, manifest.Export{
					TaskName:   taskTok.Value,
					OutputName: outTok.Value,
				})
				if _, err := p.expect(NEWLINE); err != nil {
					return nil, err
				}
			}
			if _, err := p.expect(DEDENT); err != nil {
				return nil, err
			}
		default:
			tok := p.peek()
			return nil, p.errorf(tok, "unexpected module statement %q", tok.Value)
		}
	}

	if _, err := p.expect(DEDENT); err != nil {
		return nil, err
	}

	if mod.Name == "" {
		return nil, fmt.Errorf("manifest is missing MODULE.NAME")
	}
	if mod.BaseImage == "" {
		return nil, fmt.Errorf("manifest is missing MODULE.BASE_IMAGE")
	}
	return mod, nil
}

func (p *parseState) parseTask() (*manifest.Task, []rawInput, error) {
	nameTok, err := p.expect(IDENT)
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(COLON); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(NEWLINE); err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(INDENT); err != nil {
		return nil, nil, err
	}

	task := &manifest.Task{Name: nameTok.Value}
	var raws []rawInput

	for p.peek().Type == IDENT {
		switch p.peek().Value {
		case "CMD":
			p.consume()
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.Cmd = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "DOCKERFILE":
			p.consume()
			tok, err := p.expect(STRING)
			if err != nil {
				return nil, nil, err
			}
			task.Dockerfile = tok.Value
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "OUTPUT":
			p.consume()
			outName, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			// path is STRING or bare IDENT (e.g. OUTPUT DOCKERFILE DOCKERFILE)
			var path string
			switch p.peek().Type {
			case STRING:
				path = p.consume().Value
			case IDENT:
				path = p.consume().Value
			default:
				tok := p.peek()
				return nil, nil, p.errorf(tok, "expected output path (string or identifier), got %s", tok.Type)
			}
			task.Outputs = append(task.Outputs, manifest.Output{Name: outName.Value, Path: path})
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		case "INPUT":
			p.consume()
			taskTok, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			outTok, err := p.expect(IDENT)
			if err != nil {
				return nil, nil, err
			}
			var dest string
			if p.peek().Type == STRING {
				dest = p.consume().Value
			}
			raws = append(raws, rawInput{taskName: taskTok.Value, outputName: outTok.Value, dest: dest})
			if _, err := p.expect(NEWLINE); err != nil {
				return nil, nil, err
			}
		default:
			tok := p.peek()
			return nil, nil, p.errorf(tok, "unexpected task statement %q", tok.Value)
		}
	}

	if _, err := p.expect(DEDENT); err != nil {
		return nil, nil, err
	}
	return task, raws, nil
}

func checkCycles(tasks map[string]*manifest.Task) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle detected at task %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, inp := range tasks[name].Inputs {
			if err := dfs(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		return nil
	}

	for name := range tasks {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: Delete the stub `pkg/manifest/parser/bongo_parser.go`**

```bash
git rm pkg/manifest/parser/bongo_parser.go
```

- [ ] **Step 3: Replace the stub `pkg/manifest/parse.go` with the real entry points**

```go
package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crikke/ci/pkg/manifest/parser"
)

// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	return parser.Parse(string(data), filepath.Dir(filePath))
}

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	return parser.Parse(content, dir)
}
```

- [ ] **Step 4: Run all manifest tests**

```bash
go test ./pkg/manifest/... -v 2>&1
```

Expected: all tests PASS.

- [ ] **Step 5: Run a quick build check to confirm the package compiles cleanly**

```bash
go build ./pkg/manifest/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/manifest/parser/parser.go pkg/manifest/parse.go
git commit -m "feat: implement .bongo recursive descent parser and wire entry points"
```
