package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binary string
var repository string

func TestMain(m *testing.M) {
	var err error
	repository, err = filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "lo-integration-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "lo")
	command := exec.Command("go", "build", "-o", binary, "./cmd/lo")
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build public CLI: %v\n%s", err, output)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	status := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}

type result struct {
	stdout string
	stderr string
	status int
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = repository
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatal(err)
		}
	}
	return result{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
}

func sourceFile(t *testing.T, source string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "*.lo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(source); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

type diagnosticOutput struct {
	Diagnostics []struct {
		Code     string `json:"code"`
		Stage    string `json:"stage"`
		Message  string `json:"message"`
		Expected string `json:"expected,omitempty"`
		Actual   string `json:"actual,omitempty"`
		Span     *struct {
			File  string `json:"file"`
			Start struct {
				Offset int `json:"offset"`
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"start"`
			End struct {
				Offset int `json:"offset"`
			} `json:"end"`
		} `json:"span,omitempty"`
	} `json:"diagnostics"`
}

func diagnostics(t *testing.T, text string) diagnosticOutput {
	t.Helper()
	var output diagnosticOutput
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("diagnostic JSON: %v\n%s", err, text)
	}
	if len(output.Diagnostics) == 0 {
		t.Fatal("missing diagnostics")
	}
	return output
}

func TestFirstVerifierSlice(t *testing.T) {
	ok := run(t, "check", "examples/list_sum.lo", "--format", "json")
	if ok.status != 0 || ok.stderr != "" {
		t.Fatalf("check list sum: %+v", ok)
	}
	var checked struct {
		Command string `json:"command"`
		Status  string `json:"status"`
		Type    string `json:"result_type"`
	}
	if err := json.Unmarshal([]byte(ok.stdout), &checked); err != nil {
		t.Fatal(err)
	}
	if checked.Command != "check" || checked.Status != "ok" || checked.Type != "Int" {
		t.Fatalf("unexpected check result: %+v", checked)
	}
	path := sourceFile(t, "numbers = List.new(String.new(\"one\"))\nreturn numbers |> Math.sum()\n")
	bad := run(t, "check", "--format", "json", path)
	if bad.status != 1 || bad.stdout != "" {
		t.Fatalf("check wrong list: %+v", bad)
	}
	d := diagnostics(t, bad.stderr).Diagnostics[0]
	if d.Code != "check.type-mismatch" || d.Expected != "List<Int>" || d.Actual != "List<String>" {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}
	if d.Span == nil || d.Span.File != path || d.Span.Start.Line != 2 || d.Span.Start.Column != 8 {
		t.Fatalf("wrong argument span: %+v", d.Span)
	}
}

func TestConformance(t *testing.T) {
	var fixtures []struct {
		ID         string `json:"id"`
		File       string `json:"file"`
		Command    string `json:"command"`
		Exit       int    `json:"exit"`
		Diagnostic string `json:"diagnostic"`
		Stage      int    `json:"stage"`
	}
	data, err := os.ReadFile(filepath.Join(repository, "testdata/conformance/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			path := filepath.Join(repository, "testdata/conformance", fixture.File)
			got := run(t, fixture.Command, path, "--format", "json")
			if got.status != fixture.Exit {
				t.Fatalf("exit %d, want %d: %+v", got.status, fixture.Exit, got)
			}
			if fixture.Diagnostic != "" {
				d := diagnostics(t, got.stderr).Diagnostics[0]
				if d.Code != fixture.Diagnostic || d.Span == nil || d.Span.File != path {
					t.Fatalf("diagnostic = %+v, want %s with source span", d, fixture.Diagnostic)
				}
				if got.stdout != "" {
					t.Fatalf("failed command produced a result: %s", got.stdout)
				}
			} else if !json.Valid([]byte(got.stdout)) || got.stderr != "" {
				t.Fatalf("invalid result: %+v", got)
			}
		})
	}
}

func TestSyntaxStructure(t *testing.T) {
	path := sourceFile(t, "return (1 |> Math.add(2)) |> Int.to_string()\n")
	got := run(t, "parse", path, "--format", "json")
	if got.status != 0 {
		t.Fatalf("parse pipe: %+v", got)
	}
	type expression struct {
		Kind string          `json:"kind"`
		Left json.RawMessage `json:"left"`
		Name string          `json:"name"`
	}
	var tree struct {
		Program struct {
			Statements []struct {
				Kind  string     `json:"kind"`
				Value expression `json:"value"`
			} `json:"statements"`
		} `json:"program"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree.Program.Statements) != 1 || tree.Program.Statements[0].Kind != "return" {
		t.Fatalf("unexpected tree: %s", got.stdout)
	}
	outer := tree.Program.Statements[0].Value
	var inner expression
	if err := json.Unmarshal(outer.Left, &inner); err != nil {
		t.Fatal(err)
	}
	if outer.Kind != "pipe" || outer.Name != "Int.to_string" || inner.Kind != "pipe" || inner.Name != "Math.add" {
		t.Fatalf("pipe structure: %s", got.stdout)
	}
}

func TestCLIErrorBoundary(t *testing.T) {
	for _, args := range [][]string{
		{}, {"unknown"}, {"parse"}, {"parse", "missing.lo", "extra.lo"},
		{"parse", "--format", "yaml", "missing.lo"}, {"parse", "missing.lo", "--format", "json"},
	} {
		got := run(t, args...)
		if got.status != 2 || got.stdout != "" || got.stderr == "" {
			t.Fatalf("args %v: %+v", args, got)
		}
	}
	got := run(t, "parse", "missing.lo", "--format", "json")
	d := diagnostics(t, got.stderr).Diagnostics[0]
	if d.Code != "source.read-failed" || !strings.Contains(d.Message, "missing.lo") {
		t.Fatalf("source access context: %+v", d)
	}
}

func TestParserExternalInputs(t *testing.T) {
	for _, source := range []string{"return \"\xff\"", "return \"\x00\"", "return " + strings.Repeat("(", 600) + "1" + strings.Repeat(")", 600)} {
		got := run(t, "parse", sourceFile(t, source), "--format", "json")
		if got.status != 1 || got.stdout != "" {
			t.Fatalf("invalid external source accepted: %+v", got)
		}
		diagnostics(t, got.stderr)
	}
	got := run(t, "parse", sourceFile(t, "a = \"é\"; return @\n"), "--format", "json")
	d := diagnostics(t, got.stderr).Diagnostics[0]
	if d.Span == nil || d.Span.Start.Offset != 17 || d.Span.Start.Column != 17 {
		t.Fatalf("UTF-8 source location: %+v", d.Span)
	}
	got = run(t, "parse", sourceFile(t, "x = 1\r\nreturn x\r\n"))
	if got.status != 0 {
		t.Fatalf("CRLF source: %+v", got)
	}
	got = run(t, "parse", sourceFile(t, strings.Repeat(" ", (16<<20)+1)), "--format", "json")
	if got.status != 1 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "source.limit" {
		t.Fatalf("source size limit: %+v", got)
	}
}
