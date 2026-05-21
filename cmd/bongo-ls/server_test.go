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
	if *d.Severity != protocol.DiagnosticSeverityError {
		t.Error("expected error severity")
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
	if *d.Severity != protocol.DiagnosticSeverityError {
		t.Error("expected error severity")
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
