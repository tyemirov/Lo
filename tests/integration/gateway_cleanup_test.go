package integration_test

import (
	"encoding/json"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gatewaySource(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "docs/examples/gateway-log-cleanup.lo"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(string(data), `File.Path.new("gateway-logs")`, fmt.Sprintf("File.Path.new(%q)", root), 1)
}

func gatewayFields(t *testing.T, v runtimeValue) map[string]runtimeValue {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(v.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestGatewayLogCleanupEmptyAndMissing(t *testing.T) {
	record := installFileProbeFixture(t)
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			root := fileFixtureRoot(t)
			if missing {
				root = filepath.Join(root, "missing", "deployments")
			}
			got := fileOutput(t, runProgram(t, sourceFile(t, gatewaySource(t, root)), "run", "--list-root", root, "--remove-root", root, "--format", "json"))
			fields := gatewayFields(t, got)
			valueJSON(t, fields["removed_count"], `"0"`)
			valueJSON(t, fields["skipped_count"], `"0"`)
			status := "present"
			if missing {
				status = "absent"
			}
			valueJSON(t, fields["directory_status"], fmt.Sprintf("%q", status))
			if _, err := os.Stat(record); !os.IsNotExist(err) {
				t.Fatalf("empty category probed: %v", err)
			}
		})
	}
}

func TestGatewayLogCleanupChangedAndMissing(t *testing.T) {
	installFileProbeFixture(t)
	for _, action := range []string{"rewrite", "remove"} {
		t.Run(action, func(t *testing.T) {
			root := fileFixtureRoot(t)
			for _, name := range []string{"deployment-1.log", "deployment-2.log"} {
				fileFixture(t, root, name, "old")
			}
			t.Setenv("LO_TEST_PROBE_CHANGE", "deployment-1.log")
			t.Setenv("LO_TEST_PROBE_ACTION", action)
			fields := gatewayFields(t, fileOutput(t, runProgram(t, sourceFile(t, gatewaySource(t, root)), "run", "--list-root", root, "--remove-root", root, "--open-files-root", root, "--format", "json")))
			valueJSON(t, fields["removed_count"], `"1"`)
			valueJSON(t, fields["skipped_count"], `"1"`)
			valueJSON(t, fields["observed_bytes"], `"3"`)
			if _, err := os.Stat(filepath.Join(root, "deployment-2.log")); !os.IsNotExist(err) {
				t.Fatalf("later candidate remains: %v", err)
			}
		})
	}
}

func TestGatewayLogCleanupProbeFailure(t *testing.T) {
	installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	path := fileFixture(t, root, "deployment-1.log", "old")
	t.Setenv("LO_TEST_PROBE_STDERR", "probe failed")
	got := runProgram(t, sourceFile(t, gatewaySource(t, root)), "run", "--list-root", root, "--remove-root", root, "--open-files-root", root, "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"File.OpenFilesFailed"`) {
		t.Fatalf("probe failure: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("probe failure changed candidate: %v", err)
	}
}

func TestGatewayLogCleanupInspection(t *testing.T) {
	root := filepath.Join(fileFixtureRoot(t), "never-created")
	text := gatewaySource(t, root)
	report, _, _ := inspectSource(t, text)
	capabilities := map[string]bool{}
	for _, item := range report.Capabilities {
		capabilities[item.Operation] = true
	}
	for _, name := range []string{"File.list", "File.open_paths", "File.remove"} {
		if !capabilities[name] {
			t.Fatalf("missing capability %s", name)
		}
	}
	preview, _, _ := inspectSource(t, strings.Replace(text, "return Cleanup(root, cutoff_unix_ns)", "return Preview(root, cutoff_unix_ns)", 1))
	programEntry := ""
	for _, entry := range preview.EntryPoints {
		if entry.Kind == "program" {
			programEntry = entry.ID
		}
	}
	if programEntry == "" {
		t.Fatal("preview has no program entry")
	}
	for _, activation := range preview.Activations {
		if activation.Entry == programEntry {
			t.Fatal("preview activates effects")
		}
	}
	for _, capability := range preview.Capabilities {
		if capability.Entry == programEntry && capability.Operation == "File.remove" {
			t.Fatal("preview requires removal authority")
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("inspection created root: %v", err)
	}
}

func TestFileOpenPathsOutputLimit(t *testing.T) {
	installFileProbeFixture(t)
	root := fileFixtureRoot(t)
	t.Setenv("LO_TEST_PROBE_BYTES", fmt.Sprint((8<<20)+1))
	started := time.Now()
	got := runProgram(t, sourceFile(t, fmt.Sprintf(`flow Main() -> List<File.Path>! {return File.open_paths(File.Path.new(%q))};return Main()`, root)), "run", "--open-files-root", root, "--format", "json")
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.limit") {
		t.Fatalf("probe limit after %s: %+v", time.Since(started), got)
	}
}
