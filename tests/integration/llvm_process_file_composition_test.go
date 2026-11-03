package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLLVMProcessBinaryFileComposition(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-file-command.c")
	for _, exitCode := range []int{0, 7} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			path := filepath.Join(fixture.writes, fmt.Sprintf("captured-%d", exitCode))
			text := fmt.Sprintf(`flow Empty()->List<Process.Result>{return List.new()};flow Save(total:Int,result:Process.Result)->Int!{target=File.Write.new(File.Path.new(%q),result.stdout);receipt=commit target;return Math.add(total,receipt.bytes_written)};flow Main()->Int!{context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(context,executable,List.new(%q),Bytes.new(""));result=commit target;code=Process.Termination.exit_code(result.termination);selected=Match.new(code,0:List.new(result),_:Empty());return Flow.fold(selected,0,Save)};return Main()`, path, fixture.root, fixture.executable, strconv.Itoa(exitCode))
			arguments := append(fixture.flags(), "--write-root", fixture.writes, "--format", "json")
			value := nativeValue(t, runProgram(t, sourceFile(t, text), "run", arguments...))
			data, err := os.ReadFile(path)
			if exitCode == 0 {
				valueJSON(t, value, `"3"`)
				if err != nil || !bytes.Equal(data, []byte{0, 255, 'a'}) {
					t.Fatalf("binary stdout did not reach File: %v (%v)", data, err)
				}
			} else {
				valueJSON(t, value, `"0"`)
				if !os.IsNotExist(err) {
					t.Fatalf("nonzero exit activated File: %v (%v)", data, err)
				}
			}
		})
	}
}

func TestLLVMProcessResultChargeCompletion(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-file-command.c")
	later := filepath.Join(fixture.writes, "later")
	text := fmt.Sprintf(`flow Main()->Int!{context=Process.Context.new(Map.new(),File.Path.new(%q));executable=Process.Executable.new(File.Path.new(%q));target=Process.Command.new(context,executable,List.new("large"),Bytes.new(""));result=commit target;write=File.Write.new(File.Path.new(%q),Bytes.new("wrong"));commit write;return 0};return Main()`, fixture.root, fixture.executable, later)
	arguments := append(fixture.flags(), "--write-root", fixture.writes, "--max-value-bytes", "4096", "--format", "json")
	got := runProgram(t, sourceFile(t, text), "run", arguments...)
	if got.status != 2 || got.stdout != "" {
		t.Fatalf("Process result quota: %+v", got)
	}
	var report struct {
		Diagnostics []struct {
			Code      string
			Completed *struct {
				Operation  string
				ResultType string       `json:"result_type"`
				Process    runtimeValue `json:"process"`
			} `json:"last_completed_activation"`
		}
	}
	if err := json.Unmarshal([]byte(got.stderr), &report); err != nil || len(report.Diagnostics) != 1 {
		t.Fatalf("Process quota diagnostic: %s (%v)", got.stderr, err)
	}
	diagnostic := report.Diagnostics[0]
	if diagnostic.Code != "runtime.limit" || diagnostic.Completed == nil || diagnostic.Completed.Operation != "Process.run" || diagnostic.Completed.ResultType != "Process.Result" {
		t.Fatalf("result charge lost completed activation: %s", got.stderr)
	}
	progress := processFields(t, diagnostic.Completed.Process)
	if len(progress) != 3 {
		t.Fatalf("unsafe Process completion fields: %s", got.stderr)
	}
	valueJSON(t, progress["stdout_bytes"], `"8192"`)
	valueJSON(t, progress["stderr_bytes"], `"0"`)
	termination := processFields(t, progress["termination"])
	valueJSON(t, termination["kind"], `"exited"`)
	valueJSON(t, termination["exit_code"], `"0"`)
	const secret = "process-charge-private-payload"
	if strings.Contains(got.stderr, secret) || strings.Contains(got.stderr, base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Fatal("Process completion exposed output payload")
	}
	if _, err := os.Stat(later); !os.IsNotExist(err) {
		t.Fatalf("File activated after result charge failure: %v", err)
	}
}
