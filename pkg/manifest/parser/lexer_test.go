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

func TestTokenize_string_with_double_backslash(t *testing.T) {
	// "path\\" should lex as the string value: path\
	tokens, err := Tokenize(`CMD "path\\"` + "\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens) < 2 || tokens[1].Type != STRING || tokens[1].Value != `path\` {
		t.Errorf("double-backslash string: got %+v", tokens)
	}
}
