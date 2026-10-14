package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCommitOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")
	program := fmt.Sprintf(`
p = File.Path.new(%q)
unused = File.write(p, Bytes.new("unused"))
first = File.write(p, Bytes.new("A"))
again = File.append(p, "é")
commit first
commit again
commit again
return File.read(p)
`, path)
	v := runValue(t, program, "--read-root", dir, "--write-root", dir)
	valueJSON(t, v, `{"type":"Bytes","value":"QcOpw6k="}`)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "Aéé" {
		t.Fatalf("commit order: %q", contents)
	}
	program = fmt.Sprintf(`target = File.append(File.Path.new(%q), "new"); commit target`, filepath.Join(dir, "created.txt"))
	got := run(t, "run", sourceFile(t, program), "--write-root", dir, "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("append creates target: %+v", got)
	}
	contents, err = os.ReadFile(filepath.Join(dir, "created.txt"))
	if err != nil || string(contents) != "new" {
		t.Fatalf("created target: %q, %v", contents, err)
	}
}

func TestFileCommitFailure(t *testing.T) {
	dir := t.TempDir()
	later := filepath.Join(dir, "later")
	for _, op := range []string{"File.write", "File.append"} {
		t.Run(op, func(t *testing.T) {
			body := `Bytes.new("content")`
			kind := "File.WriteFailed"
			if op == "File.append" {
				body = `"content"`
				kind = "File.AppendFailed"
			}
			program := fmt.Sprintf(`bad = %s(File.Path.new(%q), %s); later = File.write(File.Path.new(%q), Bytes.new("later")); commit bad; commit later`, op, dir, body, later)
			got := run(t, "run", sourceFile(t, program), "--write-root", dir, "--format", "json")
			if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, `"kind":"`+kind+`"`) {
				t.Fatalf("commit failure: %+v", got)
			}
			if _, err := os.ReadFile(later); !os.IsNotExist(err) {
				t.Fatalf("later commit executed: %v", err)
			}
			program = fmt.Sprintf(`flow Handle(e: Error) -> Error { return e }; flow Write() -> Error onError Handle { e = %s(File.Path.new(%q), %s); commit e; return Error.new("missed", "", Record.new()) }; return Write()`, op, dir, body)
			v := runValue(t, program, "--write-root", dir)
			if v.Kind != kind || len(v.Trace) != 1 {
				t.Fatalf("self-handled activation: %+v", v)
			}
		})
	}
}

func TestFileCommitAuthorization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output")
	program := fmt.Sprintf(`target = File.write(File.Path.new(%q), Bytes.new("data")); commit target`, path)
	got := run(t, "run", sourceFile(t, program), "--read-root", dir, "--format", "json")
	if got.status != 2 || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.capability-denied" {
		t.Fatalf("write authorization: %+v", got)
	}
	if _, err := os.ReadFile(path); !os.IsNotExist(err) {
		t.Fatal("unauthorized target was created")
	}
	unused := fmt.Sprintf(`target = File.write(File.Path.new(%q), Bytes.new("data")); return 1`, path)
	v := runValue(t, unused, "--write-root", dir)
	valueJSON(t, v, `"1"`)
	if _, err := os.ReadFile(path); !os.IsNotExist(err) {
		t.Fatal("uncommitted target was created")
	}
	outside := t.TempDir()
	file, err := os.Create(filepath.Join(outside, "existing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("original"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(file.Name(), link); err != nil {
		t.Fatal(err)
	}
	program = fmt.Sprintf(`target = File.write(File.Path.new(%q), Bytes.new("changed")); commit target`, link)
	got = run(t, "run", sourceFile(t, program), "--write-root", dir, "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"File.WriteFailed"`) {
		t.Fatalf("write root escape: %+v", got)
	}
	contents, err := os.ReadFile(file.Name())
	if err != nil || string(contents) != "original" {
		t.Fatalf("outside target changed: %q, %v", contents, err)
	}
}

func TestFileWorkflowExample(t *testing.T) {
	for _, tc := range []struct{ name, input, output, content, result string }{
		{"nonempty", "café", "summary.log", "café", "4"},
		{"empty", "", "empty.txt", "empty input", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input, err := os.Create(filepath.Join(dir, "input.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := input.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			if err := input.Close(); err != nil {
				t.Fatal(err)
			}
			cmd := runtimeCommand(t, "run", filepath.Join(repository, "examples/agent_file_workflow.lo"), "--read-root", ".", "--write-root", ".", "--format", "json")
			cmd.Dir = dir
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil || stderr.Len() != 0 {
				t.Fatalf("file example: %v, %s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), `"type":"Int","value":"`+tc.result+`"`) {
				t.Fatalf("file example result: %s", stdout.String())
			}
			data, err := os.ReadFile(filepath.Join(dir, tc.output))
			if err != nil || string(data) != tc.content {
				t.Fatalf("file example effect: %q, %v", data, err)
			}
		})
	}
}
