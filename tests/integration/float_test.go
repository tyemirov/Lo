package integration_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func successfulValue(t *testing.T, v runtimeValue) runtimeValue {
	t.Helper()
	if v.Status == "ok" {
		var inner runtimeValue
		if err := json.Unmarshal(v.Value, &inner); err != nil {
			t.Fatal(err)
		}
		return inner
	}
	return v
}

func TestFloatArithmetic(t *testing.T) {
	for _, tc := range []struct{ name, expression, typ, want string }{
		{"add", `Float.add(right: 2.5, left: 1.25)`, "Float!", `"3.75"`},
		{"sub", `Float.sub(1.25, 2.5)`, "Float!", `"-1.25"`},
		{"mul", `Float.mul(1.25, 2.0)`, "Float!", `"2.5"`},
		{"div", `Float.div(3.0, 2.0)`, "Float!", `"1.5"`},
		{"sqrt", `Float.sqrt(2.25)`, "Float!", `"1.5"`},
		{"sqrt-rounded", `Float.sqrt(2.0)`, "Float!", `"1.4142135623730951"`},
		{"eq", `Float.eq(-0.0, 0.0)`, "Bool", `true`},
		{"lt", `Float.lt(1.0, 2.0)`, "Bool", `true`},
		{"lte", `Float.lte(2.0, 2.0)`, "Bool", `true`},
		{"gt", `Float.gt(1.0, 2.0)`, "Bool", `false`},
		{"gte", `Float.gte(2.0, 2.0)`, "Bool", `true`},
		{"integer-conversion", `Int.to_float(-7)`, "Float!", `"-7"`},
		{"integer-tie-even-down", `Int.to_float(9007199254740993)`, "Float!", `"9.007199254740992e+15"`},
		{"integer-tie-even-up", `Int.to_float(9007199254740995)`, "Float!", `"9.007199254740996e+15"`},
		{"truncate", `Float.to_int(-1.9)`, "Int", `"-1"`},
		{"large-integer", `Float.to_int(1e20)`, "Int", `"100000000000000000000"`},
		{"string", `Float.to_string(1.25)`, "String", `"1.25"`},
		{"string-signed-zero", `Float.to_string(-0.0)`, "String", `"-0"`},
		{"tie-even-down", `Float.add(1.0, 1.1102230246251565e-16)`, "Float!", `"1"`},
		{"tie-even-up", `Float.add(1.0000000000000002, 1.1102230246251565e-16)`, "Float!", `"1.0000000000000004"`},
		{"subnormal", `Float.mul(2.2250738585072014e-308, 0.5)`, "Float!", `"1.1125369292536007e-308"`},
		{"smallest-subnormal", `Float.add(5e-324, 0.0)`, "Float!", `"5e-324"`},
		{"underflow-zero", `Float.div(5e-324, 2.0)`, "Float!", `"0"`},
		{"underflow-negative-zero", `Float.mul(-5e-324, 0.5)`, "Float!", `"-0"`},
		{"negative-zero-sqrt", `Float.sqrt(-0.0)`, "Float!", `"-0"`},
		{"maximum-finite", `Float.add(1.7976931348623157e308, 0.0)`, "Float!", `"1.7976931348623157e+308"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "return " + tc.expression
			checked := run(t, "check", sourceFile(t, source), "--format", "json")
			if checked.status != 0 || !strings.Contains(checked.stdout, `"result_type":"`+tc.typ+`"`) {
				t.Fatalf("check: %+v", checked)
			}
			v := runValue(t, source)
			if v.Type != tc.typ {
				t.Fatalf("type %s, want %s", v.Type, tc.typ)
			}
			valueJSON(t, successfulValue(t, v), tc.want)
		})
	}
	valueJSON(t, successfulValue(t, runValue(t, `flow Separate() -> Float! {
  product = Float.mul(1.0000000000000002, 0.9999999999999998)
  return Float.sub(product, 1.0)
}; return Separate()`)), `"0"`)
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 1024), new(big.Int).Lsh(big.NewInt(1), 971))
	valueJSON(t, runValue(t, `return Float.to_int(1.7976931348623157e308)`), fmt.Sprintf("%q", maximum.String()))
}

func TestFloatFailures(t *testing.T) {
	overflow := new(big.Int).Lsh(big.NewInt(1), 1024).String()
	for _, tc := range []struct{ expression, kind string }{
		{`Float.add(1e308, 1e308)`, "Numeric.NonFinite"},
		{`Float.sub(-1e308, 1e308)`, "Numeric.NonFinite"},
		{`Float.mul(1e308, 2.0)`, "Numeric.NonFinite"},
		{`Float.div(1e308, 0.5)`, "Numeric.NonFinite"},
		{`Float.div(1.0, 0.0)`, "Numeric.DivideByZero"},
		{`Float.div(1.0, -0.0)`, "Numeric.DivideByZero"},
		{`Float.div(0.0, 0.0)`, "Numeric.DivideByZero"},
		{`Float.sqrt(-1.0)`, "Numeric.Domain"},
		{`Int.to_float(` + overflow + `)`, "Numeric.NonFinite"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			got := run(t, "run", sourceFile(t, "return "+tc.expression), "--format", "json")
			var out struct {
				Diagnostics []struct {
					Code  string
					Error runtimeValue
				}
			}
			if err := json.Unmarshal([]byte(got.stderr), &out); err != nil {
				t.Fatal(err)
			}
			if got.status != 1 || got.stdout != "" || len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "runtime.unhandled-error" || out.Diagnostics[0].Error.Kind != tc.kind {
				t.Fatalf("numeric failure: %+v", got)
			}
		})
	}
	v := runValue(t, `flow Handler(error: Error) -> Error { return error }
flow Inner(x: Float) -> Float! { return Float.sqrt(x) }
flow Outer() -> Error onError Handler { unused = Inner(-1.0); return Error.new("missed", "", Record.new()) }
return Outer()`)
	if v.Kind != "Numeric.Domain" || len(v.Trace) != 2 {
		t.Fatalf("handled numeric failure and trace: %+v", v)
	}
}

func TestFloatTypeAndInspectionContracts(t *testing.T) {
	for _, tc := range []struct{ source, code string }{
		{`return Float.add(1, 2.0)`, "check.type-mismatch"},
		{`return Math.add(1.0, 2.0)`, "check.type-mismatch"},
		{`return Float.new(1)`, "check.type-mismatch"},
		{`return Int.to_float(1.0)`, "check.type-mismatch"},
		{`flow Total() -> Float { x = Float.add(1.0, 2.0); return x }; return Total()`, "check.unhandled-failure"},
		{`return Range.new(1.0, 2.0, true, true)`, "check.type-constraint"},
		{`return Map.new(Record.new(key: 1.0, value: 1))`, "check.type-constraint"},
	} {
		got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != tc.code {
			t.Fatalf("type rejection: %+v, want %s", got, tc.code)
		}
	}
	report, _, _ := inspectSource(t, `flow Work() -> Float! { x = Float.div(1.0, 2.0); return Float.sqrt(x) }; return Work()`)
	if len(report.Capabilities) != 0 || len(report.Reads) != 0 || len(report.Activations) != 0 {
		t.Fatalf("numeric operations have effects: %+v", report)
	}
	want := map[string]bool{"Numeric.NonFinite": false, "Numeric.DivideByZero": false, "Numeric.Domain": false}
	for _, source := range report.ErrorSources {
		for _, kind := range source.Kinds {
			want[kind] = true
		}
	}
	for kind, present := range want {
		if !present {
			t.Errorf("missing numeric error kind %s", kind)
		}
	}
}
