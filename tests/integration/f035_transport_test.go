package integration_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
)

func f035CommandBindings(t *testing.T, fixture cleanupFixture, arguments ...string) string {
	t.Helper()
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = processLiteral(t, argument)
	}
	return fmt.Sprintf(`context = Process.Context.new(Map.new(), File.Path.new(%q)); executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(context, executable, List.new(%s), Bytes.new("")); `, fixture.root, fixture.executable, strings.Join(quoted, ","))
}

func TestF035DurationAdmission(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	for _, duration := range []int{30000, 60000, 180000, 1800000} {
		for _, admitted := range []bool{true, false} {
			t.Run(fmt.Sprintf("duration-%d-admitted-%t", duration, admitted), func(t *testing.T) {
				marker := fileFixture(t, fixture.writes, fmt.Sprintf("marker-%d-%t", duration, admitted), "preserved")
				maximum := duration
				if !admitted {
					maximum--
				}
				body := f035CommandBindings(t, fixture, "mark", marker) + fmt.Sprintf(`configured = Process.Command.with_timeout(command, %d); result = commit configured; return Process.Termination.exit_code(result.termination)`, duration)
				flags := append(fixture.flags(), "--max-process-ms", fmt.Sprint(maximum), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
				got := runProgram(t, sourceFile(t, processContextProgram("Int", body)), "run", append([]string{"--format", "json"}, flags...)...)
				data, err := os.ReadFile(marker)
				if err != nil {
					t.Fatal(err)
				}
				if admitted {
					valueJSON(t, fileOutput(t, got), `"0"`)
					if string(data) != "started" {
						t.Fatalf("admitted command did not run: %q", data)
					}
				} else {
					nativeFailure(t, got, 2, "runtime.capability-denied", "")
					if string(data) != "preserved" {
						t.Fatalf("excessive deadline launched command: %q", data)
					}
				}
			})
		}
	}
}

func TestF035TimeoutRecoveryPreservesOriginal(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	flows := `
flow Timeout(error: Error) -> Bool { return String.eq(error.kind, "Process.Timeout") }
flow Attempt(command: Process.Command) -> Bool onError Timeout {
    result = commit command
    return false
}
`
	body := f035CommandBindings(t, fixture, "sleep", "30") + `
timed = Process.Command.with_timeout(command, 1)
timed_out = Attempt(timed)
original = commit command
return Record.new(timed_out: timed_out, code: Process.Termination.exit_code(original.termination))`
	source := flows + processContextProgram("Record{timed_out: Bool, code: Int}", body)
	got := runProgram(t, sourceFile(t, source), "run", append([]string{"--format", "json", "--max-process-ms", "5000"}, fixture.flags()...)...)
	valueJSON(t, fileOutput(t, got), `{"timed_out":{"type":"Bool","value":true},"code":{"type":"Int","value":"0"}}`)
}

func TestF035CapturedOutput(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	body := f035CommandBindings(t, fixture, "output") + `configured = Process.Command.with_output(command, "capture"); result = commit configured; return Record.new(stdout: result.stdout, stderr: result.stderr, stdout_bytes: result.stdout_bytes, stderr_bytes: result.stderr_bytes)`
	source := processContextProgram("Record{stdout: Bytes, stderr: Bytes, stdout_bytes: Int, stderr_bytes: Int}", body)
	flags := append(fixture.flags(), "--max-process-output-bytes", "6")
	got := runProgram(t, sourceFile(t, source), "run", append([]string{"--format", "json"}, flags...)...)
	valueJSON(t, fileOutput(t, got), `{"stdout":{"type":"Bytes","value":"b3V0AP8K"},"stderr":{"type":"Bytes","value":"ZXJyDQo="},"stdout_bytes":{"type":"Int","value":"6"},"stderr_bytes":{"type":"Int","value":"5"}}`)
}

func f035ForwardProgram(t *testing.T, fixture cleanupFixture) string {
	t.Helper()
	body := f035CommandBindings(t, fixture, "output") + `
configured = Process.Command.with_output(command, "forward")
result = commit configured
stdout_empty = Math.eq(Bytes.length(result.stdout), 0)
stderr_empty = Math.eq(Bytes.length(result.stderr), 0)
stdout_count = Math.eq(result.stdout_bytes, 6)
stderr_count = Math.eq(result.stderr_bytes, 5)
valid = Match.new(stdout_empty, true: Match.new(stderr_empty, true: Match.new(stdout_count, true: stderr_count, _: false), _: false), _: false)
return Program.Exit.new(Match.new(valid, true: 0, _: 1), Bytes.new(""), Bytes.new(""))`
	return processContextProgram("Program.Exit", body)
}

func TestF035ForwardedOutputText(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	flags := append(fixture.flags(), "--process-forward-output", "--max-process-output-bytes", "6", "--max-console-bytes", "11")
	got := runProgram(t, sourceFile(t, f035ForwardProgram(t, fixture)), "run", flags...)
	if got.status != 0 || got.stdout != "out\x00\xff\n" || got.stderr != "err\r\n" {
		t.Fatalf("forwarded binary streams: %+v", got)
	}
}

func TestF035ForwardedOutputJSON(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	flags := append(fixture.flags(), "--process-forward-output", "--max-process-output-bytes", "6", "--max-console-bytes", "11")
	got := runProgram(t, sourceFile(t, f035ForwardProgram(t, fixture)), "run", append([]string{"--format", "json"}, flags...)...)
	if got.status != 0 {
		t.Fatalf("forwarded JSON streams: %+v", got)
	}
	for _, selected := range []struct{ stream, output, expected string }{
		{"stdout", got.stdout, "out\x00\xff\n"},
		{"stderr", got.stderr, "err\r\n"},
	} {
		var payload strings.Builder
		terminal := 0
		for _, event := range cliStackJSON(t, selected.output) {
			if event["event"] != "console" {
				if selected.stream != "stdout" || event["status"] != "ok" || event["exit_code"] != float64(0) {
					t.Fatalf("unexpected forwarded output envelope: %#v", event)
				}
				terminal++
				continue
			}
			encoded, ok := event["data"].(string)
			if !ok || event["stream"] != selected.stream || event["encoding"] != "base64" {
				t.Fatalf("invalid forwarded console event: %#v", event)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || event["bytes"] != float64(len(decoded)) {
				t.Fatalf("invalid forwarded payload: %v %#v", err, event)
			}
			payload.Write(decoded)
		}
		if payload.String() != selected.expected || (selected.stream == "stdout" && terminal != 1) {
			t.Fatalf("forwarded %s bytes or terminal envelope: %q, %d", selected.stream, payload.String(), terminal)
		}
	}
}

func TestF035ForwardingAdmissionPrecedesLaunch(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	marker := fileFixture(t, fixture.writes, "marker", "preserved")
	body := f035CommandBindings(t, fixture, "mark", marker) + `configured = Process.Command.with_output(command, "forward"); result = commit configured; return result`
	flags := append(fixture.flags(), "--process-write-root", cleanupScope(t, fixture.executable, "root", fixture.writes))
	got := runProgram(t, sourceFile(t, processContextProgram("Process.Result", body)), "run", append([]string{"--format", "json"}, flags...)...)
	nativeFailure(t, got, 2, "runtime.capability-denied", "")
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "preserved" {
		t.Fatalf("ungranted forwarding launched command: %q %v", data, err)
	}
}

func TestF035OutputLimitsRemainTerminal(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	for _, test := range []struct{ name, output, processMaximum, consoleMaximum string }{
		{"capture-limit", "capture", "5", "11"},
		{"forward-process-limit", "forward", "5", "11"},
		{"forward-console-limit", "forward", "6", "10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			marker := fileFixture(t, fixture.writes, test.name, "preserved")
			flows := `
flow Recovered(error: Error) -> Bool { return true }
flow Attempt(command: Process.Command) -> Bool onError Recovered {
    result = commit command
    return false
}

`
			body := f035CommandBindings(t, fixture, "output") + fmt.Sprintf(`configured = Process.Command.with_output(command, %q); completed = Attempt(configured); later = File.Write.new(File.Path.new(%q), Bytes.new("wrong")); commit later; return Program.Exit.new(0, Bytes.new(""), Bytes.new(""))`, test.output, marker)
			flags := append(fixture.flags(), "--process-forward-output", "--max-process-output-bytes", test.processMaximum, "--max-console-bytes", test.consoleMaximum, "--write-root", fixture.writes)
			got := runProgram(t, sourceFile(t, flows+processContextProgram("Program.Exit", body)), "run", flags...)
			if got.status != 2 || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("output limit failed to remain terminal: %+v", got)
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "preserved" {
				t.Fatalf("terminal output failure permitted later mutation: %q %v", data, err)
			}
		})
	}
}

func TestF035CaptureValueLimit(t *testing.T) {
	fixture := newCleanupFixtureSource(t, "process-command-options.c")
	marker := fileFixture(t, fixture.writes, "value-limit", "preserved")
	body := f035CommandBindings(t, fixture, "repeat", "8192") + fmt.Sprintf(`result=commit command;later=File.Write.new(File.Path.new(%q),Bytes.new("wrong"));commit later;return result.stdout_bytes`, marker)
	flags := append(fixture.flags(), "--format", "json", "--max-value-bytes", "4096", "--write-root", fixture.writes)
	got := runProgram(t, sourceFile(t, processContextProgram("Int", body)), "run", flags...)
	if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") || !strings.Contains(got.stderr, "process_progress") {
		t.Fatalf("capture value bound lost terminal progress: %+v", got)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "preserved" {
		t.Fatalf("capture value limit permitted later mutation: %q %v", data, err)
	}
}
