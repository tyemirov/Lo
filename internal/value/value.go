package value

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type Value interface {
	json.Marshaler
	Type() types.Type
	Weight() uint64
	value()
}
type base struct {
	typ    types.Type
	weight uint64
}

func (base) value()                             {}
func (b base) Type() types.Type                 { return b.typ }
func (b base) Weight() uint64                   { return b.weight }
func newBase(t types.Type, payload uint64) base { return base{t, 16 + payload} }

type Integer interface {
	Value
	Big() *big.Int
}
type integer struct {
	base
	n *big.Int
}

func (v integer) Big() *big.Int                { return new(big.Int).Set(v.n) }
func (v integer) MarshalJSON() ([]byte, error) { return encode(v, v.n.String()) }
func NewInt(n *big.Int) (Integer, error) {
	if n == nil {
		return nil, fmt.Errorf("absent integer")
	}
	return integer{newBase(types.Int(), uint64((n.BitLen()+7)/8)), new(big.Int).Set(n)}, nil
}
func Int64(n int64) Integer { return must(NewInt(big.NewInt(n))) }

type Text interface {
	Value
	Text() string
}
type text struct {
	base
	text string
}

func (v text) Text() string                 { return v.text }
func (v text) MarshalJSON() ([]byte, error) { return encode(v, v.text) }
func NewText(t types.Type, s string) (Text, error) {
	if t == nil || !utf8.ValidString(s) {
		return nil, fmt.Errorf("text requires a type and valid UTF-8")
	}
	switch t.Kind() {
	case types.StringKind, types.PathKind:
	case types.MethodKind:
		if _, err := CanonicalHeaderName(s); err != nil {
			return nil, fmt.Errorf("HTTP method must be a nonempty ASCII token")
		}
	case types.ScalarKind:
		if utf8.RuneCountInString(s) != 1 {
			return nil, fmt.Errorf("scalar requires one Unicode scalar")
		}
	default:
		return nil, fmt.Errorf("invalid text type %s", t)
	}
	return text{newBase(t, uint64(len(s))), s}, nil
}

type Boolean interface {
	Value
	Bool() bool
}
type boolean bool

func (boolean) value()                         {}
func (boolean) Type() types.Type               { return types.Bool() }
func (boolean) Weight() uint64                 { return 17 }
func (v boolean) Bool() bool                   { return bool(v) }
func (v boolean) MarshalJSON() ([]byte, error) { return encode(v, bool(v)) }
func Bool(b bool) Boolean                      { return boolean(b) }

type null struct{}

func (null) value()                         {}
func (null) Type() types.Type               { return types.Null() }
func (null) Weight() uint64                 { return 16 }
func (v null) MarshalJSON() ([]byte, error) { return encode(v, nil) }
func Null() Value                           { return null{} }

type Float interface {
	Value
	Number() float64
}
type float struct {
	base
	number float64
}

func (v float) Number() float64 { return v.number }
func (v float) MarshalJSON() ([]byte, error) {
	return encode(v, strconv.FormatFloat(v.number, 'g', -1, 64))
}
func NewFloat(n float64) (Float, error) {
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return nil, fmt.Errorf("Float requires a finite value")
	}
	return float{newBase(types.Float(), 8), n}, nil
}

type Bytes interface {
	Value
	Len() int
	Bytes() []byte
}
type bytes struct {
	base
	data []byte
}

func (v bytes) Len() int                     { return len(v.data) }
func (v bytes) Bytes() []byte                { return slices.Clone(v.data) }
func (v bytes) MarshalJSON() ([]byte, error) { return encode(v, v.data) }
func NewBytes(data []byte) Bytes {
	return bytes{newBase(types.Bytes(), uint64(len(data))), append([]byte{}, data...)}
}

type List interface {
	Value
	Items() []Value
	Len() int
}
type list struct {
	base
	items []Value
}

func (v list) Len() int                     { return len(v.items) }
func (v list) Items() []Value               { return slices.Clone(v.items) }
func (v list) MarshalJSON() ([]byte, error) { return encode(v, v.items) }
func NewList(t types.List, items []Value) (List, error) {
	if t == nil {
		return nil, fmt.Errorf("absent list type")
	}
	for _, v := range items {
		if v == nil || !types.Equal(v.Type(), t.Element()) {
			return nil, fmt.Errorf("invalid %s element", t)
		}
	}
	var weight uint64
	for _, v := range items {
		weight = addWeight(weight, v.Weight())
	}
	return list{newBase(t, weight), append([]Value{}, items...)}, nil
}

type Field struct {
	Name  string
	Value Value
}
type Record interface {
	Value
	Fields() []Field
	Field(string) Value
}
type record struct {
	base
	fields []Field
}

func (v record) Fields() []Field { return slices.Clone(v.fields) }
func (v record) Field(name string) Value {
	for _, f := range v.fields {
		if f.Name == name {
			return f.Value
		}
	}
	panic("absent checked record field: " + name)
}
func (v record) MarshalJSON() ([]byte, error) {
	fields := make(map[string]Value, len(v.fields))
	for _, f := range v.fields {
		fields[f.Name] = f.Value
	}
	return encode(v, fields)
}
func NewRecord(fields []Field) (Record, error) {
	fields = append([]Field{}, fields...)
	slices.SortFunc(fields, func(a, b Field) int { return strings.Compare(a.Name, b.Name) })
	ts := make([]types.Field, len(fields))
	for i, f := range fields {
		if f.Value == nil {
			return nil, fmt.Errorf("absent record value %s", f.Name)
		}
		ts[i] = types.Field{Name: f.Name, Type: f.Value.Type()}
	}
	t, err := types.NewRecord(ts)
	if err != nil {
		return nil, err
	}
	var weight uint64
	for _, f := range fields {
		weight = addWeight(weight, uint64(len(f.Name))+f.Value.Weight())
	}
	return record{newBase(t, weight), fields}, nil
}
func RecordView(r Record) Record { return record{base{types.AnyRecord(), r.Weight()}, r.Fields()} }

type Entry struct {
	Key   Value `json:"key"`
	Value Value `json:"value"`
}
type Map interface {
	Value
	Entries() []Entry
	Get(Value) (Value, bool)
}
type mapValue struct {
	base
	entries []Entry
	index   map[string]int
}

func (v mapValue) Entries() []Entry { return slices.Clone(v.entries) }
func (v mapValue) Get(key Value) (Value, bool) {
	i, ok := v.index[keyID(key)]
	if !ok {
		return nil, false
	}
	return v.entries[i].Value, true
}
func (v mapValue) MarshalJSON() ([]byte, error) { return encode(v, v.entries) }
func NewMap(t types.Map, entries []Entry) (Map, error) {
	if t == nil {
		return nil, fmt.Errorf("absent map type")
	}
	out := mapValue{newBase(t, 0), []Entry{}, map[string]int{}}
	for _, e := range entries {
		if e.Key == nil || e.Value == nil || !types.Equal(t.Key(), e.Key.Type()) || !types.Equal(t.Value(), e.Value.Type()) {
			return nil, fmt.Errorf("invalid %s entry", t)
		}
		key := keyID(e.Key)
		if i, ok := out.index[key]; ok {
			out.entries[i] = e
		} else {
			out.index[key] = len(out.entries)
			out.entries = append(out.entries, e)
		}
	}
	for _, e := range out.entries {
		out.weight = addWeight(out.weight, addWeight(e.Key.Weight(), e.Value.Weight()))
	}
	return out, nil
}
func keyID(v Value) string {
	switch v.Type().Kind() {
	case types.IntKind:
		return v.(Integer).Big().String()
	case types.StringKind, types.ScalarKind:
		return v.(Text).Text()
	case types.BoolKind:
		return strconv.FormatBool(v.(Boolean).Bool())
	case types.NullKind:
		return "null"
	default:
		panic("noncomparable checked key")
	}
}
func Equal(a, b Value) bool { return types.Equal(a.Type(), b.Type()) && keyID(a) == keyID(b) }
func Compare(a, b Value) int {
	switch a.Type().Kind() {
	case types.IntKind:
		return a.(Integer).Big().Cmp(b.(Integer).Big())
	case types.StringKind, types.ScalarKind:
		return strings.Compare(a.(Text).Text(), b.(Text).Text())
	default:
		panic("unordered checked value")
	}
}

type Range interface {
	Value
	Start() Value
	End() Value
	IncludeStart() bool
	IncludeEnd() bool
	Contains(Value) bool
	Empty() bool
}
type rangeValue struct {
	base
	start, end               Value
	includeStart, includeEnd bool
}

func (v rangeValue) Start() Value       { return v.start }
func (v rangeValue) End() Value         { return v.end }
func (v rangeValue) IncludeStart() bool { return v.includeStart }
func (v rangeValue) IncludeEnd() bool   { return v.includeEnd }
func (v rangeValue) Contains(item Value) bool {
	a, b := Compare(item, v.start), Compare(item, v.end)
	return (a > 0 || a == 0 && v.includeStart) && (b < 0 || b == 0 && v.includeEnd)
}
func (v rangeValue) Empty() bool {
	cmp := Compare(v.start, v.end)
	if cmp > 0 {
		return true
	}
	if cmp == 0 {
		return !v.includeStart || !v.includeEnd
	}
	if v.includeStart || v.includeEnd {
		return false
	}
	switch v.start.Type().Kind() {
	case types.IntKind:
		return new(big.Int).Sub(v.end.(Integer).Big(), v.start.(Integer).Big()).Cmp(big.NewInt(1)) == 0
	case types.ScalarKind:
		a, _ := utf8.DecodeRuneInString(v.start.(Text).Text())
		b, _ := utf8.DecodeRuneInString(v.end.(Text).Text())
		next := a + 1
		if next == 0xd800 {
			next = 0xe000
		}
		return next == b
	case types.StringKind:
		return v.end.(Text).Text() == v.start.(Text).Text()+"\x00"
	default:
		panic("invalid checked range")
	}
}
func (v rangeValue) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Start        Value `json:"start"`
		End          Value `json:"end"`
		IncludeStart bool  `json:"include_start"`
		IncludeEnd   bool  `json:"include_end"`
	}{v.start, v.end, v.includeStart, v.includeEnd})
}
func NewRange(t types.Range, start, end Value, includeStart, includeEnd bool) (Range, error) {
	if t == nil || start == nil || end == nil || !types.Equal(start.Type(), t.Element()) || !types.Equal(end.Type(), t.Element()) {
		return nil, fmt.Errorf("invalid range endpoints")
	}
	return rangeValue{newBase(t, addWeight(start.Weight(), end.Weight())+2), start, end, includeStart, includeEnd}, nil
}

func addWeight(a, b uint64) uint64 {
	const ceiling = 1 << 62
	if a >= ceiling || b >= ceiling || a > ceiling-b {
		return ceiling
	}
	return a + b
}

func encode(v Value, data any) ([]byte, error) {
	return json.Marshal(struct {
		Type  string `json:"type"`
		Value any    `json:"value"`
	}{v.Type().String(), data})
}

func TextResult(v Value) string {
	switch x := v.(type) {
	case Integer:
		return x.Big().String()
	case Text:
		return x.Text()
	case Boolean:
		return strconv.FormatBool(x.Bool())
	case Float:
		return strconv.FormatFloat(x.Number(), 'g', -1, 64)
	case Fallible:
		result, failure := x.Outcome()
		if failure != nil {
			panic("unhandled error in successful command result")
		}
		return TextResult(result)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		return string(data)
	}
}
