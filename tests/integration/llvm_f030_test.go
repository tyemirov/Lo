package integration_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func f030PortableSource(t *testing.T) string {
	t.Helper()
	var declarations, fields []string
	for index, fixture := range f030Cases() {
		source := strings.ReplaceAll(fixture.source, "Main", fmt.Sprintf("Main%d", index))
		source = strings.ReplaceAll(source, "Compare", fmt.Sprintf("Compare%d", index))
		last := strings.LastIndex(source, "return ")
		if last < 0 {
			t.Fatal("missing fixture return")
		}
		declarations = append(declarations, source[:last])
		fields = append(fields, fmt.Sprintf("v%d: %s", index, source[last+7:]))
	}
	for index, fixture := range f030FailureCases() {
		declarations = append(declarations, fmt.Sprintf(`flow FailureKind%d(error: Error) -> String { return error.kind }; flow Failure%d() -> String onError FailureKind%d { unused = %s; return "unexpected success" }`, index, index, index, fixture.expression))
		fields = append(fields, fmt.Sprintf("e%d: Failure%d()", index, index))
	}
	return strings.Join(declarations, "\n") + "\nreturn Record.new(" + strings.Join(fields, ",") + ")"
}

func checkF030Portable(t *testing.T, got result) {
	t.Helper()
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("portable value processors: %+v", got)
	}
	var envelope struct{ Result runtimeValue }
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	for index, fixture := range f030Cases() {
		value := recordField(t, envelope.Result, fmt.Sprintf("v%d", index))
		if strings.HasSuffix(value.Type, "!") {
			var inner runtimeValue
			if err := json.Unmarshal(value.Value, &inner); err != nil {
				t.Fatal(err)
			}
			value = inner
		}
		if value.Type != fixture.typ {
			t.Fatalf("%s type %s, want %s", fixture.name, value.Type, fixture.typ)
		}
		valueJSON(t, value, fixture.value)
	}
	for index, fixture := range f030FailureCases() {
		valueJSON(t, recordField(t, envelope.Result, fmt.Sprintf("e%d", index)), fmt.Sprintf("%q", fixture.kind))
	}
}

func TestLLVMF030Linux(t *testing.T) {
	artifact := linuxLLVMArtifact(t, f030PortableSource(t))
	checkF030Portable(t, runLinuxLLVMExecutable(t, artifact, "run", "--format", "json"))
}

func TestLLVMF030MetalArtifact(t *testing.T) {
	manifest, _ := llvmMetalManifest(t)
	artifact := filepath.Join(t.TempDir(), "value-processors-metal")
	built := runExecutable(t, binary, "build", sourceFile(t, f030PortableSource(t)), "--toolchain", manifest, "--device-backend", "metal", "--output", artifact, "--format", "json")
	if built.status != 0 || built.stderr != "" {
		t.Fatalf("build Metal value processors: %+v", built)
	}
	checkF030Portable(t, runExecutable(t, artifact, "run", "--format", "json"))
}

func TestF030ResourceAccounting(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		source func(int) string
	}{
		{"glob", func(size int) string {
			return fmt.Sprintf(`flow Main() -> Bool! { mask = Glob.new("*a*a*a*a*b"); return Glob.matches(mask, %q) }; return Main()`, strings.Repeat("a", size))
		}},
		{"replace", func(size int) string {
			return fmt.Sprintf(`flow Main() -> String! { return String.replace_all(%q, "a", "bb") }; return Main()`, strings.Repeat("a", size))
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			small := minimumLLVMWorkSteps(t, fixture.source(4))
			large := minimumLLVMWorkSteps(t, fixture.source(64))
			if large <= small {
				t.Fatalf("input work not charged: small=%d large=%d", small, large)
			}
		})
	}
	const control = `return String.length("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")`
	got := runProgram(t, sourceFile(t, control), "run", "--max-live-bytes", "8192")
	if got.status != 0 {
		t.Fatalf("quota control: %+v", got)
	}
	source := fmt.Sprintf(`flow Recover(error: Error) -> String { return "caught" }; flow Main() -> String onError Recover { return String.replace_all(%q, "a", %q) }; return Main()`, strings.Repeat("a", 256), strings.Repeat("b", 256))
	got = runProgram(t, sourceFile(t, source), "run", "--max-live-bytes", "8192")
	if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
		t.Fatalf("expanded result must exhaust the host quota: %+v", got)
	}
}
