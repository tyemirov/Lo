package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestF031JSONRemovalSnapshots(t *testing.T) {
	for _, tree := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("tree-%t-error-%t", tree, failed), func(t *testing.T) {
				parent := fileFixtureRoot(t)
				root := f033Directory(t, parent, "tree")
				preserved := fileFixture(t, root, "leaf", "preserved")
				declaration := `flow Eligible(context:JSON.Value,original:File.Entry,current:File.Entry)->Bool{return String.eq(JSON.kind(context),"object")}`
				setup := `target=File.Remove.checked(List.new(),document,Eligible)`
				typ := "File.Remove"
				if tree {
					declaration = `flow Descend(entry:File.Entry)->Bool{return true};flow Eligible(context:JSON.Value,row:Record{relative:String,entry:File.Entry})->Bool{return String.eq(JSON.kind(context),"object")}`
					setup = fmt.Sprintf(`parent=File.directory(File.Path.new(%q));root=File.Directory.entry(parent,"tree");tree=File.Tree.snapshot(parent,root,Descend);target=File.Tree.Remove.checked(tree,document,Eligible)`, parent)
					typ = "File.Tree.Remove"
				}
				resultType, returned := typ, "target"
				if failed {
					resultType = "Int"
					returned = `Error.new("Fixture.Outer","outer",Record.new(target:target,cause:Error.new("Fixture.Inner","inner",Record.new(document:document))))`
				}
				source := declaration + fmt.Sprintf(`;flow Main()->%s!{document=%s;%s;return %s};return Main()`, resultType, f031Decode(`{"token":"retained"}`), setup, returned)
				actual := runProgram(t, sourceFile(t, source), "run", "--observe-tree-root", parent, "--format", "json")
				text := actual.stdout
				if failed {
					text = actual.stderr
					if actual.status != 1 || actual.stdout != "" || !strings.Contains(text, "Fixture.Outer") || !strings.Contains(text, "Fixture.Inner") {
						t.Fatalf("nested Error lost captured JSON: %+v", actual)
					}
				} else if actual.status != 0 || actual.stderr != "" {
					t.Fatalf("returned target lost captured JSON: %+v", actual)
				}
				if !strings.Contains(text, `"type":"`+typ+`"`) || !strings.Contains(text, `"context":[{"type":"JSON.Value","value":{"kind":"object","value":{"token":{"kind":"string","value":"retained"}}}}]`) {
					t.Fatalf("guard snapshot lost canonical JSON context: %+v", actual)
				}
				macCleanupPresent(t, preserved, true)
			})
		}
	}
}

func TestF031DeepJSONFlowProgress(t *testing.T) {
	const depth = 16384
	document := strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
	source := `flow Step(previous:JSON.Value,item:Int)->JSON.Value!{return Error.new("Fixture.Progress","stop",Record.new())};flow Main()->JSON.Value!{initial=` + f031Decode(document) + `;return Flow.fold(List.new(1),initial,Step)};return Main()`
	actual := runProgram(t, sourceFile(t, source), "run", "--max-json-depth", "0", "--max-steps", "10000000", "--format", "json")
	want := strings.Repeat(`{"kind":"array","value":[`, depth) + `{"kind":"number","value":"0"}` + strings.Repeat(`]}`, depth)
	progress := strings.Index(actual.stderr, `"flow_progress":[`)
	if actual.status != 1 || actual.stdout != "" || !strings.Contains(actual.stderr, "Fixture.Progress") || progress < 0 || !strings.Contains(actual.stderr[progress:], `"state":{"type":"JSON.Value","value":`+want) {
		t.Fatalf("deep flow progress lost completed JSON state: status=%d stderr size=%d", actual.status, len(actual.stderr))
	}
}
