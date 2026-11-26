package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

const matcherInspectionSource = `
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
flow First(value: Int) -> File.Append { return File.Append.new(File.Path.new("first"), Bytes.new("x")) }
flow Other(value: Int) -> File.Append { return File.Append.new(File.Path.new("other"), Bytes.new("y")) }
flow Build() -> Match<Int, File.Append> {
    return Match.new(List.new(Match.Case.new(Positive, First)), Other)
}
`

func TestMatcherInspectionDefersCallbacks(t *testing.T) {
	report, _, _ := inspectSource(t, matcherInspectionSource+`matcher = Build(); return 1`)
	for _, target := range report.Targets {
		if target.Entry == "program" {
			t.Fatalf("matcher construction analyzed a handler as executed: %+v", target)
		}
	}
	if len(report.Capabilities) != 0 || len(report.Activations) != 0 {
		t.Fatalf("matcher construction activated behavior: %+v %+v", report.Capabilities, report.Activations)
	}
}

func TestMatcherInspectionCarriesRulesThroughValues(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"record and list", `rules = Record.new(value: List.new(Build())); matcher = List.at(rules.value, 0)`},
		{"map", `rules = Map.new(Record.new(key: "rules", value: Build())); matcher = Map.get(rules, "rules")`},
		{"collection join", `flow Rules(value: Bool) -> List<Match<Int, File.Append>> { return List.new(Build()) }; rules = true |> Match.apply(Match.new(List.new(), Rules)); matcher = List.at(rules, 0)`},
		{"fold", `flow Keep(state: Match<Int, File.Append>, item: Match<Int, File.Append>) -> Match<Int, File.Append> { return item }; matcher = List.fold(List.new(Build()), Build(), Keep)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			program := matcherInspectionSource + fmt.Sprintf("flow Main() -> Int! { %s; action = 1 |> Match.apply(matcher); commit action; return 1 }; return Main()", test.source)
			report, _, _ := inspectSource(t, program)
			paths := map[string]bool{}
			for _, capability := range report.Capabilities {
				if capability.Operation != "File.append" {
					continue
				}
				paths[capability.Target.Value] = true
				conditional := false
				for _, condition := range capability.Conditions {
					conditional = conditional || condition.Kind == "match-case"
				}
				if !conditional {
					t.Fatalf("matcher handler lost its branch condition: %+v", capability)
				}
			}
			if !paths["first"] || !paths["other"] {
				t.Fatalf("matcher handler targets were omitted: %+v", report.Capabilities)
			}
		})
	}
}

func TestMatcherInspectionUnknownParameter(t *testing.T) {
	source := matcherInspectionSource + `
flow Apply(value: Int, matcher: Match<Int, File.Append>) -> File.Append {
    return value |> Match.apply(matcher)
}
return Build()
`
	report, _, _ := inspectSource(t, source)
	paths := map[string]bool{}
	handlers := map[string]bool{}
	for _, invocation := range report.Contexts {
		if invocation.Kind == "matcher-handler" {
			handlers[invocation.ID] = true
		}
	}
	for _, target := range report.Targets {
		if handlers[target.Context] {
			paths[target.Target.Value] = true
		}
	}
	if !paths["first"] || !paths["other"] {
		t.Fatalf("unknown matcher omitted admitted handler targets: %+v", report.Targets)
	}
	if len(report.Capabilities) != 0 {
		t.Fatalf("unused matcher helper requested host grants: %+v", report.Capabilities)
	}
}

func TestMatcherInspectionApplicationEffects(t *testing.T) {
	const source = `
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
flow Move(value: Int) -> Tensor<FP32>! {
    unused = Math.div(value, 0)
    return Tensor.to(Tensor.new(List.new(1.0), List.new(1)), Device.new("metal:0"))
}

flow Keep(value: Int) -> Tensor<FP32>! { return Tensor.new(List.new(1.0), List.new(1)) }
flow Build() -> Match<Int, Tensor<FP32>!> {
    return Match.new(List.new(Match.Case.new(Positive, Move)), Keep)
}
flow Apply(value: Int, matcher: Match<Int, Tensor<FP32>!>) -> Tensor<FP32>! {
    return value |> Match.apply(matcher)
}
return Apply(1, Build())
`
	report, _, raw := inspectSource(t, source)
	conditionalError := false
	for _, source := range report.ErrorSources {
		if source.Entry != "program" || source.Operation != "Math.div" {
			continue
		}
		for _, condition := range source.Conditions {
			conditionalError = conditionalError || condition.Kind == "match-case"
		}
	}
	if !conditionalError {
		t.Fatalf("matcher application omitted the handler error: %+v", report.ErrorSources)
	}
	var envelope struct {
		Report struct {
			Flows []struct {
				Name    string
				Effects struct{ Devices bool }
			}
			DeviceResources []inspectionResource `json:"device_resources"`
		}
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, flow := range envelope.Report.Flows {
		if flow.Name == "Build" && flow.Effects.Devices || flow.Name == "Apply" && !flow.Effects.Devices {
			t.Fatalf("matcher callback device phase differs: %+v", flow)
		}
	}
	conditionalDevice := false
	for _, resource := range envelope.Report.DeviceResources {
		if resource.Entry != "program" || resource.Operation != "Tensor.to" {
			continue
		}
		for _, condition := range resource.Conditions {
			conditionalDevice = conditionalDevice || condition.Kind == "match-case"
		}
	}
	if !conditionalDevice {
		t.Fatalf("matcher application omitted the device requirement: %+v", envelope.Report.DeviceResources)
	}
}

func TestMatcherInspectionNestedProcessorState(t *testing.T) {
	const source = `
flow Terminal(value: Int) -> Int { return value }
flow Later(value: Int) -> Int { return Math.add(value, 1) }
flow Keep(state: Match<Int, Int>, item: Int) -> Match<Int, Int> { return state }
flow Emit(state: Match<Int, Int>) -> Record{value: Int, next_state: Match<Int, Int>} {
    return Record.new(value: 1 |> Match.apply(state), next_state: Match.new(List.new(), Later))
}
flow Visit(count: Int, matcher: Match<Int, Int>) -> Record{state: Int, items: List<Match<Int, Int>>, stop: Bool} {
    next = 1 |> Match.apply(matcher)
    return Record.new(state: next, items: List.new(Match.new(List.new(), Later)), stop: Math.eq(next, 2))
}
`
	for _, test := range []struct {
		name, body   string
		invokesLater bool
	}{
		{"fold", "matcher = List.fold(List.new(1), initial, Keep); return value |> Match.apply(matcher)", false},
		{"unfold", "return List.length(List.unfold(initial, 2, Emit))", true},
		{"worklist items", "return Flow.worklist(List.new(initial), 0, Visit)", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			program := source + fmt.Sprintf(`flow Outer(value: Int) -> Int {
    initial = Match.new(List.new(), Terminal)
    %s
}
return 1 |> Match.apply(Match.new(List.new(), Outer))
`, test.body)
			report, _, _ := inspectSource(t, program)
			flows := map[string]string{}
			for _, flow := range report.Flows {
				flows[flow.ID] = flow.Name
			}
			invoked := map[string]bool{}
			for _, invocation := range report.Contexts {
				if invocation.Entry == "program" && invocation.Kind == "matcher-handler" {
					invoked[flows[invocation.Flow]] = true
				}
			}
			if !invoked["Outer"] || !invoked["Terminal"] || invoked["Later"] != test.invokesLater || len(report.Capabilities) != 0 {
				t.Fatalf("nested matcher inspection: contexts=%+v capabilities=%+v", report.Contexts, report.Capabilities)
			}
		})
	}
}

func TestMatcherInspectionChangingFoldState(t *testing.T) {
	const source = `
flow First(value: Int) -> File.Append! {
    return File.Append.new(File.Path.new("first"), Bytes.new("x"))
}
flow Later(value: Int) -> File.Append! {
    unused = Math.div(1, value)
    return File.Append.new(File.Path.new("later"), Bytes.new("y"))
}
flow Step(state: Match<Int, File.Append!>, item: Int) -> Match<Int, File.Append!>! {
    observed = item |> Match.apply(state)
    return Match.new(List.new(), Later)
}
flow Outer(value: Int) -> File.Append! {
    initial = Match.new(List.new(), First)
    matcher = List.fold(List.new(1, 2), initial, Step)
    return value |> Match.apply(matcher)
}
flow Main() -> Int! {
    target = 1 |> Match.apply(Match.new(List.new(), Outer))
    commit target
    return 1
}
return Main()
`
	report, _, _ := inspectSource(t, source)
	paths := map[string]bool{}
	for _, capability := range report.Capabilities {
		if capability.Operation == "File.append" {
			paths[capability.Target.Value] = true
		}
	}
	if !paths["first"] || !paths["later"] {
		t.Fatalf("fold omitted possible matcher targets: %+v", report.Capabilities)
	}
	laterError := false
	for _, source := range report.ErrorSources {
		if source.Entry != "program" || source.Operation != "Math.div" {
			continue
		}
		for _, condition := range source.Conditions {
			laterError = laterError || condition.Kind == "list-iteration"
		}
	}
	if !laterError {
		t.Fatalf("fold omitted a later matcher handler error: %+v", report.ErrorSources)
	}
}
