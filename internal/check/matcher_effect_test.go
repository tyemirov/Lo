package check_test

import (
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

const matcherReadFlows = `
flow WantsRead(request: Bool) -> Bool { return request }
flow ReadFile(request: Bool) -> Bytes! { return File.read(File.Path.new("input")) }
flow Empty(request: Bool) -> Bytes! { return Bytes.new("") }
flow ReadIndirect(request: Bool) -> Bytes! { return ReadFile(request) }
`

func TestMatchApplicationTracksHostReads(t *testing.T) {
	file, err := source.New("matcher-effects.lo", `
flow WantsRead(request: Bool) -> Bool { return request }
flow ReadFile(request: Bool) -> Bytes! { return File.read(File.Path.new("input")) }
flow Default(request: Bool) -> Bytes! { return Bytes.new("") }
flow Construct() -> Int {
    matcher = Match.new(List.new(Match.Case.new(WantsRead, ReadFile)), Default)
    return 1
}
flow Apply(request: Bool) -> Bytes! {
    matcher = Match.new(List.new(Match.Case.new(WantsRead, ReadFile)), Default)
    return request |> Match.apply(matcher)
}
return Apply(true)
`)
	if err != nil {
		t.Fatal(err)
	}
	program, failure := compiler.Check(file)
	if failure != nil {
		t.Fatal(failure)
	}
	effects := map[string]bool{}
	for _, flow := range program.Flows() {
		effects[flow.Name()] = flow.Effects().Reads
	}
	if effects["Construct"] || !effects["Apply"] {
		t.Fatalf("expected a host-read effect only at application, got %v", effects)
	}
}

func TestMatcherReadQualifierInference(t *testing.T) {
	for _, test := range []struct {
		name, body, output string
	}{
		{"pure first", "return List.new(Match.Case.new(WantsRead, Empty), Match.Case.new(WantsRead, ReadFile))", "List<Match.Case<Bool, Bytes!, Read>>"},
		{"read first", "return List.new(Match.Case.new(WantsRead, ReadFile), Match.Case.new(WantsRead, Empty))", "List<Match.Case<Bool, Bytes!, Read>>"},
		{"reading cases", "return Match.new(List.new(Match.Case.new(WantsRead, ReadFile)), Empty)", "Match<Bool, Bytes!, Read>"},
		{"reading default", "cases = List.new(Match.Case.new(WantsRead, Empty)); return Match.new(cases, ReadFile)", "Match<Bool, Bytes!, Read>"},
		{"empty reading default", "return Match.new(List.new(), ReadFile)", "Match<Bool, Bytes!, Read>"},
		{"pure matcher", "return Match.new(List.new(Match.Case.new(WantsRead, Empty)), Empty)", "Match<Bool, Bytes!>"},
		{"transitive reading handler", "return Match.new(List.new(), ReadIndirect)", "Match<Bool, Bytes!, Read>"},
		{"widened matcher", "flow Build() -> Match<Bool, Bytes!, Read> { return Match.new(List.new(), Empty) }; return Build()", "Match<Bool, Bytes!, Read>"},
		{"widened case list", "flow Cases() -> List<Match.Case<Bool, Bytes!, Read>> { return List.new(Match.Case.new(WantsRead, Empty)) }; return Cases()", "List<Match.Case<Bool, Bytes!, Read>>"},
		{"fallible cases", "flow Build() -> Match<Bool, Bytes!, Read>! { cases = List.new(Match.Case.new(WantsRead, ReadFile)); return Match.new(List.slice(cases, 0, 1), Empty) }; return Build()", "Match<Bool, Bytes!, Read>!"},
		{"fallible inferred read matcher", "flow Build() -> List<Match<Bool, Bytes!, Read>>! { pure = Match.new(List.new(), Empty); reading = Match.new(List.new(), ReadFile); items = List.new(pure, List.at(List.new(reading), 0)); return items }; return Build()", "List<Match<Bool, Bytes!, Read>>!"},
		{"fallible inferred pure matcher", "flow Build() -> List<Match<Bool, Bytes!, Read>>! { pure = Match.new(List.new(), Empty); reading = Match.new(List.new(), ReadFile); items = List.new(reading, List.at(List.new(pure), 0)); return items }; return Build()", "List<Match<Bool, Bytes!, Read>>!"},
		{"construction in pure callback", "flow Build(request: Bool) -> Match<Bool, Bytes!, Read> { return Match.new(List.new(), ReadFile) }; return List.map(List.new(true), Build)", "List<Match<Bool, Bytes!, Read>>"},
		{"pure application in pure callback", "flow Apply(request: Bool) -> Bytes! { return request |> Match.apply(Match.new(List.new(), Empty)) }; return List.map(List.new(true), Apply)", "List<Bytes>!"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := source.New("matcher-inference.lo", matcherReadFlows+test.body)
			if err != nil {
				t.Fatal(err)
			}
			program, failure := compiler.Check(file)
			if failure != nil {
				t.Fatal(failure)
			}
			if got := program.ResultType().String(); got != test.output {
				t.Fatalf("result type %s, want %s", got, test.output)
			}
		})
	}
}

func TestMatcherReadsSurviveValueBoundaries(t *testing.T) {
	file, err := source.New("matcher-boundaries.lo", matcherReadFlows+`
flow Build() -> Match<Bool, Bytes!, Read> {
    return Match.new(List.new(Match.Case.new(WantsRead, ReadFile)), Empty)
}
flow Apply(request: Bool, matcher: Match<Bool, Bytes!, Read>) -> Bytes! {
    return request |> Match.apply(matcher)
}
flow Stored(request: Bool) -> Bytes! {
    rules = Record.new(items: List.new(Build()))
    matcher = List.at(rules.items, 0)
    return Apply(request, matcher)
}
return Stored(true)
`)
	if err != nil {
		t.Fatal(err)
	}
	program, failure := compiler.Check(file)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, flow := range program.Flows() {
		want := flow.Name() == "ReadFile" || flow.Name() == "ReadIndirect" || flow.Name() == "Apply" || flow.Name() == "Stored"
		if flow.Effects().Reads != want {
			t.Fatalf("flow %s read effect %t, want %t", flow.Name(), flow.Effects().Reads, want)
		}
	}
}

func TestMatcherReadQualifierRejections(t *testing.T) {
	for _, test := range []struct{ name, body, code string }{
		{"narrow matcher", "flow Build() -> Match<Bool, Bytes!> { return Match.new(List.new(), ReadFile) }; return Build()", "check.type-mismatch"},
		{"pure expected list pure first", "flow Cases() -> List<Match.Case<Bool, Bytes!>> { return List.new(Match.Case.new(WantsRead, Empty), Match.Case.new(WantsRead, ReadFile)) }; return Cases()", "check.type-mismatch"},
		{"pure expected list read first", "flow Cases() -> List<Match.Case<Bool, Bytes!>> { return List.new(Match.Case.new(WantsRead, ReadFile), Match.Case.new(WantsRead, Empty)) }; return Cases()", "check.type-mismatch"},
		{"bound list stays exact", "flow Cases() -> List<Match.Case<Bool, Bytes!, Read>> { items = List.new(Match.Case.new(WantsRead, Empty)); return items }; return Cases()", "check.type-mismatch"},
		{"application in pure callback", "flow Apply(request: Bool) -> Bytes! { return request |> Match.apply(Match.new(List.new(), ReadFile)) }; return List.map(List.new(true), Apply)", "check.effectful-callback"},
		{"parameter application in pure callback", "flow Apply(request: Bool, matcher: Match<Bool, Bytes!, Read>) -> Bytes! { return request |> Match.apply(matcher) }; flow Use(request: Bool) -> Bytes! { return Apply(request, Match.new(List.new(), ReadFile)) }; return List.map(List.new(true), Use)", "check.effectful-callback"},
		{"reading predicate", "flow Predicate(request: Bool) -> Bool { unused = Host.Invocation.current(); return true }; return Match.Case.new(Predicate, Empty)", "check.unhandled-failure"},
		{"self handled reading predicate", "flow Recover(error: Error) -> Bool { return false }; flow Predicate(request: Bool) -> Bool onError Recover { unused = Host.Invocation.current(); return true }; return Match.Case.new(Predicate, Empty)", "check.effectful-callback"},
		{"committing handler", "flow Commit(request: Bool) -> Bytes! { target = File.Write.new(File.Path.new(\"out\"), Bytes.new(\"\")); commit target; return Bytes.new(\"\") }; return Match.new(List.new(), Commit)", "check.effectful-callback"},
		{"indirect committing handler", "flow Commit(request: Bool) -> Bytes! { target = File.Write.new(File.Path.new(\"out\"), Bytes.new(\"\")); commit target; return Bytes.new(\"\") }; flow Indirect(request: Bool) -> Bytes! { return Commit(request) }; return Match.new(List.new(), Indirect)", "check.effectful-callback"},
		{"invalid qualifier", "flow Build() -> Match<Bool, Bytes!, Bool> { return Match.new(List.new(), Empty) }; return Build()", "check.type-constraint"},
		{"input covariance", "flow Handler(request: Match<Bool, Bytes!>) -> Bytes! { return true |> Match.apply(request) }; flow Build() -> Match<Match<Bool, Bytes!, Read>, Bytes!, Read> { return Match.new(List.new(), Handler) }; return Build()", "check.type-mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := source.New("matcher-rejection.lo", matcherReadFlows+test.body)
			if err != nil {
				t.Fatal(err)
			}
			_, failure := compiler.Check(file)
			if failure == nil || string(failure.Data().Code) != test.code {
				t.Fatalf("diagnostic %v, want %s", failure, test.code)
			}
		})
	}
}

func TestInlineMatchIsRejected(t *testing.T) {
	file, err := source.New("inline-match.lo", "return Match.select(true, true: 1, _: 0)")
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := compiler.Check(file); failure == nil {
		t.Fatal("expected inline selection to be rejected")
	}
}
