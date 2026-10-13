package value

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type Target interface {
	Value
	target()
}
type Effect interface {
	Target
	Operation() catalog.Operation
	Path() Text
	Content() Value
}
type effect struct {
	base
	operation catalog.Operation
	path      Text
	content   Value
}

func (effect) target()                        {}
func (v effect) Operation() catalog.Operation { return v.operation }
func (v effect) Path() Text                   { return v.path }
func (v effect) Content() Value               { return v.content }
func (v effect) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Operation string `json:"operation"`
		Path      Text   `json:"file_path"`
		Content   Value  `json:"file_content"`
	}{v.operation.Name(), v.path, v.content})
}
func NewEffect(op catalog.Operation, path Text, content Value) (Effect, error) {
	if op == nil || path == nil || path.Type().Kind() != types.PathKind || content == nil {
		return nil, fmt.Errorf("incomplete file effect")
	}
	if op.Name() == catalog.FileAppend && content.Type().Kind() == types.StringKind || op.Name() == catalog.FileWrite && content.Type().Kind() == types.BytesKind {
		return effect{newBase(types.Effect(), addWeight(uint64(len(op.Name())), addWeight(path.Weight(), content.Weight()))), op, path, content}, nil
	}
	return nil, fmt.Errorf("invalid file effect")
}

type Server interface {
	Target
	Port() Integer
	Handler() ir.FlowID
	OnError() ir.FlowID
}
type server struct {
	base
	port             Integer
	handler, onError ir.FlowID
}

func (server) target()              {}
func (v server) Port() Integer      { return v.port }
func (v server) Handler() ir.FlowID { return v.handler }
func (v server) OnError() ir.FlowID { return v.onError }
func (v server) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Port    Integer   `json:"port"`
		Handler ir.FlowID `json:"handler"`
		OnError ir.FlowID `json:"on_error"`
	}{v.port, v.handler, v.onError})
}
func NewServer(port Integer, handler, onError ir.FlowID) (Server, error) {
	if port == nil || handler == "" || onError == "" {
		return nil, fmt.Errorf("incomplete server target")
	}
	return server{newBase(types.Server(), port.Weight()+uint64(len(handler)+len(onError))), port, handler, onError}, nil
}

type HTTPValue interface {
	Value
	Fields() Record
}
type httpValue struct {
	base
	fields Record
}

func (v httpValue) Fields() Record { return v.fields }
func (v httpValue) MarshalJSON() ([]byte, error) {
	fields := map[string]Value{}
	for _, f := range v.fields.Fields() {
		fields[f.Name] = f.Value
	}
	return encode(v, fields)
}
func NewHTTP(t types.Type, fields Record) (HTTPValue, error) {
	if t == nil || fields == nil || t.Kind() != types.RequestKind && t.Kind() != types.ResponseKind {
		return nil, fmt.Errorf("invalid HTTP value type")
	}
	wanted := []string{"status", "headers", "body"}
	if t.Kind() == types.RequestKind {
		wanted = []string{"method", "path", "headers", "body"}
	}
	if len(fields.Fields()) != len(wanted) {
		return nil, fmt.Errorf("invalid HTTP field count")
	}
	actual := map[string]types.Type{}
	for _, f := range fields.Fields() {
		actual[f.Name] = f.Value.Type()
	}
	for _, name := range wanted {
		expected, _ := types.FieldType(t, name)
		got := actual[name]
		if got == nil || !types.Equal(got, expected) {
			return nil, fmt.Errorf("invalid HTTP field %s", name)
		}
	}
	return httpValue{newBase(t, fields.Weight()), fields}, nil
}
