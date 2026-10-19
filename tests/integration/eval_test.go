package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"math/big"
	"reflect"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type observedBoundary struct {
	reads   []string
	read    func(string, int) (value.Bytes, error)
	commits int
}

func (b *observedBoundary) Read(_ context.Context, path value.Text) (value.Bytes, error) {
	b.reads = append(b.reads, path.Text())
	return b.read(path.Text(), len(b.reads))
}
func (b *observedBoundary) List(context.Context, value.Text) (value.List, error) {
	return nil, errors.New("unexpected file listing")
}
func (b *observedBoundary) OpenPaths(context.Context, value.Text) (value.List, error) {
	return nil, errors.New("unexpected open-file observation")
}
func (b *observedBoundary) Commit(_ context.Context, _ value.Target) (value.Value, error) {
	b.commits++
	return nil, errors.New("injected activation failure")
}
func evaluate(t *testing.T, text string, b execution.Boundary) (value.Value, error) {
	t.Helper()
	s, err := source.New("eval.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(s)
	if d != nil {
		t.Fatal(d)
	}
	return eval.Run(context.Background(), p, b, execution.DefaultBudget(), device.DefaultConfig())
}

func TestEvaluatorReadOrder(t *testing.T) {
	b := &observedBoundary{read: func(path string, n int) (value.Bytes, error) {
		return value.NewBytes([]byte(fmt.Sprintf("%s:%d;", path, n))), nil
	}}
	v, err := evaluate(t, `
flow Read(path: String) -> String! { bytes = File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
flow Join(a: String, b: String) -> String { return String.concat(a,b) }
flow Main() -> String! {
  unused = Read("unused")
  first = Read("repeat")
  second = Read("repeat")
  ordered = Join(b: Read("b"), a: Read("a"))
  return String.concat(first,second,ordered)
}
return Main()`, b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.reads, []string{"unused", "repeat", "repeat", "b", "a"}) || value.TextResult(v) != "repeat:2;repeat:3;a:5;b:4;" {
		t.Fatalf("order: %v, value %s", b.reads, value.TextResult(v))
	}
	if b.commits != 0 {
		t.Fatal("read activated a commit")
	}
}

func TestEvaluatorMatchRegions(t *testing.T) {
	b := &observedBoundary{read: func(path string, n int) (value.Bytes, error) { return value.NewBytes([]byte(path)), nil }}
	v, err := evaluate(t, `
flow Handle(error: Error) -> String { return "failed" }
flow Read(path: String) -> String onError Handle { bytes = File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
return Match.new(Read("selector"),
  "miss": Read("unselected"),
  "selector": Read("selected"),
  "unreached": Read("unreached-body"),
  _: Read("default")
)`, b)
	if err != nil {
		t.Fatal(err)
	}
	if value.TextResult(v) != "selected" || !reflect.DeepEqual(b.reads, []string{"selector", "selected"}) {
		t.Fatalf("regions: %v, %s", b.reads, value.TextResult(v))
	}
}

func TestEvaluatorErrorPrecedence(t *testing.T) {
	b := &observedBoundary{read: func(path string, n int) (value.Bytes, error) {
		return nil, fmt.Errorf("read %s: injected failure", path)
	}}
	v, err := evaluate(t, `
flow H(error: Error) -> Error { return error }
flow Main() -> Error onError H {
  unused = File.read(File.Path.new("first"))
  skipped = File.read(File.Path.new("later"))
  return Error.new("missed", "", Record.new())
}
return Main()`, b)
	if err != nil {
		t.Fatal(err)
	}
	e := v.(value.Error)
	if e.Kind() != "File.ReadFailed" || len(e.Trace()) != 1 || !reflect.DeepEqual(b.reads, []string{"first"}) {
		t.Fatalf("propagation: %v, %+v", b.reads, e)
	}
	if got := e.Input().Field("file_path").(value.Text).Text(); got != "first" {
		t.Fatal(got)
	}
	b.reads = nil
	v, err = evaluate(t, `unused = File.read(File.Path.new("first")); later = File.read(File.Path.new("later")); return 7`, b)
	if err != nil || value.TextResult(v) != "7" || !reflect.DeepEqual(b.reads, []string{"first", "later"}) {
		t.Fatalf("stored top-level failures: %v, %v", b.reads, err)
	}
}

func TestEvaluatorImmutableResults(t *testing.T) {
	b := &observedBoundary{read: func(string, int) (value.Bytes, error) { t.Fatal("unexpected read"); return nil, nil }}
	v, err := evaluate(t, `return Record.new(n: 18446744073709551616, bytes: Bytes.new("abc"), items: List.new(1,2), table: Map.new(Record.new(key: "a", value: 1)))`, b)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	r := v.(value.Record)
	r.Field("n").(value.Integer).Big().Set(big.NewInt(0))
	r.Field("bytes").(value.Bytes).Bytes()[0] = 'z'
	r.Field("items").(value.List).Items()[0] = value.Int64(9)
	r.Field("table").(value.Map).Entries()[0].Value = value.Int64(9)
	r.Fields()[0].Value = value.Null()
	after, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("value changed: %s -> %s", before, after)
	}
}

func TestEvaluatorCancellation(t *testing.T) {
	s, err := source.New("cancel.lo", "return 1")
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(s)
	if d != nil {
		t.Fatal(d)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &observedBoundary{read: func(string, int) (value.Bytes, error) { t.Fatal("unexpected read"); return nil, nil }}
	_, err = eval.Run(ctx, p, b, execution.DefaultBudget(), device.DefaultConfig())
	if h, ok := err.(execution.HostError); !ok || h.Code() != "runtime.cancelled" || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestEvaluatorBoundaryHostFailures(t *testing.T) {
	for _, cause := range []error{context.Canceled, fmt.Errorf("adapter: %w", execution.Denied("File.read", "input"))} {
		b := &observedBoundary{read: func(string, int) (value.Bytes, error) { return nil, cause }}
		_, err := evaluate(t, `flow H(e: Error) -> Bytes { return Bytes.new("caught") }; flow F() -> Bytes onError H { return File.read(File.Path.new("input")) }; return F()`, b)
		var host execution.HostError
		if !errors.As(err, &host) {
			t.Fatalf("host failure entered a Lo handler: %v", err)
		}
	}
}

func FuzzRuntimeSource(f *testing.F) {
	for _, example := range builtinExamples() {
		f.Add(example.source)
	}
	for _, text := range []string{
		`flow H(e: Error) -> Error { return e }; flow F() -> Error onError H { unused = Math.div(1, 0); return Error.new("missed", "", Record.new()) }; return F()`,
		`flow F() -> List<Int!> { return List.new(1, Math.div(1, 0)) }; return F()`,
		`return Match.new(1, Range.new(1, 3, false, false): 2, _: 3)`,
		`e = File.Write.new(File.Path.new("fuzz"), Bytes.new("data")); commit e`,
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 64*1024 {
			t.Skip("fuzz input exceeds test bound")
		}
		s, err := source.New("fuzz.lo", text)
		if err != nil {
			t.Fatal(err)
		}
		p, d := compiler.Check(s)
		if d != nil {
			return
		}
		b := &observedBoundary{read: func(string, int) (value.Bytes, error) { return value.NewBytes([]byte("input")), nil }}
		budget, err := execution.NewBudget(1000)
		if err != nil {
			t.Fatal(err)
		}
		v, err := eval.Run(context.Background(), p, b, budget, device.DefaultConfig())
		if err != nil {
			if v != nil {
				t.Fatal("failed execution published a result")
			}
			var failure execution.Failure
			var host execution.HostError
			if !errors.As(err, &failure) && !errors.As(err, &host) {
				t.Fatalf("unclassified runtime failure: %v", err)
			}
			return
		}
		if v == nil {
			if p.ResultType() != nil {
				t.Fatal("missing checked result")
			}
			return
		}
		if p.ResultType() == nil || !types.Equal(v.Type(), p.ResultType()) {
			t.Fatalf("result type: %s, expected %v", v.Type(), p.ResultType())
		}
		if _, err := json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	})
}
