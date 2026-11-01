package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func cliOptionFlag(name, aliases, group, help string) string {
	return fmt.Sprintf(`Record.new(name: %q, aliases: %s, group: %q, help: %q)`, name, aliases, group, help)
}

func cliOptionFailure(t *testing.T, arguments, flags, name string, abbreviations bool, kind, reason string) {
	t.Helper()
	body := fmt.Sprintf(`return CLI.parse(%s, %s, %q, "", %t)`, arguments, flags, name, abbreviations)
	got := run(t, "run", sourceFile(t, cliStackProgram("CLI.Options", body)), "--format", "json")
	if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"`+kind+`"`) || !strings.Contains(got.stderr, `"value":"`+reason+`"`) || strings.Contains(got.stderr, "private-parser") {
		t.Fatalf("sanitized option failure kind=%s reason=%s: %+v", kind, reason, got)
	}
}

func TestCLIOptionsArgumentFailures(t *testing.T) {
	flags := `List.new(` + cliOptionFlag("--report", `List.new("-r", "--review")`, "mode", "report") + `, ` + cliOptionFlag("--remove", `List.new("-x")`, "mode", "remove") + `)`
	for _, test := range []struct {
		name, arguments, reason string
		abbreviations           bool
	}{
		{"exclusive-group", `List.new("--report", "-x")`, "exclusive-group", true},
		{"unknown", `List.new("--private-parser-unknown")`, "unknown-option", true},
		{"ambiguous", `List.new("--re")`, "ambiguous-option", true},
		{"abbreviations-disabled", `List.new("--rep")`, "unknown-option", false},
		{"short-bundle", `List.new("-rx")`, "unsupported-token", true},
		{"assigned-value", `List.new("--report=true")`, "unsupported-token", true},
		{"operand", `List.new("private-parser-operand")`, "unsupported-token", true},
		{"separator", `List.new("--")`, "unsupported-token", true},
		{"control", `List.new("--private-parser\u0001")`, "invalid-argument", true},
		{"before-help", `List.new("--private-parser-unknown", "--help")`, "unknown-option", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cliOptionFailure(t, test.arguments, flags, "example", test.abbreviations, "CLI.InvalidArguments", test.reason)
		})
	}
}

func TestCLIOptionsDefinitionFailuresBeforeHelp(t *testing.T) {
	for _, test := range []struct{ name, flags, program, reason string }{
		{"canonical-collision", `List.new(` + cliOptionFlag("--one", `List.new()`, "", "") + `, ` + cliOptionFlag("--one", `List.new()`, "", "") + `)`, "example", "duplicate-spelling"},
		{"alias-collision", `List.new(` + cliOptionFlag("--one", `List.new("-x")`, "", "") + `, ` + cliOptionFlag("--two", `List.new("-x")`, "", "") + `)`, "example", "duplicate-spelling"},
		{"self-alias", `List.new(` + cliOptionFlag("--one", `List.new("--one")`, "", "") + `)`, "example", "duplicate-spelling"},
		{"reserved-short", `List.new(` + cliOptionFlag("--one", `List.new("-h")`, "", "") + `)`, "example", "reserved-spelling"},
		{"reserved-long", `List.new(` + cliOptionFlag("--help", `List.new()`, "", "") + `)`, "example", "reserved-spelling"},
		{"invalid-canonical", `List.new(` + cliOptionFlag("private-parser-name", `List.new()`, "", "") + `)`, "example", "invalid-flag-name"},
		{"invalid-alias", `List.new(` + cliOptionFlag("--one", `List.new("-long")`, "", "") + `)`, "example", "invalid-alias"},
		{"trailing-hyphen", `List.new(` + cliOptionFlag("--one-", `List.new()`, "", "") + `)`, "example", "invalid-flag-name"},
		{"control-group", `List.new(` + cliOptionFlag("--one", `List.new()`, "private-parser\n", "") + `)`, "example", "invalid-group"},
		{"empty-program", `List.new()`, "", "invalid-name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cliOptionFailure(t, `List.new("--help")`, test.flags, test.program, true, "CLI.InvalidDefinition", test.reason)
		})
	}
}

func TestCLIOptionsCanonicalResolution(t *testing.T) {
	for _, test := range []struct{ name, flags, arguments, expected string }{
		{
			"exact-before-ambiguous-prefix",
			`List.new(` + cliOptionFlag("--re", `List.new()`, "", "") + `, ` + cliOptionFlag("--report", `List.new()`, "", "") + `)`,
			`List.new("--re")`,
			`[{"key":{"type":"String","value":"--re"},"value":{"type":"Bool","value":true}},{"key":{"type":"String","value":"--report"},"value":{"type":"Bool","value":false}}]`,
		},
		{
			"aliases-deduplicate-prefix-owner",
			`List.new(` + cliOptionFlag("--report", `List.new("--review")`, "", "") + `)`,
			`List.new("--re")`,
			`[{"key":{"type":"String","value":"--report"},"value":{"type":"Bool","value":true}}]`,
		},
		{
			"empty-group-is-independent",
			`List.new(` + cliOptionFlag("--one", `List.new("-1")`, "", "") + `, ` + cliOptionFlag("--two", `List.new("-2")`, "", "") + `)`,
			`List.new("-2", "-1")`,
			`[{"key":{"type":"String","value":"--one"},"value":{"type":"Bool","value":true}},{"key":{"type":"String","value":"--two"},"value":{"type":"Bool","value":true}}]`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`options = CLI.parse(%s, %s, "example", "", true); return options.flags`, test.arguments, test.flags)
			got := run(t, "run", sourceFile(t, cliStackProgram("Map<String, Bool>", body)), "--format", "json")
			valueJSON(t, fileOutput(t, got), test.expected)
		})
	}
}

func TestCLIOptionsFailureEvidence(t *testing.T) {
	flags := `List.new(` + cliOptionFlag("--report", `List.new("--review")`, "mode", "") + `, ` + cliOptionFlag("--remove", `List.new("--removal")`, "mode", "") + `)`
	for _, test := range []struct{ name, arguments, reason, index string }{
		{"ambiguous", `List.new("--re")`, "ambiguous-option", `"0"`},
		{"group-order", `List.new("--remove", "--review")`, "exclusive-group", `"1"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := fmt.Sprintf(`return CLI.parse(%s, %s, "example", "", true)`, test.arguments, flags)
			got := run(t, "run", sourceFile(t, cliStackProgram("CLI.Options", body)), "--format", "json")
			diagnostic := receiptHostDiagnostic(t, got)
			if got.status != 1 || got.stdout != "" || diagnostic.Error == nil || diagnostic.Error.Kind != "CLI.InvalidArguments" || diagnostic.Error.Message != "declared option parsing failed" || diagnostic.Error.Input == nil {
				t.Fatalf("typed parser evidence: %+v", got)
			}
			fields := processFields(t, *diagnostic.Error.Input)
			if len(fields) != 3 {
				t.Fatalf("unexpected parser failure inputs: %#v", fields)
			}
			valueJSON(t, fields["reason"], fmt.Sprintf("%q", test.reason))
			valueJSON(t, fields["index"], test.index)
			valueJSON(t, fields["candidates"], `[{"type":"String","value":"--report"},{"type":"String","value":"--remove"}]`)
		})
	}
}

func TestCLIOptionsHelpDoesNotRetainSkippedArguments(t *testing.T) {
	flags := `List.new(` + cliOptionFlag("--review", `List.new()`, "", "Review") + `)`
	body := fmt.Sprintf(`return CLI.parse(List.new("--review", "--he", "--private-parser-skipped"), %s, "example", "", true)`, flags)
	got := run(t, "run", sourceFile(t, cliStackProgram("CLI.Options", body)), "--format", "json")
	value := fileOutput(t, got)
	if strings.Contains(got.stdout, "private-parser") {
		t.Fatalf("options retained skipped arguments: %s", got.stdout)
	}
	var fields map[string]runtimeValue
	if err := json.Unmarshal(value.Value, &fields); err != nil {
		t.Fatal(err)
	}
	valueJSON(t, fields["help_requested"], "true")
	valueJSON(t, fields["flags"], `[{"key":{"type":"String","value":"--review"},"value":{"type":"Bool","value":true}}]`)
	valueJSON(t, fields["help_text"], `"Usage: example [OPTIONS]\n\nOptions:\n  -h, --help  Show this help message and exit.\n  --review  Review\n"`)
}

func TestCLIOptionsPureCallback(t *testing.T) {
	source := `flow Parse(arguments: List<String>) -> CLI.Options! { return CLI.parse(arguments, List.new(), "example", "", true) }; flow Main() -> List<CLI.Options>! { return List.map(List.new(List.new("--help")), Parse) }; return Main()`
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"help_requested":{"type":"Bool","value":true}`) {
		t.Fatalf("pure parser callback: %+v", got)
	}
}

func TestCLIOptionsWorkLimit(t *testing.T) {
	body := fmt.Sprintf(`options = CLI.parse(List.new("--help"), List.new(), "example", %q, true); return options.help_requested`, strings.Repeat("x", 4096))
	path := sourceFile(t, cliStackProgram("Bool", body))
	limited := run(t, "run", path, "--max-steps", "200", "--format", "json")
	if limited.status != 2 || !strings.Contains(limited.stderr, "runtime.limit") || limited.stdout != "" {
		t.Fatalf("parser scan escaped work limit: %+v", limited)
	}
	complete := run(t, "run", path, "--max-steps", "20000")
	if complete.status != 0 || complete.stdout != "true\n" || complete.stderr != "" {
		t.Fatalf("admitted parser scan: %+v", complete)
	}
}
