package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type f031ValueCase struct{ name, expression, typ, value string }

func f031Decode(document string) string {
	return fmt.Sprintf(`JSON.decode(Bytes.new(%q))`, document)
}

func f031ValueCases() []f031ValueCase {
	cases := []f031ValueCase{
		{"document-whitespace", `JSON.string(JSON.get(` + f031Decode(" \r\n{\"name\":\"value\"}\t ") + `,"name"))`, "String", `"value"`},
		{"json-array-index", `JSON.string(JSON.at(` + f031Decode(`["first","second"]`) + `,1))`, "String", `"second"`},
		{"json-paired-surrogates", `JSON.string(` + f031Decode(`"\ud83d\ude00"`) + `)`, "String", `"😀"`},
		{"json-escaped-nul", `JSON.string(` + f031Decode(`"a\u0000b"`) + `)`, "String", `"a\u0000b"`},
		{"json-case-sensitive-keys", `JSON.string(JSON.get(` + f031Decode(`{"a":"lower","A":"upper"}`) + `,"A"))`, "String", `"upper"`},
		{"json-distinct-normalization", `JSON.length(` + f031Decode(`{"é":1,"é":2}`) + `)`, "Int", `"2"`},
		{"json-bool", `JSON.bool(` + f031Decode(`false`) + `)`, "Bool", `false`},
		{"json-array-length", `JSON.length(` + f031Decode(`[null,1,true]`) + `)`, "Int", `"3"`},
		{"json-empty-object", `JSON.length(` + f031Decode(`{}`) + `)`, "Int", `"0"`},
		{"json-exact-integer", `JSON.int(` + f031Decode(`9007199254740993`) + `)`, "Int", `"9007199254740993"`},
		{"json-negative-zero-int", `JSON.int(` + f031Decode(`-0`) + `)`, "Int", `"0"`},
		{"json-float-rounding", `Float.to_int(JSON.float(` + f031Decode(`9007199254740993`) + `))`, "Int", `"9007199254740992"`},
		{"json-negative-zero-float", `Float.to_string(JSON.float(` + f031Decode(`-0e9999`) + `))`, "String", `"-0"`},
		{"json-finite-subnormal", `Float.gt(JSON.float(` + f031Decode(`5e-324`) + `),0.0)`, "Bool", `true`},
		{"json-opaque-number", f031Decode(`1e400`), "JSON.Value", `{"kind":"number","value":"1e400"}`},
		{"json-opaque-small-number", f031Decode(`1e-4000`), "JSON.Value", `{"kind":"number","value":"1e-4000"}`},
		{"json-opaque-tree", f031Decode(`{"z":[true,null],"a":1.2300e+2}`), "JSON.Value", `{"kind":"object","value":{"a":{"kind":"number","value":"1.2300e+2"},"z":{"kind":"array","value":[{"kind":"bool","value":true},{"kind":"null","value":null}]}}}`},
		{"integer-plus-leading-zero", `Int.parse("+000123")`, "Int", `"123"`},
		{"integer-negative-zero", `Int.parse("-000")`, "Int", `"0"`},
		{"integer-exact-large", `Int.parse("-90071992547409931234567890")`, "Int", `"-90071992547409931234567890"`},
		{"fixed-even-down", `Float.format_fixed(2.5,0)`, "String", `"2"`},
		{"fixed-even-up", `Float.format_fixed(3.5,0)`, "String", `"4"`},
		{"fixed-negative-even", `Float.format_fixed(-2.5,0)`, "String", `"-2"`},
		{"fixed-binary-value", `Float.format_fixed(2.675,2)`, "String", `"2.67"`},
		{"fixed-negative-zero", `Float.format_fixed(-0.0,2)`, "String", `"-0.00"`},
		{"fixed-rounded-negative-zero", `Float.format_fixed(-0.004,2)`, "String", `"-0.00"`},
		{"fixed-trailing-zeros", `Float.format_fixed(1.5,4)`, "String", `"1.5000"`},
		{"format-scalars", `String.format("{text}/{integer}/{float}/{bool}",Record.new(text:"x",integer:-2,float:-0.0,bool:true))`, "String", `"x/-2/-0/true"`},
		{"format-grouping", `String.format("{n:,}/{z:,}",Record.new(n:-1234567890,z:0))`, "String", `"-1,234,567,890/0"`},
		{"format-width", `String.format("[{text:>3}]",Record.new(text:"界"))`, "String", `"[  界]"`},
		{"format-combining-width", `String.format("[{text:>3}]",Record.new(text:"e\u0301"))`, "String", `"[ é]"`},
		{"format-grouped-width", `String.format("[{n:>9,}]",Record.new(n:-1234))`, "String", `"[   -1,234]"`},
		{"format-no-truncation", `String.format("{text:>1}",Record.new(text:"long"))`, "String", `"long"`},
		{"format-zero-width", `String.format("{text:>0}",Record.new(text:"x"))`, "String", `"x"`},
		{"format-braces-repeated-unused", `String.format("{{{x}}}/{x}",Record.new(x:"ok",unused:99))`, "String", `"{ok}/ok"`},
		{"format-no-recursion", `String.format("{x}",Record.new(x:"{missing}"))`, "String", `"{missing}"`},
	}
	for _, kind := range []struct{ document, name string }{{"null", "null"}, {"true", "bool"}, {"1", "number"}, {`"x"`, "string"}, {"[]", "array"}, {"{}", "object"}} {
		cases = append(cases, f031ValueCase{"kind-" + kind.name, `JSON.kind(` + f031Decode(kind.document) + `)`, "String", fmt.Sprintf("%q", kind.name)})
	}
	cases = append(cases,
		f031ValueCase{"fixed-subnormal", `Float.format_fixed(5e-324,324)`, "String", fmt.Sprintf("%q", "0."+strings.Repeat("0", 323)+"5")},
		f031ValueCase{"fixed-maximum-precision", `Float.format_fixed(1.0,1000)`, "String", fmt.Sprintf("%q", "1."+strings.Repeat("0", 1000))},
		f031ValueCase{"fixed-maximum-finite", `Float.format_fixed(1.7976931348623157e308,0)`, "String", `"179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222948165808559332123348274797826204144723168738177180919299881250404026184124858368"`},
	)
	return cases
}

func f031PortableSource() string {
	var declarations, fields []string
	for index, fixture := range f031ValueCases() {
		declarations = append(declarations, fmt.Sprintf(`flow Value%d() -> %s! { return %s }`, index, fixture.typ, fixture.expression))
		fields = append(fields, fmt.Sprintf("v%d: Value%d()", index, index))
	}
	for index, fixture := range f031FailureCases() {
		declarations = append(declarations, fmt.Sprintf(`flow FailureKind%d(error:Error)->String{return error.kind};flow Failure%d()->String onError FailureKind%d{unused=%s;return "unexpected success"}`, index, index, index, fixture.expression))
		fields = append(fields, fmt.Sprintf("e%d: Failure%d()", index, index))
	}
	return strings.Join(declarations, "\n") + "\nreturn Record.new(" + strings.Join(fields, ",") + ")"
}

func f031VerifyPortable(t *testing.T, actual result) {
	t.Helper()
	root := fileOutput(t, actual)
	for index, fixture := range f031ValueCases() {
		value := recordField(t, root, fmt.Sprintf("v%d", index))
		if strings.HasSuffix(value.Type, "!") {
			var inner runtimeValue
			if err := json.Unmarshal(value.Value, &inner); err != nil {
				t.Fatal(err)
			}
			value = inner
		}
		if value.Type != fixture.typ {
			t.Fatalf("%s: type %s, want %s", fixture.name, value.Type, fixture.typ)
		}
		t.Run(fixture.name, func(t *testing.T) { valueJSON(t, value, fixture.value) })
	}
	for index, fixture := range f031FailureCases() {
		t.Run(fixture.name, func(t *testing.T) {
			valueJSON(t, recordField(t, root, fmt.Sprintf("e%d", index)), fmt.Sprintf("%q", fixture.kind))
		})
	}
}

func TestF031ValuesAndFailures(t *testing.T) {
	f031VerifyPortable(t, runProgram(t, sourceFile(t, f031PortableSource()), "run", "--format", "json"))
}

func TestF031LinuxValuesAndFailures(t *testing.T) {
	artifact := linuxLLVMArtifact(t, f031PortableSource())
	f031VerifyPortable(t, runLinuxLLVMExecutable(t, artifact, "run", "--format", "json"))
}
