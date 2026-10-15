package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

type builtinExample struct{ name, source, kind string }

func builtinExamples() []builtinExample {
	examples := []builtinExample{
		{"Autodiff.grad", `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Example() -> Record{value: Tensor<FP32>, gradients: Record{x: Tensor<FP32>}}! { x=Tensor.new(List.new(2.0),List.new()); return Autodiff.grad(Record.new(x:x),Loss) }; return Example()`, "Record{gradients: Record{x: Tensor<FP32>}, value: Tensor<FP32>}!"},
		{"Int.new", `return Int.new(123456789012345678901234567890)`, "Int"},
		{"String.new", `return String.new("héllo")`, "String"},
		{"Bool.new", `return Bool.new(false)`, "Bool"},
		{"Float.new", `return Float.new(-1.25e3)`, "Float"},
		{"Float.add", `return Float.add(1.25, 2.5)`, "Float!"},
		{"Float.sub", `return Float.sub(1.25, 2.5)`, "Float!"},
		{"Float.mul", `return Float.mul(1.25, 2.0)`, "Float!"},
		{"Float.div", `return Float.div(3.0, 2.0)`, "Float!"},
		{"Float.sqrt", `return Float.sqrt(2.25)`, "Float!"},
		{"Float.eq", `return Float.eq(-0.0, 0.0)`, "Bool"},
		{"Float.lt", `return Float.lt(1.0, 2.0)`, "Bool"},
		{"Float.lte", `return Float.lte(1.0, 1.0)`, "Bool"},
		{"Float.gt", `return Float.gt(1.0, 2.0)`, "Bool"},
		{"Float.gte", `return Float.gte(1.0, 1.0)`, "Bool"},
		{"Int.to_float", `return Int.to_float(7)`, "Float!"},
		{"Float.to_int", `return Float.to_int(-1.9)`, "Int"},
		{"Float.to_string", `return Float.to_string(-0.0)`, "String"},
		{"Bytes.new", `return Bytes.new("bytes")`, "Bytes"},
		{"Scalar.new", `return Scalar.new("界")`, "Scalar"},
		{"List.new", `return List.new(null, null)`, "List<Null>"},
		{"Map.new", `return Map.new(Record.new(key: true, value: 1))`, "Map<Bool, Int>"},
		{"Record.new", `return Record.new(x: 1, y: "a")`, "Record{x: Int, y: String}"},
		{"Range.new", `return Range.new("a", "z", true, false)`, "Range<String>"},
		{"Error.new", `return Error.new("Custom", "message", Record.new())`, "Error"},
		{"File.Path.new", `return File.Path.new("unread")`, "File.Path"},
		{"HTTP.Server.new", `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; return HTTP.Server.new(8080, H, E)`, "HTTP.Server"},
		{"HTTP.Response.Text.new", `return HTTP.Response.Text.new(200, "ok")`, "HTTP.Response"},
		{"HTTP.Response.Bytes.new", `return HTTP.Response.Bytes.new(200, Bytes.new("ok"))`, "HTTP.Response"},
		{"UTF8.String.new", `return UTF8.String.new(Bytes.new("ok"))`, "String!"},
		{"String.FromScalars.new", `return String.FromScalars.new(List.new(Scalar.new("a")))`, "String"},
		{"String.concat", `return String.concat("a", "b")`, "String"},
		{"String.upper", `return String.upper("a")`, "String"},
		{"String.lower", `return String.lower("A")`, "String"},
		{"String.length", `return String.length("界")`, "Int"},
		{"String.eq", `return String.eq("a", "b")`, "Bool"},
		{"Scalar.eq", `return Scalar.eq(Scalar.new("a"), Scalar.new("b"))`, "Bool"},
		{"Int.to_string", `return Int.to_string(1)`, "String"},
		{"Math.add", `return Math.add(1, 2)`, "Int"},
		{"Math.sub", `return Math.sub(1, 2)`, "Int"},
		{"Math.mul", `return Math.mul(1, 2)`, "Int"},
		{"Math.div", `return Math.div(1, 0)`, "Int!"},
		{"Math.mod", `return Math.mod(1, 0)`, "Int!"},
		{"Math.lt", `return Math.lt(1, 2)`, "Bool"},
		{"Math.lte", `return Math.lte(1, 2)`, "Bool"},
		{"Math.gt", `return Math.gt(1, 2)`, "Bool"},
		{"Math.gte", `return Math.gte(1, 2)`, "Bool"},
		{"Math.eq", `return Math.eq(1, 2)`, "Bool"},
		{"Math.sum", `return Math.sum(List.new(1, 2))`, "Int"},
		{"Math.product", `return Math.product(List.new())`, "Int"},
		{"Math.min", `return Math.min(List.new())`, "Int!"},
		{"Math.max", `return Math.max(List.new())`, "Int!"},
		{"Range.contains", `return Range.contains(Range.new(1, 4, true, true), 3)`, "Bool"},
		{"Range.is_empty", `return Range.is_empty(Range.new(1, 4, true, true))`, "Bool"},
		{"Map.has_key", `return Map.has_key(Map.new(Record.new(key: "k", value: 1)), "k")`, "Bool"},
		{"Map.get", `return Map.get(Map.new(Record.new(key: null, value: 1)), null)`, "Int!"},
		{"Record.merge", `return Record.merge(Record.new(a: 1), Record.new(a: "a"))`, "Record{a: String}"},
		{"List.map", `flow F(x: Int) -> String { return Int.to_string(x) }; return List.map(List.new(1), F)`, "List<String>"},
		{"List.filter", `flow F(x: Scalar) -> Bool { return true }; return List.filter("abc", F)`, "List<Scalar>"},
		{"List.fold", `flow F(acc: Int, x: Int) -> Int { return Math.add(acc, x) }; return List.fold(List.new(1), 0, F)`, "Int"},
		{"List.flat_map", `flow F(x: Int) -> List<Int>! { return List.new(x) }; return List.flat_map(List.new(1), F)`, "List<Int>!"},
		{"List.unfold", `flow F(x: Int) -> Record{value: String, next_state: Int}! { return Record.new(value: Int.to_string(x), next_state: Math.add(x, 1)) }; return List.unfold(0, 2, F)`, "List<String>!"},
		{"List.length", `return List.length("héllo")`, "Int"},
		{"File.read", `return File.read(File.Path.new("missing"))`, "Bytes!"},
		{"File.append", `return File.append(File.Path.new("unwritten"), "data")`, "Effect"},
		{"File.write", `return File.write(File.Path.new("unwritten"), Bytes.new("data"))`, "Effect"},
	}
	examples = append(examples, parameterBuiltinExamples()...)
	examples = append(examples, optimizerBuiltinExamples()...)
	return append(examples, tensorBuiltinExamples()...)
}

func TestVerifierBuiltinContracts(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range builtinExamples() {
		covered[tc.name] = true
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%+v", got)
			}
			var result struct {
				ResultType string `json:"result_type"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
				t.Fatal(err)
			}
			if result.ResultType != tc.kind {
				t.Fatalf("got %s, want %s", result.ResultType, tc.kind)
			}
		})
	}
	for _, op := range catalog.All() {
		if !covered[op.Name()] {
			t.Errorf("no public signature example for %s", op.Name())
		}
	}
}

func tensorBuiltinExamples() []builtinExample {
	examples := []builtinExample{{"Device.new", `return Device.new("cpu")`, "Device!"}, {"Tensor.new", `return Tensor.new(List.new(4.0),List.new())`, "Tensor<FP32>!"}}
	for _, tc := range []struct{ name, expression, typ string }{
		{"Tensor.to", `Tensor.to(a,Device.new("cpu"))`, "Tensor<FP32>"},
		{"Tensor.shape", "Tensor.shape(a)", "List<Int>"},
		{"Tensor.rank", "Tensor.rank(a)", "Int"},
		{"Tensor.dtype", "Tensor.dtype(a)", "String"},
		{"Tensor.device", "Tensor.device(a)", "Device"},
		{"Tensor.to_float", "Tensor.to_float(a)", "Float"},
		{"Tensor.values", "Tensor.values(a)", "List<Float>"},
		{"Tensor.add", "Tensor.add(a,b)", "Tensor<FP32>"},
		{"Tensor.sub", "Tensor.sub(a,b)", "Tensor<FP32>"},
		{"Tensor.mul", "Tensor.mul(a,b)", "Tensor<FP32>"},
		{"Tensor.div", "Tensor.div(a,b)", "Tensor<FP32>"},
		{"Tensor.sqrt", "Tensor.sqrt(a)", "Tensor<FP32>"},
		{"Tensor.relu", "Tensor.relu(a)", "Tensor<FP32>"},
		{"Tensor.matmul", "Tensor.matmul(Tensor.reshape(a,List.new(1,1)),Tensor.reshape(b,List.new(1,1)))", "Tensor<FP32>"},
		{"Tensor.broadcast", "Tensor.broadcast(a,List.new(1))", "Tensor<FP32>"},
		{"Tensor.reshape", "Tensor.reshape(a,List.new(1))", "Tensor<FP32>"},
		{"Tensor.transpose", "Tensor.transpose(a,List.new())", "Tensor<FP32>"},
		{"Tensor.sum", "Tensor.sum(a,List.new())", "Tensor<FP32>"},
		{"Tensor.mean", "Tensor.mean(a,List.new())", "Tensor<FP32>"},
	} {
		examples = append(examples, builtinExample{tc.name, fmt.Sprintf(`flow Example() -> %s! { a=Tensor.new(List.new(4.0),List.new()); b=Tensor.new(List.new(2.0),List.new()); return %s }; return Example()`, tc.typ, tc.expression), tc.typ + "!"})
	}
	return examples
}

func parameterBuiltinExamples() []builtinExample {
	var examples []builtinExample
	for _, tc := range []struct{ name, expression, typ string }{
		{"Parameters.new", "Parameters.new(Record.new(x:x))", "Parameters<Record{x: Tensor<FP32>}>"},
		{"Parameters.values", "Parameters.values(p)", "Record{x: Tensor<FP32>}"},
		{"Gradients.new", "Gradients.new(p,Record.new(x:x))", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.values", "Gradients.values(g)", "Record{x: Tensor<FP32>}"},
		{"Gradients.zero", "Gradients.zero(p)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.add", "Gradients.add(g,g)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.scale", "Gradients.scale(g,0.5)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Autodiff.stop", "Autodiff.stop(x)", "Tensor<FP32>"},
	} {
		examples = append(examples, builtinExample{tc.name, fmt.Sprintf(`flow Example() -> %s! { x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); g=Gradients.new(p,Record.new(x:x)); return %s }; return Example()`, tc.typ, tc.expression), tc.typ + "!"})
	}
	return examples
}

func optimizerBuiltinExamples() []builtinExample {
	var examples []builtinExample
	for _, optimizer := range []string{"SGD", "AdamW"} {
		config := "Optimizer.SGD.Config.new(0.25)"
		if optimizer == "AdamW" {
			config = "Optimizer.AdamW.Config.new(0.25,0.5,0.5,0.5,0.5)"
		}
		configType := "Optimizer." + optimizer + ".Config"
		stateType := "Optimizer." + optimizer + ".State<Record{x: Tensor<FP32>}>"
		examples = append(examples, builtinExample{configType + ".new", "return " + config, configType + "!"})
		for _, op := range []string{"State.new", "step"} {
			name := "Optimizer." + optimizer + "." + op
			expression := "Optimizer." + optimizer + ".State.new(p,config)"
			if op == "step" {
				expression = "Optimizer." + optimizer + ".step(state,g)"
			}
			source := fmt.Sprintf(`flow Example() -> %s! { x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); config=%s; state=Optimizer.%s.State.new(p,config); g=Gradients.new(p,Record.new(x:x)); return %s }; return Example()`, stateType, config, optimizer, expression)
			examples = append(examples, builtinExample{name, source, stateType + "!"})
		}
	}
	return examples
}
