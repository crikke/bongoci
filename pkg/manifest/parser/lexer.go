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
