package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
)

// cleanupFlows preserves the application's source bytes and transitive flow definitions.
func cleanupFlows(t *testing.T, roots ...string) string {
	t.Helper()
	path := filepath.Join(repository, "docs", "examples", "mac-cleanup.lo")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := source.New(path, string(data))
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := compiler.Parse(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	flows := map[string]syntax.Flow{}
	for _, statement := range program.Statements() {
		if flow, ok := statement.(syntax.Flow); ok {
			flows[flow.Name()] = flow
		}
	}
	selected := map[string]bool{}
	var visitFlow func(string)
	var visitExpression func(syntax.Expression)
	visitExpression = func(expression syntax.Expression) {
		switch value := expression.(type) {
		case syntax.Call:
			visitFlow(value.Name())
			for _, argument := range value.Arguments() {
				visitExpression(argument.Value)
			}
		case syntax.Pipe:
			visitExpression(value.Left())
			visitFlow(value.Name())
			for _, argument := range value.Arguments() {
				visitExpression(argument.Value)
			}
		case syntax.Reference:
			visitFlow(value.Name())
		case syntax.Match:
			visitExpression(value.Selector())
			for _, branch := range value.Cases() {
				switch head := branch.(type) {
				case syntax.ExactCase:
					visitExpression(head.Head())
				case syntax.PredicateCase:
					visitFlow(head.Flow())
				}
				visitExpression(branch.Body())
			}
		}
	}
	visitFlow = func(name string) {
		flow, exists := flows[name]
		if !exists || selected[name] {
			return
		}
		selected[name] = true
		visitFlow(flow.Handler())
		for _, statement := range flow.Body() {
			switch value := statement.(type) {
			case syntax.Binding:
				visitExpression(value.Value())
			case syntax.Return:
				visitExpression(value.Value())
			}
		}
	}
	for _, name := range roots {
		if _, exists := flows[name]; !exists {
			t.Fatalf("missing cleanup flow %s", name)
		}
		visitFlow(name)
	}
	var result strings.Builder
	for _, statement := range program.Statements() {
		if flow, ok := statement.(syntax.Flow); ok && selected[flow.Name()] {
			span := flow.Span()
			result.Write(data[span.Start().Offset:span.End().Offset])
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func TestMacCleanupDraftHelpers(t *testing.T) {
	for _, test := range []struct{ name, root, expression, want string }{
		{"safe-path", "SafePath", `SafePath(File.Path.new("/cache/a b-1.tmp"))`, `true`},
		{"unsafe-path", "SafePath", `SafePath(File.Path.new("/cache/a\nb.tmp"))`, `false`},
		{"builder-fields", "BuilderField", `BuilderField("Driver", Record.new(pending: false, values: List.new("first")), "Driver: docker")`, `{"pending":{"type":"Bool","value":false},"values":{"type":"List<String>","value":[{"type":"String","value":"first"},{"type":"String","value":"docker"}]}}`},
		{"exact-member", "KnownOpen", `KnownOpen(List.new(File.Path.new("/a/b")), File.Path.new("/a/bc"))`, `false`},
		{"member", "KnownOpen", `KnownOpen(List.new(File.Path.new("/a/b")), File.Path.new("/a/b"))`, `true`},
		{"probe-name", "ParseNames", `ParseNames(List.new(File.Path.new("/a")), Bytes.new("\nn/b"))`, `[{"type":"File.Path","value":"/a"},{"type":"File.Path","value":"/b"}]`},
		{"report-path", "ReportPath", `ReportPath(File.Path.new("/fixture"), "cache")`, `"/fixture/cache"`},
		{"empty-all", "All", `All(List.new())`, `true`},
		{"empty-any", "Any", `Any(List.new())`, `false`},
		{"ordered-unique", "UniquePaths", `UniquePaths(List.new(File.Path.new("b"), File.Path.new("a"), File.Path.new("b")))`, `[{"type":"File.Path","value":"b"},{"type":"File.Path","value":"a"}]`},
		{"chunks", "PathChunks", `PathChunks(List.new(File.Path.new("a"), File.Path.new("b"), File.Path.new("c")), 2)`, `[{"type":"List<File.Path>","value":[{"type":"File.Path","value":"a"},{"type":"File.Path","value":"b"}]},{"type":"List<File.Path>","value":[{"type":"File.Path","value":"c"}]}]`},
		{"equal-empty", "StringsEqual", `StringsEqual(List.new(), List.new())`, `true`},
		{"unequal-lengths", "StringsEqual", `StringsEqual(List.new("docker"), List.new())`, `false`},
		{"equal-strings", "StringsEqual", `StringsEqual(List.new("docker", "local"), List.new("docker", "local"))`, `true`},
		{"unequal-strings", "StringsEqual", `StringsEqual(List.new("docker", "local"), List.new("docker", "remote"))`, `false`},
		{"join-empty-items", "JoinStrings", `JoinStrings(List.new("", "x", ""), ", ")`, `", x, "`},
	} {
		t.Run(test.name, func(t *testing.T) {
			program := cleanupFlows(t, test.root) + "return " + test.expression
			valueJSON(t, f030Value(t, program), test.want)
		})
	}
}

func TestMacCleanupInvalidBatchSize(t *testing.T) {
	program := cleanupFlows(t, "PathChunks") + `
flow Failed(error: Error) -> String { return error.kind }
flow Main() -> String onError Failed {
    batches = PathChunks(List.new(), 0)
    return "unexpected success"
}
return Main()`
	valueJSON(t, runValue(t, program), `"Application.InvalidState"`)
}

func TestMacCleanupStoragePaths(t *testing.T) {
	program := cleanupFlows(t, "StoragePaths") + `
flow Fixture() -> Record{count: Int, first: File.Path, last: File.Path}! {
    process = Process.Context.new(Map.new(), File.Path.new("/fixture"))
    paths = StoragePaths(Record.new(apply: false, home: File.Path.new("/fixture"), uid: 501, process: process))
    return Record.new(count: List.length(paths), first: List.at(paths, 0), last: List.at(paths, -1))
}
return Fixture()`
	valueJSON(t, f030Value(t, program), `{"count":{"type":"Int","value":"30"},"first":{"type":"File.Path","value":"/fixture/Library/Caches/staticcheck"},"last":{"type":"File.Path","value":"/fixture/Library/Containers/com.docker.docker/Data/vms"}}`)
}
