package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchConstructionPropagatesCaseListFailures(t *testing.T) {
	const source = `
flow Enabled(request: Bool) -> Bool { return request }
flow One(request: Bool) -> Int { return 1 }
flow Zero(request: Bool) -> Int { return 0 }
flow Run(index: Int, end: Int) -> Int! {
    first = Match.Case.new(Enabled, One)
    cases = List.new(first, List.at(List.new(first), index))
    matcher = Match.new(List.slice(cases, 0, end), Zero)
    return true |> Match.apply(matcher)
}
`
	t.Run("valid case list", func(t *testing.T) {
		valueJSON(t, runValue(t, source+"return Run(0, 1)"), `{"type":"Int","value":"1"}`)
	})
	for _, test := range []struct{ name, call, kind string }{
		{"missing element", "Run(1, 2)", "List.InvalidIndex"},
		{"invalid case slice", "Run(0, 3)", "List.InvalidBounds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runProgram(t, sourceFile(t, source+"return "+test.call), "run", "--format", "json")
			if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, test.kind) {
				t.Fatalf("wanted %s failure, got %+v", test.kind, got)
			}
		})
	}
}

func TestMatcherSelectedHostReads(t *testing.T) {
	const source = `
flow WantsFile(request: Record{load: Bool, path: File.Path, other: File.Path}) -> Bool {
    return request.load
}
flow ReadFile(request: Record{load: Bool, path: File.Path, other: File.Path}) -> Bytes! {
    return File.read(request.path)
}
flow ReadOther(request: Record{load: Bool, path: File.Path, other: File.Path}) -> Bytes! {
    return File.read(request.other)
}
flow Build() -> Match<Record{load: Bool, path: File.Path, other: File.Path}, Bytes!, Read> {
    return Match.new(List.new(Match.Case.new(WantsFile, ReadFile)), ReadOther)
}
`
	t.Run("construction without grants", func(t *testing.T) {
		valueJSON(t, runValue(t, source+"matcher = Build(); return 1"), `"1"`)
	})
	for _, test := range []struct {
		name string
		load bool
	}{
		{"selected case skips ungranted default", true},
		{"default skips ungranted case", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := fileFixture(t, root, "selected", "selected bytes")
			outside := fileFixtureRoot(t)
			ungranted := fileFixture(t, outside, "unselected", "unselected bytes")
			first, second := path, ungranted
			if !test.load {
				first, second = ungranted, path
			}
			program := source + fmt.Sprintf(`
flow Main() -> Bytes! {
    stored = Record.new(matchers: List.new(Build()))
    matcher = List.at(stored.matchers, 0)
    request = Record.new(load: %t, path: File.Path.new(%q), other: File.Path.new(%q))
    return request |> Match.apply(matcher)
}
return Main()
`, test.load, first, second)
			actual := runProgram(t, sourceFile(t, program), "run", "--read-root", root, "--format", "json")
			valueJSON(t, fileOutput(t, actual), `"c2VsZWN0ZWQgYnl0ZXM="`)
		})
	}
	t.Run("selected read returns typed failure", func(t *testing.T) {
		root := fileFixtureRoot(t)
		program := source + fmt.Sprintf(`
return Record.new(load: true, path: File.Path.new(%q), other: File.Path.new(%q)) |> Match.apply(Build())
`, filepath.Join(root, "missing"), filepath.Join(fileFixtureRoot(t), "ungranted"))
		actual := runProgram(t, sourceFile(t, program), "run", "--read-root", root, "--format", "json")
		if actual.status != 1 || actual.stdout != "" || !strings.Contains(actual.stderr, "File.ReadFailed") {
			t.Fatalf("selected reading handler failure: %+v", actual)
		}
	})
}

func TestMatcherConstructionDefersCallbacks(t *testing.T) {
	source := fmt.Sprintf(`
flow Expensive(value: Int) -> Bool {
    return Math.gt(Math.sum(List.new(%s)), value)
}
flow One(value: Int) -> Bool { return Math.eq(value, 1) }
flow Keep(value: Int) -> Int! { return Math.add(value, 6) }
flow Fail(value: Int) -> Int! { return Math.div(value, 0) }
flow Build() -> Match<Int, Int!> {
    return Match.new(
        cases: List.new(Match.Case.new(One, Keep), Match.Case.new(Expensive, Fail)),
        default: Fail
    )
}
`, strings.TrimSuffix(strings.Repeat("1,", 256), ","))

	t.Run("construction", func(t *testing.T) {
		program := source + "matcher = Build(); return 4"
		checked := run(t, "check", sourceFile(t, program), "--format", "json")
		if checked.status != 0 {
			t.Fatalf("check matcher construction: %+v", checked)
		}
		valueJSON(t, runValue(t, program, "--max-steps", "100"), `"4"`)
	})
	t.Run("first match", func(t *testing.T) {
		program := source + "return 1 |> Match.apply(Build())"
		valueJSON(t, runValue(t, program, "--max-steps", "100"), `{"type":"Int","value":"7"}`)
	})
}

func TestMatcherReusableValues(t *testing.T) {
	const source = `
flow One(value: Int) -> Bool { return Math.eq(value, 1) }
flow Two(value: Int) -> Bool { return Math.eq(value, 2) }
flow AddTen(value: Int) -> Int! { return Math.add(value, 10) }
flow Fail(value: Int) -> Int! { return Math.div(value, 0) }
flow Fallback(value: Int) -> Int! { return Math.add(value, 20) }
flow Build() -> Match<Int, Int!> {
    cases = List.new(Match.Case.new(One, AddTen), Match.Case.new(Two, Fail))
    return Match.new(cases: cases, default: Fallback)
}
flow Apply(value: Int, matchers: List<Match<Int, Int!>>) -> Int! {
    matcher = List.at(matchers, 0)
    return value |> Match.apply(matcher)
}
flow Repeat() -> List<Int>! {
    choices = Record.new(rules: List.new(Build()))
    first = Apply(1, choices.rules)
    fallback = Apply(3, choices.rules)
    again = Apply(1, choices.rules)
    return List.new(first, fallback, again)
}
`

	t.Run("repeated applications", func(t *testing.T) {
		valueJSON(t, runValue(t, source+"return Repeat()"), `{"type":"List<Int>","value":[{"type":"Int","value":"11"},{"type":"Int","value":"23"},{"type":"Int","value":"11"}]}`)
	})
	t.Run("selected handler failure", func(t *testing.T) {
		program := source + "flow Main() -> Int! { result = Apply(2, List.new(Build())); return result }; return Main()"
		actual := runProgram(t, sourceFile(t, program), "run", "--format", "json")
		if actual.status != 1 || actual.stdout != "" || !strings.Contains(actual.stderr, "Math.DivideByZero") {
			t.Fatalf("selected handler failure: %+v", actual)
		}
	})
	t.Run("default only", func(t *testing.T) {
		program := source + "matcher = Match.new(cases: List.new(), default: Fallback); return Match.apply(matcher: matcher, request: 9)"
		valueJSON(t, runValue(t, program), `{"type":"Int","value":"29"}`)
	})
	t.Run("stored matcher output", func(t *testing.T) {
		value := runValue(t, source+"return Record.new(case: Match.Case.new(One, AddTen), matcher: Build())")
		valueJSON(t, recordField(t, value, "case"), `{"predicate":"One","handler":"AddTen"}`)
		valueJSON(t, recordField(t, value, "matcher"), `{"cases":[{"predicate":"One","handler":"AddTen"},{"predicate":"Two","handler":"Fail"}],"default":"Fallback"}`)
	})
}

func TestMatcherChecksEveryCallback(t *testing.T) {
	const source = `
flow Predicate(value: Int) -> Bool { return true }
flow WrongPredicate(value: Int) -> Int { return value }
flow Handler(value: Int) -> Int { return value }
flow WrongHandler(value: Int) -> String { return Int.to_string(value) }
flow ReadHandler(value: Int) -> Int! {
    bytes = File.read(File.Path.new("input"))
    return value
}
flow FallibleHandler(value: Int) -> Int! { return value }
flow WriteHandler(value: Int) -> Int! {
    target = File.Write.new(File.Path.new("unused"), Bytes.new("unused"))
    commit target
    return value
}
flow CallsWrite(value: Int) -> Int! { return WriteHandler(value) }
`
	for _, test := range []struct {
		name, body, code string
	}{
		{"predicate output", "return Match.Case.new(WrongPredicate, Handler)", "check.type-mismatch"},
		{"handler output", "return Match.new(List.new(Match.Case.new(Predicate, WrongHandler)), Handler)", "check.type-mismatch"},
		{"host observation in pure callback", "flow Apply(value: Int) -> Int! { return value |> Match.apply(Match.new(List.new(Match.Case.new(Predicate, ReadHandler)), FallibleHandler)) }; return List.map(List.new(1), Apply)", "check.effectful-callback"},
		{"indirect commit", "return Match.new(List.new(), CallsWrite)", "check.effectful-callback"},
		{"named handler", "return Match.Case.new(Predicate, Handler(1))", "check.flow-reference"},
		{"input type", `return "wrong" |> Match.apply(Match.new(List.new(), Handler))`, "check.type-mismatch"},
		{"callback cycle", "flow Cycle(value: Int) -> Int { unused = Match.new(List.new(), Cycle); return value }; return 1", "check.cycle"},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := run(t, "check", sourceFile(t, source+test.body), "--format", "json")
			if actual.status != 1 || !strings.Contains(actual.stderr, test.code) {
				t.Fatalf("check: %+v, want %s", actual, test.code)
			}
		})
	}
	t.Run("obsolete inline syntax", func(t *testing.T) {
		actual := run(t, "check", sourceFile(t, "return Match.new(1, 1: 2, _: 3)"), "--format", "json")
		if actual.status != 1 {
			t.Fatalf("obsolete inline Match.new: %+v", actual)
		}
	})
}
