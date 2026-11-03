package integration_test

import (
	"encoding/json"

	"path/filepath"
	"strings"

	"testing"
)

func goLockPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(fileFixtureRoot(t), "lease")
	registryByteFixture(t, path, []byte("unchanged"))
	return path
}

func TestProgramLockGoInspection(t *testing.T) {
	path := sourceFile(t, `flow Main() -> Lock.Lease! { target=Lock.File.new(Program.path()); lease=commit target; return lease }; return Main()`)
	got := run(t, "inspect", path, "--format", "json")
	var doc struct {
		Report struct {
			Reads []struct {
				Operation string
				Target    struct{ Type string }
			}
			Targets     []struct{ Operation string }
			Activations []json.RawMessage
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if got.status != 0 || len(doc.Report.Reads) != 1 || doc.Report.Reads[0].Operation != "Program.path" || doc.Report.Reads[0].Target.Type != "File.Path" || len(doc.Report.Targets) != 1 || doc.Report.Targets[0].Operation != "Lock.File.new" || len(doc.Report.Activations) != 1 || !strings.Contains(got.stdout, "Program.EntryUnavailable") || !strings.Contains(got.stdout, "Lock.acquire") || !strings.Contains(got.stdout, "Lock.Busy") || !strings.Contains(got.stdout, "Lock.Failed") {
		t.Fatalf("inspection metadata: %+v", got)
	}
}
