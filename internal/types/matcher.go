package types

import "fmt"

type matcherType struct {
	kind   Kind
	input  Type
	output Type
	reads  bool
}

func (matcherType) loType() {}

func (m matcherType) Kind() Kind   { return m.kind }
func (m matcherType) Input() Type  { return m.input }
func (m matcherType) Output() Type { return m.output }
func (m matcherType) Reads() bool  { return m.reads }
func (m matcherType) String() string {
	qualifier := ""
	if m.reads {
		qualifier = ", Read"
	}
	return string(m.kind) + "<" + m.input.String() + ", " + m.output.String() + qualifier + ">"
}

// NewMatcher constructs the nominal type of an ordered matcher or matcher case.
func NewMatcher(kind Kind, input, output Type, reads bool) (Matcher, error) {
	if (kind != MatchKind && kind != MatchCaseKind) || input == nil || output == nil {
		return nil, fmt.Errorf("invalid matcher signature")
	}
	return matcherType{kind: kind, input: input, output: output, reads: reads}, nil
}

// MatcherReadWidening permits a pure matcher to acquire only its own read effect.
func MatcherReadWidening(from, to Type) bool {
	source, sourceOK := from.(Matcher)
	target, targetOK := to.(Matcher)
	return sourceOK && targetOK && source.Kind() == target.Kind() &&
		!source.Reads() && target.Reads() &&
		Equal(source.Input(), target.Input()) && Equal(source.Output(), target.Output())
}
