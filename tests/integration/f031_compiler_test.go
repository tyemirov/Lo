package integration_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

func TestF031CompilerContracts(t *testing.T) {
	for _, fixture := range f031ValueCases() {
		t.Run(fixture.name, func(t *testing.T) {
			source := fmt.Sprintf(`flow Main()->%s!{return %s};return Main()`, fixture.typ, fixture.expression)
			got := run(t, "check", sourceFile(t, source), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("F031 typed value contract: %+v", got)
			}
		})
	}
	source := `flow Decode(text:String)->JSON.Value!{return JSON.decode(Bytes.new(text))};return List.map(List.new("null","true"),Decode)`
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("pure JSON operation rejected in List.map: %+v", got)
	}
}

func TestF031CompilerRejections(t *testing.T) {
	for _, fixture := range []struct{ name, source, code string }{
		{"decode-bytes", `return JSON.decode("null")`, "check.type-mismatch"},
		{"opaque-json", `flow Field(value:JSON.Value)->String{return value.kind};return 0`, "check.unknown-field"},
		{"nominal-json", `return JSON.string(Record.new(kind:"string",value:"text"))`, "check.type-mismatch"},
		{"index-int", `flow Main(value:JSON.Value)->JSON.Value!{return JSON.at(value,true)};return 0`, "check.type-mismatch"},
		{"format-list", `return String.format("{value}",Record.new(value:List.new(1)))`, "check.type-mismatch"},
		{"format-nested-record", `return String.format("{value}",Record.new(value:Record.new(x:1)))`, "check.type-mismatch"},
		{"format-path", `return String.format("{value}",Record.new(value:File.Path.new("path")))`, "check.type-mismatch"},
		{"format-fallible-field", `return String.format("{value}",Record.new(value:Math.div(1,0)))`, "check.type-mismatch"},
		{"format-opaque-record", `flow Format(fields:Record)->String!{return String.format("{value}",fields)};return 0`, "check.type-mismatch"},
		{"format-null", `return String.format("{value}",Record.new(value:null))`, "check.type-mismatch"},
		{"format-scalar", `return String.format("{value}",Record.new(value:Scalar.new("x")))`, "check.type-mismatch"},
		{"format-reordered-fallible-field", `return String.format(fields:Record.new(value:Math.div(1,0)),template:"{value}")`, "check.type-mismatch"},
		{"fixed-precision-int", `return Float.format_fixed(1.0,2.0)`, "check.type-mismatch"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, fixture.source), "--format", "json")
			if got.status != 1 || got.stdout != "" {
				t.Fatalf("invalid F031 call accepted: %+v", got)
			}
			diagnostic := diagnostics(t, got.stderr)
			if len(diagnostic.Diagnostics) != 1 || diagnostic.Diagnostics[0].Code != fixture.code {
				t.Fatalf("expected %s: %+v", fixture.code, got)
			}
		})
	}
}

func TestF031FormattingArgumentBinding(t *testing.T) {
	for _, source := range []string{
		`return String.format(fields:Record.new(text:"x",number:1,float:1.0,boolean:true),template:"{text}{number}{float}{boolean}")`,
		`return "{value}" |> String.format(fields:Record.new(value:1))`,
		`return String.format("literal",Record.new())`,
		`flow Fields(value:Int)->Record{value:Int}{return Record.new(value:value)};flow Main()->String!{return String.format("{value}",Fields(Math.div(1,0)))};return Main()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("canonical formatting argument binding: %+v", got)
		}
		for _, target := range []string{"darwin/arm64", "linux/arm64"} {
			output := filepath.Join(t.TempDir(), "format.ll")
			built := run(t, "build", sourceFile(t, source), "--emit", "llvm", "--target", target, "--output", output, "--format", "json")
			if built.status != 0 || built.stderr != "" {
				t.Fatalf("formatting argument lowering for %s: %+v", target, built)
			}
		}
	}
}

func TestF031InspectionPureFailures(t *testing.T) {
	for _, fixture := range []struct {
		operation, declaration, expression string
		errors                             []string
	}{
		{"JSON.decode", "data:Bytes", "JSON.decode(data)", []string{"JSON.InvalidData"}},
		{"JSON.get", "value:JSON.Value", `JSON.get(value,"field")`, []string{"JSON.TypeMismatch", "JSON.FieldAbsent"}},
		{"JSON.at", "value:JSON.Value", "JSON.at(value,0)", []string{"JSON.TypeMismatch", "JSON.InvalidIndex"}},
		{"JSON.int", "value:JSON.Value", "JSON.int(value)", []string{"JSON.TypeMismatch", "JSON.InvalidNumber"}},
		{"JSON.float", "value:JSON.Value", "JSON.float(value)", []string{"JSON.TypeMismatch", "JSON.InvalidNumber"}},
		{"Float.format_fixed", "value:Float", "Float.format_fixed(value,2)", []string{"Numeric.InvalidPrecision"}},
		{"Int.parse", "text:String", "Int.parse(text)", []string{"Int.InvalidText"}},
		{"String.format", "text:String", `String.format(text,Record.new(value:1))`, []string{"String.InvalidFormat"}},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			source := fmt.Sprintf(`flow Example(%s)->Bool!{result=%s;return true}`, fixture.declaration, fixture.expression)
			report, _, output := inspectSource(t, source)
			if len(report.Reads) != 0 || len(report.Capabilities) != 0 || len(report.Activations) != 0 {
				t.Fatalf("pure F031 operation gained authority: %s", output)
			}
			if len(report.ErrorSources) != 1 || report.ErrorSources[0].Operation != fixture.operation || !slices.Equal(report.ErrorSources[0].Kinds, fixture.errors) {
				t.Fatalf("F031 operation failure metadata mismatch: %s", output)
			}
		})
	}
}

func TestF031TypedFormattingFailurePropagation(t *testing.T) {
	source := `flow Failed(error:Error)->String{return error.kind};flow Fields(value:Int)->Record{value:Int}{return Record.new(value:value)};flow Main()->String onError Failed{return String.format("{value:,}",Fields(Math.div(1,0)))};return Main()`
	valueJSON(t, fileOutput(t, runProgram(t, sourceFile(t, source), "run", "--format", "json")), `"Math.DivideByZero"`)
}
