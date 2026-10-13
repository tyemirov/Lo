package integration_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func tensorResult(t *testing.T, source string, shape, elements []string) {
	t.Helper()
	v := successfulValue(t, runValue(t, source))
	if v.Type != "Tensor<FP32>" {
		t.Fatalf("tensor type: %s", v.Type)
	}
	var tensor struct {
		DType  string   `json:"dtype"`
		Device string   `json:"device"`
		Shape  []string `json:"shape"`
		Values []string `json:"values"`
	}
	if err := json.Unmarshal(v.Value, &tensor); err != nil {
		t.Fatal(err)
	}
	if tensor.DType != "fp32" || tensor.Device != "cpu" || !reflect.DeepEqual(tensor.Shape, shape) || !reflect.DeepEqual(tensor.Values, elements) {
		t.Fatalf("tensor = %s; want shape %v, elements %v", v.Value, shape, elements)
	}
}

func TestTensorConstructionAndMetadata(t *testing.T) {
	tensorResult(t, `return Tensor.new(List.new(1.25, -0.0, 2.0, 3.5), List.new(2, 2))`, []string{"2", "2"}, []string{"1.25", "-0", "2", "3.5"})
	tensorResult(t, `return Tensor.new(List.new(1.0), List.new())`, []string{}, []string{"1"})
	tensorResult(t, `return Tensor.new(List.new(), List.new(2, 0, 3))`, []string{"2", "0", "3"}, []string{})
	tensorResult(t, `return Tensor.new(List.new(1.0000000596046448, 1.0000001788139343), List.new(2))`, []string{"2"}, []string{"1", "1.0000002"})
	tensorResult(t, `return Tensor.new(List.new(1.401298464324817e-45, -1e-60), List.new(2))`, []string{"2"}, []string{"1e-45", "-0"})
	for _, tc := range []struct{ expression, typ, want string }{
		{`Tensor.rank(x)`, "Int", `"2"`},
		{`Tensor.shape(x)`, "List<Int>", `[{"type":"Int","value":"2"},{"type":"Int","value":"1"}]`},
		{`Tensor.dtype(x)`, "String", `"fp32"`},
		{`Tensor.device(x)`, "Device", `"cpu"`},
		{`Tensor.values(x)`, "List<Float>", `[{"type":"Float","value":"1"},{"type":"Float","value":"2"}]`},
	} {
		v := successfulValue(t, runValue(t, fmt.Sprintf(`flow Read() -> %s! { x = Tensor.new(List.new(1.0, 2.0), List.new(2, 1)); return %s }; return Read()`, tc.typ, tc.expression)))
		if v.Type != tc.typ {
			t.Fatalf("metadata type: %s", v.Type)
		}
		valueJSON(t, v, tc.want)
	}
	valueJSON(t, successfulValue(t, runValue(t, `flow ScalarValue() -> Float! { x = Tensor.new(List.new(0.1), List.new()); return Tensor.to_float(x) }; return ScalarValue()`)), `"0.10000000149011612"`)
	for _, selector := range []string{"cpu", "cuda:0", "cuda-sim:0"} {
		v := successfulValue(t, runValue(t, fmt.Sprintf(`return Device.new(%q)`, selector)))
		if v.Type != "Device" {
			t.Fatal(v.Type)
		}
		valueJSON(t, v, fmt.Sprintf("%q", selector))
	}
}

func TestTensorLayout(t *testing.T) {
	const input = `a = Tensor.new(List.new(1.0, 2.0, 3.0, 4.0, 5.0, 6.0), List.new(2, 3))`
	for _, tc := range []struct {
		expression    string
		shape, values []string
	}{
		{`Tensor.reshape(a, List.new(3, 2))`, []string{"3", "2"}, []string{"1", "2", "3", "4", "5", "6"}},
		{`Tensor.transpose(a, List.new(1, 0))`, []string{"3", "2"}, []string{"1", "4", "2", "5", "3", "6"}},
		{`Tensor.broadcast(a, List.new(2, 2, 3))`, []string{"2", "2", "3"}, []string{"1", "2", "3", "4", "5", "6", "1", "2", "3", "4", "5", "6"}},
	} {
		tensorResult(t, fmt.Sprintf(`flow Layout() -> Tensor<FP32>! { %s; return %s }; return Layout()`, input, tc.expression), tc.shape, tc.values)
	}
	tensorResult(t, `flow Empty() -> Tensor<FP32>! { a = Tensor.new(List.new(2.0), List.new(1)); return Tensor.broadcast(a, List.new(0)) }; return Empty()`, []string{"0"}, []string{})
	tensorResult(t, `flow Empty() -> Tensor<FP32>! { a = Tensor.new(List.new(), List.new(2, 0)); return Tensor.transpose(a, List.new(1, 0)) }; return Empty()`, []string{"0", "2"}, []string{})
	v := successfulValue(t, runValue(t, `flow Original() -> List<Float>! {
  a = Tensor.new(List.new(1.0, 2.0), List.new(2))
  b = Tensor.reshape(a, List.new(1, 2))
  c = Tensor.transpose(b, List.new(1, 0))
  changed = Tensor.add(c, c)
  return Tensor.values(a)
}; return Original()`))
	valueJSON(t, v, `[{"type":"Float","value":"1"},{"type":"Float","value":"2"}]`)
}

func TestTensorStaticShapes(t *testing.T) {
	for _, source := range []string{
		`return Tensor.new(List.new(1.0), List.new(-1))`,
		`return Tensor.new(List.new(1.0), List.new(2))`,
		`flow Bad() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0, 2.0), List.new(2)); return Tensor.reshape(a, List.new(3)) }; return Bad()`,
		`flow Bad() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0, 2.0), List.new(2)); return Tensor.transpose(a, List.new(1)) }; return Bad()`,
		`flow Bad() -> Tensor<FP32>! { a = Tensor.new(List.new(), List.new(2, 0)); b = Tensor.new(List.new(1.0, 2.0, 3.0), List.new(3)); return Tensor.add(a, b) }; return Bad()`,
		`flow Add(a: Tensor<FP32>, b: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.add(a,b) }; flow Bad() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0, 2.0), List.new(2)); b = Tensor.new(List.new(1.0, 2.0, 3.0), List.new(3)); return Add(a,b) }; return Bad()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 {
			t.Fatalf("accepted invalid shape: %+v", got)
		}
		d := diagnostics(t, got.stderr).Diagnostics[0]
		if d.Code != "check.tensor-shape" || d.Span == nil || d.Expected == "" || d.Actual == "" {
			t.Fatalf("shape diagnostic needs expected, actual, and source: %+v", d)
		}
	}
	for _, source := range []string{
		`flow F(x: FP32) -> Int { return 0 }`,
		`flow F(x: Tensor<Int>) -> Int { return 0 }`,
		`flow F(x: Tensor) -> Int { return 0 }`,
		`flow F(x: Tensor<FP32, Float>) -> Int { return 0 }`,
		`return Tensor.new(List.new(1), List.new(1))`,
		`Tensor = 3`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 {
			t.Fatalf("accepted invalid tensor type: %+v", got)
		}
	}
}

func TestTensorDynamicFailures(t *testing.T) {
	for _, tc := range []struct{ source, kind string }{
		{`flow Shape(n: Int) -> Tensor<FP32>! { return Tensor.new(List.new(1.0), List.new(n)) }; return Shape(Math.sub(0,1))`, "Tensor.InvalidShape"},
		{`flow Shape(n: Int) -> Tensor<FP32>! { return Tensor.new(List.new(1.0), List.new(n)) }; return Shape(Math.add(1,1))`, "Tensor.InvalidShape"},
		{`return Tensor.new(List.new(1e40), List.new(1))`, "Numeric.NonFinite"},
		{`return Device.new("cuda:1")`, "Device.InvalidSelector"},
		{`flow ReadScalar(n: Int) -> Float! { a = Tensor.new(List.new(1.0, 2.0), List.new(n)); return Tensor.to_float(a) }; return ReadScalar(Math.add(1,1))`, "Tensor.ShapeMismatch"},
		{`flow Shape(n: Int) -> Tensor<FP32>! { return Tensor.new(List.new(), List.new(n,n,n,n)) }; return Shape(Math.mul(1000,1000))`, "Tensor.InvalidShape"},
	} {
		got := run(t, "run", sourceFile(t, tc.source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"kind":"`+tc.kind+`"`) || got.stdout != "" {
			t.Fatalf("dynamic tensor failure %s: %+v", tc.kind, got)
		}
	}
}

func TestTensorCPUOperations(t *testing.T) {
	for _, tc := range []struct {
		operation string
		want      []string
	}{
		{"add", []string{"11", "22", "13", "24"}},
		{"sub", []string{"-9", "-18", "-7", "-16"}},
		{"mul", []string{"10", "40", "30", "80"}},
		{"div", []string{"0.1", "0.1", "0.3", "0.2"}},
	} {
		tensorResult(t, fmt.Sprintf(`flow Work() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0,2.0,3.0,4.0),List.new(2,2)); b = Tensor.new(List.new(10.0,20.0),List.new(2)); return Tensor.%s(a,b) }; return Work()`, tc.operation), []string{"2", "2"}, tc.want)
	}
	tensorResult(t, `flow Product() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0),List.new(2,3)); b = Tensor.new(List.new(7.0,8.0,9.0,10.0,11.0,12.0),List.new(3,2)); return Tensor.matmul(a,b) }; return Product()`, []string{"2", "2"}, []string{"58", "64", "139", "154"})
	tensorResult(t, `flow Product() -> Tensor<FP32>! { a = Tensor.new(List.new(),List.new(2,0)); b = Tensor.new(List.new(),List.new(0,3)); return Tensor.matmul(a,b) }; return Product()`, []string{"2", "3"}, []string{"0", "0", "0", "0", "0", "0"})
	tensorResult(t, `flow Root() -> Tensor<FP32>! { a = Tensor.new(List.new(-0.0,0.0,2.25),List.new(3)); return Tensor.sqrt(a) }; return Root()`, []string{"3"}, []string{"-0", "0", "1.5"})
	tensorResult(t, `flow Rectify() -> Tensor<FP32>! { a = Tensor.new(List.new(-1.0,-0.0,0.0,2.0),List.new(4)); return Tensor.relu(a) }; return Rectify()`, []string{"4"}, []string{"0", "0", "0", "2"})
	for _, tc := range []struct {
		operation, axes string
		shape, values   []string
	}{
		{"sum", "List.new(0)", []string{"3"}, []string{"5", "7", "9"}},
		{"sum", "List.new(1)", []string{"2"}, []string{"6", "15"}},
		{"sum", "List.new(0,1)", []string{}, []string{"21"}},
		{"mean", "List.new(0)", []string{"3"}, []string{"2.5", "3.5", "4.5"}},
		{"mean", "List.new(0,1)", []string{}, []string{"3.5"}},
		{"mean", "List.new()", []string{"2", "3"}, []string{"1", "2", "3", "4", "5", "6"}},
	} {
		tensorResult(t, fmt.Sprintf(`flow Reduce() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0),List.new(2,3)); return Tensor.%s(a,%s) }; return Reduce()`, tc.operation, tc.axes), tc.shape, tc.values)
	}
	tensorResult(t, `flow Sum() -> Tensor<FP32>! { a = Tensor.new(List.new(16777216.0,1.0,-16777216.0),List.new(3)); return Tensor.sum(a,List.new(0)) }; return Sum()`, []string{}, []string{"0"})
	tensorResult(t, `flow Sum() -> Tensor<FP32>! { a = Tensor.new(List.new(),List.new(0,2)); return Tensor.sum(a,List.new(0)) }; return Sum()`, []string{"2"}, []string{"0", "0"})
}

func TestTensorCPUFailures(t *testing.T) {
	for _, tc := range []struct{ body, kind string }{
		{`a = Tensor.new(List.new(1.0,2.0),List.new(2)); b = Tensor.new(List.new(1.0,0.0),List.new(2)); return Tensor.div(a,b)`, "Numeric.DivideByZero"},
		{`a = Tensor.new(List.new(1.0,-1.0),List.new(2)); return Tensor.sqrt(a)`, "Numeric.Domain"},
		{`a = Tensor.new(List.new(3e38),List.new()); return Tensor.add(a,a)`, "Numeric.NonFinite"},
		{`a = Tensor.new(List.new(3e38),List.new()); return Tensor.mul(a,a)`, "Numeric.NonFinite"},
		{`a = Tensor.new(List.new(),List.new(0,2)); return Tensor.mean(a,List.new(0))`, "Tensor.EmptyReduction"},
		{`a = Tensor.new(List.new(3e38,3e38,-3e38),List.new(3)); return Tensor.sum(a,List.new(0))`, "Numeric.NonFinite"},
	} {
		got := run(t, "run", sourceFile(t, `flow Bad() -> Tensor<FP32>! { `+tc.body+` }; return Bad()`), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"kind":"`+tc.kind+`"`) || got.stdout != "" {
			t.Fatalf("CPU error %s: %+v", tc.kind, got)
		}
	}
}
