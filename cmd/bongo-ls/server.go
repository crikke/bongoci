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
	"CACHE":      "Whether BuildKit caches this step. Defaults to TRUE. Set to FALSE to always re-run.",
	"ENV":        "Sets an environment variable. Arguments: `KEY \"value\"`. Valid inside MODULE (defaults) and tasks (override per-key).",
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
	if err != nil || u.Scheme != "file" {
		return "."
	}
	return filepath.Dir(u.Path)
}

// Callers must release docsMu before calling publishDiagnostics; the parser may block.
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
	context.Notify("textDocument/publishDiagnostics", &protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}
