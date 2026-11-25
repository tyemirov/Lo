package testsession_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/testsession"
)

func TestFixtureSessionRuntimeValuesSeparateCodeAndSourceData(t *testing.T) {
	server, directory := sessionFixture(t, 2)
	compile := func(name, source string, purpose string) testsession.CompileResult {
		t.Helper()
		path := filepath.Join(directory, name+".lo")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(source); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		connection, err := net.Dial("unix", server.Address())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := connection.Close(); err != nil {
				t.Error(err)
			}
		})
		if err := json.NewEncoder(connection).Encode(map[string]any{"operation": "compile-retained", "compile": map[string]string{"source": path, "purpose": purpose}}); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Compile testsession.CompileResult `json:"compile"`
			Error   string                    `json:"error"`
		}
		if err := json.NewDecoder(connection).Decode(&response); err != nil || response.Error != "" {
			t.Fatalf("compile purpose %q: %v: %s", purpose, err, response.Error)
		}
		metadata, err := exec.Command(response.Compile.Artifact, "metadata").Output()
		var identity struct{ Source, SourceSHA256 string }
		var raw map[string]string
		if err != nil || json.Unmarshal(metadata, &raw) != nil {
			t.Fatalf("private artifact metadata: %v %s", err, metadata)
		}
		identity.Source, identity.SourceSHA256 = raw["source"], raw["source_sha256"]
		if identity.Source != path || identity.SourceSHA256 != fmtDigest([]byte(source)) {
			t.Fatalf("private source identity lost: %s", metadata)
		}
		return response.Compile
	}
	for _, fixture := range []struct{ name, source, want string }{
		{"first", "return 1", "1\n"},
		{"second", "return 2", "2\n"},
		{"other-code", "return Math.add(1, 2)", "3\n"},
		{"other-data", "return Math.add(4, 1)", "5\n"},
		{"other-column", "return Math.add(40, 1)", "41\n"},
	} {
		artifact := compile(fixture.name, fixture.source, "runtime-values")
		if output, err := exec.Command(artifact.Artifact, "run").CombinedOutput(); err != nil || string(output) != fixture.want {
			t.Fatalf("private runtime value %s: %v %q", fixture.name, err, output)
		}
	}
	stats, _ := json.Marshal(server.Stats())
	var counts map[string]uint64
	if err := json.Unmarshal(stats, &counts); err != nil || counts["CodeBuilds"] != 3 || counts["CodeReused"] != 2 {
		t.Fatalf("exact code/data/column cache identity: %s (%v)", stats, err)
	}
	for _, name := range []string{"diagnostic-first", "diagnostic-second"} {
		artifact := compile(name, "return Math.div(1, 0)", "runtime-values")
		path := filepath.Join(directory, name+".lo")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(artifact.Artifact, "run", "--format", "json").CombinedOutput(); err == nil || !strings.Contains(string(output), path) || !strings.Contains(string(output), "Math.DivideByZero") {
			t.Fatalf("private lexical diagnostic: %v %s", err, output)
		}
	}
	before := server.Stats()
	ordinary := compile("ordinary", "return 2", "")
	if output, err := exec.Command(ordinary.Artifact, "run").CombinedOutput(); err != nil || string(output) != "2\n" {
		t.Fatalf("ordinary source compilation changed: %v %q", err, output)
	}
	after := server.Stats()
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(after)
	var beforeCounts, afterCounts map[string]uint64
	_ = json.Unmarshal(left, &beforeCounts)
	_ = json.Unmarshal(right, &afterCounts)
	if beforeCounts["CodeBuilds"] != afterCounts["CodeBuilds"] || beforeCounts["CodeReused"] != afterCounts["CodeReused"] {
		t.Fatal("ordinary source compilation entered the value-only code cache")
	}
}

func fmtDigest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
