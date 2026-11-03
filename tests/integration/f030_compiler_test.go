package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestF030CompilerGenericContracts(t *testing.T) {
	for _, test := range []struct{ name, source, output string }{
		{"element", `flow Main()->Record{x:Int}!{return List.at(List.new(Record.new(x:1)),0)};return Main()`, "Record{x: Int}!"},
		{"empty-slice", `flow Main()->List<Int>!{return List.slice(List.new(),0,0)};return Main()`, "List<Int>!"},
		{"empty-at-slice", `flow Main()->Int!{return List.at(List.slice(List.new(),0,0),0)};return Main()`, "Int!"},
		{"empty-at-named-slice", `flow Main()->Int!{return List.at(List.slice(start:0,end:0,values:List.new()),0)};return Main()`, "Int!"},
		{"empty-named-at-named-slice", `flow Main()->Int!{return List.at(index:0,values:List.slice(end:0,values:List.new(),start:0))};return Main()`, "Int!"},
		{"empty-at-piped-slice", `flow Main()->Int!{return List.at(List.new() |> List.slice(0,0),0)};return Main()`, "Int!"},
		{"empty-at-piped-named-slice", `flow Main()->Int!{return List.at(List.new() |> List.slice(end:0,start:0),0)};return Main()`, "Int!"},
		{"empty-piped-at-slice", `flow Main()->Int!{return List.new() |> List.slice(0,0) |> List.at(0)};return Main()`, "Int!"},
		{"empty-slice-named-at", `flow Main()->List<Int>!{return List.slice(List.at(index:0,values:List.new(List.new())),0,0)};return Main()`, "List<Int>!"},
		{"empty-slice-piped-at", `flow Main()->List<Int>!{return List.slice(List.new(List.new()) |> List.at(0),0,0)};return Main()`, "List<Int>!"},
		{"empty-slice-piped-new", `flow Main()->List<List<Int>>!{return List.slice(List.new() |> List.new(),0,0)};return Main()`, "List<List<Int>>!"},
		{"sort-context", `flow Compare(direction:Int,left:String,right:String)->Int{return Math.mul(direction,String.compare(left,right))};return List.sort(List.new("b","a"),-1,Compare)`, "List<String>"},
		{"empty-sort", `flow Compare(left:Int,right:Int)->Int{return Math.sub(left,right)};return List.sort(List.new(),Compare)`, "List<Int>"},
		{"fallible-element", `return List.at(List.new(Math.div(1,0)),0)`, "Int!"},
		{"sort-fallible-elements", `flow Compare(left:Int!,right:Int!)->Int{return 0};return List.sort(List.new(Math.div(1,0),Math.div(2,1)),Compare)`, "List<Int!>"},
		{"scalar-at", `return List.at("a界",1)`, "Scalar!"},
		{"scalar-slice", `return List.slice("a界",0,1)`, "List<Scalar>!"},
		{"scalar-sort", `flow Compare(left:Scalar,right:Scalar)->Int{return String.compare(String.FromScalars.new(List.new(left)),String.FromScalars.new(List.new(right)))};return List.sort("b界a",Compare)`, "List<Scalar>"},
		{"nominal-glob", `flow Main()->Bool!{mask=Glob.new("*.lo");return Glob.matches(mask,"test.lo")};return Main()`, "Bool!"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.source), "--format", "json")
			var checked struct {
				ResultType string `json:"result_type"`
			}
			if got.status != 0 || got.stderr != "" || json.Unmarshal([]byte(got.stdout), &checked) != nil || checked.ResultType != test.output {
				t.Fatalf("expected %s: %+v", test.output, got)
			}
		})
	}
}

func TestF030CompilerRejections(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"index-type", `return List.at(List.new(1),true)`, "check.type-mismatch"},
		{"slice-bound-type", `return String.slice("abc",0,true)`, "check.type-mismatch"},
		{"byte-separator-type", `return Bytes.split(Bytes.new("a"),"a")`, "check.type-mismatch"},
		{"glob-nominal", `return Glob.matches("*.lo","test.lo")`, "check.type-mismatch"},
		{"glob-opaque", `flow Field(mask:Glob)->String{return mask.pattern};return 0`, "check.unknown-field"},
		{"sort-nonlist", `flow Compare(left:Int,right:Int)->Int{return Math.sub(left,right)};return List.sort(1,Compare)`, "check.type-mismatch"},
		{"sort-context-type", `flow Compare(direction:Int,left:Int,right:Int)->Int{return Math.mul(direction,Math.sub(left,right))};return List.sort(List.new(1),true,Compare)`, "check.type-mismatch"},
		{"sort-item-type", `flow Compare(left:String,right:String)->Int{return String.compare(left,right)};return List.sort(List.new(1),Compare)`, "check.type-mismatch"},
		{"sort-operand-types", `flow Compare(left:Int,right:String)->Int{return 0};return List.sort(List.new(1),Compare)`, "check.type-mismatch"},
		{"sort-arity", `flow Compare(left:Int)->Int{return left};return List.sort(List.new(1),Compare)`, "check.argument-count"},
		{"sort-output", `flow Compare(left:Int,right:Int)->Bool{return Math.lt(left,right)};return List.sort(List.new(1),Compare)`, "check.type-mismatch"},
		{"sort-fallible", `flow Compare(left:Int,right:Int)->Int!{return Math.div(left,right)};return List.sort(List.new(1),Compare)`, "check.type-mismatch"},
		{"sort-transitive-read", `flow Read(left:Int)->Int!{invocation=Host.Invocation.current();return invocation.real_uid};flow Compare(left:Int,right:Int)->Int!{return Read(left)};return List.sort(List.new(1),Compare)`, "check.effectful-callback"},
		{"sort-commit", `flow Compare(left:Int,right:Int)->Int!{target=Console.Line.new("stdout","side effect",false);receipt=commit target;return 0};return List.sort(List.new(1),Compare)`, "check.effectful-callback"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.source), "--format", "json")
			out := diagnostics(t, got.stderr)
			if got.status != 1 || got.stdout != "" || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != test.code {
				t.Fatalf("expected %s: %+v", test.code, got)
			}
		})
	}
}

func TestF030CompilerInspectionErrors(t *testing.T) {
	source := `flow Main()->Bool!{pieces=String.split_first("left:right",":");pattern=Glob.new(pieces.before);path=File.Path.join(File.Path.new("root"),pieces.after);return Glob.matches(pattern,File.Path.name(path))};return Main()`
	report, _, _ := inspectSource(t, source)
	if len(report.Capabilities) != 0 || len(report.Reads) != 0 || len(report.Activations) != 0 {
		t.Fatalf("pure processors require host authority: %+v", report)
	}
	wanted := map[string]string{"String.split_first": "String.SeparatorAbsent", "Glob.new": "Glob.InvalidPattern", "File.Path.join": "File.InvalidPath"}
	for operation, kind := range wanted {
		found := false
		for _, failure := range report.ErrorSources {
			if failure.Operation != operation {
				continue
			}
			for _, actual := range failure.Kinds {
				if actual == kind {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("missing %s error %s", operation, kind)
		}
	}
	for _, node := range report.Nodes {
		if _, selected := wanted[node.Operation]; !selected {
			continue
		}
		var span struct{ Start, End struct{ Offset int } }
		if json.Unmarshal(node.Span, &span) != nil || span.Start.Offset < 0 || span.End.Offset > len(source) || span.End.Offset <= span.Start.Offset || !strings.HasPrefix(source[span.Start.Offset:span.End.Offset], node.Operation+"(") {
			t.Fatalf("incorrect operation source span: %+v", node)
		}
	}
}

func TestF030CompilerPreservesFallibleElements(t *testing.T) {
	for _, source := range []string{
		`flow Main()->Int!{return List.at(List.new(Math.div(1,0),Math.div(2,1)),1)};return Main()`,
		`flow Main()->List<Int!>!{return List.slice(List.new(Math.div(1,0),Math.div(2,1)),0,2)};return Main()`,
		`flow Main()->Int!{return List.at(List.slice(start:0,end:2,values:List.new(Math.div(1,0),Math.div(2,1))),1)};return Main()`,
		`flow Main()->Int!{return List.new(Math.div(1,0),Math.div(2,1)) |> List.slice(end:2,start:0) |> List.at(1)};return Main()`,
	} {
		report, _, _ := inspectSource(t, source)
		found := false
		for _, node := range report.Nodes {
			if node.Operation == "List.new" {
				found = true
				if node.Type != "List<Int!>" {
					t.Fatalf("collection context changed element failures: %+v", node)
				}
			}
		}
		if !found {
			t.Fatal("missing collection construction")
		}
	}
}

func TestF030CompilerInspectionProvenance(t *testing.T) {
	source := `flow Compare(direction:Int,left:File.Entry,right:File.Entry)->Int{return Math.mul(direction,String.compare(left.name,right.name))};flow Main()->Int!{entries=File.list(File.Path.new("/fixture/f030"));ordered=List.sort(List.slice(entries,0,List.length(entries)),1,Compare);pending=File.Remove.new(List.new(List.at(ordered,0)));receipt=commit pending;return receipt.completed_count};return Main()`
	report, _, serialized := inspectSource(t, source)
	var found bool
	for _, capability := range report.Capabilities {
		if capability.Operation == "File.remove" {
			found = true
			if capability.Target.Value != "/fixture/f030" {
				t.Fatalf("lost list provenance: %+v", capability)
			}
		}
	}
	if !found || !strings.Contains(serialized, `"list-callback"`) || !strings.Contains(serialized, "String.compare") {
		t.Fatalf("incomplete processor inspection: %s", serialized)
	}
}
