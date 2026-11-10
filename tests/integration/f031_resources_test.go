package integration_test

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestF031IntegerMagnitudeLimit(t *testing.T) {
	number := new(big.Int).Lsh(big.NewInt(1), 1048576).String()
	for _, fixture := range []struct {
		name, expression string
		status           int
	}{
		{"exact-token-without-conversion", `JSON.kind(` + f031Decode(number) + `)`, 0},
		{"json-integer-conversion", `JSON.int(` + f031Decode(number) + `)`, 2},
		{"decimal-integer-conversion", fmt.Sprintf(`Int.parse(%q)`, number), 2},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			typ := "Int"
			if fixture.status == 0 {
				typ = "String"
			}
			source := fmt.Sprintf("flow Main()->%s!{return %s};return Main()", typ, fixture.expression)
			got := runProgram(t, sourceFile(t, source), "run", "--max-steps", "10000000")
			if got.status != fixture.status {
				t.Fatalf("integer magnitude boundary: status=%d stderr=%s", got.status, got.stderr)
			}
			if fixture.status == 0 {
				if got.stdout != "number\n" || got.stderr != "" {
					t.Fatalf("exact JSON token was converted implicitly: %+v", got)
				}
			} else if got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("integer magnitude overflow was not terminal: %+v", got)
			}
		})
	}
}

func TestF031JSONContainerLimits(t *testing.T) {
	for _, fixture := range []struct{ name, document, flag, denied, admitted string }{
		{"array-depth", "[[[0]]]", "--max-json-depth", "2", "3"},
		{"object-depth", `{"a":{"b":{}}}`, "--max-json-depth", "2", "3"},
		{"array-items", "[1,2,3]", "--max-list-items", "2", "3"},
		{"object-members", `{"a":1,"b":2,"c":3}`, "--max-list-items", "2", "3"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := `flow Main()->String!{return JSON.kind(` + f031Decode(fixture.document) + `)};return Main()`
			artifact, built := buildForTest(t, sourceFile(t, source))
			if built.status != 0 || built.stderr != "" {
				t.Fatalf("build container limit: %+v", built)
			}
			got := runExecutable(t, artifact, "run", fixture.flag, fixture.denied)
			if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("container limit was not terminal: %+v", got)
			}
			got = runExecutable(t, artifact, "run", fixture.flag, fixture.admitted)
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("exact container limit rejected: %+v", got)
			}
		})
	}
}

func TestF031TerminalResourceFailuresPreventEffects(t *testing.T) {
	for _, fixture := range []struct{ name, expression, flag, limit string }{
		{"template-width", `String.format("{x:>1000000}",Record.new(x:"x"))`, "--max-value-bytes", "4096"},
		{"json-depth", `JSON.kind(` + f031Decode(strings.Repeat("[", 257)+"0"+strings.Repeat("]", 257)) + `)`, "--max-json-depth", "256"},
		{"json-items", `JSON.kind(` + f031Decode("[1,2,3]") + `)`, "--max-list-items", "2"},
		{"json-expanded-value", `JSON.kind(` + f031Decode("["+strings.Repeat("0,", 254)+"0]") + `)`, "--max-value-bytes", "2048"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := fileFixtureRoot(t)
			path := filepath.Join(root, "must-not-exist")
			source := fmt.Sprintf(`flow Caught(error:Error)->Int{return 0};flow Main()->Int onError Caught{length=String.length(%s);write=File.Write.new(File.Path.new(%q),Bytes.new("unexpected"));commit write;return length};return Main()`, fixture.expression, path)
			got := runProgram(t, sourceFile(t, source), "run", "--write-root", root, fixture.flag, fixture.limit)
			if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("resource failure was caught or lost: %+v", got)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("resource failure allowed later effect: %v", err)
			}
		})
	}
}

func TestF031TemporaryParserStorage(t *testing.T) {
	document := "[" + strings.Repeat("0,", 127) + "0]"
	control := fmt.Sprintf(`return Bytes.length(Bytes.new(%q))`, document)
	subject := `flow Main()->Int!{return JSON.length(` + f031Decode(document) + `)};return Main()`
	if f030MinimumLive(t, subject) <= f030MinimumLive(t, control) {
		t.Fatal("JSON parser tree and temporary storage are not charged")
	}
}

func TestF031ParsingWorkGrowth(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		source func(int) string
	}{
		{"json-whitespace", func(n int) string {
			return `flow Main()->String!{return JSON.kind(` + f031Decode(strings.Repeat(" ", n)+"null") + `)};return Main()`
		}},
		{"integer-leading-zeros", func(n int) string { return fmt.Sprintf(`return Int.parse(%q)`, strings.Repeat("0", n)+"1") }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			short := minimumLLVMWorkSteps(t, fixture.source(64))
			long := minimumLLVMWorkSteps(t, fixture.source(1024))
			if long <= short {
				t.Fatalf("input work is uncharged: short=%d long=%d", short, long)
			}
		})
	}
}
