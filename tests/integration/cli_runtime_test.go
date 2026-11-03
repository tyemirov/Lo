package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func cliStackProgram(output, body string) string {
	return fmt.Sprintf("flow Main() -> %s! { %s }; return Main()", output, body)
}

func TestCLIStackConsoleText(t *testing.T) {
	source := cliStackProgram("Int", `first = Console.Line.new("stdout", "alpha", true); second = Console.Line.new("stderr", "γ", true); third = Console.Line.new("stdout", "", false); commit first; commit second; commit third; return 7`)
	got := runProgram(t, sourceFile(t, source), "run")
	if got.status != 0 || got.stdout != "alpha\n\n7\n" || got.stderr != "γ\n" {
		t.Fatalf("ordered console output: %+v", got)
	}
}

func TestCLIStackConsoleReceipt(t *testing.T) {
	source := cliStackProgram("Int", `line = Console.Line.new("stdout", "é", true); receipt = commit line; return receipt.bytes_written`)
	got := runProgram(t, sourceFile(t, source), "run")
	if got.status != 0 || got.stdout != "é\n3\n" || got.stderr != "" {
		t.Fatalf("logical UTF-8 receipt: %+v", got)
	}
}

func cliStackJSON(t *testing.T, output string) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var events []map[string]any
	for {
		var event map[string]any
		err := decoder.Decode(&event)
		if err == io.EOF {
			return events
		}
		if err != nil {
			t.Fatalf("console/result JSON framing: %v\n%s", err, output)
		}
		events = append(events, event)
	}
}

func cliStackConsoleEvent(t *testing.T, event map[string]any, stream, payload string, flushed bool) {
	t.Helper()
	if event["command"] != "run" || event["event"] != "console" || event["stream"] != stream || event["encoding"] != "base64" || event["flush"] != flushed || event["bytes"] != float64(len(payload)) {
		t.Fatalf("console event metadata: %#v", event)
	}
	encoded, ok := event["data"].(string)
	if !ok {
		t.Fatalf("missing console payload: %#v", event)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(data) != payload {
		t.Fatalf("console event bytes: %q, %v", data, err)
	}
}

func TestCLIStackConsoleJSON(t *testing.T) {
	source := cliStackProgram("Int", `line = Console.Line.new("stdout", "hello", true); error_line = Console.Line.new("stderr", "error", false); commit line; commit error_line; return 4`)
	got := runProgram(t, sourceFile(t, source), "run", "--format", "json")
	if got.status != 0 {
		t.Fatalf("console JSON execution: %+v", got)
	}
	stdout, stderr := cliStackJSON(t, got.stdout), cliStackJSON(t, got.stderr)
	if len(stdout) != 2 || len(stderr) != 1 {
		t.Fatalf("event counts stdout=%#v stderr=%#v", stdout, stderr)
	}
	cliStackConsoleEvent(t, stdout[0], "stdout", "hello\n", true)
	cliStackConsoleEvent(t, stderr[0], "stderr", "error\n", false)
	if stdout[1]["status"] != "ok" || stdout[1]["result"] == nil {
		t.Fatalf("final result envelope: %#v", stdout[1])
	}
}

func TestCLIStackConsoleInertAndRedacted(t *testing.T) {
	source := cliStackProgram("Record{line: Console.Line}", `line = Console.Line.new("stdout", "uncommitted-private-payload", true); return Record.new(line: line)`)
	got := runProgram(t, sourceFile(t, source), "run", "--format", "json")
	if got.status != 0 || got.stderr != "" || strings.Contains(got.stdout, "uncommitted-private-payload") || !strings.Contains(got.stdout, `"redacted":true`) {
		t.Fatalf("inert nominal snapshot: %+v", got)
	}
}

func TestCLIStackTerminalStatus(t *testing.T) {
	for _, status := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			source := cliStackProgram("Program.Exit", fmt.Sprintf(`return Program.Exit.new(%d, UTF8.Bytes.new("out\n"), UTF8.Bytes.new("err\n"))`, status))
			got := runProgram(t, sourceFile(t, source), "run")
			if got.status != status || got.stdout != "out\n" || got.stderr != "err\n" {
				t.Fatalf("terminal result: %+v", got)
			}
		})
	}
}

func TestCLIStackTerminalJSON(t *testing.T) {
	source := cliStackProgram("Program.Exit", `return Program.Exit.new(2, Bytes.new("out"), Bytes.new("err"))`)
	got := runProgram(t, sourceFile(t, source), "run", "--format", "json")
	if got.status != 2 {
		t.Fatalf("terminal status: %+v", got)
	}
	stdout, stderr := cliStackJSON(t, got.stdout), cliStackJSON(t, got.stderr)
	if len(stdout) != 2 || len(stderr) != 1 {
		t.Fatalf("terminal event counts: %+v", got)
	}
	cliStackConsoleEvent(t, stdout[0], "stdout", "out", true)
	cliStackConsoleEvent(t, stderr[0], "stderr", "err", true)
	if stdout[1]["exit_code"] != float64(2) || stdout[1]["status"] != "ok" {
		t.Fatalf("execution success and application status: %#v", stdout[1])
	}
}

func TestCLIStackNestedTerminalIsInert(t *testing.T) {
	source := cliStackProgram("Record{terminal: Program.Exit}", `terminal = Program.Exit.new(2, Bytes.new("private-out"), Bytes.new("private-err")); return Record.new(terminal: terminal)`)
	got := runProgram(t, sourceFile(t, source), "run", "--format", "json")
	if got.status != 0 || got.stderr != "" || strings.Contains(got.stdout, "private-out") || strings.Contains(got.stdout, "private-err") {
		t.Fatalf("nested terminal value selected effects: %+v", got)
	}
	events := cliStackJSON(t, got.stdout)
	if len(events) != 1 || events[0]["exit_code"] != nil {
		t.Fatalf("nested value selected terminal status: %#v", events)
	}
}

const cliStackFlags = `List.new(Record.new(name: "--dry-run", aliases: List.new("-n", "--preview"), group: "mode", help: "preview changes"), Record.new(name: "--report", aliases: List.new("-r"), group: "mode", help: "show storage"))`

func TestCLIStackDeclaredArguments(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"default", nil, "false\n"},
		{"canonical", []string{"--dry-run"}, "true\n"},
		{"alias", []string{"-n"}, "true\n"},
		{"abbreviation", []string{"--dry"}, "true\n"},
		{"repeat", []string{"--dry-run", "-n"}, "true\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`initial = Host.Invocation.current(); options = CLI.parse(initial.arguments, %s, "example", "Example description", true); return Map.get(options.flags, "--dry-run")`, cliStackFlags)
			args := []string{"run", sourceFile(t, cliStackProgram("Bool", body)), "--"}
			got := runProgram(t, args[1], "run", append(args[2:], test.args...)...)
			if got.status != 0 || got.stdout != test.want || got.stderr != "" {
				t.Fatalf("declared options: %+v", got)
			}
		})
	}
}

func TestCLIStackDeclaredHelp(t *testing.T) {
	body := fmt.Sprintf(`options = CLI.parse(List.new("--help", "--unknown-after-help"), %s, "example", "Example description", true); return options.help_text`, cliStackFlags)
	got := runProgram(t, sourceFile(t, cliStackProgram("String", body)), "run", "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("declared help: %+v", got)
	}
	events := cliStackJSON(t, got.stdout)
	if len(events) != 1 {
		t.Fatalf("help result: %+v", got)
	}
	want := "Usage: example [OPTIONS]\n\nExample description\n\nOptions:\n  -h, --help  Show this help message and exit.\n  --dry-run, -n, --preview  preview changes\n  --report, -r  show storage\n"
	result, ok := events[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing help result: %#v", events)
	}
	text, ok := result["value"].(map[string]any)
	if !ok || text["value"] != want {
		t.Fatalf("deterministic help: %#v", events)
	}
}

func TestCLIStackInvalidConstructorsRedactPayload(t *testing.T) {
	for _, test := range []struct{ name, output, body, kind string }{
		{"destination", "Console.Line", `return Console.Line.new("private-destination", "private-output", true)`, "Console.InvalidLine"},
		{"negative", "Program.Exit", `return Program.Exit.new(-1, Bytes.new("private-output"), Bytes.new("private-error"))`, "Program.InvalidExit"},
		{"large", "Program.Exit", `return Program.Exit.new(256, Bytes.new("private-output"), Bytes.new("private-error"))`, "Program.InvalidExit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runProgram(t, sourceFile(t, cliStackProgram(test.output, test.body)), "run", "--format", "json")
			if got.status != 1 || !strings.Contains(got.stderr, `"kind":"`+test.kind+`"`) || strings.Contains(got.stdout+got.stderr, "private-") {
				t.Fatalf("constructor failure metadata: %+v", got)
			}
		})
	}
}

func TestCLIStackQuotaPreservesPrefixAndReservesTerminal(t *testing.T) {
	for _, test := range []struct{ name, output, body, quota, prefix string }{
		{"prefix", "Int", `first = Console.Line.new("stdout", "a", false); second = Console.Line.new("stdout", "b", true); commit first; commit second; return 0`, "3", "a\n"},
		{"terminal", "Program.Exit", `return Program.Exit.new(1, Bytes.new("abc"), Bytes.new("def"))`, "5", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runProgram(t, sourceFile(t, cliStackProgram(test.output, test.body)), "run", "--max-console-bytes", test.quota)
			if got.status != 2 || got.stdout != test.prefix || !strings.Contains(got.stderr, "runtime.limit") || strings.Contains(got.stderr, "def") {
				t.Fatalf("output quota admission: %+v", got)
			}
		})
	}
}

func TestCLIStackServeRejectsTerminalBeforeEvaluation(t *testing.T) {
	source := cliStackProgram("Program.Exit", `line = Console.Line.new("stdout", "must-not-evaluate", true); commit line; return Program.Exit.new(0, Bytes.new(""), Bytes.new(""))`)
	got := runProgram(t, sourceFile(t, source), "serve", "--format", "json")
	if got.status != 2 || got.stdout != "" || strings.Contains(got.stderr, "must-not-evaluate") || !strings.Contains(got.stderr, "Program.Exit") {
		t.Fatalf("serve terminal preflight: %+v", got)
	}
}

func TestCLIStackConsoleInspection(t *testing.T) {
	source := cliStackProgram("Int", `line = Console.Line.new("stderr", "example", true); commit line; return 0`)
	got := run(t, "inspect", sourceFile(t, source), "--format", "json")
	if got.status != 0 || !strings.Contains(got.stdout, `"operation":"Console.Line.new"`) || !strings.Contains(got.stdout, "Console.write") || !strings.Contains(got.stdout, "Console.WriteFailed") || !strings.Contains(got.stdout, "stderr") {
		t.Fatalf("console effect inspection: %+v", got)
	}
}

func TestCLIStackExecutableExample(t *testing.T) {
	path := filepath.Join(repository, "docs", "examples", "cli-cleanup-preview.lo")
	for _, test := range []struct {
		name           string
		args           []string
		status         int
		stdout, stderr string
	}{
		{"default", nil, 0, "default selected\n", ""},
		{"dry-run", []string{"--dry-run"}, 0, "dry-run selected\n", ""},
		{"report", []string{"--report"}, 0, "report selected\n", ""},
		{"error", []string{"--private-unknown"}, 2, "", "declared option parsing failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runProgram(t, path, "run", append([]string{"--"}, test.args...)...)
			if got.status != test.status || got.stdout != test.stdout || got.stderr != test.stderr {
				t.Fatalf("example mode: %+v", got)
			}
		})
	}
	got := runProgram(t, path, "run", "--", "--help", "--private-unknown")
	if got.status != 0 || got.stderr != "" || !strings.HasPrefix(got.stdout, "Usage: cleanup-preview [OPTIONS]\n") || strings.Contains(got.stdout, "selected") {
		t.Fatalf("help before application actions: %+v", got)
	}
}

func TestCLIStackHelpPrecedesHostActions(t *testing.T) {
	source := `flow Work(count: Int, selected: Bool) -> Int! {
    paths = File.open_paths(File.Path.new("ungranted-fixture"))
    target = File.Write.new(File.Path.new("must-not-write"), Bytes.new("wrong"))
    commit target
    return Math.add(count, 1)
};
flow WorkItems(help: Bool) -> List<Bool> {
    return Match.new(help, true: List.new(), _: List.new(true))
};
flow Main() -> Program.Exit! {
    initial = Host.Invocation.current()
    options = CLI.parse(initial.arguments, List.new(), "example", "", true)
    work = WorkItems(options.help_requested)
    count = Flow.fold(work, 0, Work)
    output = Match.new(options.help_requested, true: options.help_text, _: "")
    return Program.Exit.new(0, UTF8.Bytes.new(output), Bytes.new(""))
}; return Main()`
	path := sourceFile(t, source)
	help := runProgram(t, path, "run", "--", "--help")
	if help.status != 0 || help.stderr != "" || !strings.HasPrefix(help.stdout, "Usage: example [OPTIONS]\n") {
		t.Fatalf("help reached a later host action: %+v", help)
	}
	work := runProgram(t, path, "run", "--format", "json")
	if work.status != 2 || work.stdout != "" || !strings.Contains(work.stderr, "runtime.capability-denied") {
		t.Fatalf("normal path did not reach the host authority gate: %+v", work)
	}
}

func TestCLIStackRecordSizeBoundaries(t *testing.T) {
	const maximum = 1048576
	for _, test := range []struct{ name, output, body, kind string }{
		{"line-over-limit", "Console.Line", fmt.Sprintf(`return Console.Line.new("stdout", %q, true)`, strings.Repeat("x", maximum)), "Console.InvalidLine"},
		{"terminal-over-limit", "Program.Exit", fmt.Sprintf(`return Program.Exit.new(0, UTF8.Bytes.new(%q), Bytes.new("x"))`, strings.Repeat("x", maximum)), "Program.InvalidExit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runProgram(t, sourceFile(t, cliStackProgram(test.output, test.body)), "run", "--format", "json")
			if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"`+test.kind+`"`) || strings.Contains(got.stderr, strings.Repeat("x", 64)) {
				t.Fatalf("record limit or snapshot redaction: status=%d stdout=%d stderr=%d", got.status, len(got.stdout), len(got.stderr))
			}
		})
	}
	t.Run("line-at-limit", func(t *testing.T) {
		source := cliStackProgram("Int", fmt.Sprintf(`line = Console.Line.new("stdout", %q, true); receipt = commit line; return receipt.bytes_written`, strings.Repeat("x", maximum-1)))
		got := runProgram(t, sourceFile(t, source), "run")
		if got.status != 0 || len(got.stdout) != maximum+len("1048576\n") || !strings.HasSuffix(got.stdout, "\n1048576\n") || got.stderr != "" {
			t.Fatalf("inclusive record size boundary: status=%d stdout=%d stderr=%d", got.status, len(got.stdout), len(got.stderr))
		}
	})
}

func TestCLIStackTerminalBinaryPayload(t *testing.T) {
	root := t.TempDir()
	payload := string([]byte{0, 255, '\n', 128})
	path := fileFixture(t, root, "payload", payload)
	source := cliStackProgram("Program.Exit", fmt.Sprintf(`bytes = File.read(File.Path.new(%q)); return Program.Exit.new(2, bytes, Bytes.new(""))`, path))
	program := sourceFile(t, source)
	text := runProgram(t, program, "run", "--read-root", root)
	if text.status != 2 || text.stdout != payload || text.stderr != "" {
		t.Fatalf("terminal binary bytes: status=%d stdout=%x stderr=%q", text.status, text.stdout, text.stderr)
	}
	encoded := runProgram(t, program, "run", "--read-root", root, "--format", "json")
	if encoded.status != 2 || encoded.stderr != "" {
		t.Fatalf("terminal binary JSON: %+v", encoded)
	}
	events := cliStackJSON(t, encoded.stdout)
	if len(events) != 2 {
		t.Fatalf("terminal binary event count: %#v", events)
	}
	cliStackConsoleEvent(t, events[0], "stdout", payload, true)
	if events[1]["exit_code"] != float64(2) {
		t.Fatalf("terminal binary status: %#v", events[1])
	}
}
