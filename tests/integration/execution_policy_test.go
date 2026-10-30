package integration_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const executionPolicySteps = `flow Count(index: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: index, next_state: Math.add(index, 1)) }; `

func TestExecutionPolicyLiveReads(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "data", strings.Repeat("x", 128<<10))
	program := executionPolicySteps + fmt.Sprintf(`flow Read(path: File.Path, state: Int, item: Int) -> Int! { bytes = File.read(path); return Math.add(state, 1) }; flow Run() -> Int! { return Flow.fold(List.unfold(0, 512, Count), 0, File.Path.new(%q), Read) }; return Run()`, path)
	source := sourceFile(t, program)
	valueJSON(t, fileOutput(t, run(t, "run", source, "--read-root", root, "--max-live-bytes", "4194304", "--format", "json")), `"512"`)
	got := run(t, "run", source, "--read-root", root, "--max-live-bytes", "4194304", "--max-data-bytes", "1048576", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" || !strings.Contains(got.stderr, "data bytes") {
		t.Fatalf("explicit cumulative data policy: %+v", got)
	}
}

func TestExecutionPolicyRetainedData(t *testing.T) {
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "data", strings.Repeat("x", 128<<10))
	program := executionPolicySteps + fmt.Sprintf(`flow Read(path: File.Path, state: String, item: Int) -> String! { bytes = File.read(path); return String.concat(state, UTF8.String.new(bytes)) }; flow Run() -> String! { return Flow.fold(List.unfold(0, 64, Count), "", File.Path.new(%q), Read) }; return Run()`, path)
	got := run(t, "run", sourceFile(t, program), "--read-root", root, "--max-live-bytes", "4194304", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" || !strings.Contains(got.stderr, "live") {
		t.Fatalf("retained data escaped live policy: %+v", got)
	}
}

func TestExecutionPolicyOptions(t *testing.T) {
	path := sourceFile(t, "return 1")
	for _, option := range []string{"--max-value-bytes", "--max-live-bytes", "--max-data-bytes", "--max-list-items", "--max-file-entries", "--max-file-directories"} {
		t.Run(option, func(t *testing.T) {
			valueJSON(t, runValue(t, "return 1", option, "0"), `"1"`)
			for _, arguments := range [][]string{{option, "-1"}, {option, "no"}, {option, "18446744073709551616"}, {option, "1", option, "2"}, {option}} {
				got := run(t, append([]string{"run", path, "--format", "json"}, arguments...)...)
				if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
					t.Fatalf("invalid execution policy accepted %q: %+v", arguments, got)
				}
			}
			got := run(t, "check", path, "--format", "json", option, "1")
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "cli.invalid-invocation" {
				t.Fatalf("policy accepted outside execution: %+v", got)
			}
		})
	}
}

func TestExecutionPolicyConfiguredLimits(t *testing.T) {
	for _, test := range []struct{ name, source, option string }{
		{"value", "return 1", "--max-value-bytes"},
		{"live", "return 1", "--max-live-bytes"},
		{"list", "return List.new(1, 2)", "--max-list-items"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "run", sourceFile(t, test.source), test.option, "1", "--format", "json")
			if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
				t.Fatalf("configured policy was not applied: %+v", got)
			}
		})
	}
}

func TestExecutionPolicyNestedListItems(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "a")
	fileFixture(t, root, "b", "b")
	for _, fixture := range []struct {
		name, source, resultType string
		options, arguments       []string
	}{
		{"fallible-file-list", fmt.Sprintf(`return File.list(File.Path.new(%q))`, root), "List<File.Entry>", []string{"--list-root", root}, nil},
		{"nominal-file-scan", fmt.Sprintf(`return File.scan(File.Path.new(%q))`, root), "File.Scan", []string{"--list-root", root}, nil},
		{"inherited-invocation", `return Host.Invocation.current()`, "Host.Invocation", nil, []string{"--", "first", "second"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := sourceFile(t, fixture.source)
			for _, limit := range []string{"1", "0"} {
				t.Run(limit, func(t *testing.T) {
					args := []string{"run", path, "--format", "json", "--max-list-items", limit}
					args = append(args, fixture.options...)
					args = append(args, fixture.arguments...)
					got := run(t, args...)
					if limit == "0" {
						if out := fileOutput(t, got); out.Type != fixture.resultType {
							t.Fatalf("unlimited nested list changed type: %s, want %s", out.Type, fixture.resultType)
						}
						return
					}
					if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" || !strings.Contains(got.stderr, "list elements") {
						t.Fatalf("nested list escaped configured policy: %+v", got)
					}
				})
			}
		})
	}
}

func TestExecutionPolicyLogicalWeightOverflow(t *testing.T) {
	var program strings.Builder
	program.WriteString("items0 = List.new(0); ")
	for depth := 1; depth <= 64; depth++ {
		fmt.Fprintf(&program, "items%d = List.new(items%d, items%d); ", depth, depth-1, depth-1)
	}
	program.WriteString("return List.length(items64)")
	got := run(t, "run", sourceFile(t, program.String()), "--max-value-bytes", "0", "--max-live-bytes", "0", "--max-data-bytes", "0", "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
		t.Fatalf("logical weight overflow must fail with a typed limit: %+v", got)
	}
}

func TestExecutionPolicyFileQuotas(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "a")
	fileFixture(t, root, "b", "b")
	first, err := os.MkdirTemp(fileFixtureRoot(t), "first-")
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.MkdirTemp(fileFixtureRoot(t), "second-")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name, source, option string
		grants               []string
	}{
		{"entries", fmt.Sprintf(`flow Main() -> Int! { entries = File.list(File.Path.new(%q)); return List.length(entries) }; return Main()`, root), "--max-file-entries", []string{"--list-root", root}},
		{"directories", fmt.Sprintf(`flow Main() -> Int! { first = File.directory(File.Path.new(%q)); second = File.directory(File.Path.new(%q)); return List.length(List.new(first, second)) }; return Main()`, first, second), "--max-file-directories", []string{"--list-root", first, "--list-root", second}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := sourceFile(t, fixture.source)
			for _, limit := range []string{"1", "2", "0"} {
				t.Run(limit, func(t *testing.T) {
					arguments := []string{"run", path, "--format", "json", fixture.option, limit}
					got := run(t, append(arguments, fixture.grants...)...)
					if limit != "1" {
						valueJSON(t, fileOutput(t, got), `"2"`)
						return
					}
					if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
						t.Fatalf("configured file quota was not applied: %+v", got)
					}
				})
			}
		})
	}
}

func TestExecutionPolicyScalarIdentity(t *testing.T) {
	for _, test := range []struct{ name, literal string }{{"boolean", "false"}, {"null", "null"}, {"integer", "1"}, {"float", "1.0"}, {"text", `"x"`}} {
		t.Run(test.name, func(t *testing.T) {
			for _, aliases := range []bool{false, true} {
				var program strings.Builder
				literal := test.literal
				if aliases {
					fmt.Fprintf(&program, "original = %s; ", literal)
					literal = "original"
				}
				for index := 0; index < 100; index++ {
					fmt.Fprintf(&program, "item%d = %s; ", index, literal)
				}
				program.WriteString("return 1")
				got := run(t, "run", sourceFile(t, program.String()), "--max-data-bytes", "1000", "--format", "json")
				if aliases {
					valueJSON(t, fileOutput(t, got), `"1"`)
				} else if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
					t.Fatalf("distinct scalar productions escaped data quota: %+v", got)
				}
			}
		})
	}
}
