package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLLVMCLIStackLinuxExample(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repository, "docs", "examples", "cli-cleanup-preview.lo"))
	if err != nil {
		t.Fatal(err)
	}
	artifact := linuxLLVMArtifact(t, string(source))
	for _, test := range []struct {
		name           string
		arguments      []string
		status         int
		stdout, stderr string
	}{
		{"default", nil, 0, "default selected\n", ""},
		{"preview", []string{"--", "--dry-run"}, 0, "dry-run selected\n", ""},
		{"report", []string{"--", "--report"}, 0, "report selected\n", ""},
		{"invalid", []string{"--", "--private-unknown"}, 2, "", "declared option parsing failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runLinuxLLVMExecutable(t, artifact, append([]string{"run"}, test.arguments...)...)
			if got.status != test.status || got.stdout != test.stdout || got.stderr != test.stderr {
				t.Fatalf("Linux CLI example: %+v", got)
			}
		})
	}
	got := runLinuxLLVMExecutable(t, artifact, "run", "--", "--help", "--private-unknown")
	if got.status != 0 || got.stderr != "" || !strings.HasPrefix(got.stdout, "Usage: cleanup-preview [OPTIONS]\n") || strings.Contains(got.stdout, "selected") {
		t.Fatalf("Linux early help: %+v", got)
	}
}

func TestLLVMCLIStackLinuxTerminal(t *testing.T) {
	for _, status := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			source := cliStackProgram("Program.Exit", fmt.Sprintf(`line = Console.Line.new("stdout", "é", true); commit line; return Program.Exit.new(%d, Bytes.new("out"), Bytes.new("err"))`, status))
			artifact := linuxLLVMArtifact(t, source)
			got := runLinuxLLVMExecutable(t, artifact, "run", "--format", "json")
			if got.status != status {
				t.Fatalf("Linux requested status: %+v", got)
			}
			stdout, stderr := cliStackJSON(t, got.stdout), cliStackJSON(t, got.stderr)
			if len(stdout) != 3 || len(stderr) != 1 {
				t.Fatalf("Linux console event framing: %+v", got)
			}
			cliStackConsoleEvent(t, stdout[0], "stdout", "é\n", true)
			cliStackConsoleEvent(t, stdout[1], "stdout", "out", true)
			cliStackConsoleEvent(t, stderr[0], "stderr", "err", true)
			if stdout[2]["status"] != "ok" || stdout[2]["exit_code"] != float64(status) {
				t.Fatalf("Linux execution/status distinction: %#v", stdout[2])
			}
			limited := runLinuxLLVMExecutable(t, artifact, "run", "--max-console-bytes", "5")
			if limited.status != 2 || limited.stdout != "é\n" || !strings.Contains(limited.stderr, "runtime.limit") || strings.Contains(limited.stderr, "err") {
				t.Fatalf("Linux terminal reservation: %+v", limited)
			}
		})
	}
}

func TestLLVMCLIStackMetalArtifact(t *testing.T) {
	manifest, _ := llvmMetalManifest(t)
	artifact := filepath.Join(t.TempDir(), "cli-metal")
	source := filepath.Join(repository, "docs", "examples", "cli-cleanup-preview.lo")
	built := runExecutable(t, binary, "build", source, "--toolchain", manifest, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 {
		t.Fatalf("build Metal-linked CLI example: %+v", built)
	}
	for _, test := range []struct {
		flag, output string
		status       int
	}{
		{"--dry-run", "dry-run selected\n", 0}, {"--report", "report selected\n", 0}, {"--private-unknown", "", 2},
	} {
		got := runExecutable(t, artifact, "run", "--", test.flag)
		if got.status != test.status || got.stdout != test.output || (test.status == 0 && got.stderr != "") || (test.status == 2 && got.stderr != "declared option parsing failed") {
			t.Fatalf("Metal-linked generic CLI: %+v", got)
		}
	}
}
