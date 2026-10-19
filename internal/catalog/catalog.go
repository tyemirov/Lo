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
	FlowProcessor
	ListLength
	ServerConstructor
	Gradient
	TensorBundleOperation
	OptimizerOperation
	CheckpointOperation
)

type Boundary uint8

const (
	Pure Boundary = iota
	Read
	Effect
	Server
)
const (
	ScalarNew            = "Scalar.new"
	IntNew               = "Int.new"
	StringNew            = "String.new"
	BoolNew              = "Bool.new"
	FloatNew             = "Float.new"
	FloatAdd             = "Float.add"
	FloatSub             = "Float.sub"
	FloatMul             = "Float.mul"
	FloatDiv             = "Float.div"
	FloatSqrt            = "Float.sqrt"
	FloatEqual           = "Float.eq"
	FloatLT              = "Float.lt"
	FloatLTE             = "Float.lte"
	FloatGT              = "Float.gt"
	FloatGTE             = "Float.gte"
	IntToFloat           = "Int.to_float"
	FloatToInt           = "Float.to_int"
	FloatToString        = "Float.to_string"
	BytesNew             = "Bytes.new"
	ListNew              = "List.new"
	MapNew               = "Map.new"
	RecordNew            = "Record.new"
	RangeNew             = "Range.new"
	ErrorNew             = "Error.new"
	PathNew              = "File.Path.new"
	ServerNew            = "HTTP.Server.new"
	HeadersNew           = "HTTP.Headers.new"
	HeaderValues         = "HTTP.Headers.values"
	ResponseNew          = "HTTP.Response.new"
	InvalidHeaders       = "HTTP.InvalidHeaders"
	ResponseTextNew      = "HTTP.Response.Text.new"
	ResponseBytesNew     = "HTTP.Response.Bytes.new"
	UTF8BytesNew         = "UTF8.Bytes.new"
	UTF8New              = "UTF8.String.new"
	FromScalarsNew       = "String.FromScalars.new"
	Concat               = "String.concat"
	Upper                = "String.upper"
	Lower                = "String.lower"
	StringLength         = "String.length"
	StringEqual          = "String.eq"
	ScalarEqual          = "Scalar.eq"
	IntToString          = "Int.to_string"
	MathAdd              = "Math.add"
	MathSub              = "Math.sub"
	MathMul              = "Math.mul"
	MathDiv              = "Math.div"
	MathMod              = "Math.mod"
	MathLT               = "Math.lt"
	MathLTE              = "Math.lte"
	MathGT               = "Math.gt"
	MathGTE              = "Math.gte"
	MathEqual            = "Math.eq"
	MathSum              = "Math.sum"
	MathProduct          = "Math.product"
	MathMin              = "Math.min"
	MathMax              = "Math.max"
	Contains             = "Range.contains"
	IsEmpty              = "Range.is_empty"
	HasKey               = "Map.has_key"
	Get                  = "Map.get"
	Merge                = "Record.merge"
	ListMap              = "List.map"
	ListFilter           = "List.filter"
	ListFold             = "List.fold"
	FlowFold             = "Flow.fold"
	ListFlatMap          = "List.flat_map"
	ListUnfold           = "List.unfold"
	Length               = "List.length"
	FileRead             = "File.read"
	FileList             = "File.list"
	FileOpenPaths        = "File.open_paths"
	FilePathEqual        = "File.Path.eq"
	OpenFilesFailed      = "File.OpenFilesFailed"
	DirectoryAbsent      = "File.DirectoryAbsent"
	FileRemoveCapability = "File.remove"
	FileWriteCapability  = "File.write"
	FileAppendCapability = "File.append"
	FileRemove           = "File.Remove.new"
	ListFailed           = "File.ListFailed"
	InvalidName          = "File.InvalidName"
	InvalidSelection     = "File.InvalidSelection"
	EntryChanged         = "File.EntryChanged"
	RemoveFailed         = "File.RemoveFailed"
	FileAppend           = "File.Append.new"
	FileWrite            = "File.Write.new"
	DivideByZero         = "Math.DivideByZero"
	ModuloByZero         = "Math.ModuloByZero"
	EmptyList            = "Math.EmptyList"
	InvalidUTF8          = "UTF8.InvalidBytes"
	MissingKey           = "Map.MissingKey"
	ReadFailed           = "File.ReadFailed"
	AppendFailed         = "File.AppendFailed"
	WriteFailed          = "File.WriteFailed"
	ServerStartFailed    = "HTTP.ServerStartFailed"
	NumericNonFinite     = "Numeric.NonFinite"
	NumericZero          = "Numeric.DivideByZero"
	NumericDomain        = "Numeric.Domain"
)
const (
	ParamValue        = "value"
	ParamLeft         = "left_value"
	ParamRight        = "right_value"
	ParamNumbers      = "number_list"
	ParamPath         = "file_path"
	ParamContent      = "file_content"
	ParamEntries      = "entries"
	ParamRoot         = "root"
	ParamLeftOperand  = "left"
	ParamRightOperand = "right"
)

const (
	ProcessExecutableNew       = "Process.Executable.new"
	ProcessCommandNew          = "Process.Command.new"
	ProcessRun                 = "Process.run"
	ProcessInvalidExecutable   = "Process.InvalidExecutable"
	ProcessInvalidCommand      = "Process.InvalidCommand"
	ProcessExecutionFailed     = "Process.ExecutionFailed"
	ProcessTerminationExitCode = "Process.Termination.exit_code"
	ProcessTerminationSignal   = "Process.Termination.signal"
	ProcessTerminationKind     = "Process.TerminationKind"
	HTTPListen                 = "HTTP.listen"
	URLNew                     = "HTTP.URL.new"
	MethodNew                  = "HTTP.Method.new"
	ClientRequestNew           = "HTTP.Client.Request.new"
	HTTPExchange               = "HTTP.request"
	HTTPExchangeFailed         = "HTTP.ExchangeFailed"
	InvalidURL                 = "HTTP.InvalidURL"
	InvalidMethod              = "HTTP.InvalidMethod"
	HTTPRequest                = "HTTP.request"
	HTTPRespond                = "HTTP.respond"
	RequestTooLarge            = "HTTP.RequestTooLarge"
	InvalidRequest             = "HTTP.InvalidRequest"
	RequestReadFailed          = "HTTP.RequestReadFailed"
	InvalidResponse            = "HTTP.InvalidResponse"
)

type Operation interface {
	Capability() string
	Name() string
	Rule() Rule
	Parameters() []types.Parameter
	Output() types.Type
	Boundary() Boundary
	TargetPosition() int
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
	targetPosition     int
	errors, activation []string
	boundaryErrors     []BoundaryError
}

func (operation) operation()            {}
func (o operation) TargetPosition() int { return o.targetPosition }
func (o operation) Capability() string {
	switch o.name {
	case ProcessCommandNew:
		return ProcessRun
	case ClientRequestNew:
		return HTTPExchange
	case FileWrite:
		return FileWriteCapability
	case FileAppend:
		return FileAppendCapability
	case FileRemove:
		return FileRemoveCapability
	}
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
	fixed(ProcessExecutableNew, []types.Parameter{{Name: "path", Type: types.Path()}}, must(types.NewFallible(types.ProcessExecutable())))
	fixed(ProcessCommandNew, []types.Parameter{{Name: "executable", Type: types.ProcessExecutable()}, {Name: "arguments", Type: must(types.NewList(types.String()))}, {Name: "environment", Type: must(types.NewMap(types.String(), types.String()))}, {Name: "directory", Type: types.Path()}, {Name: "stdin", Type: types.Bytes()}}, must(types.NewFallible(types.ProcessCommand())))
	executable := m[ProcessExecutableNew].(operation)
	executable.errors = []string{ProcessInvalidExecutable}
	m[ProcessExecutableNew] = executable
	command := m[ProcessCommandNew].(operation)
	command.boundary = Effect
	command.errors = []string{ProcessInvalidCommand}
	command.activation = []string{ProcessExecutionFailed}
	m[ProcessCommandNew] = command
	for _, name := range []string{ProcessTerminationExitCode, ProcessTerminationSignal} {
		one(name, types.ProcessTermination(), must(types.NewFallible(types.Int())))
		accessor := m[name].(operation)
		accessor.errors = []string{ProcessTerminationKind}
		m[name] = accessor
	}
	one(BytesNew, types.String(), types.Bytes())
	fixed(URLNew, []types.Parameter{{Name: "text", Type: types.String()}}, must(types.NewFallible(types.URL())))
	fixed(MethodNew, []types.Parameter{{Name: "text", Type: types.String()}}, must(types.NewFallible(types.Method())))
	fixed(ClientRequestNew, []types.Parameter{{Name: "method", Type: types.Method()}, {Name: "url", Type: types.URL()}, {Name: "headers", Type: types.Headers()}, {Name: "body", Type: types.Bytes()}}, must(types.NewFallible(types.ClientRequest())))
	for name, kind := range map[string]string{URLNew: InvalidURL, MethodNew: InvalidMethod, ClientRequestNew: InvalidRequest} {
		o := m[name].(operation)
		o.errors = []string{kind}
		m[name] = o
	}
	client := m[ClientRequestNew].(operation)
	client.boundary = Effect
	client.targetPosition = 1
	client.activation = []string{HTTPExchangeFailed}
	m[ClientRequestNew] = client
	fixed(UTF8BytesNew, []types.Parameter{{Name: "text", Type: types.String()}}, types.Bytes())
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
	fixed(FilePathEqual, []types.Parameter{{Name: ParamLeftOperand, Type: types.Path()}, {Name: ParamRightOperand, Type: types.Path()}}, types.Bool())
	fixed(FileOpenPaths, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, must(types.NewFallible(must(types.NewList(types.Path())))))
	fixed(FileList, []types.Parameter{{Name: ParamRoot, Type: types.Path()}}, must(types.NewFallible(must(types.NewList(types.FileEntry())))))
	fixed(FileRemove, []types.Parameter{{Name: ParamEntries, Type: must(types.NewList(types.FileEntry()))}}, must(types.NewFallible(types.FileRemove())))
	fixed(FileAppend, []types.Parameter{{Name: ParamPath, Type: types.Path()}, {Name: ParamContent, Type: types.Bytes()}}, types.FileAppend())
	fixed(FileWrite, []types.Parameter{{Name: ParamPath, Type: types.Path()}, {Name: ParamContent, Type: types.Bytes()}}, types.FileWrite())
	fixed(HeadersNew, []types.Parameter{{Name: "entries", Type: types.HeaderEntries()}}, must(types.NewFallible(types.Headers())))
	fixed(HeaderValues, []types.Parameter{{Name: "headers", Type: types.Headers()}, {Name: "name", Type: types.String()}}, must(types.NewFallible(must(types.NewList(types.Bytes())))))
	fixed(ResponseNew, []types.Parameter{{Name: "status", Type: types.Int()}, {Name: "headers", Type: types.Headers()}, {Name: "body", Type: types.Bytes()}}, types.Response())
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
		{FlowFold, FlowProcessor, nil}, {ListMap, ListProcessor, nil}, {ListFilter, ListProcessor, nil}, {ListFold, ListProcessor, nil}, {ListFlatMap, ListProcessor, nil}, {ListUnfold, ListProcessor, nil},
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
	listing := m[FileList].(operation)
	listing.boundary = Read
	listing.errors = []string{DirectoryAbsent, ListFailed, InvalidName}
	m[FileList] = listing
	probe := m[FileOpenPaths].(operation)
	probe.boundary = Read
	probe.errors = []string{OpenFilesFailed}
	m[FileOpenPaths] = probe
	removal := m[FileRemove].(operation)
	removal.boundary = Effect
	removal.errors = []string{InvalidSelection}
	removal.activation = []string{EntryChanged, RemoveFailed}
	m[FileRemove] = removal
	o := m[FileRead].(operation)
	o.boundary = Read
	m[FileRead] = o
	for name, kind := range map[string]string{FileAppend: AppendFailed, FileWrite: WriteFailed, ServerNew: ServerStartFailed} {
		o := m[name].(operation)
		o.boundary = Effect
		if name == ServerNew {
			o.boundary = Server
			o.parameters[0].Type = types.Int()
			o.boundaryErrors = []BoundaryError{
				{HTTPRequest, InvalidRequest}, {HTTPRequest, RequestTooLarge},
				{HTTPRequest, RequestReadFailed}, {HTTPRespond, InvalidResponse},
			}
		}
		o.activation = []string{kind}
		m[name] = o
	}
	for _, name := range []string{HeadersNew, HeaderValues} {
		op := m[name].(operation)
		op.errors = []string{InvalidHeaders}
		m[name] = op
	}
	addTensors(m)
	addAutodiff(m)
	addParameters(m)
	addOptimizers(m)
	addCheckpoints(m)
	addComputeFailures(m)
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
	case "List", "Map", "Range", "File", "HTTP", "Process", "UTF8", "Math", "Match", "Tensor", "FP32", "Autodiff", "Parameters", "Gradients", "Optimizer", "Checkpoint":
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

// ActivationResult returns the success type for each nominal committable target.
func ActivationResult(target types.Type) (types.Type, bool) {
	if target == nil {
		return nil, false
	}
	definition, ok := activationTargets[target.Kind()]
	return definition.result, ok
}

// ActivationOperation returns the constructor metadata for a nominal target.
func ActivationOperation(target types.Type) (Operation, bool) {
	if target == nil {
		return nil, false
	}
	definition, ok := activationTargets[target.Kind()]
	if !ok {
		return nil, false
	}
	return Lookup(definition.constructor)
}

type activationTarget struct {
	constructor string
	result      types.Type
}

var activationTargets = map[types.Kind]activationTarget{
	types.ProcessCommandKind: {ProcessCommandNew, types.ProcessResult()},
	types.FileWriteKind:      {FileWrite, types.FileWriteReceipt()},
	types.FileAppendKind:     {FileAppend, types.FileAppendReceipt()},
	types.FileRemoveKind:     {FileRemove, types.FileRemoveReceipt()},
	types.ServerKind:         {ServerNew, types.ServerReceipt()},
	types.ClientRequestKind:  {ClientRequestNew, types.Response()},
}
