package integration_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func checked(t *testing.T, text string) ir.Program {
	t.Helper()
	src, err := source.New("contract.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	graph, d := compiler.Check(src)
	if d != nil {
		t.Fatal(d)
	}
	if graph == nil {
		t.Fatal("accepted source has no graph")
	}
	return graph
}
func flowNamed(t *testing.T, p ir.Program, name string) ir.Flow {
	t.Helper()
	for _, f := range p.Flows() {
		if f.Name() == name {
			return f
		}
	}
	t.Fatalf("missing flow %s", name)
	return nil
}
func TestCheckedGraphContracts(t *testing.T) {
	t.Run("pipe-and-source-spans", func(t *testing.T) {
		text := "x = 4; return x |> Math.sub(1)"
		p := checked(t, text)
		binding := p.Body()[0].(ir.Binding)
		call := p.Body()[1].(ir.Return).Value().(ir.BuiltinCall)
		if call.Operation().Name() != catalog.MathSub || call.Type().String() != "Int" {
			t.Fatalf("wrong lowered call: %v", call)
		}
		a := call.Arguments()
		first := a[0].(ir.ValueArgument).Value().(ir.Reference)
		if first.Binding() != binding.ID() || a[0].Position() != 0 || a[1].Position() != 1 {
			t.Fatal("pipe did not supply the first argument")
		}
		if got := text[call.Span().Start().Offset:call.Span().End().Offset]; got != "x |> Math.sub(1)" {
			t.Fatalf("call span: %s", got)
		}
		if got := text[first.Span().Start().Offset:first.Span().End().Offset]; got != "x" {
			t.Fatalf("pipe input span: %s", got)
		}
	})
	t.Run("named-argument-evaluation-order", func(t *testing.T) {
		p := checked(t, `flow F(x: Int) -> Int { return x }; return Math.sub(right_value: F(2), left_value: F(9))`)
		args := p.Body()[0].(ir.Return).Value().(ir.BuiltinCall).Arguments()
		if args[0].Position() != 1 || args[1].Position() != 0 || args[0].Span().Start().Offset >= args[1].Span().Start().Offset {
			t.Fatal("argument evaluation order changed")
		}
	})
	t.Run("propagation-and-stored-fallibility", func(t *testing.T) {
		p := checked(t, `flow F() -> Int! { x = Math.div(1, 0); return x }; flow G() -> List<Int!> { return List.new(Match.new(1, 1: Math.div(1, 0), _: 2)) }`)
		f := flowNamed(t, p, "F")
		propagation := f.Body()[0].(ir.Binding).Value().(ir.Conversion)
		if propagation.Conversion() != ir.Propagate || propagation.Type().String() != "Int" || propagation.Input().Type().String() != "Int!" {
			t.Fatal("missing explicit propagation")
		}
		returned := f.Body()[1].(ir.Return).Value().(ir.Conversion)
		if returned.Conversion() != ir.Success || returned.Type().String() != "Int!" {
			t.Fatal("missing successful return tag")
		}
		g := flowNamed(t, p, "G")
		list := g.Body()[0].(ir.Return).Value().(ir.BuiltinCall)
		match := list.Arguments()[0].(ir.ValueArgument).Value().(ir.Match)
		if match.Type().String() != "Int!" || len(match.Cases()) != 2 {
			t.Fatal("match did not retain fallibility")
		}
		if match.Cases()[0].Body().(ir.BuiltinCall).Operation().Name() != catalog.MathDiv {
			t.Fatal("branch failure was moved outside its region")
		}
		if match.Cases()[1].Body().(ir.Conversion).Conversion() != ir.Success {
			t.Fatal("total branch lacks success tag")
		}
	})
	t.Run("commit-sites", func(t *testing.T) {
		p := checked(t, `e = File.Append.new(File.Path.new("unused"), UTF8.Bytes.new("x")); commit e; commit e`)
		first := p.Body()[1].(ir.Commit)
		second := p.Body()[2].(ir.Commit)
		if first.Target().(ir.Reference).Binding() != second.Target().(ir.Reference).Binding() {
			t.Fatal("commits changed target identity")
		}
		if first.Span().Start().Offset == second.Span().Start().Offset {
			t.Fatal("commit sites were merged")
		}
	})
	t.Run("effect-summaries-and-adapter-construction", func(t *testing.T) {
		p := checked(t, `flow F(p: File.Path) -> Bytes! { return File.read(p) }; flow G(p: File.Path) -> Bytes! { return F(p) }; flow H(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; flow W(r: HTTP.Request) -> HTTP.Response onError H { e = File.Append.new(File.Path.new("unused"), UTF8.Bytes.new("x")); commit e; return HTTP.Response.Text.new(200, "ok") }; flow S() -> HTTP.Server { return HTTP.Server.new(8080, W, H) }`)
		g := flowNamed(t, p, "G")
		if !g.Effects().Reads || g.Effects().Commits {
			t.Fatal("read summary was lost or became activation")
		}
		w := flowNamed(t, p, "W")
		if !w.Effects().Commits || w.Handler() == "" {
			t.Fatal("self-handler concealed activation")
		}
		s := flowNamed(t, p, "S")
		if s.Effects().Commits || s.Effects().Reads {
			t.Fatal("adapter construction invoked a handler")
		}
		if len(s.Body()[0].(ir.Return).Value().(ir.BuiltinCall).Arguments()) != 3 {
			t.Fatal("adapter references missing")
		}
	})
	t.Run("immutable-public-views", func(t *testing.T) {
		p := checked(t, `flow F(x: Record{a: Int}) -> Int { return x.a }; return F(Record.new(a: 1))`)
		flows := p.Flows()
		f := flows[0]
		flows[0] = nil
		body := p.Body()
		body[0] = nil
		parameters := f.Parameters()
		parameters[0].Name = "changed"
		fields := f.Parameters()[0].Type.(types.Record).Fields()
		fields[0].Name = "changed"
		fbody := f.Body()
		fbody[0] = nil
		if p.Flows()[0] == nil || p.Body()[0] == nil || f.Parameters()[0].Name != "x" || f.Parameters()[0].Type.(types.Record).Fields()[0].Name != "a" || f.Body()[0] == nil {
			t.Fatal("graph view changed the checked program")
		}
	})
	t.Run("diagnostic-prevents-publication", func(t *testing.T) {
		for _, text := range []string{`return Math.add(1, "x")`, `flow Unused() -> Int { return "x" }`, `return List.new(`} {
			src, err := source.New("invalid.lo", text)
			if err != nil {
				t.Fatal(err)
			}
			graph, d := compiler.Check(src)
			if graph != nil || d == nil {
				t.Fatalf("invalid source published a graph: %v, %v", graph, d)
			}
		}
	})
}
func TestVerifierDoesNotExecuteBoundaries(t *testing.T) {
	existing := sourceFile(t, "original")
	absent := existing + ".missing"
	text := fmt.Sprintf(`data = File.read(File.Path.new(%q)); e = File.Write.new(File.Path.new(%q), Bytes.new("replacement")); commit e; return data`, absent, existing)
	result := run(t, "check", sourceFile(t, text), "--format", "json")
	if result.status != 0 || result.stderr != "" {
		t.Fatalf("verification executed a boundary: %+v", result)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "original" {
		t.Fatalf("check activated a write: %q", data)
	}
}

func FuzzCompilerSource(f *testing.F) {
	for _, seed := range []string{
		`return Math.add(1, 2)`,
		`flow F(x: Int) -> Int! { return Math.div(1, x) }; return F(0)`,
		`return Match.new(1, 1: "a", _: "b")`,
		`flow F(x: Map<String, List<Int!>>) -> Int { return 1 }`,
		`flow F(x: Record{flow: Int}) -> Int { return x.flow }; return F(Record.new(flow: 1))`,
		`flow F(x: Int) -> Int { return F(x) }`,
		`return "\uD800"`,
		"\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 64*1024 {
			t.Skip("fuzz input exceeds test bound")
		}
		src, err := source.New("fuzz.lo", text)
		if err != nil {
			t.Fatal(err)
		}
		graph, d := compiler.Check(src)
		if (graph == nil) == (d == nil) {
			t.Fatalf("compiler must return a graph or a diagnostic, got %v, %v", graph, d)
		}
	})
}

func FuzzInspectionSource(f *testing.F) {
	for _, seed := range []string{
		`return 1`,
		`flow F(x: String) -> File.Append { return File.Append.new(File.Path.new(x), UTF8.Bytes.new("data")) }; e = F("output"); commit e`,
		`flow H(e: Error) -> Int { return 0 }; flow F(p: File.Path) -> Int onError H { bytes = File.read(p); return 1 }; return F(File.Path.new("input"))`,
		`return Match.new(1, 1: "a", _: "b")`,
		`flow F(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; return List.unfold(0, 3, F)`,
		"\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 64*1024 {
			t.Skip("fuzz input exceeds test bound")
		}
		src, err := source.New("fuzz.lo", text)
		if err != nil {
			t.Fatal(err)
		}
		report, d := compiler.Inspect(src)
		if (report == nil) == (d == nil) {
			t.Fatalf("inspection must return a report or a diagnostic, got %v, %v", report, d)
		}
		if report != nil {
			if _, err := report.MarshalJSON(); err != nil {
				t.Fatalf("report serialization: %v", err)
			}
		}
	})
}
