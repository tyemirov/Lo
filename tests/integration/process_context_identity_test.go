package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestProcessExecutableReplacementBeforeActivation(t *testing.T) {
	for _, scenario := range []string{"identity-replacement", "digest-change"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCleanupFixture(t)
			preserved := fileFixture(t, fixture.writes, "preserved", "before")
			later := filepath.Join(fixture.writes, "later")
			replacement := filepath.Join(fixture.root, "replacement")
			copyCommand := exec.Command("cp", fixture.executable, replacement)
			if output, err := copyCommand.CombinedOutput(); err != nil {
				t.Fatalf("copy registered executable fixture: %v: %s", err, output)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if scenario == "identity-replacement" {
					if err := os.Rename(replacement, fixture.executable); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
				} else {
					file, err := os.OpenFile(fixture.executable, os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					_, writeErr := file.WriteString("unreviewed bytes")
					closeErr := file.Close()
					if writeErr != nil || closeErr != nil {
						t.Errorf("change registered executable fixture: %v %v", writeErr, closeErr)
						w.WriteHeader(500)
						return
					}
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			body := fmt.Sprintf(`context = Process.Context.new(Map.new(Record.new(key: "PATH", value: %q)), File.Path.new(%q)); executable = Process.Executable.resolve(context, "command"); command = Process.Command.new(context, executable, List.new("write", %q), Bytes.new("")); method = HTTP.Method.new("GET"); url = HTTP.URL.new(%q); headers = HTTP.Headers.new(List.new()); barrier = HTTP.Client.Request.new(method, url, headers, Bytes.new("")); observed = commit barrier; result = commit command; later = File.Write.new(File.Path.new(%q), Bytes.new("wrong")); commit later; return Process.Termination.exit_code(result.termination)`, fixture.root, fixture.root, preserved, server.URL, later)
			flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes), "--write-root", fixture.writes, "--http-allow", httpGrant("GET", server.URL))
			got := run(t, append([]string{"run", sourceFile(t, processContextProgram("Int", body)), "--format", "json"}, flags...)...)
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 2 || diagnostic.Code != "runtime.host-failure" || diagnostic.ProcessProgress == nil || calls.Load() != 1 {
				t.Fatalf("replaced executable admission: %+v calls=%d", got, calls.Load())
			}
			progress := processFields(t, *diagnostic.ProcessProgress)
			valueJSON(t, progress["start_confirmed"], `false`)
			valueJSON(t, progress["cleanup_complete"], `true`)
			data, err := os.ReadFile(preserved)
			if err != nil || string(data) != "before" {
				t.Fatalf("replaced executable ran: %q %v", data, err)
			}
			if _, err := os.Stat(later); !os.IsNotExist(err) {
				t.Fatalf("later commit ran after executable replacement: %v", err)
			}
		})
	}
}
