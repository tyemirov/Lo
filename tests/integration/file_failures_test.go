package integration_test

import (
	"encoding/json"

	"fmt"
	"os"

	"strings"
	"testing"
)

type invalidNameEntry struct{ os.DirEntry }

func (invalidNameEntry) Name() string { return string([]byte{0xff}) }

func progressFields(t *testing.T, got result, completed, total int, stage, kind string) {
	t.Helper()
	var output struct {
		Diagnostics []struct {
			Code     string
			Error    *runtimeValue
			Progress *runtimeValue `json:"removal_progress"`
			Entries  *runtimeValue `json:"removal_entries"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &output); err != nil {
		t.Fatalf("%v: %+v", err, got)
	}
	if len(output.Diagnostics) != 1 {
		t.Fatalf("missing diagnostic: %+v", got)
	}
	d := output.Diagnostics[0]
	var progress runtimeValue
	if kind != "" {
		if d.Error == nil || d.Error.Kind != kind {
			t.Fatalf("error kind: %+v", got)
		}
		var input map[string]runtimeValue
		if err := json.Unmarshal(d.Error.Input.Value, &input); err != nil {
			t.Fatal(err)
		}
		progress = input["progress"]
		if _, ok := input["entries"]; !ok {
			t.Fatalf("missing ordered entries: %+v", got)
		}
	} else {
		if d.Progress == nil || d.Entries == nil {
			t.Fatalf("missing host removal receipt: %+v", got)
		}
		progress = *d.Progress
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(progress.Value, &fields); err != nil {
		t.Fatalf("progress: %v: %+v", err, got)
	}
	valueJSON(t, fields["completed_count"], fmt.Sprintf("%q", fmt.Sprint(completed)))
	valueJSON(t, fields["total_count"], fmt.Sprintf("%q", fmt.Sprint(total)))
	valueJSON(t, fields["stage"], fmt.Sprintf("%q", stage))
	if strings.Contains(got.stderr, "ChangedUnixNS") || strings.Contains(got.stderr, "DirectoryIdentity") || strings.Contains(got.stderr, "FileSession") {
		t.Fatalf("opaque provenance in output: %+v", got)
	}
}

// This controlled uncoordinated replacement is outside identity protection.

func TestFileRemovalChangedAndRepeated(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(fmt.Sprint(repeat), func(t *testing.T) {
			root := fileFixtureRoot(t)
			name := fileFixture(t, root, "a", "old")
			middle := fmt.Sprintf(`change = File.Append.new(File.Path.new(%q), UTF8.Bytes.new("changed")); commit change;`, name)
			if repeat {
				middle = "commit removal;"
			}
			program := fmt.Sprintf(`flow Clean() -> Int! { entries = File.list(File.Path.new(%q)); removal = File.Remove.new(entries); %s commit removal; return 0 }; return Clean()`, root, middle)
			got := runProgram(t, sourceFile(t, program), "run", "--list-root", root, "--remove-root", root, "--write-root", root, "--format", "json")
			if got.status != 1 {
				t.Fatalf("expected stale observation: %+v", got)
			}
			progressFields(t, got, 0, 1, "preflight", "File.EntryChanged")
		})
	}
}
