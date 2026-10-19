package diagnostic

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type Stage string

const (
	Build      Stage = "build"
	CLI        Stage = "cli"
	Inspection Stage = "inspect"
	Source     Stage = "source"
	Lexer      Stage = "lexer"
	Parser     Stage = "parser"
	Resolve    Stage = "resolve"
	Check      Stage = "check"
	Runtime    Stage = "runtime"
)

const TensorPublication Code = "runtime.tensor-publication"

type Code string

const (
	BuildTarget         Code = "build.unsupported-target"
	BuildBackend        Code = "build.backend-unavailable"
	BuildOutput         Code = "build.output"
	BuildToolchain      Code = "build.toolchain"
	BuildGenerate       Code = "build.generation"
	BuildFailed         Code = "build.failed"
	BuildLimit          Code = "build.limit"
	BuildCancelled      Code = "build.cancelled"
	RuntimeUnhandled    Code = "runtime.unhandled-error"
	RuntimeHost         Code = "runtime.host-failure"
	RuntimeLimit        Code = "runtime.limit"
	RuntimeCancelled    Code = "runtime.cancelled"
	CapabilityDenied    Code = "runtime.capability-denied"
	ServerRequiresServe Code = "runtime.server-requires-serve"
	ProcessUnavailable  Code = "runtime.process-unavailable"
	NoServer            Code = "runtime.no-server"
	InspectionLimit     Code = "inspect.limit"
	Invocation          Code = "cli.invalid-invocation"
	ReadFailed          Code = "source.read-failed"
	SourceLimit         Code = "source.limit"
	InvalidUTF8         Code = "syntax.invalid-utf8"
	InvalidCharacter    Code = "syntax.invalid-character"
	InvalidString       Code = "syntax.invalid-string"
	InvalidEscape       Code = "syntax.invalid-escape"
	InvalidNumber       Code = "syntax.invalid-number"
	UnexpectedToken     Code = "syntax.unexpected-token"
	ExpectedSep         Code = "syntax.expected-separator"
	MissingReturn       Code = "syntax.missing-return"
	ReturnPosition      Code = "syntax.return-position"
	FallibleHandler     Code = "syntax.fallible-handler"
	NestingLimit        Code = "syntax.nesting-limit"
	UnknownName         Code = "resolve.unknown-name"
	DuplicateName       Code = "resolve.duplicate-name"
	PredeclaredName     Code = "resolve.predeclared-name"
	TypeMismatch        Code = "check.type-mismatch"
	ArgumentCount       Code = "check.argument-count"
	DuplicateArg        Code = "check.duplicate-argument"
	UnknownArg          Code = "check.unknown-argument"
	Capture             Code = "resolve.capture"
	Cycle               Code = "check.cycle"
	Unhandled           Code = "check.unhandled-failure"
	EffectfulTotal      Code = "check.effectful-total"
	EffectfulCallback   Code = "check.effectful-callback"
	EffectfulMatch      Code = "check.effectful-match"
	MatchDefault        Code = "check.match-default"
	FallibleCase        Code = "check.fallible-case"
	UnknownField        Code = "check.unknown-field"
	TypeConstraint      Code = "check.type-constraint"
	NotCommittable      Code = "check.not-committable"
	InvalidLiteral      Code = "check.invalid-literal"
	ArgumentOrder       Code = "check.argument-order"
	FlowReference       Code = "check.flow-reference"
	MissingType         Code = "check.missing-type"
	TensorShape         Code = "check.tensor-shape"
	TensorAnalysisLimit Code = "check.tensor-analysis-limit"
	AutodiffUnsupported Code = "check.autodiff-unsupported"
	AutodiffLoss        Code = "check.autodiff-loss"
	AutodiffLimit       Code = "check.autodiff-limit"
)

type Diagnostic interface {
	error
	Data() Data
	diagnostic()
}

type Data struct {
	Code     Code          `json:"code"`
	Stage    Stage         `json:"stage"`
	Message  string        `json:"message"`
	Span     source.Span   `json:"span,omitempty"`
	Expected string        `json:"expected,omitempty"`
	Actual   string        `json:"actual,omitempty"`
	Related  []source.Span `json:"related,omitempty"`
}

type diagnostic struct{ data Data }

func (d diagnostic) diagnostic() {}
func (d diagnostic) Data() Data  { return d.data }
func (d diagnostic) Error() string {
	if d.data.Span == nil {
		return fmt.Sprintf("%s: %s", d.data.Code, d.data.Message)
	}
	s := d.data.Span
	return fmt.Sprintf("%s:%d:%d: %s: %s", s.File(), s.Start().Line, s.Start().Column, d.data.Code, d.data.Message)
}

func New(code Code, stage Stage, message string, span source.Span) Diagnostic {
	return diagnostic{Data{Code: code, Stage: stage, Message: message, Span: span}}
}

func Mismatch(expected, actual string, span source.Span) Diagnostic {
	return diagnostic{Data{
		Code: TypeMismatch, Stage: Check, Span: span, Expected: expected, Actual: actual,
		Message: fmt.Sprintf("expected %s, got %s", expected, actual),
	}}
}

func ShapeMismatch(operation, expected, actual string, span source.Span) Diagnostic {
	return diagnostic{Data{Code: TensorShape, Stage: Check, Span: span, Expected: expected, Actual: actual,
		Message: fmt.Sprintf("%s: expected %s, got %s", operation, expected, actual)}}
}

func WithRelated(d Diagnostic, span source.Span) Diagnostic {
	data := d.Data()
	data.Related = append(append([]source.Span(nil), data.Related...), span)
	return diagnostic{data}
}
