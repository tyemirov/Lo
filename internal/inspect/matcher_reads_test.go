package inspect_test

import (
	"encoding/json"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestUnknownMatcherReadEffect(t *testing.T) {
	file, err := source.New("matcher-inspection.lo", `
flow WantsFile(request: Bool) -> Bool { return request }
flow ReadFile(request: Bool) -> Bytes! { return File.read(File.Path.new("input")) }
flow Empty(request: Bool) -> Bytes! { return Bytes.new("") }
flow Build() -> Match<Bool, Bytes!, Read> {
    return Match.new(List.new(Match.Case.new(WantsFile, ReadFile)), Empty)
}
flow ApplyPure(request: Bool, matcher: Match<Bool, Bytes!>) -> Bytes! {
    return request |> Match.apply(matcher)
}
flow ApplyReading(request: Bool, matcher: Match<Bool, Bytes!, Read>) -> Bytes! {
    return request |> Match.apply(matcher)
}
return Build()
`)
	if err != nil {
		t.Fatal(err)
	}
	report, failure := compiler.Inspect(file)
	if failure != nil {
		t.Fatal(failure)
	}
	data, err := report.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	type invocation struct{ ID, Parent, Flow string }
	var result struct {
		Flows    []struct{ ID, Name string }
		Contexts []invocation
		Reads    []struct {
			Context, Entry, Operation string
			Conditions                []struct{ Kind string }
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	names := make(map[string]string)
	for _, flow := range result.Flows {
		names[flow.ID] = flow.Name
	}
	contexts := make(map[string]invocation)
	for _, context := range result.Contexts {
		contexts[context.ID] = context
	}
	conditionalRead := false
	for _, read := range result.Reads {
		if read.Operation != "File.read" {
			continue
		}
		if read.Entry == "program" {
			t.Fatal("constructing the matcher appeared as a program read")
		}
		for context := contexts[read.Context]; context.ID != ""; context = contexts[context.Parent] {
			switch names[context.Flow] {
			case "ApplyPure":
				t.Fatal("a pure matcher acquired a reading handler during inspection")
			case "ApplyReading":
				for _, condition := range read.Conditions {
					conditionalRead = conditionalRead || condition.Kind == "match-case"
				}
			}
		}
	}
	if !conditionalRead {
		t.Fatal("read-qualified matcher lost its conditional read")
	}
}
