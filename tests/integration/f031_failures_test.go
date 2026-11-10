package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type f031FailureCase struct{ name, expression, kind string }

func f031FailureCases() []f031FailureCase {
	cases := []f031FailureCase{
		{"get-kind", `JSON.get(` + f031Decode(`[]`) + `,"x")`, "JSON.TypeMismatch"},
		{"get-missing", `JSON.get(` + f031Decode(`{"X":1}`) + `,"x")`, "JSON.FieldAbsent"},
		{"at-kind", `JSON.at(` + f031Decode(`{}`) + `,0)`, "JSON.TypeMismatch"},
		{"at-negative", `JSON.at(` + f031Decode(`[1]`) + `,-1)`, "JSON.InvalidIndex"},
		{"at-length", `JSON.at(` + f031Decode(`[1]`) + `,1)`, "JSON.InvalidIndex"},
		{"at-huge", `JSON.at(` + f031Decode(`[1]`) + `,9999999999999999999999999999)`, "JSON.InvalidIndex"},
		{"string-kind", `JSON.string(` + f031Decode(`1`) + `)`, "JSON.TypeMismatch"},
		{"bool-kind", `JSON.bool(` + f031Decode(`"true"`) + `)`, "JSON.TypeMismatch"},
		{"int-kind", `JSON.int(` + f031Decode(`"1"`) + `)`, "JSON.TypeMismatch"},
		{"float-kind", `JSON.float(` + f031Decode(`true`) + `)`, "JSON.TypeMismatch"},
		{"length-kind", `JSON.length(` + f031Decode(`"abc"`) + `)`, "JSON.TypeMismatch"},
		{"int-fraction", `JSON.int(` + f031Decode(`1.0`) + `)`, "JSON.InvalidNumber"},
		{"int-exponent", `JSON.int(` + f031Decode(`1e0`) + `)`, "JSON.InvalidNumber"},
		{"float-overflow", `JSON.float(` + f031Decode(`1e400`) + `)`, "JSON.InvalidNumber"},
		{"float-underflow", `JSON.float(` + f031Decode(`1e-4000`) + `)`, "JSON.InvalidNumber"},
		{"float-negative-underflow", `JSON.float(` + f031Decode(`-1e-4000`) + `)`, "JSON.InvalidNumber"},
		{"precision-negative", `Float.format_fixed(1.0,-1)`, "Numeric.InvalidPrecision"},
		{"precision-over-bound", `Float.format_fixed(1.0,1001)`, "Numeric.InvalidPrecision"},
		{"precision-huge", `Float.format_fixed(1.0,999999999999999999999999999)`, "Numeric.InvalidPrecision"},
		{"upstream-nonfinite", `Float.format_fixed(Float.mul(1e308,1e308),2)`, "Numeric.NonFinite"},
	}
	for _, malformed := range []struct{ name, document string }{
		{"empty", ""}, {"trailing", "{} null"}, {"bom", "\ufeff{}"}, {"comment", "/*comment*/{}"},
		{"array-comma", "[1,]"}, {"object-comma", `{"a":1,}`}, {"leading-zero", "01"}, {"plus", "+1"},
		{"fraction-missing", "1."}, {"exponent-missing", "1e"}, {"nan", "NaN"}, {"infinity", "Infinity"},
		{"control", "\"raw\nline\""}, {"unpaired-high", `"\ud800"`}, {"unpaired-low", `"\udc00"`},
		{"wrong-surrogate-order", `"\udc00\ud800"`}, {"invalid-escape", `"\x41"`},
		{"duplicate", `{"a":1,"a":2}`}, {"duplicate-decoded", `{"a":1,"\u0061":2}`},
		{"nested-duplicate", `{"x":[{"a":1,"a":2}]}`},
	} {
		cases = append(cases, f031FailureCase{"json-" + malformed.name, f031Decode(malformed.document), "JSON.InvalidData"})
	}
	for index, text := range []string{"", "+", "-", " 1", "1 ", "1_000", "0x10", "1.0", "1e2", "١", "１２", "--1"} {
		cases = append(cases, f031FailureCase{fmt.Sprintf("integer-syntax-%d", index), fmt.Sprintf("Int.parse(%q)", text), "Int.InvalidText"})
	}
	for index, template := range []string{"{", "}", "{}", "{missing}", "{x!r}", "{x:.2}", "{x:<3}", "{x:>03}", "{x:>-1}", "{x:>18446744073709551616}", "{x:,}", "{x.y}"} {
		cases = append(cases, f031FailureCase{fmt.Sprintf("template-syntax-%d", index), fmt.Sprintf(`String.format(%q,Record.new(x:"text"))`, template), "String.InvalidFormat"})
	}
	return cases
}

func TestF031StrictUTF8(t *testing.T) {
	for _, invalid := range [][]byte{{0xc0, 0xaf}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80}, {0xe2, 0x82}} {
		root := fileFixtureRoot(t)
		path := filepath.Join(root, "invalid-json")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(`{"value":"`), invalid...)
		data = append(data, []byte(`"}`)...)
		if _, err := file.Write(data); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprintf(`flow Kind(error:Error)->String{return error.kind};flow Main()->String onError Kind{value=JSON.decode(File.read(File.Path.new(%q)));return "unexpected success"};return Main()`, path)
		valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--read-root", root, "--format", "json")), `"JSON.InvalidData"`)
	}
}

func TestF031InvalidDataIsLocated(t *testing.T) {
	got := runProgram(t, sourceFile(t, `return JSON.decode(Bytes.new("[1,]"))`), "run", "--format", "json")
	if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, "JSON.InvalidData") {
		t.Fatalf("invalid JSON did not produce a recoverable operation failure: %+v", got)
	}
	report := diagnostics(t, got.stderr)
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Span == nil {
		t.Fatalf("JSON failure lacks its public source location: %+v", got)
	}
}
