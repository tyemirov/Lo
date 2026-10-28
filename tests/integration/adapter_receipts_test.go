package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/runtimeops"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"os"
	"path/filepath"
	"testing"
)

func TestAdapterFileReceipts(t *testing.T) {
	root := fileFixtureRoot(t)
	path := filepath.Join(root, "receipt.txt")
	source := sourceFile(t, fmt.Sprintf(`path = File.Path.new(%q); write = File.Write.new(path, UTF8.Bytes.new(text: "hé")); written = commit write; append = File.Append.new(path, UTF8.Bytes.new("!")); appended = commit append; return Record.new(write: written, append: appended)`, path))
	result := fileOutput(t, run(t, "run", source, "--write-root", root, "--format", "json"))
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
	receipt := fileOutput(t, run(t, "run", source, "--list-root", root, "--remove-root", root, "--format", "json"))
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
	got := fileOutput(t, run(t, "run", source, "--write-root", root, "--format", "json"))
	var fields map[string]runtimeValue
	if err := json.Unmarshal(got.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["path"], fmt.Sprintf("%q", path))
	valueJSON(t, fields["count"], `"2"`)
}

type receiptFileIO struct {
	adapter.FileIO
	write func(*os.File, []byte) (int, error)
	close func(*os.File) error
}

func (f receiptFileIO) Write(_ context.Context, file *os.File, data []byte) (int, error) {
	return f.write(file, data)
}
func (f receiptFileIO) CloseFile(file *os.File) error { return f.close(file) }

func TestAdapterReceiptWriteFailureProgress(t *testing.T) {
	for _, operation := range []string{catalog.FileWrite, catalog.FileAppend} {
		for _, stage := range []string{"write", "finalize"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				root := fileFixtureRoot(t)
				failure := errors.New("injected file failure")
				expected := 3
				filesystem := receiptFileIO{FileIO: adapter.NativeFileIO()}
				filesystem.write = func(file *os.File, data []byte) (int, error) {
					if stage == "write" {
						n, err := file.Write(data[:2])
						if err != nil {
							return n, err
						}
						return n, failure
					}
					return file.Write(data)
				}
				filesystem.close = func(file *os.File) error {
					err := file.Close()
					if stage == "finalize" {
						return errors.Join(err, failure)
					}
					return err
				}
				files, err := adapter.OpenFiles(adapter.FileRoots{Write: []string{root}}, filesystem)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := files.Close(); err != nil {
						t.Error(err)
					}
				}()
				path, err := value.NewText(types.Path(), filepath.Join(root, "output"))
				if err != nil {
					t.Fatal(err)
				}
				op, _ := catalog.Lookup(operation)
				target, err := value.NewEffect(op, path, value.NewBytes([]byte("abc")))
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := files.Commit(context.Background(), target)
				if receipt != nil || !errors.Is(err, failure) {
					t.Fatalf("failed activation published result %v, error %v", receipt, err)
				}
				if stage == "write" {
					expected = 2
				}
				confirmed, actualStage, ok := execution.WriteProgress(err)
				if !ok || confirmed != expected || actualStage != stage {
					t.Fatalf("write evidence: confirmed=%d stage=%s present=%t", confirmed, actualStage, ok)
				}
				content, readErr := os.ReadFile(path.Text())
				if readErr != nil || len(content) != expected {
					t.Fatalf("confirmed file content %q, error %v", content, readErr)
				}
			})
		}
	}
}

func TestAdapterEmptyRemovalReceipt(t *testing.T) {
	source := sourceFile(t, `flow Empty() -> File.RemoveReceipt! { target = File.Remove.new(List.new()); result = commit target; return result }; return Empty()`)
	receipt := fileOutput(t, run(t, "run", source, "--format", "json"))
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

func receiptProgramResult(t *testing.T, ctx context.Context, filesystem adapter.FileIO, body func(execution.Session, value.Target, source.Span) (value.Value, error)) (result, string) {
	t.Helper()
	root := fileFixtureRoot(t)
	outputPath := filepath.Join(root, "output")
	files, err := adapter.OpenFiles(adapter.FileRoots{Write: []string{root}}, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	path, err := value.NewText(types.Path(), outputPath)
	if err != nil {
		t.Fatal(err)
	}
	op, _ := catalog.Lookup(catalog.FileWrite)
	target, err := value.NewEffect(op, path, value.NewBytes([]byte("abc")))
	if err != nil {
		t.Fatal(err)
	}
	text, err := source.New(filepath.Join(root, "program.lo"), "commit target")
	if err != nil {
		t.Fatal(err)
	}
	program, err := execution.NewProgram(func(session execution.Session) (value.Value, error) { return body(session, target, text.Span(0, 13)) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := program.Run(ctx, files, execution.DefaultBudget(), device.DefaultConfig())
	runErr = execution.PreserveCleanup(runErr, files.Close())
	if output != nil {
		t.Fatalf("failed execution published %v", output)
	}
	var stderr bytes.Buffer
	status := runner.ReportRuntime(&stderr, runner.JsonFormat, runErr)
	return result{stderr: stderr.String(), status: status}, outputPath
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

func TestAdapterReceiptCanceledWriteDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	filesystem := receiptFileIO{FileIO: adapter.NativeFileIO(), close: (*os.File).Close}
	filesystem.write = func(file *os.File, data []byte) (int, error) {
		count, err := file.Write(data[:2])
		if err != nil {
			return count, err
		}
		cancel()
		return count, ctx.Err()
	}
	got, path := receiptProgramResult(t, ctx, filesystem, runtimeops.Commit)
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.cancelled" || diagnostic.WriteProgress == nil || diagnostic.LastCompleted != nil {
		t.Fatalf("missing cancelled write evidence: %+v", got)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(diagnostic.WriteProgress.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["bytes_written"], `"2"`)
	valueJSON(t, fields["stage"], `"write"`)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "ab" {
		t.Fatalf("partial file %q, error %v", content, err)
	}
}

func TestAdapterReceiptPostCommitBudgetDiagnostic(t *testing.T) {
	got, path := receiptProgramResult(t, context.Background(), adapter.NativeFileIO(), func(session execution.Session, target value.Target, span source.Span) (value.Value, error) {
		charge := value.NewBytes(make([]byte, execution.MaxValueWeight-16))
		for index := 0; index < 4; index++ {
			if err := session.ChargeValue(charge); err != nil {
				return nil, err
			}
		}
		return runtimeops.Commit(session, target, span)
	})
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.limit" {
		t.Fatalf("missing completed activation evidence: %+v", got)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(receiptCompletedWrite(t, got).Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["bytes_written"], `"3"`)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "abc" {
		t.Fatalf("completed file %q, error %v", content, err)
	}
}

func TestAdapterReceiptPrimaryWriteFailure(t *testing.T) {
	filesystem := receiptFileIO{FileIO: adapter.NativeFileIO()}
	filesystem.write = func(file *os.File, data []byte) (int, error) {
		count, err := file.Write(data[:2])
		return count, errors.Join(err, errors.New("injected primary write failure"))
	}
	filesystem.close = func(file *os.File) error { return errors.Join(file.Close(), context.Canceled) }
	got, _ := receiptProgramResult(t, context.Background(), filesystem, runtimeops.Commit)
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 1 || diagnostic.Code != "runtime.unhandled-error" || diagnostic.Error == nil || diagnostic.Error.Kind != "File.WriteFailed" {
		t.Fatalf("cleanup changed primary write failure: %+v", got)
	}
	var input map[string]runtimeValue
	if err := json.Unmarshal(diagnostic.Error.Input.Value, &input); err != nil {
		t.Fatal(err)
	}
	var progress map[string]runtimeValue
	if err := json.Unmarshal(input["progress"].Value, &progress); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, progress["bytes_written"], `"2"`)
}

type invalidReceiptBoundary struct {
	execution.Boundary
	result value.Value
}

func (b invalidReceiptBoundary) Commit(context.Context, value.Target) (value.Value, error) {
	return b.result, nil
}

func TestAdapterReceiptInvalidBoundaryResult(t *testing.T) {
	for _, invalid := range []value.Value{nil, value.Int64(0)} {
		t.Run(fmt.Sprintf("%v", invalid), func(t *testing.T) {
			got, _ := receiptProgramResult(t, context.Background(), adapter.NativeFileIO(), func(session execution.Session, target value.Target, span source.Span) (value.Value, error) {
				nested, err := execution.NewProgram(func(inner execution.Session) (value.Value, error) { return runtimeops.Commit(inner, target, span) }, nil)
				if err != nil {
					return nil, err
				}
				return nested.Run(session.Context(), invalidReceiptBoundary{Boundary: session.Boundary(), result: invalid}, execution.DefaultBudget(), device.DefaultConfig())
			})
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 2 || diagnostic.Code != "runtime.host-failure" || diagnostic.LastCompleted != nil {
				t.Fatalf("invalid successful boundary result: %+v", got)
			}
		})
	}
}

func TestAdapterReceiptSessionCleanupDiagnostic(t *testing.T) {
	got, path := receiptProgramResult(t, context.Background(), adapter.NativeFileIO(), func(session execution.Session, target value.Target, span source.Span) (value.Value, error) {
		if err := session.OnClose(func() error { return errors.New("injected session cleanup failure") }); err != nil {
			return nil, err
		}
		return runtimeops.Commit(session, target, span)
	})
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.host-failure" {
		t.Fatalf("session cleanup class: %+v", got)
	}
	receiptCompletedWrite(t, got)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "abc" {
		t.Fatalf("completed file %q, error %v", content, err)
	}
}

func TestAdapterReceiptPriorSuccessAndCanceledWriteDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	filesystem := receiptFileIO{FileIO: adapter.NativeFileIO(), close: (*os.File).Close}
	filesystem.write = func(file *os.File, data []byte) (int, error) {
		calls++
		if calls == 1 {
			return file.Write(data)
		}
		count, err := file.Write(data[:2])
		if err != nil {
			return count, err
		}
		cancel()
		return count, ctx.Err()
	}
	got, path := receiptProgramResult(t, ctx, filesystem, func(session execution.Session, target value.Target, span source.Span) (value.Value, error) {
		text, err := source.New(span.File(), "commit first; commit second")
		if err != nil {
			return nil, err
		}
		if _, err := runtimeops.Commit(session, target, text.Span(0, 12)); err != nil {
			return nil, err
		}
		appendOp, _ := catalog.Lookup(catalog.FileAppend)
		next, err := value.NewEffect(appendOp, target.(value.ContentEffect).Path(), value.NewBytes([]byte("abc")))
		if err != nil {
			return nil, err
		}
		return runtimeops.Commit(session, next, text.Span(14, 27))
	})
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 2 || diagnostic.Code != "runtime.cancelled" || diagnostic.WriteProgress == nil {
		t.Fatalf("missing current failed write evidence: %+v", got)
	}
	receiptCompletedWrite(t, got)
	var fields map[string]runtimeValue
	if err := json.Unmarshal(diagnostic.WriteProgress.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["bytes_written"], `"2"`)
	valueJSON(t, fields["stage"], `"write"`)
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "abcab" {
		t.Fatalf("confirmed file %q, error %v", content, err)
	}
}
