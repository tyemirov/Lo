package types

import (
	"fmt"
	"slices"
	"strings"
)

type Kind string

// FieldFlowProgress names the explicit completed-fold evidence on Error values.
const FieldFlowProgress = "flow_progress"

const (
	IntKind                Kind = "Int"
	StringKind             Kind = "String"
	BoolKind               Kind = "Bool"
	NullKind               Kind = "Null"
	ScalarKind             Kind = "Scalar"
	BytesKind              Kind = "Bytes"
	FloatKind              Kind = "Float"
	TensorKind             Kind = "Tensor<FP32>"
	DeviceKind             Kind = "Device"
	ErrorKind              Kind = "Error"
	PathKind               Kind = "File.Path"
	FileEntryKind          Kind = "File.Entry"
	HeadersKind            Kind = "HTTP.Headers"
	RequestKind            Kind = "HTTP.Request"
	URLKind                Kind = "HTTP.URL"
	MethodKind             Kind = "HTTP.Method"
	ClientRequestKind      Kind = "HTTP.Client.Request"
	ResponseKind           Kind = "HTTP.Response"
	ServerKind             Kind = "HTTP.Server"
	FileWriteKind          Kind = "File.Write"
	FileAppendKind         Kind = "File.Append"
	FileRemoveKind         Kind = "File.Remove"
	FileWriteReceiptKind   Kind = "File.WriteReceipt"
	FileAppendReceiptKind  Kind = "File.AppendReceipt"
	FileRemoveReceiptKind  Kind = "File.RemoveReceipt"
	ProcessExecutableKind  Kind = "Process.Executable"
	ProcessContextKind     Kind = "Process.Context"
	ProcessCommandKind     Kind = "Process.Command"
	ProcessResultKind      Kind = "Process.Result"
	ProcessTerminationKind Kind = "Process.Termination"
	ServerReceiptKind      Kind = "HTTP.ServerReceipt"
	AnyRecordKind          Kind = "Record"
	RecordKind             Kind = "ExactRecord"
	ListKind               Kind = "List"
	MapKind                Kind = "Map"
	RangeKind              Kind = "Range"
	FallibleKind           Kind = "Fallible"
)

type Type interface {
	fmt.Stringer
	Kind() Kind
	loType()
}
type List interface {
	Type
	Element() Type
	listType()
}
type Map interface {
	Type
	Key() Type
	Value() Type
}
type Range interface {
	Type
	Element() Type
	rangeType()
}
type Fallible interface {
	Type
	Base() Type
}
type Record interface {
	Type
	Fields() []Field
}
type Field struct {
	Name string
	Type Type
}
type Parameter struct {
	Name string
	Type Type
}
type Signature interface {
	Parameters() []Parameter
	Output() Type
	signature()
}
type signature struct {
	parameters []Parameter
	output     Type
}

func (signature) signature()                {}
func (s signature) Parameters() []Parameter { return slices.Clone(s.parameters) }
func (s signature) Output() Type            { return s.output }
func NewSignature(parameters []Parameter, output Type) (Signature, error) {
	if output == nil {
		return nil, fmt.Errorf("absent flow output")
	}
	seen := map[string]bool{}
	for _, p := range parameters {
		if p.Name == "" || p.Type == nil || seen[p.Name] {
			return nil, fmt.Errorf("invalid flow parameter %q", p.Name)
		}
		seen[p.Name] = true
	}
	return signature{slices.Clone(parameters), output}, nil
}

type atom Kind

func (atom) loType()          {}
func (a atom) Kind() Kind     { return Kind(a) }
func (a atom) String() string { return string(a) }
func Int() Type               { return atom(IntKind) }
func String() Type            { return atom(StringKind) }
func Bool() Type              { return atom(BoolKind) }
func Null() Type              { return atom(NullKind) }
func Scalar() Type            { return atom(ScalarKind) }
func Bytes() Type             { return atom(BytesKind) }
func Float() Type             { return atom(FloatKind) }
func Tensor() Type            { return atom(TensorKind) }
func Device() Type            { return atom(DeviceKind) }
func Error() Type             { return atom(ErrorKind) }
func Path() Type              { return atom(PathKind) }
func FileEntry() Type         { return atom(FileEntryKind) }
func Headers() Type           { return atom(HeadersKind) }
func HeaderEntries() List {
	return must(NewList(must(NewRecord([]Field{{Name: "name", Type: String()}, {Name: "value", Type: Bytes()}}))))
}
func Request() Type            { return atom(RequestKind) }
func URL() Type                { return atom(URLKind) }
func Method() Type             { return atom(MethodKind) }
func ClientRequest() Type      { return atom(ClientRequestKind) }
func Response() Type           { return atom(ResponseKind) }
func Server() Type             { return atom(ServerKind) }
func FileWrite() Type          { return atom(FileWriteKind) }
func FileAppend() Type         { return atom(FileAppendKind) }
func FileRemove() Type         { return atom(FileRemoveKind) }
func FileWriteReceipt() Type   { return atom(FileWriteReceiptKind) }
func FileAppendReceipt() Type  { return atom(FileAppendReceiptKind) }
func FileRemoveReceipt() Type  { return atom(FileRemoveReceiptKind) }
func ServerReceipt() Type      { return atom(ServerReceiptKind) }
func ProcessExecutable() Type  { return atom(ProcessExecutableKind) }
func ProcessContext() Type     { return atom(ProcessContextKind) }
func ProcessCommand() Type     { return atom(ProcessCommandKind) }
func ProcessResult() Type      { return atom(ProcessResultKind) }
func ProcessTermination() Type { return atom(ProcessTerminationKind) }
func AnyRecord() Type          { return atom(AnyRecordKind) }
func Named(name string) (Type, bool) {
	switch Kind(name) {
	case URLKind, MethodKind, ClientRequestKind, ProcessContextKind, ProcessExecutableKind, ProcessCommandKind, ProcessResultKind, ProcessTerminationKind:
		return atom(name), true
	case IntKind, StringKind, BoolKind, NullKind, ScalarKind, BytesKind, FloatKind, DeviceKind, ErrorKind, PathKind, FileEntryKind, HeadersKind, RequestKind, ResponseKind, ServerKind, FileWriteKind, FileAppendKind, FileRemoveKind, FileWriteReceiptKind, FileAppendReceiptKind, FileRemoveReceiptKind, ServerReceiptKind, AnyRecordKind, SGDConfigKind, AdamWConfigKind:
		return atom(name), true
	}
	return nil, false
}

type list struct{ element Type }

func (list) loType()          {}
func (list) listType()        {}
func (list) Kind() Kind       { return ListKind }
func (l list) Element() Type  { return l.element }
func (l list) String() string { return "List<" + l.element.String() + ">" }
func NewList(element Type) (List, error) {
	if element == nil {
		return nil, fmt.Errorf("absent list element type")
	}
	return list{element}, nil
}

type mapType struct{ key, value Type }

func (mapType) loType()          {}
func (mapType) Kind() Kind       { return MapKind }
func (m mapType) Key() Type      { return m.key }
func (m mapType) Value() Type    { return m.value }
func (m mapType) String() string { return "Map<" + m.key.String() + ", " + m.value.String() + ">" }
func NewMap(key, value Type) (Map, error) {
	if key == nil || value == nil || !Comparable(key) {
		return nil, fmt.Errorf("map key must be equality-comparable")
	}
	return mapType{key, value}, nil
}

type rangeType struct{ element Type }

func (rangeType) loType()          {}
func (rangeType) rangeType()       {}
func (rangeType) Kind() Kind       { return RangeKind }
func (r rangeType) Element() Type  { return r.element }
func (r rangeType) String() string { return "Range<" + r.element.String() + ">" }
func NewRange(element Type) (Range, error) {
	if element == nil || !Ordered(element) {
		return nil, fmt.Errorf("range endpoint must be ordered-comparable")
	}
	return rangeType{element}, nil
}

type fallible struct{ base Type }

func (fallible) loType()          {}
func (fallible) Kind() Kind       { return FallibleKind }
func (f fallible) Base() Type     { return f.base }
func (f fallible) String() string { return f.base.String() + "!" }
func NewFallible(base Type) (Fallible, error) {
	if base == nil || base.Kind() == FallibleKind {
		return nil, fmt.Errorf("invalid fallible base")
	}
	return fallible{base}, nil
}

type record struct{ fields []Field }

func (record) loType()           {}
func (record) Kind() Kind        { return RecordKind }
func (r record) Fields() []Field { return slices.Clone(r.fields) }
func (r record) String() string {
	parts := make([]string, len(r.fields))
	for i, f := range r.fields {
		parts[i] = f.Name + ": " + f.Type.String()
	}
	return "Record{" + strings.Join(parts, ", ") + "}"
}
func NewRecord(fields []Field) (Record, error) {
	fields = slices.Clone(fields)
	slices.SortFunc(fields, func(a, b Field) int { return strings.Compare(a.Name, b.Name) })
	for i, f := range fields {
		if f.Name == "" || f.Type == nil || i > 0 && fields[i-1].Name == f.Name {
			return nil, fmt.Errorf("invalid record field %q", f.Name)
		}
	}
	return record{fields}, nil
}
func Comparable(t Type) bool {
	switch t.Kind() {
	case ScalarKind, StringKind, IntKind, BoolKind, NullKind:
		return true
	}
	return false
}
func Ordered(t Type) bool {
	switch t.Kind() {
	case ScalarKind, StringKind, IntKind:
		return true
	}
	return false
}
func Equal(a, b Type) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case SGDStateKind, AdamWStateKind:
		return Equal(a.(OptimizerState).Schema(), b.(OptimizerState).Schema())
	case ParametersKind, GradientsKind:
		return Equal(a.(TensorBundle).Schema(), b.(TensorBundle).Schema())
	case ListKind:
		return Equal(a.(List).Element(), b.(List).Element())
	case MapKind:
		x, y := a.(Map), b.(Map)
		return Equal(x.Key(), y.Key()) && Equal(x.Value(), y.Value())
	case RangeKind:
		return Equal(a.(Range).Element(), b.(Range).Element())
	case FallibleKind:
		return Equal(a.(Fallible).Base(), b.(Fallible).Base())
	case RecordKind:
		x, y := a.(Record).Fields(), b.(Record).Fields()
		if len(x) != len(y) {
			return false
		}
		for i, f := range x {
			if f.Name != y[i].Name || !Equal(f.Type, y[i].Type) {
				return false
			}
		}
		return true
	}
	return true
}
func FieldType(t Type, name string) (Type, bool) {
	if s, ok := t.(OptimizerState); ok {
		switch name {
		case FieldStep:
			return Int(), true
		case FieldParameters:
			p, err := NewTensorBundle(ParametersKind, s.Schema())
			if err != nil {
				panic(err)
			}
			return p, true
		}
		return nil, false
	}
	if r, ok := t.(Record); ok {
		for _, f := range r.Fields() {
			if f.Name == name {
				return f.Type, true
			}
		}
		return nil, false
	}
	switch t.Kind() {
	case ProcessResultKind:
		switch name {
		case "termination":
			return ProcessTermination(), true
		case "stdout", "stderr":
			return Bytes(), true
		}
	case ProcessTerminationKind:
		if name == "kind" {
			return String(), true
		}
	case FileWriteReceiptKind, FileAppendReceiptKind:
		switch name {
		case "path":
			return Path(), true
		case "bytes_written":
			return Int(), true
		}
	case FileRemoveReceiptKind:
		switch name {
		case "total_count", "completed_count", "observed_bytes":
			return Int(), true
		}
	case ServerReceiptKind:
		switch name {
		case "address":
			return String(), true
		case "port":
			return Int(), true
		}
	case FileEntryKind:
		switch name {
		case "path":
			return Path(), true
		case "name", "kind":
			return String(), true
		case "size_bytes", "modified_unix_ns", "link_count":
			return Int(), true
		}
	case HeadersKind:
		if name == "entries" {
			return HeaderEntries(), true
		}
	case RequestKind:
		switch name {
		case "method", "path":
			return String(), true
		case "body":
			return Bytes(), true
		case "headers":
			return Headers(), true
		}
	case ResponseKind:
		switch name {
		case "status":
			return Int(), true
		case "body":
			return Bytes(), true
		case "headers":
			return Headers(), true
		}
	case ErrorKind:
		switch name {
		case "kind", "message":
			return String(), true
		case "input":
			return AnyRecord(), true
		case "trace":
			return must(NewList(must(NewRecord([]Field{{"flow", String()}, {"operation", String()}, {"input", AnyRecord()}})))), true
		case FieldFlowProgress:
			return must(NewList(AnyRecord())), true
		}
	}
	return nil, false
}
func must[T Type](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}
