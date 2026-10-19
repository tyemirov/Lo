package integration_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypedCommitFailuresAndBindings(t *testing.T) {
	for _, test := range []struct{ name, text, code string }{
		{"non-target", `target = 1; receipt = commit target; return receipt`, "check.not-committable"},
		{"duplicate", `target = File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("x")); receipt = 0; receipt = commit target`, "resolve.duplicate-name"},
		{"forward-reference", `target = File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("x")); earlier = receipt; receipt = commit target`, "resolve.unknown-name"},
		{"total-flow", `flow Invalid() -> Int { target = File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("x")); receipt = commit target; return receipt.bytes_written }; return Invalid()`, "check.unhandled-failure"},
		{"receipt-field", `target = File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("x")); receipt = commit target; return receipt.completed_count`, "check.unknown-field"},
		{"inline-target", `receipt = commit File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("x"))`, "syntax.expected-separator"},
		{"expression", `return commit target`, "syntax.unexpected-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, test.text), "--format", "json")
			if got.status == 0 || !strings.Contains(got.stderr, test.code) {
				t.Fatalf("want %s, got %+v", test.code, got)
			}
		})
	}
}

func TestTypedCommitFailureStopsLaterEffects(t *testing.T) {
	root := fileFixtureRoot(t)
	later := filepath.Join(root, "later")
	program := sourceFile(t, fmt.Sprintf(`
target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("first"))
receipt = commit target
next = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("later"))
commit next
return receipt
`, root, later))
	got := run(t, "run", program, "--write-root", root, "--format", "json")
	if got.status == 0 || !strings.Contains(got.stderr, "File.WriteFailed") {
		t.Fatalf("expected write failure: %+v", got)
	}
	if _, err := os.ReadFile(later); !os.IsNotExist(err) {
		t.Fatalf("later effect ran: %v", err)
	}
	if strings.Contains(got.stdout, "File.WriteReceipt") {
		t.Fatalf("failed activation published a receipt: %s", got.stdout)
	}
}

func TestTypedCommitSelfHandler(t *testing.T) {
	root := fileFixtureRoot(t)
	program := sourceFile(t, fmt.Sprintf(`
flow Failure(error: Error) -> Int { return 17 }
flow Attempt() -> Int onError Failure {
  target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("data"))
  receipt = commit target
  return receipt.bytes_written
}
return Attempt()
`, root))
	got := fileOutput(t, run(t, "run", program, "--write-root", root, "--format", "json"))
	valueJSON(t, got, `"17"`)
}

func TestTypedCommitInspectionDependencies(t *testing.T) {
	report, _, _ := inspectSource(t, `
target = File.Write.new(File.Path.new("unused"), UTF8.Bytes.new("data"))
receipt = commit target
next = File.Append.new(receipt.path, UTF8.Bytes.new("more"))
commit next
return receipt.bytes_written
`)
	var firstCommit string
	for _, node := range report.Nodes {
		if node.Kind == "commit" && node.Type == "File.WriteReceipt" {
			firstCommit = node.ID
		}
	}
	if firstCommit == "" {
		t.Fatal("inspection has no typed activation result")
	}
	bound := false
	for _, binding := range report.Bindings {
		bound = bound || binding.Name == "receipt" && binding.Node == firstCommit
	}
	dependent := false
	for _, resource := range report.Capabilities {
		if resource.Operation != "File.append" {
			continue
		}
		for _, dependency := range resource.Target.Dependencies {
			dependent = dependent || dependency == firstCommit
		}
	}
	if !bound || !dependent {
		t.Fatalf("missing receipt binding or dependency: bound=%t dependent=%t", bound, dependent)
	}
}

func TestTypedCommitServerReceipt(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "server-receipt")
	server := startServer(t, fmt.Sprintf(`
flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(status: 200, body: "ok") }
flow Failure(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(status: 500, body: "error") }
server = HTTP.Server.new(port: 0, handler: Handle, onError: Failure)
receipt = commit server
copy = receipt
text = String.concat(copy.address, "|", Int.to_string(copy.port))
output = File.Write.new(File.Path.new(%q), UTF8.Bytes.new(text))
commit output
`, path), "--write-root", root)
	_, port, err := net.SplitHostPort(server.address)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != server.address+"|"+port {
		t.Fatalf("server receipt %q: %v", data, err)
	}
	stopServer(t, server)
}

func TestTypedCommitRepeatedActivation(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "repeated")
	program := sourceFile(t, fmt.Sprintf(`
flow Count(receipt: File.AppendReceipt) -> Int { return receipt.bytes_written }
target = File.Append.new(File.Path.new(%q), UTF8.Bytes.new("x"))
first = commit target
second = commit target
copies = List.new(first, second, first)
return List.map(copies, Count)
`, path))
	got := fileOutput(t, run(t, "run", program, "--write-root", root, "--format", "json"))
	valueJSON(t, got, `[{"type":"Int","value":"1"},{"type":"Int","value":"1"},{"type":"Int","value":"1"}]`)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "xx" {
		t.Fatalf("repeated activation produced %q: %v", data, err)
	}
}
