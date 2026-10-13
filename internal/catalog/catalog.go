package catalog

import (
	"slices"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type Rule uint8

const (
	Fixed Rule = iota + 1
	ScalarConstructor
	StringConcat
	ListConstructor
	MapConstructor
	RecordConstructor
	RangeConstructor
	RecordMerge
	MapLookup
	RangeProcessor
	ListProcessor
	ListLength
	ServerConstructor
)

type Boundary uint8

const (
	Pure Boundary = iota
	Read
	Effect
	Server
)
const (
	ScalarNew         = "Scalar.new"
	IntNew            = "Int.new"
	StringNew         = "String.new"
	BoolNew           = "Bool.new"
	FloatNew          = "Float.new"
	FloatAdd          = "Float.add"
	FloatSub          = "Float.sub"
	FloatMul          = "Float.mul"
	FloatDiv          = "Float.div"
	FloatSqrt         = "Float.sqrt"
	FloatEqual        = "Float.eq"
	FloatLT           = "Float.lt"
	FloatLTE          = "Float.lte"
	FloatGT           = "Float.gt"
	FloatGTE          = "Float.gte"
	IntToFloat        = "Int.to_float"
	FloatToInt        = "Float.to_int"
	FloatToString     = "Float.to_string"
	BytesNew          = "Bytes.new"
	ListNew           = "List.new"
	MapNew            = "Map.new"
	RecordNew         = "Record.new"
	RangeNew          = "Range.new"
	ErrorNew          = "Error.new"
	PathNew           = "File.Path.new"
	ServerNew         = "HTTP.Server.new"
	ResponseTextNew   = "HTTP.Response.Text.new"
	ResponseBytesNew  = "HTTP.Response.Bytes.new"
	UTF8New           = "UTF8.String.new"
	FromScalarsNew    = "String.FromScalars.new"
	Concat            = "String.concat"
	Upper             = "String.upper"
	Lower             = "String.lower"
	StringLength      = "String.length"
	StringEqual       = "String.eq"
	ScalarEqual       = "Scalar.eq"
	IntToString       = "Int.to_string"
	MathAdd           = "Math.add"
	MathSub           = "Math.sub"
	MathMul           = "Math.mul"
	MathDiv           = "Math.div"
	MathMod           = "Math.mod"
	MathLT            = "Math.lt"
	MathLTE           = "Math.lte"
	MathGT            = "Math.gt"
	MathGTE           = "Math.gte"
	MathEqual         = "Math.eq"
	MathSum           = "Math.sum"
	MathProduct       = "Math.product"
	MathMin           = "Math.min"
	MathMax           = "Math.max"
	Contains          = "Range.contains"
	IsEmpty           = "Range.is_empty"
	HasKey            = "Map.has_key"
	Get               = "Map.get"
	Merge             = "Record.merge"
	ListMap           = "List.map"
	ListFilter        = "List.filter"
	ListFold          = "List.fold"
	ListFlatMap       = "List.flat_map"
	ListUnfold        = "List.unfold"
	Length            = "List.length"
	FileRead          = "File.read"
	FileAppend        = "File.append"
	FileWrite         = "File.write"
	DivideByZero      = "Math.DivideByZero"
	ModuloByZero      = "Math.ModuloByZero"
	EmptyList         = "Math.EmptyList"
	InvalidUTF8       = "UTF8.InvalidBytes"
	MissingKey        = "Map.MissingKey"
	ReadFailed        = "File.ReadFailed"
	AppendFailed      = "File.AppendFailed"
	WriteFailed       = "File.WriteFailed"
	ServerStartFailed = "HTTP.ServerStartFailed"
	NumericNonFinite  = "Numeric.NonFinite"
	NumericZero       = "Numeric.DivideByZero"
	NumericDomain     = "Numeric.Domain"
)
const (
	ParamValue        = "value"
	ParamLeft         = "left_value"
	ParamRight        = "right_value"
	ParamNumbers      = "number_list"
	ParamPath         = "file_path"
	ParamContent      = "file_content"
	ParamLeftOperand  = "left"
	ParamRightOperand = "right"
)

const (
	HTTPListen        = "HTTP.listen"
	HTTPRequest       = "HTTP.request"
	HTTPRespond       = "HTTP.respond"
	RequestTooLarge   = "HTTP.RequestTooLarge"
	InvalidRequest    = "HTTP.InvalidRequest"
	RequestReadFailed = "HTTP.RequestReadFailed"
	InvalidResponse   = "HTTP.InvalidResponse"
)

type Operation interface {
	Capability() string
	Name() string
	Rule() Rule
	Parameters() []types.Parameter
	Output() types.Type
	Boundary() Boundary
	ErrorKinds() []string
	ActivationKinds() []string
	BoundaryErrors() []BoundaryError
	operation()
}
type BoundaryError struct{ Operation, Kind string }
type operation struct {
	name               string
	rule               Rule
	parameters         []types.Parameter
	output             types.Type
	boundary           Boundary
	errors, activation []string
	boundaryErrors     []BoundaryError
}

func (operation) operation() {}
func (o operation) Capability() string {
	switch o.boundary {
	case Read, Effect:
		return o.name
	case Server:
		return HTTPListen
	default:
		return ""
	}
}
func (o operation) Name() string                    { return o.name }
func (o operation) Rule() Rule                      { return o.rule }
func (o operation) Parameters() []types.Parameter   { return slices.Clone(o.parameters) }
func (o operation) Output() types.Type              { return o.output }
func (o operation) Boundary() Boundary              { return o.boundary }
func (o operation) ErrorKinds() []string            { return slices.Clone(o.errors) }
func (o operation) ActivationKinds() []string       { return slices.Clone(o.activation) }
func (o operation) BoundaryErrors() []BoundaryError { return slices.Clone(o.boundaryErrors) }

var operations = build()

func build() map[string]Operation {
	m := map[string]Operation{}
	fixed := func(name string, params []types.Parameter, out types.Type) {
		m[name] = operation{name: name, rule: Fixed, parameters: params, output: out}
	}
	one := func(name string, input, output types.Type) {
		fixed(name, []types.Parameter{{Name: ParamValue, Type: input}}, output)
	}
	binary := func(name string, input, output types.Type) {
		fixed(name, []types.Parameter{{Name: ParamLeft, Type: input}, {Name: ParamRight, Type: input}}, output)
	}
	for _, pair := range []struct {
		name string
		t    types.Type
	}{{IntNew, types.Int()}, {StringNew, types.String()}, {BoolNew, types.Bool()}, {FloatNew, types.Float()}} {
		one(pair.name, pair.t, pair.t)
	}
	one(BytesNew, types.String(), types.Bytes())
	fixed(PathNew, []types.Parameter{{Name: ParamPath, Type: types.String()}}, types.Path())
	fixed(FromScalarsNew, []types.Parameter{{Name: "scalar_list", Type: must(types.NewList(types.Scalar()))}}, types.String())
	for _, name := range []string{Upper, Lower} {
		one(name, types.String(), types.String())
	}
	one(StringLength, types.String(), types.Int())
	one(IntToString, types.Int(), types.String())
	one(IntToFloat, types.Int(), must(types.NewFallible(types.Float())))
	one(FloatToInt, types.Float(), types.Int())
	one(FloatToString, types.Float(), types.String())
	one(FloatSqrt, types.Float(), must(types.NewFallible(types.Float())))
	for _, name := range []string{FloatAdd, FloatSub, FloatMul, FloatDiv, FloatEqual, FloatLT, FloatLTE, FloatGT, FloatGTE} {
		out := types.Bool()
		if name == FloatAdd || name == FloatSub || name == FloatMul || name == FloatDiv {
			out = must(types.NewFallible(types.Float()))
		}
		fixed(name, []types.Parameter{{Name: ParamLeftOperand, Type: types.Float()}, {Name: ParamRightOperand, Type: types.Float()}}, out)
	}
	binary(StringEqual, types.String(), types.Bool())
	binary(ScalarEqual, types.Scalar(), types.Bool())
	for _, name := range []string{MathAdd, MathSub, MathMul} {
		binary(name, types.Int(), types.Int())
	}
	for _, name := range []string{MathLT, MathLTE, MathGT, MathGTE, MathEqual} {
		binary(name, types.Int(), types.Bool())
	}
	for _, name := range []string{MathDiv, MathMod} {
		binary(name, types.Int(), must(types.NewFallible(types.Int())))
	}
	for _, name := range []string{MathSum, MathProduct, MathMin, MathMax} {
		out := types.Int()
		if name == MathMin || name == MathMax {
			out = must(types.NewFallible(out))
		}
		fixed(name, []types.Parameter{{Name: ParamNumbers, Type: must(types.NewList(types.Int()))}}, out)
	}
	fixed(UTF8New, []types.Parameter{{Name: "bytes", Type: types.Bytes()}}, must(types.NewFallible(types.String())))
	fixed(FileRead, []types.Parameter{{Name: ParamPath, Type: types.Path()}}, must(types.NewFallible(types.Bytes())))
	fixed(FileAppend, []types.Parameter{{Name: ParamPath, Type: types.Path()}, {Name: ParamContent, Type: types.String()}}, types.Effect())
	fixed(FileWrite, []types.Parameter{{Name: ParamPath, Type: types.Path()}, {Name: ParamContent, Type: types.Bytes()}}, types.Effect())
	fixed(ResponseTextNew, []types.Parameter{{Name: "status", Type: types.Int()}, {Name: "body", Type: types.String()}}, types.Response())
	fixed(ResponseBytesNew, []types.Parameter{{Name: "status", Type: types.Int()}, {Name: "body", Type: types.Bytes()}}, types.Response())
	fixed(ErrorNew, []types.Parameter{{Name: "kind", Type: types.String()}, {Name: "message", Type: types.String()}, {Name: "input", Type: types.AnyRecord()}}, types.Error())
	for _, spec := range []struct {
		name   string
		rule   Rule
		params []string
	}{
		{ScalarNew, ScalarConstructor, []string{ParamValue}}, {Concat, StringConcat, nil}, {ListNew, ListConstructor, nil},
		{MapNew, MapConstructor, nil}, {RecordNew, RecordConstructor, nil},
		{RangeNew, RangeConstructor, []string{"start", "end", "include_start", "include_end"}},
		{Merge, RecordMerge, []string{"left_record", "right_record"}},
		{HasKey, MapLookup, []string{"target_map", "target_key"}}, {Get, MapLookup, []string{"target_map", "target_key"}},
		{Contains, RangeProcessor, []string{"target_range", "target_value"}}, {IsEmpty, RangeProcessor, []string{"target_range"}},
		{Length, ListLength, []string{"target_list"}}, {ServerNew, ServerConstructor, []string{"port", "handler", "onError"}},
		{ListMap, ListProcessor, nil}, {ListFilter, ListProcessor, nil}, {ListFold, ListProcessor, nil}, {ListFlatMap, ListProcessor, nil}, {ListUnfold, ListProcessor, nil},
	} {
		params := make([]types.Parameter, len(spec.params))
		for i, p := range spec.params {
			params[i].Name = p
		}
		m[spec.name] = operation{name: spec.name, rule: spec.rule, parameters: params}
	}
	for name, kind := range map[string]string{MathDiv: DivideByZero, MathMod: ModuloByZero, MathMin: EmptyList, MathMax: EmptyList, UTF8New: InvalidUTF8, FileRead: ReadFailed, Get: MissingKey} {
		o := m[name].(operation)
		o.errors = []string{kind}
		m[name] = o
	}
	for _, name := range []string{FloatAdd, FloatSub, FloatMul, FloatDiv, FloatSqrt, IntToFloat} {
		o := m[name].(operation)
		o.errors = []string{NumericNonFinite}
		switch name {
		case FloatDiv:
			o.errors = append(o.errors, NumericZero)
		case FloatSqrt:
			o.errors = append(o.errors, NumericDomain)
		}
		m[name] = o
	}
	o := m[FileRead].(operation)
	o.boundary = Read
	m[FileRead] = o
	for name, kind := range map[string]string{FileAppend: AppendFailed, FileWrite: WriteFailed, ServerNew: ServerStartFailed} {
		o := m[name].(operation)
		o.boundary = Effect
		if name == ServerNew {
			o.boundary = Server
			o.boundaryErrors = []BoundaryError{
				{HTTPRequest, InvalidRequest}, {HTTPRequest, RequestTooLarge},
				{HTTPRequest, RequestReadFailed}, {HTTPRespond, InvalidResponse},
			}
		}
		o.activation = []string{kind}
		m[name] = o
	}
	addTensors(m)
	return m
}
func Lookup(name string) (Operation, bool) { o, ok := operations[name]; return o, ok }
func All() []Operation {
	out := make([]Operation, 0, len(operations))
	for _, o := range operations {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b Operation) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}
func Predeclared(name string) bool {
	if _, ok := types.Named(name); ok {
		return true
	}
	switch name {
	case "List", "Map", "Range", "File", "HTTP", "UTF8", "Math", "Match", "Tensor", "FP32":
		return true
	}
	return false
}
func must[T types.Type](t T, e error) T {
	if e != nil {
		panic(e)
	}
	return t
}
