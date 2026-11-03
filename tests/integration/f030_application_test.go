package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f030ExampleFlows(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	entry := strings.LastIndex(source, "\nreturn ")
	if entry < 0 {
		t.Fatalf("example %s has no final entry", name)
	}
	return source[:entry] + "\n"
}

func TestF030ApplicationChecks(t *testing.T) {
	for _, name := range []string{"file_policy.lo", "probe_fields.lo"} {
		t.Run(name, func(t *testing.T) {
			got := run(t, "check", filepath.Join("examples", name), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("application example check: %+v", got)
			}
		})
	}
}

func TestF030DirectFilePolicy(t *testing.T) {
	flows := f030ExampleFlows(t, "file_policy.lo")
	root := fileFixtureRoot(t)
	for _, name := range []string{"z.tmp", "a.tmp", ".hidden.tmp", "keep.txt", "é.tmp", "line\nbreak.tmp"} {
		fileFixture(t, root, name, "preserved")
	}
	directory, err := os.MkdirTemp(root, "directory-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, filepath.Join(root, "directory.tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.tmp", filepath.Join(root, "symlink.tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "keep.txt"), filepath.Join(root, "hardlink.tmp")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		owner    int
		cutoff   string
		expected string
	}{
		{"selected", os.Getuid(), "9999999999999999999", `[{"type":"String","value":"a.tmp"},{"type":"String","value":"z.tmp"},{"type":"String","value":"é.tmp"}]`},
		{"other-owner", os.Getuid() + 1, "9999999999999999999", `[]`},
		{"recent", os.Getuid(), "0", `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := flows + fmt.Sprintf(`return DirectFilePolicy(File.Path.new(%q), %d, %s, "*.tmp")`, root, test.owner, test.cutoff)
			value := fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--list-root", root, "--format", "json"))
			fields := processFields(t, value)
			valueJSON(t, fields["names"], test.expected)
			valueJSON(t, fields["vanished"], `[]`)
		})
	}
	for _, name := range []string{"z.tmp", "a.tmp", ".hidden.tmp", "keep.txt", "é.tmp", "line\nbreak.tmp"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != "preserved" {
			t.Fatalf("policy changed %q: %q %v", name, data, err)
		}
	}
}

func TestF030ExactProbeFields(t *testing.T) {
	flows := f030ExampleFlows(t, "probe_fields.lo")
	batch := `List.new(File.Path.new("/candidate/a"), File.Path.new("/candidate/é"))`
	root := fileFixtureRoot(t)
	invalidUTF8 := fileFixture(t, root, "invalid-utf8", string([]byte{'n', '/', 0xff, 0}))
	for _, test := range []struct{ name, data, kind, names string }{
		{"empty", `Bytes.new("")`, "", `[]`},
		{"ordered-duplicates", `Bytes.new("p12\u0000\nn/candidate/é\u0000n/candidate/a\u0000n/candidate/é\u0000\n")`, "", `[{"type":"File.Path","value":"/candidate/é"},{"type":"File.Path","value":"/candidate/a"}]`},
		{"missing-terminator", `Bytes.new("n/candidate/a")`, "Probe.InvalidFields", `[]`},
		{"newline-only", `Bytes.new("\n")`, "Probe.InvalidFields", `[]`},
		{"incomplete-last-record", `Bytes.new("n/candidate/a\u0000n/candidate/é")`, "Probe.InvalidFields", `[]`},
		{"empty-field", `Bytes.new("n/candidate/a\u0000\u0000")`, "Probe.InvalidFields", `[]`},
		{"empty-name", `Bytes.new("n\u0000")`, "Probe.EmptyName", `[]`},
		{"unknown-field", `Bytes.new("x12\u0000")`, "Probe.InvalidFields", `[]`},
		{"invalid-process", `Bytes.new("pabc\u0000")`, "Probe.InvalidFields", `[]`},
		{"out-of-batch", `Bytes.new("n/candidate/a\u0000n/outside\u0000")`, "Probe.UnexpectedPath", `[]`},
		{"lexical-alias", `Bytes.new("n/candidate/../candidate/a\u0000")`, "Probe.UnexpectedPath", `[]`},
		{"malformed-utf8", `Bytes.slice(Bytes.new("né\u0000"), 0, 2)`, "Probe.InvalidFields", `[]`},
		{"malformed-utf8-framed", fmt.Sprintf(`File.read(File.Path.new(%q))`, invalidUTF8), "UTF8.InvalidBytes", `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := flows + fmt.Sprintf(`flow Failed(error: Error) -> Record{kind: String, names: List<File.Path>} { return Record.new(kind: error.kind, names: List.new()) }; flow Main() -> Record{kind: String, names: List<File.Path>} onError Failed { return Record.new(kind: "", names: ParseProbe(%s, %s)) }; return Main()`, test.data, batch)
			fields := processFields(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--read-root", root, "--format", "json")))
			valueJSON(t, fields["kind"], fmt.Sprintf("%q", test.kind))
			valueJSON(t, fields["names"], test.names)
		})
	}
}

func TestF030ProbeComposition(t *testing.T) {
	flows := f030ExampleFlows(t, "probe_fields.lo")
	source := flows + `flow CompareRank(left: Record{rank: Int, tag: String}, right: Record{rank: Int, tag: String}) -> Int { return Math.sub(left.rank, right.rank) }; flow RankTag(row: Record{rank: Int, tag: String}) -> String { return row.tag }; flow Main() -> Record{batches: List<List<File.Path>>, empty: List<List<File.Path>>, joined: String, equal: Bool, reversed: Bool, stable: List<String>}! { paths = List.new(File.Path.new("/b"), File.Path.new("/a"), File.Path.new("/b")); unique = UniquePaths(paths); rows = List.new(Record.new(rank: 1, tag: "first"), Record.new(rank: 0, tag: "second"), Record.new(rank: 1, tag: "third")); return Record.new(batches: PathChunks(paths, 2), empty: PathChunks(List.new(), 2), joined: JoinPathNames(unique, " | "), equal: PathsEqual(unique, List.new(File.Path.new("/b"), File.Path.new("/a"))), reversed: PathsEqual(unique, List.new(File.Path.new("/a"), File.Path.new("/b"))), stable: List.map(List.sort(rows, CompareRank), RankTag)) }; return Main()`
	fields := processFields(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")))
	valueJSON(t, fields["batches"], `[{"type":"List<File.Path>","value":[{"type":"File.Path","value":"/b"},{"type":"File.Path","value":"/a"}]},{"type":"List<File.Path>","value":[{"type":"File.Path","value":"/b"}]}]`)
	valueJSON(t, fields["empty"], `[]`)
	valueJSON(t, fields["joined"], `"/b | /a"`)
	valueJSON(t, fields["equal"], `true`)
	valueJSON(t, fields["reversed"], `false`)
	valueJSON(t, fields["stable"], `[{"type":"String","value":"second"},{"type":"String","value":"first"},{"type":"String","value":"third"}]`)
	for _, size := range []int{0, -1} {
		source := flows + fmt.Sprintf(`flow Failed(error: Error) -> String { return error.kind }; flow Main() -> String onError Failed { chunks = PathChunks(List.new(), %d); return "accepted" }; return Main()`, size)
		valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")), `"Probe.InvalidBatchSize"`)
	}
}
