package syntax

import (
	"fmt"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

const MaxNesting = 256
const matchConstructor = "Match.new"

type parser struct {
	file               source.File
	tokens             []token
	index, soft, depth int
	err                diagnostic.Diagnostic
}

func Parse(file source.File) (Program, diagnostic.Diagnostic) {
	tokens, d := lex(file)
	if d != nil {
		return nil, d
	}
	p := parser{file: file, tokens: tokens}
	statements := p.body(tEOF)
	if p.err != nil {
		return nil, p.err
	}
	return program{base{file.Span(0, len(file.Text()))}, file, statements}, nil
}

func (p *parser) peek() token {
	if p.soft > 0 {
		for p.tokens[p.index].kind == tNewline {
			p.index++
		}
	}
	return p.tokens[p.index]
}
func (p *parser) take() token {
	t := p.peek()
	if t.kind != tEOF {
		p.index++
	}
	return t
}
func (p *parser) accept(kind tokenKind) bool {
	if p.peek().kind != kind {
		return false
	}
	p.take()
	return true
}
func (p *parser) fail(code diagnostic.Code, message string, t token) {
	if p.err == nil {
		p.err = diagnostic.New(code, diagnostic.Parser, message, p.file.Span(t.start, t.end))
	}
}
func (p *parser) expect(kind tokenKind, description string) token {
	t := p.peek()
	if t.kind != kind {
		p.fail(diagnostic.UnexpectedToken, fmt.Sprintf("expected %s, got %q", description, t.text), t)
		return t
	}
	return p.take()
}
func (p *parser) span(start int) source.Span {
	end := start
	if p.index > 0 {
		end = p.tokens[p.index-1].end
	}
	return p.file.Span(start, end)
}
func (p *parser) enter() bool {
	p.depth++
	if p.depth > MaxNesting {
		p.fail(diagnostic.NestingLimit, "source nesting exceeds 256 constructs", p.peek())
		p.depth--
		return false
	}
	return true
}
func (p *parser) separators() {
	for p.peek().kind == tNewline || p.peek().kind == tSemicolon {
		p.take()
	}
}
func (p *parser) body(end tokenKind) []Statement {
	statements := make([]Statement, 0)
	p.separators()
	returned := false
	for p.err == nil && p.peek().kind != end && p.peek().kind != tEOF {
		if returned {
			p.fail(diagnostic.ReturnPosition, "return must be the final statement", p.peek())
			break
		}
		statement := p.statement()
		if p.err != nil {
			break
		}
		statements = append(statements, statement)
		_, returned = statement.(Return)
		t := p.peek()
		if t.kind == end || t.kind == tEOF {
			break
		}
		if t.kind != tNewline && t.kind != tSemicolon {
			p.fail(diagnostic.ExpectedSep, "expected a newline or semicolon between statements", t)
			break
		}
		p.separators()
	}
	if end == tRBrace && !returned && p.err == nil {
		p.fail(diagnostic.MissingReturn, "flow body requires a final return", p.peek())
	}
	return statements
}
func (p *parser) statement() Statement {
	t := p.take()
	switch t.kind {
	case tIdentifier:
		p.expect(tEqual, "=")
		if p.accept(tCommit) {
			target := p.expect(tIdentifier, "commit target")
			return commit{statementBase{base{p.span(t.start)}}, target.text, t.text}
		}
		value := p.expression()
		return binding{statementBase{base{p.span(t.start)}}, t.text, value}
	case tReturn:
		value := p.expression()
		return returnNode{statementBase{base{p.span(t.start)}}, value}
	case tCommit:
		name := p.expect(tIdentifier, "commit target")
		return commit{statementBase{base{p.span(t.start)}}, name.text, ""}
	case tFlow:
		return p.flow(t.start)
	default:
		p.fail(diagnostic.UnexpectedToken, "expected a binding, flow, commit, or return", t)
		return nil
	}
}
func (p *parser) flow(start int) Statement {
	if !p.enter() {
		return nil
	}
	defer func() { p.depth-- }()
	name := p.expect(tIdentifier, "flow name")
	p.expect(tLParen, "(")
	p.soft++
	params := make([]Parameter, 0)
	for p.err == nil && p.peek().kind != tRParen {
		n := p.expect(tIdentifier, "parameter name")
		p.expect(tColon, ":")
		t := p.typeExpression()
		params = append(params, Parameter{n.text, t, p.span(n.start)})
		if !p.accept(tComma) {
			break
		}
	}
	p.expect(tRParen, ")")
	p.soft--
	p.expect(tArrow, "->")
	output := p.typeExpression()
	handler := ""
	if p.peek().kind == tOnError {
		if _, fallible := output.(FallibleType); fallible {
			p.fail(diagnostic.FallibleHandler, "self-handling output must be nonfallible", p.peek())
		}
		p.take()
		handler = p.expect(tIdentifier, "handler flow name").text
	}
	p.expect(tLBrace, "{")
	body := p.body(tRBrace)
	p.expect(tRBrace, "}")
	return flow{statementBase{base{p.span(start)}}, name.text, params, output, handler, body}
}
func (p *parser) expression() Expression {
	if p.err != nil || !p.enter() {
		return nil
	}
	defer func() { p.depth-- }()
	left := p.primary()
	for p.err == nil {
		look := p.index
		for p.tokens[look].kind == tNewline {
			look++
		}
		if p.tokens[look].kind != tPipe {
			break
		}
		p.index = look + 1
		for p.peek().kind == tNewline {
			p.take()
		}
		name, _ := p.qualifiedName()
		arguments := p.arguments()
		left = pipe{expressionBase{base{p.span(left.Span().Start().Offset)}}, left, name, arguments}
	}
	return left
}
func (p *parser) primary() Expression {
	t := p.peek()
	kind := LiteralKind("")
	switch t.kind {
	case tString:
		kind = StringLiteral
	case tInteger:
		kind = IntLiteral
	case tFloat:
		kind = FloatLiteral
	case tTrue, tFalse:
		kind = BoolLiteral
	case tNull:
		kind = NullLiteral
	}
	if kind != "" {
		p.take()
		return literal{expressionBase{base{p.file.Span(t.start, t.end)}}, kind, t.text}
	}
	if t.kind == tLParen {
		p.take()
		p.soft++
		expr := p.expression()
		p.expect(tRParen, ")")
		p.soft--
		return expr
	}
	if t.kind != tIdentifier {
		p.fail(diagnostic.UnexpectedToken, "expected an expression", t)
		return nil
	}
	name, parts := p.qualifiedName()
	if p.peek().kind == tLParen {
		if name == matchConstructor {
			return p.match(t.start)
		}
		args := p.arguments()
		return call{expressionBase{base{p.span(t.start)}}, name, args}
	}
	if len(parts) > 1 {
		return projection{expressionBase{base{p.span(t.start)}}, parts}
	}
	return reference{expressionBase{base{p.span(t.start)}}, name}
}
func (p *parser) qualifiedName() (string, []string) {
	n := p.expect(tIdentifier, "identifier")
	parts := []string{n.text}
	for p.err == nil && p.accept(tDot) {
		parts = append(parts, p.expectLabel("identifier after .").text)
	}
	return strings.Join(parts, "."), parts
}
func (p *parser) arguments() []Argument {
	p.expect(tLParen, "(")
	p.soft++
	defer func() { p.soft-- }()
	args := make([]Argument, 0)
	for p.err == nil && p.peek().kind != tRParen {
		start := p.peek().start
		name := ""
		i := p.index
		if isLabel(p.peek().kind) {
			t := p.take()
			if p.accept(tColon) {
				name = t.text
			} else {
				p.index = i
			}
		}
		value := p.expression()
		args = append(args, Argument{name, value, p.span(start)})
		if !p.accept(tComma) {
			break
		}
	}
	p.expect(tRParen, ")")
	return args
}
func (p *parser) match(start int) Expression {
	p.expect(tLParen, "(")
	p.soft++
	defer func() { p.soft-- }()
	selector := p.expression()
	p.expect(tComma, ",")
	cases := make([]Case, 0)
	for p.err == nil {
		t := p.peek()
		var head Expression
		predicate := ""
		if t.kind == tUnderscore {
			p.take()
		} else if t.kind == tWhen {
			p.take()
			predicate = p.expect(tIdentifier, "predicate flow name").text
		} else {
			head = p.expression()
		}
		p.expect(tColon, ":")
		body := p.expression()
		b := caseBase{base{p.span(t.start)}, body}
		if t.kind == tUnderscore {
			cases = append(cases, defaultCase{b})
		} else if t.kind == tWhen {
			cases = append(cases, predicateCase{b, predicate})
		} else {
			cases = append(cases, exactCase{b, head})
		}
		if !p.accept(tComma) || p.peek().kind == tRParen {
			break
		}
	}
	p.expect(tRParen, ")")
	return match{expressionBase{base{p.span(start)}}, selector, cases}
}
func (p *parser) typeExpression() TypeExpression {
	if p.err != nil || !p.enter() {
		return nil
	}
	defer func() { p.depth-- }()
	start := p.peek().start
	name, _ := p.qualifiedName()
	var result TypeExpression
	if name == "Record" && p.accept(tLBrace) {
		p.soft++
		fields := make([]Field, 0)
		for p.err == nil && p.peek().kind != tRBrace {
			t := p.expectLabel("record field")
			p.expect(tColon, ":")
			fieldType := p.typeExpression()
			fields = append(fields, Field{t.text, fieldType, p.span(t.start)})
			if !p.accept(tComma) {
				break
			}
		}
		p.expect(tRBrace, "}")
		p.soft--
		result = recordType{typeBase{base{p.span(start)}}, fields}
	} else {
		args := make([]TypeExpression, 0)
		if p.accept(tLess) {
			p.soft++
			for p.err == nil {
				args = append(args, p.typeExpression())
				if !p.accept(tComma) || p.peek().kind == tGreater {
					break
				}
			}
			p.expect(tGreater, ">")
			p.soft--
		}
		result = namedType{typeBase{base{p.span(start)}}, name, args}
	}
	if p.accept(tBang) {
		result = fallibleType{typeBase{base{p.span(start)}}, result}
	}
	return result
}

func isLabel(kind tokenKind) bool {
	return kind == tIdentifier || kind >= tTrue && kind <= tWhen
}
func (p *parser) expectLabel(description string) token {
	if isLabel(p.peek().kind) {
		return p.take()
	}
	return p.expect(tIdentifier, description)
}
