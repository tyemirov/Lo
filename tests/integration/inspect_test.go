package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inspectionDescription struct {
	Kind, Type, Node, Value, Expression string
	Dependencies                        []string
	Span                                json.RawMessage
}
type inspectionCondition struct {
	Kind, Node, Context string
	Case                int
}
type inspectionResource struct {
	ID, Operation, Context, Entry, Node string
	Target                              inspectionDescription
	Conditions                          []inspectionCondition
	Handler                             string
	OnError                             string `json:"on_error"`
}
type inspectionActivation struct {
	ID, Context, Entry, Node string
	Conditions               []inspectionCondition
	Targets                  []struct {
		ID         string
		Conditions []inspectionCondition
	}
}
type inspectionError struct {
	ID, Category, Operation, Context, Entry, Node, Handler string
	Kinds                                                  []string
	Dynamic                                                *inspectionDescription
	Conditions                                             []inspectionCondition
}
type inspectionReport struct {
	File       string
	ResultType string `json:"result_type"`
	Nodes      []struct {
		ID, Kind, Type, Operation, Flow, Binding string
		Inputs                                   []string
		Span                                     json.RawMessage
	}
	Bindings []struct{ ID, Name, Node string }
	Flows    []struct {
		ID, Name, Output, Handler string
		Parameters                []struct{ ID, Name, Type string }
		Uses                      []string
		Effects                   struct{ Reads, Commits bool }
		Span                      json.RawMessage
	}
	Contexts                     []struct{ ID, Entry, Parent, Flow, Node, Kind string }
	EntryPoints                  []struct{ ID, Kind, Flow, Activation string } `json:"entry_points"`
	Reads, Targets, Capabilities []inspectionResource
	Activations                  []inspectionActivation
	ErrorSources                 []inspectionError `json:"error_sources"`
	UnusedFlows                  []string          `json:"unused_flows"`
}

func inspectSource(t *testing.T, text string) (inspectionReport, string, string) {
	t.Helper()
	path := sourceFile(t, text)
	got := run(t, "inspect", path, "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("inspect: %+v", got)
	}
	var envelope struct {
		Command string
		Report  inspectionReport
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "inspect" || envelope.Report.File != path {
		t.Fatalf("wrong report envelope: %+v", envelope)
	}
	validateInspectionReferences(t, envelope.Report)
	return envelope.Report, path, got.stdout
}
func TestInspectionResources(t *testing.T) {
	existing := sourceFile(t, "untouched")
	absent := existing + ".missing"
	text := fmt.Sprintf(`flow Never() -> Int! { unused = File.read(File.Path.new("unused-resource")); return 1 }
 input = File.read(File.Path.new(%q))
 pending = File.Append.new(File.Path.new("uncommitted"), UTF8.Bytes.new("unused"))
 output = File.Write.new(File.Path.new(%q), Bytes.new("changed"))
 commit output
 commit output`, absent, existing)
	report, path, first := inspectSource(t, text)
	second := run(t, "inspect", path, "--format", "json")
	if second.status != 0 || second.stdout != first {
		t.Fatal("inspection is not deterministic")
	}
	if len(report.Activations) != 2 || report.Activations[0].ID == report.Activations[1].ID {
		t.Fatalf("commit sites: %+v", report.Activations)
	}
	if report.Activations[0].Targets[0].ID != report.Activations[1].Targets[0].ID {
		t.Fatal("shared target identity changed")
	}
	if len(report.Capabilities) != 3 {
		t.Fatalf("want one read and two writes, got %+v", report.Capabilities)
	}
	for _, cap := range report.Capabilities {
		if cap.Target.Kind != "known" || cap.Target.Value == "uncommitted" || cap.Target.Value == "unused-resource" {
			t.Fatalf("incorrect program capability: %+v", cap)
		}
	}
	if len(report.UnusedFlows) != 1 || len(report.Nodes) == 0 || len(report.Bindings) == 0 {
		t.Fatalf("incomplete report: %+v", report)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "untouched" {
		t.Fatal("inspect executed a write")
	}
	for _, kind := range []string{"File.ReadFailed", "File.WriteFailed"} {
		found := false
		for _, e := range report.ErrorSources {
			for _, k := range e.Kinds {
				if k == kind {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("missing error kind %s", kind)
		}
	}
	textResult := run(t, "inspect", path)
	if textResult.status != 0 || !strings.Contains(textResult.stdout, "capabilities:") || !strings.Contains(textResult.stdout, "File.write") {
		t.Fatalf("text report: %+v", textResult)
	}
}
func TestInspectionBranchesAndCallbacks(t *testing.T) {
	report, _, _ := inspectSource(t, `flow Build(path: String, flag: Bool) -> File.Append {
   return Match.new(flag, true: File.Append.new(File.Path.new(path), UTF8.Bytes.new("a")), _: File.Append.new(File.Path.new("other"), Bytes.new("b")))
 }
 flow Handle(request: HTTP.Request) -> HTTP.Response! {
   target = Build(request.path, String.eq(request.method, "POST"))
   commit target
   return HTTP.Response.Text.new(200, "ok")
 }
 flow Failed(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, error.kind) }
 server = HTTP.Server.new(8080, Handle, Failed)
 commit server`)
	ops := map[string]int{}
	dynamic := false
	conditional := 0
	for _, cap := range report.Capabilities {
		ops[cap.Operation]++
		if cap.Operation == "File.append" {
			dynamic = dynamic || cap.Target.Kind == "dynamic" && len(cap.Target.Dependencies) > 0 && cap.Target.Expression != ""
		}
		if cap.Operation == "File.append" || cap.Operation == "File.write" {
			for _, condition := range cap.Conditions {
				if condition.Kind == "match-case" {
					conditional++
				}
			}
		}
	}
	if ops["HTTP.listen"] != 1 || ops["File.append"] != 2 || !dynamic || conditional != 2 {
		t.Fatalf("capabilities: %+v", report.Capabilities)
	}
	if len(report.EntryPoints) != 3 || len(report.UnusedFlows) != 0 {
		t.Fatalf("entry points or unused flows: %+v, %+v", report.EntryPoints, report.UnusedFlows)
	}
	indirect := false
	for _, ctx := range report.Contexts {
		if ctx.Kind == "call" && ctx.Parent != "" {
			indirect = true
		}
	}
	if !indirect {
		t.Fatal("missing indirect invocation")
	}
}
func TestInspectionReturnedTargetsAndErrors(t *testing.T) {
	report, _, _ := inspectSource(t, `flow Make(path: String) -> File.Append { return File.Append.new(File.Path.new(path), UTF8.Bytes.new("x")) }
 flow H(e: Error) -> File.Append { return Make("handled") }
 flow Select(kind: String) -> File.Append onError H { n = Math.div(1, 0); detail = Error.new(kind, "custom", Record.new()); return Make("normal") }
 flow Dynamic(r: HTTP.Request) -> Error { return Error.new(r.path, "custom", Record.new()) }
 e = Select("My.Error")
 commit e`)
	paths := map[string]bool{}
	for _, cap := range report.Capabilities {
		if cap.Operation == "File.append" {
			paths[cap.Target.Value] = true
		}
	}
	if !paths["normal"] || !paths["handled"] {
		t.Fatalf("returned targets: %+v", report.Capabilities)
	}
	known, dynamic, handled := false, false, false
	for _, err := range report.ErrorSources {
		if err.Dynamic != nil && err.Dynamic.Kind == "dynamic" {
			dynamic = true
		}
		for _, k := range err.Kinds {
			if k == "My.Error" {
				known = true
			}
			if k == "Math.DivideByZero" && err.Handler != "" {
				handled = true
			}
		}
	}
	if !known || !dynamic || !handled {
		t.Fatalf("error sources: %+v", report.ErrorSources)
	}
}
func TestInspectionUncommittedServer(t *testing.T) {
	report, _, _ := inspectSource(t, `flow H(r: HTTP.Request) -> HTTP.Response! { data = File.read(File.Path.new("handler-read")); return HTTP.Response.Bytes.new(200, data) }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }; server = HTTP.Server.new(8080, H, E)`)
	if len(report.EntryPoints) != 1 || len(report.Capabilities) != 0 || len(report.UnusedFlows) != 2 {
		t.Fatalf("uncommitted server was treated as active: %+v", report)
	}
	if len(report.Targets) != 1 || report.Targets[0].Handler == "" || report.Targets[0].OnError == "" {
		t.Fatalf("missing configured handlers: %+v", report.Targets)
	}
}

func TestInspectionHTTPBoundaryErrors(t *testing.T) {
	const program = `flow Recovered(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(200, "recovered") }
flow H(r: HTTP.Request) -> HTTP.Response onError Recovered { n = Math.div(1, 0); return HTTP.Response.Text.new(200, Int.to_string(n)) }
flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }
server = HTTP.Server.new(0, H, E)
`
	for commits := 0; commits <= 2; commits++ {
		t.Run(fmt.Sprint(commits), func(t *testing.T) {
			report, _, _ := inspectSource(t, program+strings.Repeat("commit server\n", commits))
			var errorHandler string
			for _, flow := range report.Flows {
				if flow.Name == "E" {
					errorHandler = flow.ID
				}
			}
			entries := map[string]string{}
			for _, entry := range report.EntryPoints {
				if entry.Kind == "http-handler" {
					entries[entry.ID] = entry.Activation
				}
			}
			want := map[string]string{"HTTP.InvalidRequest": "HTTP.request", "HTTP.RequestTooLarge": "HTTP.request", "HTTP.RequestReadFailed": "HTTP.request", "HTTP.InvalidResponse": "HTTP.respond"}
			seen := map[string]int{}
			for _, source := range report.ErrorSources {
				if source.Category != "boundary" {
					continue
				}
				if source.Handler != errorHandler || entries[source.Entry] == "" {
					t.Fatalf("wrong adapter error boundary: %+v", source)
				}
				conditions := map[string]bool{}
				for _, c := range source.Conditions {
					conditions[c.Kind] = true
				}
				if !conditions["commit-success"] || !conditions["adapter-request"] {
					t.Fatalf("unconditional boundary error: %+v", source)
				}
				for _, kind := range source.Kinds {
					if want[kind] != source.Operation {
						t.Fatalf("wrong boundary operation: %+v", source)
					}
					key := entries[source.Entry] + ":" + kind
					seen[key]++
					if seen[key] != 1 {
						t.Fatalf("duplicate error kind for activation: %s", key)
					}
				}
			}
			if len(seen) != commits*len(want) {
				t.Fatalf("want %d boundary kinds, got %v", commits*len(want), seen)
			}
		})
	}
}
func TestInspectionListContext(t *testing.T) {
	report, _, _ := inspectSource(t, `flow Read(count: Int, path: String) -> Int! { bytes = File.read(File.Path.new(path)); return Math.add(count, 1) }; return Flow.fold(List.new("one", "two"), 0, Read)`)
	if len(report.Capabilities) != 1 || report.Capabilities[0].Operation != "File.read" {
		t.Fatalf("missing callback read: %+v", report.Capabilities)
	}
	found := false
	for _, condition := range report.Capabilities[0].Conditions {
		if condition.Kind == "flow-iteration" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing repeated invocation condition")
	}
}
func TestInspectionRejectsInvalidSource(t *testing.T) {
	got := run(t, "inspect", sourceFile(t, `return Math.add(1, "wrong")`), "--format", "json")
	if got.status != 1 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
	if d := diagnostics(t, got.stderr).Diagnostics[0]; d.Code != "check.type-mismatch" {
		t.Fatalf("%+v", d)
	}
}

func TestInspectionLoopCarriedTargets(t *testing.T) {
	report, _, _ := inspectSource(t, `flow Step(state: File.Write) -> Record{value: File.Write, next_state: File.Write} { return Record.new(value: state, next_state: File.Write.new(File.Path.new("next"), Bytes.new("x"))) }; flow Last(state: File.Write, value: File.Write) -> File.Write { return value }; first = File.Write.new(File.Path.new("first"), UTF8.Bytes.new("x")); effects = List.unfold(first, 2, Step); last = List.fold(effects, first, Last); commit last`)
	found := false
	for _, cap := range report.Capabilities {
		if cap.Operation == "File.write" && cap.Target.Value == "next" {
			found = true
		}
	}
	if !found {
		t.Fatalf("inspection omitted a target from later iterations: %+v", report.Capabilities)
	}
}

func TestInspectionFoldCarriedActivation(t *testing.T) {
	for _, shape := range []struct{ name, typ, seed, target, next string }{
		{"target", "File.Write", "first", "state", "next"},
		{"record", "Record{target: File.Write}", "Record.new(target: first)", "state.target", "Record.new(target: next)"},
		{"fallible-record", "Record{target: File.Write!}", "Record.new(target: Fallible(first))", "state.target", "Record.new(target: Fallible(next))"},
		{"map", "Map<String, File.Write>", `Map.new(Record.new(key: "target", value: first))`, `Map.get(state, "target")`, `Map.new(Record.new(key: "target", value: next))`},
	} {
		t.Run(shape.name, func(t *testing.T) {
			root := fileFixtureRoot(t)
			first, next := filepath.Join(root, "first"), filepath.Join(root, "next")
			source := fmt.Sprintf(`flow Fallible(target: File.Write) -> File.Write! { return target }; flow Step(state: %s, item: Int) -> %s! { target = %s; commit target; next = File.Write.new(File.Path.new(%q), Bytes.new("next")); return %s }; first = File.Write.new(File.Path.new(%q), Bytes.new("first")); return Flow.fold(List.new(1, 2), %s, Step)`, shape.typ, shape.typ, shape.target, next, shape.next, first, shape.seed)
			report, _, _ := inspectSource(t, source)
			got := run(t, "run", sourceFile(t, source), "--write-root", root, "--format", "json")
			if got.status != 0 {
				t.Fatalf("execute fold: %+v", got)
			}
			for _, path := range []string{first, next} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
				covered := false
				for _, capability := range report.Capabilities {
					if capability.Operation == "File.write" && (capability.Target.Kind == "dynamic" || capability.Target.Value == path) {
						covered = true
					}
				}
				if !covered {
					t.Fatalf("inspection omitted executed write %s: %+v", path, report.Capabilities)
				}
			}
		})
	}
}

func TestInspectionFoldCarriedResult(t *testing.T) {
	for _, operation := range []string{"Flow.fold", "List.fold"} {
		t.Run(operation, func(t *testing.T) {
			root := fileFixtureRoot(t)
			first, next := filepath.Join(root, "first"), filepath.Join(root, "next")
			source := fmt.Sprintf(`flow Step(state: Record{previous: File.Write, next: File.Write}, item: Int) -> Record{previous: File.Write, next: File.Write} { return Record.new(previous: state.next, next: File.Write.new(File.Path.new(%q), Bytes.new("next"))) }; first = File.Write.new(File.Path.new(%q), Bytes.new("first")); result = %s(List.new(1, 2), Record.new(previous: first, next: first), Step); target = result.previous; commit target`, next, first, operation)
			report, _, _ := inspectSource(t, source)
			got := run(t, "run", sourceFile(t, source), "--write-root", root, "--format", "json")
			if got.status != 0 {
				t.Fatalf("execute fold: %+v", got)
			}
			if _, err := os.Stat(next); err != nil {
				t.Fatal(err)
			}
			for _, capability := range report.Capabilities {
				if capability.Operation == "File.write" && (capability.Target.Kind == "dynamic" || capability.Target.Value == next) {
					return
				}
			}
			t.Fatalf("inspection omitted fold result write: %+v", report.Capabilities)
		})
	}
}
func TestInspectionNestedAdapters(t *testing.T) {
	report, _, _ := inspectSource(t, `flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "failed") }; flow Inner(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow Outer(r: HTTP.Request) -> HTTP.Response! { server = HTTP.Server.new(8081, Inner, E); commit server; return HTTP.Response.Text.new(200, "ok") }; server = HTTP.Server.new(8080, Outer, E); commit server`)
	seen := map[string]bool{}
	for _, entry := range report.EntryPoints {
		if seen[entry.ID] {
			t.Fatalf("duplicate entry identity %s", entry.ID)
		}
		seen[entry.ID] = true
	}
	if len(report.EntryPoints) != 5 {
		t.Fatalf("nested adapter entries: %+v", report.EntryPoints)
	}
}

func TestInspectionCaseHeadReads(t *testing.T) {
	source := `flow H(e: Error) -> Int { return 0 }; flow Head() -> Int onError H { data = File.read(File.Path.new("head-input")); return 1 }; return Match.new(0, Head(): "match", _: "default")`
	got := run(t, "inspect", sourceFile(t, source), "--format", "json")
	if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.effectful-match" {
		t.Fatalf("observing case head accepted: %+v", got)
	}
}

func validateInspectionReferences(t *testing.T, report inspectionReport) {
	t.Helper()
	nodes := map[string]bool{}
	flows := map[string]bool{}
	bindings := map[string]bool{}
	contexts := map[string]bool{}
	entries := map[string]bool{}
	targets := map[string]bool{}
	add := func(table map[string]bool, id string) {
		t.Helper()
		if id == "" || table[id] {
			t.Fatalf("invalid or duplicate identity %q", id)
		}
		table[id] = true
	}
	need := func(table map[string]bool, id string) {
		t.Helper()
		if !table[id] {
			t.Fatalf("dangling report reference %q", id)
		}
	}
	for _, n := range report.Nodes {
		add(nodes, n.ID)
		if len(n.Span) == 0 || string(n.Span) == "null" {
			t.Fatal("node has no source span")
		}
	}
	for _, f := range report.Flows {
		add(flows, f.ID)
		for _, p := range f.Parameters {
			add(bindings, p.ID)
		}
	}
	for _, b := range report.Bindings {
		add(bindings, b.ID)
		need(nodes, b.Node)
	}
	for _, c := range report.Contexts {
		add(contexts, c.ID)
	}
	for _, e := range report.EntryPoints {
		add(entries, e.ID)
	}
	for _, r := range report.Targets {
		add(targets, r.ID)
	}
	for _, n := range report.Nodes {
		for _, input := range n.Inputs {
			need(nodes, input)
		}
		if n.Flow != "" {
			need(flows, n.Flow)
		}
		if n.Binding != "" {
			need(bindings, n.Binding)
		}
	}
	for _, f := range report.Flows {
		if f.Handler != "" {
			need(flows, f.Handler)
		}
	}
	for _, c := range report.Contexts {
		if c.Parent != "" {
			need(contexts, c.Parent)
		}
		if c.Entry != "" {
			need(entries, c.Entry)
		}
		if c.Flow != "" {
			need(flows, c.Flow)
		}
		if c.Node != "" {
			need(nodes, c.Node)
		}
	}
	for _, f := range report.UnusedFlows {
		need(flows, f)
	}
	condition := func(cs []inspectionCondition) {
		t.Helper()
		for _, c := range cs {
			need(nodes, c.Node)
			need(contexts, c.Context)
		}
	}
	description := func(d inspectionDescription) {
		t.Helper()
		need(nodes, d.Node)
		for _, n := range d.Dependencies {
			need(nodes, n)
		}
		if d.Kind != "known" && d.Kind != "dynamic" && d.Kind != "calculated" {
			t.Fatalf("unknown target kind %s", d.Kind)
		}
	}
	for _, resources := range [][]inspectionResource{report.Reads, report.Targets, report.Capabilities} {
		for _, r := range resources {
			need(contexts, r.Context)
			need(nodes, r.Node)
			if r.Entry != "" {
				need(entries, r.Entry)
			}
			description(r.Target)
			condition(r.Conditions)
		}
	}
	for _, a := range report.Activations {
		need(nodes, a.Node)
		need(contexts, a.Context)
		condition(a.Conditions)
		for _, ref := range a.Targets {
			need(targets, ref.ID)
			condition(ref.Conditions)
		}
	}
	for _, e := range report.ErrorSources {
		need(nodes, e.Node)
		need(contexts, e.Context)
		condition(e.Conditions)
		if e.Handler != "" {
			need(flows, e.Handler)
		}
		if e.Dynamic != nil {
			description(*e.Dynamic)
		}
	}
}
func TestInspectionExample(t *testing.T) {
	got := run(t, "inspect", "examples/agent_file_workflow.lo", "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	var envelope struct{ Report inspectionReport }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	report := envelope.Report
	validateInspectionReferences(t, report)
	if report.ResultType != "Int!" || len(report.Reads) != 1 || len(report.Targets) != 2 || len(report.Activations) != 2 || len(report.Capabilities) != 3 {
		t.Fatalf("incomplete file workflow report: %+v", report)
	}
	expected := map[string]string{"File.read": "input.txt", "File.append": "summary.log", "File.write": "empty.txt"}
	for _, cap := range report.Capabilities {
		if expected[cap.Operation] != cap.Target.Value || cap.Target.Kind != "known" {
			t.Fatalf("unexpected workflow capability: %+v", cap)
		}
	}
}
func TestInspectionContextLimit(t *testing.T) {
	var source strings.Builder
	source.WriteString("flow F0() -> Int { return 1 }; ")
	for i := 1; i <= 14; i++ {
		fmt.Fprintf(&source, "flow F%d() -> Int { return Math.add(F%d(), F%d()) }; ", i, i-1, i-1)
	}
	source.WriteString("return F14()")
	got := run(t, "inspect", sourceFile(t, source.String()), "--format", "json")
	if got.status != 1 || got.stdout != "" {
		t.Fatalf("expected inspection limit: %+v", got)
	}
	if d := diagnostics(t, got.stderr).Diagnostics[0]; d.Code != "inspect.limit" {
		t.Fatalf("%+v", d)
	}
}

func TestInspectionRecordTargetSelection(t *testing.T) {
	report, _, _ := inspectSource(t, `record = Record.new(active: File.Append.new(File.Path.new("active"), UTF8.Bytes.new("x")), inactive: File.Write.new(File.Path.new("inactive"), Bytes.new("x"))); selected = record.active; commit selected`)
	if len(report.Targets) != 2 || len(report.Capabilities) != 1 || report.Capabilities[0].Target.Value != "active" {
		t.Fatalf("uncommitted record field became an activation: %+v", report.Capabilities)
	}
}

func TestInspectionCalculatedTextLimit(t *testing.T) {
	literal := strings.Repeat("x", 66000)
	program := fmt.Sprintf(`known = File.Append.new(File.Path.new(%q), UTF8.Bytes.new("x")); calculated = File.Append.new(File.Path.new(String.concat(%q, %q)), UTF8.Bytes.new("x")); commit known; commit calculated`, literal, literal[:33000], literal[33000:])
	report, _, _ := inspectSource(t, program)
	if len(report.Capabilities) != 2 {
		t.Fatalf("capability count: %d", len(report.Capabilities))
	}
	known, calculated := report.Capabilities[0].Target, report.Capabilities[1].Target
	if known.Kind != "known" || known.Value != literal {
		t.Fatal("large literal lost its known value")
	}
	if calculated.Kind != "dynamic" || calculated.Expression == "" || len(calculated.Dependencies) == 0 {
		t.Fatal("large calculated target lost its dynamic source context")
	}
}
