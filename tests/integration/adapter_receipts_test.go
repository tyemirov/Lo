package integration_test

import (
	"encoding/json"

	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"

	"path/filepath"
	"testing"
)

func TestAdapterFileReceipts(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "receipt.txt")
	source := sourceFile(t, fmt.Sprintf(`path = File.Path.new(%q); write = File.Write.new(path, UTF8.Bytes.new(text: "hé")); written = commit write; append = File.Append.new(path, UTF8.Bytes.new("!")); appended = commit append; return Record.new(write: written, append: appended)`, path))
	result := fileOutput(t, runProgram(t, source, "run", "--write-root", root, "--format", "json"))
	var fields map[string]runtimeValue
	if err := json.Unmarshal(result.Value, &fields); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, kind, count string }{{"write", "File.WriteReceipt", "3"}, {"append", "File.AppendReceipt", "1"}} {
		receipt := fields[test.name]
		if receipt.Type != test.kind {
			t.Fatalf("receipt type %q, want %s", receipt.Type, test.kind)
		}
		var data map[string]runtimeValue
		if err := json.Unmarshal(receipt.Value, &data); err != nil {
			t.Fatal(err)
		}
		valueJSON(t, data["bytes_written"], fmt.Sprintf("%q", test.count))
		valueJSON(t, data["path"], fmt.Sprintf("%q", path))
	}
}

func TestAdapterRemovalReceipt(t *testing.T) {
	root := fileFixtureRoot(t)
	fileFixture(t, root, "a", "abc")
	fileFixture(t, root, "b", "de")
	source := sourceFile(t, fmt.Sprintf(`flow Remove() -> File.RemoveReceipt! { entries = File.list(File.Path.new(%q)); target = File.Remove.new(entries); receipt = commit target; return receipt }; return Remove()`, root))
	receipt := fileOutput(t, runProgram(t, source, "run", "--list-root", root, "--remove-root", root, "--format", "json"))
	if receipt.Type != "File.RemoveReceipt" {
		t.Fatalf("receipt type: %s", receipt.Type)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(receipt.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["total_count"], `"2"`)
	valueJSON(t, fields["completed_count"], `"2"`)
	valueJSON(t, fields["observed_bytes"], `"5"`)
}

func TestAdapterReceiptTypes(t *testing.T) {
	for _, text := range []string{
		`flow Bad(value: File.Write) -> File.Append { return value }`,
		`return File.Append.new(File.Path.new("unused"), "text")`,
		`return UTF8.Bytes.new(value: "text")`,
		`return File.WriteReceipt.new(File.Path.new("unused"), 0)`,
		`return File.AppendReceipt.new(File.Path.new("unused"), 0)`,
		`return File.RemoveReceipt.new(0, 0, 0)`,
		`return HTTP.ServerReceipt.new("unused", 0)`,
		`flow Bad(value: Effect) -> Int { return 0 }`,
		`return File.write(File.Path.new("unused"), Bytes.new("x"))`,
		`return File.append(File.Path.new("unused"), "x")`,
		`return File.remove(List.new())`,
	} {
		t.Run(text, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, text), "--format", "json")
			if got.status == 0 {
				t.Fatalf("invalid adapter contract accepted: %s", text)
			}
		})
	}
}

func TestAdapterReceiptProjection(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "projection")
	source := sourceFile(t, fmt.Sprintf(`target = File.Write.new(File.Path.new(%q), UTF8.Bytes.new("é")); result = commit target; copied = result; return Record.new(path: copied.path, count: copied.bytes_written)`, path))
	got := fileOutput(t, runProgram(t, source, "run", "--write-root", root, "--format", "json"))
	var fields map[string]runtimeValue
	if err := json.Unmarshal(got.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["path"], fmt.Sprintf("%q", path))
	valueJSON(t, fields["count"], `"2"`)
}

func TestAdapterEmptyRemovalReceipt(t *testing.T) {
	source := sourceFile(t, `flow Empty() -> File.RemoveReceipt! { target = File.Remove.new(List.new()); result = commit target; return result }; return Empty()`)
	receipt := fileOutput(t, runProgram(t, source, "run", "--format", "json"))
	if receipt.Type != "File.RemoveReceipt" {
		t.Fatalf("empty receipt type %s", receipt.Type)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(receipt.Value, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"total_count", "completed_count", "observed_bytes"} {
		valueJSON(t, fields[name], `"0"`)
	}
}

type receiptActivationDiagnostic struct {
	Operation string          `json:"operation"`
	Span      json.RawMessage `json:"span"`
	Receipt   runtimeValue    `json:"receipt"`
}
type receiptRuntimeDiagnostic struct {
	ProcessProgress *runtimeValue `json:"process_progress"`
	Code            string
	WriteProgress   *runtimeValue                `json:"write_progress"`
	LastCompleted   *receiptActivationDiagnostic `json:"last_completed_activation"`
	Error           *runtimeValue
}

func receiptHostDiagnostic(t *testing.T, got result) receiptRuntimeDiagnostic {
	t.Helper()
	var document struct{ Diagnostics []receiptRuntimeDiagnostic }
	if err := json.Unmarshal([]byte(got.stderr), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Diagnostics) != 1 {
		t.Fatalf("missing diagnostic: %+v", got)
	}
	return document.Diagnostics[0]
}
func receiptCompletedWrite(t *testing.T, got result) runtimeValue {
	t.Helper()
	diagnostic := receiptHostDiagnostic(t, got)
	if diagnostic.LastCompleted == nil || diagnostic.LastCompleted.Operation != catalog.FileWriteCapability || diagnostic.LastCompleted.Receipt.Type != "File.WriteReceipt" {
		t.Fatalf("missing completed write activation: %+v", got)
	}
	var span struct {
		File  string
		Start struct{ Offset int }
		End   struct{ Offset int }
	}
	if err := json.Unmarshal(diagnostic.LastCompleted.Span, &span); err != nil {
		t.Fatal(err)
	}
	if span.File == "" || span.Start.Offset != 0 || span.End.Offset == 0 {
		t.Fatalf("missing completed activation span: %s", diagnostic.LastCompleted.Span)
	}
	return diagnostic.LastCompleted.Receipt
}
