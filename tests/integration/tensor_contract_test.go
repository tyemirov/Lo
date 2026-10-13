package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTensorInspectionMetadata(t *testing.T) {
	source := `flow Dynamic(n: Int) -> Tensor<FP32>! { return Tensor.new(List.new(1.0), List.new(n)) }
flow Known() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0,2.0),List.new(2)); return Tensor.reshape(a,List.new(1,2)) }
return Known()`
	got := run(t, "inspect", sourceFile(t, source), "--format", "json")
	if got.status != 0 {
		t.Fatalf("inspect: %+v", got)
	}
	var output struct {
		Report struct {
			Nodes []struct {
				Operation string `json:"operation"`
				Tensor    *struct {
					Dtype, Device, Layout, Profile string
					Shape                          []string
					StorageBytes                   *uint64 `json:"storage_bytes"`
				} `json:"tensor"`
			}
			Capabilities, Reads []json.RawMessage
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &output); err != nil {
		t.Fatal(err)
	}
	report := output.Report
	known, unknown := false, false
	for _, n := range report.Nodes {
		if n.Operation != "Tensor.new" {
			continue
		}
		if n.Tensor == nil || n.Tensor.Dtype != "fp32" || n.Tensor.Device != "cpu" || n.Tensor.Layout != "row-major" || n.Tensor.Profile != "cpu-fp32-strict" {
			t.Fatalf("missing tensor metadata: %s", got.stdout)
		}
		if n.Tensor.Shape == nil && n.Tensor.StorageBytes == nil {
			unknown = true
		}
		if len(n.Tensor.Shape) == 1 && n.Tensor.Shape[0] == "2" && n.Tensor.StorageBytes != nil && *n.Tensor.StorageBytes == 8 {
			known = true
		}
	}
	if !known || !unknown || len(report.Capabilities) != 0 || len(report.Reads) != 0 {
		t.Fatalf("metadata/purity: %s", got.stdout)
	}
}

func TestTensorShapeBoundaries(t *testing.T) {
	for _, body := range []string{
		`return Tensor.matmul(a,a)`,
		`return Tensor.broadcast(a,List.new(3))`,
		`return Tensor.sum(a,List.new(0,0))`,
		`return Tensor.sum(a,List.new(-1))`,
	} {
		got := run(t, "check", sourceFile(t, `flow Bad() -> Tensor<FP32>! { a = Tensor.new(List.new(1.0,2.0),List.new(2)); `+body+` }; return Bad()`), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.tensor-shape" {
			t.Fatalf("known shape: %+v", got)
		}
	}
	for _, source := range []string{
		`flow Bad(a: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.sum(a,List.new(-1)) }`,
		`flow Bad(a: Tensor<FP32>) -> Tensor<FP32>! { return Tensor.transpose(a,List.new(0,0)) }`,
		`flow Bad() -> Float! { a=Tensor.new(List.new(1.0),List.new(1)); return Tensor.to_float(a) }; return Bad()`,
	} {
		got := run(t, "check", sourceFile(t, source), "--format", "json")
		if got.status != 1 || diagnostics(t, got.stderr).Diagnostics[0].Code != "check.tensor-shape" {
			t.Fatalf("known rank/axes: %+v", got)
		}
	}
	for _, tc := range []struct{ op, args, kind string }{
		{"reshape", "List.new(n)", "Tensor.ShapeMismatch"},
		{"broadcast", "List.new(n)", "Tensor.ShapeMismatch"},
		{"transpose", "List.new(n)", "Tensor.InvalidShape"},
		{"sum", "List.new(n)", "Tensor.InvalidShape"},
	} {
		source := fmt.Sprintf(`flow Bad(n: Int) -> Tensor<FP32>! { a=Tensor.new(List.new(1.0,2.0),List.new(2)); return Tensor.%s(a,%s) }; return Bad(Math.add(2,1))`, tc.op, tc.args)
		got := run(t, "run", sourceFile(t, source), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, `"kind":"`+tc.kind+`"`) {
			t.Fatalf("dynamic axes/shape: %+v", got)
		}
	}
	tensorResult(t, `return Tensor.new(List.new(),List.new(1000000,1000000,1000000,1000000,0))`, []string{"1000000", "1000000", "1000000", "1000000", "0"}, []string{})
	tensorResult(t, `flow Identity() -> Tensor<FP32>! { a=Tensor.new(List.new(-0.0),List.new()); return Tensor.sum(a,List.new()) }; return Identity()`, []string{}, []string{"-0"})
	got := run(t, "run", sourceFile(t, `flow Empty() -> Tensor<FP32>! { a=Tensor.new(List.new(),List.new(0,0)); return Tensor.mean(a,List.new(0)) }; return Empty()`), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, `"kind":"Tensor.EmptyReduction"`) {
		t.Fatalf("empty mean: %+v", got)
	}
}

func TestTensorLimits(t *testing.T) {
	for _, source := range []string{
		`return Tensor.new(List.new(),List.new(0,1,1,1,1,1,1,1,1))`,
		`return Tensor.new(List.new(),List.new(0,1000001))`,
		`flow Large() -> Tensor<FP32>! { a=Tensor.new(List.new(1.0),List.new()); return Tensor.broadcast(a,List.new(1001,1000)) }; return Large()`,
		`flow Large() -> List<Float>! { a=Tensor.new(List.new(1.0),List.new()); b=Tensor.broadcast(a,List.new(1000000)); return Tensor.values(b) }; return Large()`,
	} {
		got := run(t, "run", sourceFile(t, source), "--format", "json", "--max-steps", "10000000")
		if got.status != 2 || got.stdout != "" || diagnostics(t, got.stderr).Diagnostics[0].Code != "runtime.limit" {
			t.Fatalf("tensor limit: %+v", got)
		}
	}
	got := run(t, "run", sourceFile(t, `flow Work() -> Tensor<FP32>! { a=Tensor.new(List.new(1.0),List.new()); b=Tensor.broadcast(a,List.new(1000,1000)); return Tensor.matmul(b,b) }; return Work()`), "--format", "json", "--max-steps", "1100000")
	if got.status != 2 || !strings.Contains(got.stderr, "tensor work steps") {
		t.Fatalf("matmul preflight: %+v", got)
	}
}

func TestTensorDiagnosticSnapshots(t *testing.T) {
	source := `flow Fail(a: Tensor<FP32>) -> Tensor<FP32>! { bad=Tensor.sqrt(a); later=Tensor.div(a,Tensor.new(List.new(0.0),List.new())); return later }
flow Outer() -> Tensor<FP32>! { a=Tensor.new(List.new(1.0,-1.0,-2.0),List.new(3)); return Fail(a) }; return Outer()`
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "row-major element 1") || strings.Contains(got.stderr, `"values":`) || strings.Contains(got.stderr, `Numeric.DivideByZero`) || !strings.Contains(got.stderr, `"tensor_type"`) || !strings.Contains(got.stderr, `"trace"`) {
		t.Fatalf("snapshot/source order: %+v", got)
	}
	source = `flow Empty() -> List<Tensor<FP32>> { return List.new() }
flow Handle(e: Error) -> Error { return e }
flow Report() -> Error onError Handle { a=Tensor.new(List.new(1.0),List.new()); nested=List.new(Empty(),List.new(a)); return Error.new("Custom","snapshot",Record.new(nested: nested)) }; return Report()`
	v := successfulValue(t, runValue(t, source))
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "Custom" || !strings.Contains(string(encoded), "tensor_type") || strings.Contains(string(encoded), `"values"`) {
		t.Fatalf("nested snapshot: %s", encoded)
	}
}

func TestTensorForwardLoss(t *testing.T) {
	// Independent arithmetic: hidden rows [1,2], [2,1], [0,1];
	// predictions [9,8,4], targets [8,6,2], squared errors [1,4,4].
	data, err := os.ReadFile(filepath.Join(repository, "examples/tensor_forward.lo"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	tensorResult(t, source, []string{}, []string{"3"})
}

func TestTensorAnalysisScope(t *testing.T) {
	// The tensor analysis budget does not constrain a program with no tensors.
	var calls strings.Builder
	calls.WriteString("flow Consume(n: Int) -> Int { items=List.new(" + strings.Repeat("1,", 100) + "1); return 0 }\nreturn List.new(")
	for i := 0; i < 1100; i++ {
		if i > 0 {
			calls.WriteString(",")
		}
		fmt.Fprintf(&calls, "Consume(%d)", i)
	}
	calls.WriteString(")")
	got := run(t, "check", sourceFile(t, calls.String()), "--format", "json")
	if got.status != 0 {
		t.Fatalf("unrelated integer list acquired a tensor analysis limit: %s", got.stderr)
	}
	bounded := run(t, "check", sourceFile(t, "flow TensorInput(a: Tensor<FP32>) -> Int { return 0 }\n"+calls.String()), "--format", "json")
	if bounded.status != 1 || diagnostics(t, bounded.stderr).Diagnostics[0].Code != "check.tensor-analysis-limit" {
		t.Fatalf("tensor analysis budget: %+v", bounded)
	}
	// Shared nested containers must not expand exponentially in flow cache keys.
	var source strings.Builder
	source.WriteString("flow Consume(data: Record) -> Int { return 0 }\nv0=List.new(1)\n")
	for i := 1; i <= 24; i++ {
		fmt.Fprintf(&source, "v%d=List.new(v%d,v%d)\n", i, i-1, i-1)
	}
	source.WriteString("flow Unused(a: Tensor<FP32>) -> Int { return 0 }\nreturn Consume(Record.new(data:v24))")
	got = run(t, "check", sourceFile(t, source.String()), "--format", "json")
	if got.status != 0 {
		t.Fatalf("shared metadata graph: %s", got.stderr)
	}
}

func TestTensorPrecisionAndHigherRank(t *testing.T) {
	for _, tc := range []struct {
		body          string
		shape, values []string
	}{
		{`a=Tensor.new(List.new(1.1754943508222875e-38),List.new()); b=Tensor.new(List.new(0.5),List.new()); return Tensor.mul(a,b)`, []string{}, []string{"5.877472e-39"}},
		{`a=Tensor.new(List.new(-1.401298464324817e-45),List.new()); b=Tensor.new(List.new(2.0),List.new()); return Tensor.div(a,b)`, []string{}, []string{"-0"}},
		{`a=Tensor.new(List.new(1.0000001192092896,-1.0),List.new(1,2)); b=Tensor.new(List.new(0.9999998807907104,1.0),List.new(2,1)); return Tensor.matmul(a,b)`, []string{"1", "1"}, []string{"0"}},
		{`a=Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0,7.0,8.0),List.new(2,2,2)); return Tensor.sum(a,List.new(0,2))`, []string{"2"}, []string{"14", "22"}},
		{`a=Tensor.new(List.new(1.0,2.0,3.0,4.0,5.0,6.0,7.0,8.0),List.new(2,2,2)); return Tensor.transpose(a,List.new(2,0,1))`, []string{"2", "2", "2"}, []string{"1", "3", "5", "7", "2", "4", "6", "8"}},
		{`a=Tensor.new(List.new(1.0,2.0),List.new(2,1)); b=Tensor.new(List.new(10.0,20.0,30.0),List.new(1,3)); return Tensor.add(a,b)`, []string{"2", "3"}, []string{"11", "21", "31", "12", "22", "32"}},
	} {
		tensorResult(t, `flow Work() -> Tensor<FP32>! { `+tc.body+` }; return Work()`, tc.shape, tc.values)
	}
}
