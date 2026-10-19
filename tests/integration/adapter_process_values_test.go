package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"os"
	"strings"
	"testing"
)

const processArgumentSecret = "process-argument-secret-sentinel"
const processEnvironmentSecret = "process-environment-secret-sentinel"
const processStdinSecret = "process-stdin-secret-sentinel"

func processLiteral(t *testing.T, text string) string {
	t.Helper()
	literal, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	return string(literal)
}
func processCommandSource(t *testing.T, executable, arguments, environment, directory, stdin, finish string) string {
	t.Helper()
	return fmt.Sprintf(`flow Main() -> Process.Command! { executable = Process.Executable.new(path: File.Path.new(%s)); command = Process.Command.new(executable: executable, arguments: %s, environment: %s, directory: File.Path.new(%s), stdin: %s); %s }; return Main()`, processLiteral(t, executable), arguments, environment, processLiteral(t, directory), stdin, finish)
}
func processFields(t *testing.T, input runtimeValue) map[string]runtimeValue {
	t.Helper()
	var fields map[string]runtimeValue
	if err := json.Unmarshal(input.Value, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
func processNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{processArgumentSecret, processEnvironmentSecret, processStdinSecret} {
		if strings.Contains(text, secret) || strings.Contains(text, base64.StdEncoding.EncodeToString([]byte(secret))) {
			t.Fatalf("process payload in diagnostic: %s", text)
		}
	}
}
func processConstructorFailure(t *testing.T, source, kind string) {
	t.Helper()
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != kind {
		t.Fatalf("expected %s: %+v", kind, got)
	}
	processNoSecrets(t, got.stderr+got.stdout)
}

func TestAdapterProcessExecutableValue(t *testing.T) {
	root := fileFixtureRoot(t)
	lexical := root + "/never-created/../missing//tool"
	source := fmt.Sprintf(`return Process.Executable.new(path: File.Path.new(%s))`, processLiteral(t, lexical))
	executable := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	if executable.Type != "Process.Executable" {
		t.Fatalf("executable type %s", executable.Type)
	}
	fields := processFields(t, executable)
	if len(fields) != 1 || fields["path"].Type != "File.Path" {
		t.Fatalf("executable fields %+v", fields)
	}
	valueJSON(t, fields["path"], processLiteral(t, lexical))
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("constructor changed filesystem: entries=%v error=%v", entries, err)
	}
}

func TestAdapterProcessCommandValue(t *testing.T) {
	root := fileFixtureRoot(t)
	lexicalExecutable := root + "/not-created/../program//binary"
	lexicalDirectory := root + "/missing/../working//directory"
	stdin := fileFixture(t, root, "stdin", string([]byte{0, 255, 10, 128}))
	arguments := `List.new("one two", "$HOME", "$(printf literal); echo literal", "line\nbreak")`
	environment := `Map.new(Record.new(key: "EMPTY", value: ""), Record.new(key: "UNICODE", value: "é\nvalue"))`
	source := processCommandSource(t, lexicalExecutable, arguments, environment, lexicalDirectory, fmt.Sprintf(`File.read(File.Path.new(%s))`, processLiteral(t, stdin)), `return command`)
	t.Setenv("LO_PROCESS_AMBIENT_SECRET", "ambient-value-must-not-enter-command")
	command := fileOutput(t, run(t, "run", sourceFile(t, source), "--read-root", root, "--format", "json"))
	if command.Type != "Process.Command" {
		t.Fatalf("command type %s", command.Type)
	}
	fields := processFields(t, command)
	if len(fields) != 5 {
		t.Fatalf("command fields %+v", fields)
	}
	executable := processFields(t, fields["executable"])
	if fields["executable"].Type != "Process.Executable" {
		t.Fatalf("executable type %s", fields["executable"].Type)
	}
	valueJSON(t, executable["path"], processLiteral(t, lexicalExecutable))
	valueJSON(t, fields["directory"], processLiteral(t, lexicalDirectory))
	valueJSON(t, fields["arguments"], `[{"type":"String","value":"one two"},{"type":"String","value":"$HOME"},{"type":"String","value":"$(printf literal); echo literal"},{"type":"String","value":"line\nbreak"}]`)
	valueJSON(t, fields["environment"], `[{"key":{"type":"String","value":"EMPTY"},"value":{"type":"String","value":""}},{"key":{"type":"String","value":"UNICODE"},"value":{"type":"String","value":"é\nvalue"}}]`)
	valueJSON(t, fields["stdin"], `"AP8KgA=="`)
	if strings.Contains(string(fields["environment"].Value), "ambient-value-must-not-enter-command") {
		t.Fatal("command inherited an environment value")
	}
}

func TestAdapterProcessConstructorValidation(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, path := range []string{"", "relative", root + "/invalid\x00path"} {
		source := fmt.Sprintf(`return Process.Executable.new(File.Path.new(%s))`, processLiteral(t, path))
		processConstructorFailure(t, source, "Process.InvalidExecutable")
	}
	validArguments := `List.new("` + processArgumentSecret + `")`
	validEnvironment := `Map.new(Record.new(key: "TOKEN", value: "` + processEnvironmentSecret + `"))`
	validStdin := `UTF8.Bytes.new("` + processStdinSecret + `")`
	cases := []struct{ name, arguments, environment, directory string }{
		{"relative-directory", validArguments, validEnvironment, "relative"},
		{"nul-directory", validArguments, validEnvironment, root + "/bad\x00directory"},
		{"nul-argument", `List.new(` + processLiteral(t, processArgumentSecret+"\x00") + `)`, validEnvironment, root},
		{"empty-environment-name", validArguments, `Map.new(Record.new(key: "", value: "` + processEnvironmentSecret + `"))`, root},
		{"equals-environment-name", validArguments, `Map.new(Record.new(key: "BAD=NAME", value: "` + processEnvironmentSecret + `"))`, root},
		{"nul-environment-name", validArguments, `Map.new(Record.new(key: ` + processLiteral(t, "BAD\x00NAME") + `, value: "` + processEnvironmentSecret + `"))`, root},
		{"nul-environment-value", validArguments, `Map.new(Record.new(key: "TOKEN", value: ` + processLiteral(t, processEnvironmentSecret+"\x00") + `))`, root},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := processCommandSource(t, root+"/missing-executable", test.arguments, test.environment, test.directory, validStdin, `return command`)
			processConstructorFailure(t, source, "Process.InvalidCommand")
		})
	}
}

func TestAdapterProcessNominalTypes(t *testing.T) {
	root := fileFixtureRoot(t)
	for _, source := range []string{
		fmt.Sprintf(`return Process.Executable.new(%s)`, processLiteral(t, root)),
		fmt.Sprintf(`return Process.Command.new(File.Path.new(%s), List.new(), Map.new(), File.Path.new(%s), Bytes.new(""))`, processLiteral(t, root), processLiteral(t, root)),
		`flow Bad(command: Process.Command) -> Process.Executable { return command }`,
		`return Process.run()`,
		`return Process.Result.new()`,
		`flow Bad(result: Process.Result) -> Int { return result.exit_code }`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status == 0 {
			t.Fatalf("structural process target accepted: %s", source)
		}
	}
}

func TestAdapterProcessConstructorPurity(t *testing.T) {
	root := fileFixtureRoot(t)
	source := fmt.Sprintf(`flow Build(path: File.Path) -> Process.Command! { executable = Process.Executable.new(path); return Process.Command.new(executable, List.new(), Map.new(), path, Bytes.new("")) }; return List.map(List.new(File.Path.new(%s)), Build)`, processLiteral(t, root+"/nonexistent"))
	list := fileOutput(t, run(t, "run", sourceFile(t, source), "--format", "json"))
	if list.Type != "List<Process.Command>" {
		t.Fatalf("pure command list type %s", list.Type)
	}
	var commands []runtimeValue
	if err := json.Unmarshal(list.Value, &commands); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Type != "Process.Command" {
		t.Fatalf("pure command list %+v", commands)
	}
}

func TestAdapterProcessCommandDiagnosticSnapshot(t *testing.T) {
	root := fileFixtureRoot(t)
	arguments := `List.new("` + processArgumentSecret + `")`
	environment := `Map.new(Record.new(key: "TOKEN", value: "` + processEnvironmentSecret + `"))`
	stdin := `UTF8.Bytes.new("` + processStdinSecret + `")`
	source := fmt.Sprintf(`flow Main() -> Error! { executable = Process.Executable.new(File.Path.new(%s)); command = Process.Command.new(executable, %s, %s, File.Path.new(%s), %s); return Error.new("Probe", "safe", Record.new(command: command, nested: List.new(command))) }; return Main()`, processLiteral(t, root+"/missing"), arguments, environment, processLiteral(t, root), stdin)
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 0 {
		t.Fatalf("explicit command error: %+v", got)
	}
	processNoSecrets(t, got.stdout+got.stderr)
	errorValue := fileOutput(t, got)
	if errorValue.Kind != "Probe" || errorValue.Input == nil {
		t.Fatalf("explicit error: %+v", errorValue)
	}
	fields := processFields(t, *errorValue.Input)
	command := fields["command"]
	if command.Type != "Process.Command" {
		t.Fatalf("snapshot lost nominal command: %s", command.Type)
	}
	snapshot := processFields(t, command)
	valueJSON(t, snapshot["arguments"], `[]`)
	valueJSON(t, snapshot["environment"], `[]`)
	valueJSON(t, snapshot["stdin"], `""`)
}

func TestAdapterProcessFailureTraceRedaction(t *testing.T) {
	root := fileFixtureRoot(t)
	source := fmt.Sprintf(`flow Fail(command: Process.Command) -> Int! { return Math.div(1, 0) }; flow Main() -> Int! { executable = Process.Executable.new(File.Path.new(%s)); command = Process.Command.new(executable, List.new("%s"), Map.new(Record.new(key: "TOKEN", value: "%s")), File.Path.new(%s), UTF8.Bytes.new("%s")); return Fail(command) }; return Main()`, processLiteral(t, root+"/missing"), processArgumentSecret, processEnvironmentSecret, processLiteral(t, root), processStdinSecret)
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	diagnostic := receiptHostDiagnostic(t, got)
	if got.status != 1 || diagnostic.Error == nil || diagnostic.Error.Kind != "Math.DivideByZero" {
		t.Fatalf("expected ordinary flow failure: %+v", got)
	}
	processNoSecrets(t, got.stdout+got.stderr)
}

func TestAdapterProcessSnapshotWeight(t *testing.T) {
	root := fileFixtureRoot(t)
	path, err := value.NewText(types.Path(), root+"/missing")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := value.NewProcessExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	argumentsType, err := types.NewList(types.String())
	if err != nil {
		t.Fatal(err)
	}
	environmentType, err := types.NewMap(types.String(), types.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []int{0, 1, 4096} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			text, err := value.NewText(types.String(), strings.Repeat("payload", length))
			if err != nil {
				t.Fatal(err)
			}
			arguments, err := value.NewList(argumentsType, []value.Value{text})
			if err != nil {
				t.Fatal(err)
			}
			name, err := value.NewText(types.String(), "TOKEN")
			if err != nil {
				t.Fatal(err)
			}
			environment, err := value.NewMap(environmentType, []value.Entry{{Key: name, Value: text}})
			if err != nil {
				t.Fatal(err)
			}
			command, err := value.NewProcessCommand(executable, arguments, environment, path, value.NewBytes([]byte(text.Text())))
			if err != nil {
				t.Fatal(err)
			}
			listType, err := types.NewList(types.ProcessCommand())
			if err != nil {
				t.Fatal(err)
			}
			commands, err := value.NewList(listType, []value.Value{command})
			if err != nil {
				t.Fatal(err)
			}
			nested, err := value.NewRecord([]value.Field{{Name: "commands", Value: commands}})
			if err != nil {
				t.Fatal(err)
			}
			for _, original := range []value.Value{command, commands, nested} {
				snapshot := value.Snapshot(original)
				if actual, expected := snapshot.Weight(), value.SnapshotWeight(original); actual != expected {
					t.Fatalf("snapshot %s weight=%d, reservation=%d", original.Type(), actual, expected)
				}
				if !types.Equal(original.Type(), snapshot.Type()) {
					t.Fatalf("snapshot lost nominal type %s", original.Type())
				}
			}
		})
	}
}
