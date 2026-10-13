package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type tokenKind uint8

const (
	tEOF tokenKind = iota
	tIdentifier
	tString
	tInteger
	tFloat
	tTrue
	tFalse
	tNull
	tFlow
	tReturn
	tCommit
	tOnError
	tWhen
	tUnderscore
	tNewline
	tSemicolon
	tLParen
	tRParen
	tLBrace
	tRBrace
	tLess
	tGreater
	tComma
	tColon
	tDot
	tEqual
	tBang
	tArrow
	tPipe
)

var keywords = map[string]tokenKind{
	"flow": tFlow, "return": tReturn, "commit": tCommit, "onError": tOnError,
	"when": tWhen, "true": tTrue, "false": tFalse, "null": tNull, "_": tUnderscore,
}
var punctuation = map[byte]tokenKind{
	';': tSemicolon, '(': tLParen, ')': tRParen, '{': tLBrace, '}': tRBrace,
	'<': tLess, '>': tGreater, ',': tComma, ':': tColon, '.': tDot, '=': tEqual, '!': tBang,
}

type token struct {
	kind       tokenKind
	text       string
	start, end int
}
type lexer struct {
	source source.File
	text   string
	pos    int
}

func lex(file source.File) ([]token, diagnostic.Diagnostic) {
	l := lexer{source: file, text: file.Text()}
	if !utf8.ValidString(l.text) {
		for offset := 0; offset < len(l.text); {
			r, size := utf8.DecodeRuneInString(l.text[offset:])
			if r == utf8.RuneError && size == 1 {
				return nil, l.fail(diagnostic.InvalidUTF8, "source contains invalid UTF-8", offset, offset+1)
			}
			offset += size
		}
	}
	tokens := make([]token, 0)
	for {
		t, d := l.next()
		if d != nil {
			return nil, d
		}
		tokens = append(tokens, t)
		if t.kind == tEOF {
			return tokens, nil
		}
	}
}
func (l *lexer) fail(code diagnostic.Code, message string, start, end int) diagnostic.Diagnostic {
	return diagnostic.New(code, diagnostic.Lexer, message, l.source.Span(start, end))
}
func letter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func digit(c byte) bool  { return c >= '0' && c <= '9' }
func (l *lexer) next() (token, diagnostic.Diagnostic) {
	for l.pos < len(l.text) {
		c := l.text[l.pos]
		if c == ' ' || c == '\t' {
			l.pos++
			continue
		}
		if c == '/' && l.pos+1 < len(l.text) && l.text[l.pos+1] == '/' {
			for l.pos < len(l.text) && l.text[l.pos] != '\n' && l.text[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		break
	}
	start := l.pos
	if start == len(l.text) {
		return token{kind: tEOF, start: start, end: start}, nil
	}
	c := l.text[l.pos]
	l.pos++
	if c == '\n' {
		return token{tNewline, "\n", start, l.pos}, nil
	}
	if c == '\r' && l.pos < len(l.text) && l.text[l.pos] == '\n' {
		l.pos++
		return token{tNewline, "\r\n", start, l.pos}, nil
	}
	if letter(c) {
		for l.pos < len(l.text) && (letter(l.text[l.pos]) || digit(l.text[l.pos])) {
			l.pos++
		}
		text := l.text[start:l.pos]
		kind, ok := keywords[text]
		if !ok {
			kind = tIdentifier
		}
		return token{kind, text, start, l.pos}, nil
	}
	if c == '"' {
		return l.stringToken(start)
	}
	if c == '-' && l.pos < len(l.text) && l.text[l.pos] == '>' {
		l.pos++
		return token{tArrow, "->", start, l.pos}, nil
	}
	if c == '|' && l.pos < len(l.text) && l.text[l.pos] == '>' {
		l.pos++
		return token{tPipe, "|>", start, l.pos}, nil
	}
	if digit(c) || c == '-' && l.pos < len(l.text) && digit(l.text[l.pos]) {
		l.pos = start
		return l.number()
	}
	if kind, ok := punctuation[c]; ok {
		return token{kind, string(c), start, l.pos}, nil
	}
	_, size := utf8.DecodeRuneInString(l.text[start:])
	l.pos = start + size
	return token{}, l.fail(diagnostic.InvalidCharacter, fmt.Sprintf("invalid source character %q", l.text[start:l.pos]), start, l.pos)
}
func (l *lexer) stringToken(start int) (token, diagnostic.Diagnostic) {
	var value strings.Builder
	for l.pos < len(l.text) {
		i := l.pos
		r, size := utf8.DecodeRuneInString(l.text[i:])
		l.pos += size
		if r == '"' {
			return token{tString, value.String(), start, l.pos}, nil
		}
		if unicode.IsControl(r) {
			return token{}, l.fail(diagnostic.InvalidString, "raw control character in string", i, l.pos)
		}
		if r != '\\' {
			value.WriteRune(r)
			continue
		}
		if l.pos == len(l.text) {
			break
		}
		escape := l.text[l.pos]
		l.pos++
		switch escape {
		case '"', '\\':
			value.WriteByte(escape)
		case 'n':
			value.WriteByte('\n')
		case 'r':
			value.WriteByte('\r')
		case 't':
			value.WriteByte('\t')
		case 'b':
			value.WriteByte('\b')
		case 'f':
			value.WriteByte('\f')
		case 'u', 'U':
			width := 4
			if escape == 'U' {
				width = 8
			}
			if len(l.text)-l.pos < width {
				return token{}, l.fail(diagnostic.InvalidEscape, "incomplete Unicode escape", i, l.pos)
			}
			n, err := strconv.ParseUint(l.text[l.pos:l.pos+width], 16, 32)
			if err != nil || !utf8.ValidRune(rune(n)) || n > utf8.MaxRune {
				return token{}, l.fail(diagnostic.InvalidEscape, "escape is not a Unicode scalar", i, l.pos+width)
			}
			l.pos += width
			value.WriteRune(rune(n))
		default:
			return token{}, l.fail(diagnostic.InvalidEscape, "unknown string escape", i, l.pos)
		}
	}
	return token{}, l.fail(diagnostic.InvalidString, "unterminated string literal", start, l.pos)
}
func (l *lexer) number() (token, diagnostic.Diagnostic) {
	start := l.pos
	if l.text[l.pos] == '-' {
		l.pos++
	}
	zero := l.text[l.pos] == '0'
	l.pos++
	if zero && l.pos < len(l.text) && digit(l.text[l.pos]) {
		return token{}, l.fail(diagnostic.InvalidNumber, "leading zero in numeric literal", start, l.pos+1)
	}
	for l.pos < len(l.text) && digit(l.text[l.pos]) {
		l.pos++
	}
	kind := tInteger
	if l.pos < len(l.text) && l.text[l.pos] == '.' {
		kind = tFloat
		l.pos++
		begin := l.pos
		for l.pos < len(l.text) && digit(l.text[l.pos]) {
			l.pos++
		}
		if begin == l.pos {
			return token{}, l.fail(diagnostic.InvalidNumber, "fraction requires decimal digits", start, l.pos)
		}
	}
	if l.pos < len(l.text) && (l.text[l.pos] == 'e' || l.text[l.pos] == 'E') {
		kind = tFloat
		l.pos++
		if l.pos < len(l.text) && (l.text[l.pos] == '+' || l.text[l.pos] == '-') {
			l.pos++
		}
		begin := l.pos
		for l.pos < len(l.text) && digit(l.text[l.pos]) {
			l.pos++
		}
		if begin == l.pos {
			return token{}, l.fail(diagnostic.InvalidNumber, "exponent requires decimal digits", start, l.pos)
		}
	}
	if l.pos < len(l.text) && letter(l.text[l.pos]) {
		return token{}, l.fail(diagnostic.InvalidNumber, "invalid numeric suffix", start, l.pos+1)
	}
	return token{kind, l.text[start:l.pos], start, l.pos}, nil
}
