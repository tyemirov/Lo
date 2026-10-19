package value

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type Error interface {
	Value
	Kind() string
	Message() string
	Input() Record
	Trace() []Record
	FlowProgress() []Record
	Operation() string
	Span() source.Span
}
type loError struct {
	base
	kind, message string
	input         Record
	trace         []Record
	flowProgress  []Record
	operation     string
	span          source.Span
}

func (v loError) Kind() string           { return v.kind }
func (v loError) Message() string        { return v.message }
func (v loError) Input() Record          { return v.input }
func (v loError) Trace() []Record        { return slices.Clone(v.trace) }
func (v loError) FlowProgress() []Record { return slices.Clone(v.flowProgress) }
func (v loError) Operation() string      { return v.operation }
func (v loError) Span() source.Span      { return v.span }
func (v loError) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string   `json:"type"`
		Kind         string   `json:"kind"`
		Message      string   `json:"message"`
		Input        Record   `json:"input"`
		Trace        []Record `json:"trace"`
		FlowProgress []Record `json:"flow_progress,omitempty"`
	}{v.Type().String(), v.kind, v.message, v.input, v.trace, v.flowProgress})
}
func NewError(kind, message string, input Record) (Error, error) {
	if input == nil {
		return nil, fmt.Errorf("error requires input record")
	}
	input = Snapshot(input).(Record)
	if _, err := NewText(types.String(), kind); err != nil {
		return nil, err
	}
	if _, err := NewText(types.String(), message); err != nil {
		return nil, err
	}
	weight := addWeight(uint64(len(kind)+len(message)), input.Weight())
	weight = addWeight(weight, 16) // Empty flow-progress list.
	return loError{base: newBase(types.Error(), weight), kind: kind, message: message, input: RecordView(input), trace: []Record{}}, nil
}
func At(e Error, operation string, span source.Span) Error {
	out := e.(loError)
	out.operation, out.span = operation, span
	return out
}

// WithFlowProgress prepends an already reserved immutable fold snapshot.
func WithFlowProgress(e Error, progress Record) Error {
	out := e.(loError)
	out.flowProgress = make([]Record, len(out.flowProgress)+1)
	out.flowProgress[0] = progress
	copy(out.flowProgress[1:], e.(loError).flowProgress)
	out.weight = addWeight(out.weight, progress.Weight())
	return out
}

func WithFrame(e Error, flow, operation string, input Record) Error {
	input = frameInput(input)
	f := must(NewRecord([]Field{{"flow", must(NewText(types.String(), flow))}, {"operation", must(NewText(types.String(), operation))}, {"input", RecordView(input)}}))
	out := e.(loError)
	out.trace = append(e.Trace(), f)
	out.weight = addWeight(out.weight, f.Weight())
	return out
}

func frameInput(input Record) Record {
	fields := input.Fields()
	for index, field := range fields {
		typeName := must(NewText(types.String(), field.Value.Type().String()))
		fields[index].Value = must(NewRecord([]Field{{Name: "type", Value: typeName}}))
	}
	return must(NewRecord(fields))
}

type Fallible interface {
	Value
	Outcome() (Value, Error)
}
type success struct {
	base
	result Value
}
type failure struct {
	base
	cause Error
}

func (v success) Outcome() (Value, Error) { return v.result, nil }
func (v failure) Outcome() (Value, Error) { return nil, v.cause }
func (v success) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Value  Value  `json:"value"`
	}{v.Type().String(), "ok", v.result})
}
func (v failure) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Error  Error  `json:"error"`
	}{v.Type().String(), "error", v.cause})
}
func Success(t types.Fallible, v Value) (Fallible, error) {
	if t == nil || v == nil || !types.Equal(t.Base(), v.Type()) {
		return nil, fmt.Errorf("invalid fallible success")
	}
	return success{newBase(t, v.Weight()), v}, nil
}
func Failure(t types.Fallible, e Error) (Fallible, error) {
	if t == nil || e == nil {
		return nil, fmt.Errorf("invalid fallible failure")
	}
	return failure{newBase(t, e.Weight()), e}, nil
}

func Project(v Value, field string) Value {
	switch x := v.(type) {
	case Receipt:
		return x.FieldsRecord().Field(field)
	case OptimizerState:
		switch field {
		case types.FieldParameters:
			return x.Parameters()
		case types.FieldStep:
			return x.Step()
		}
	case Record:
		return x.Field(field)
	case Error:
		switch field {
		case "kind":
			return must(NewText(types.String(), x.Kind()))
		case "message":
			return must(NewText(types.String(), x.Message()))
		case "input":
			return x.Input()
		case "trace":
			t, _ := types.FieldType(types.Error(), "trace")
			items := []Value{}
			for _, f := range x.Trace() {
				items = append(items, f)
			}
			return must(NewList(t.(types.List), items))
		case types.FieldFlowProgress:
			t, _ := types.FieldType(types.Error(), types.FieldFlowProgress)
			items := []Value{}
			for _, progress := range x.FlowProgress() {
				items = append(items, RecordView(progress))
			}
			return must(NewList(t.(types.List), items))
		}
	case FileEntry:
		return x.FieldsRecord().Field(field)
	case Headers:
		return x.Entries()
	case HTTPValue:
		return x.Fields().Field(field)
	}
	panic("invalid checked projection")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
