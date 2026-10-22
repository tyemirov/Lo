package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestLLVMRuntimeRejectsAlteredMetadata(t *testing.T) {
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{
		name:   "operation-disguised-as-flow-call",
		source: `flow F(value: String) -> String { return value }; return Math.add(1, 2)`,
		number: 5,
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			declaration := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`)
			match := declaration.FindSubmatch(module)
			if match == nil {
				t.Fatal("missing emitted metadata")
			}
			data, err := decodeLLVMBytes(match[1])
			if err != nil {
				t.Fatal(err)
			}
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			var sites []map[string]json.RawMessage
			if err := json.Unmarshal(metadata["locations"], &sites); err != nil {
				t.Fatal(err)
			}
			changed := false
			for _, site := range sites {
				if string(site["operation"]) == `"Math.add"` {
					site["action"] = json.RawMessage(`"flow_call"`)
					changed = true
				}
			}
			if !changed {
				t.Fatal("missing Math.add operation site")
			}
			metadata["locations"], err = json.Marshal(sites)
			if err != nil {
				t.Fatal(err)
			}
			data, err = json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			var encoded strings.Builder
			for _, item := range data {
				fmt.Fprintf(&encoded, `\%02X`, item)
			}
			replacement := []byte(fmt.Sprintf(`@lo_program_metadata = constant [%d x i8] c"%s"`, len(data), encoded.String()))
			module = bytes.Replace(module, match[0], replacement, 1)
			size := regexp.MustCompile(`@lo_program_metadata_size = constant i64 [0-9]+`)
			return size.ReplaceAll(module, []byte(fmt.Sprintf("@lo_program_metadata_size = constant i64 %d", len(data))))
		},
	}})
}

func TestLLVMRuntimeRejectsDerivativeSelection(t *testing.T) {
	const source = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); result=Autodiff.grad(Record.new(x:x),Loss); return result.gradients.x }; return Run()`
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{
		name: "valid-control", source: source, number: 0,
	}, {
		name:   "selection-of-processed-result",
		source: source,
		number: 5,
		transform: func(t *testing.T, module []byte) []byte {
			return rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) {
				var plans []map[string]json.RawMessage
				if err := json.Unmarshal(metadata["derivatives"], &plans); err != nil {
					t.Fatal(err)
				}
				if len(plans) != 1 {
					t.Fatalf("expected one derivative plan, got %d", len(plans))
				}
				var selections []map[string]json.RawMessage
				if err := json.Unmarshal(plans[0]["selections"], &selections); err != nil {
					t.Fatal(err)
				}
				if len(selections) != 1 {
					t.Fatal("missing derivative selection")
				}
				selections[0]["node"] = plans[0]["result_node"]
				var err error
				plans[0]["selections"], err = json.Marshal(selections)
				if err != nil {
					t.Fatal(err)
				}
				metadata["derivatives"], err = json.Marshal(plans)
				if err != nil {
					t.Fatal(err)
				}
			})
		},
	}})
}

func TestLLVMRuntimeRejectsDerivativeContracts(t *testing.T) {
	const source = `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Run() -> Tensor<FP32>! { x=Tensor.new(List.new(2.0),List.new()); result=Autodiff.grad(Record.new(x:x),Loss); return result.gradients.x }; return Run()`
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "valid-control", source: source, number: 0}})
	for _, test := range []struct {
		name string
		edit func(*testing.T, map[string]json.RawMessage)
	}{
		{"output-schema", func(t *testing.T, plan map[string]json.RawMessage) {
			var selections []map[string]json.RawMessage
			if err := json.Unmarshal(plan["selections"], &selections); err != nil {
				t.Fatal(err)
			}
			selections[0]["key"] = json.RawMessage(`"changed"`)
			var err error
			plan["selections"], err = json.Marshal(selections)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"operand-provenance", func(t *testing.T, plan map[string]json.RawMessage) {
			var nodes []map[string]json.RawMessage
			if err := json.Unmarshal(plan["nodes"], &nodes); err != nil {
				t.Fatal(err)
			}
			changed := false
			for _, node := range nodes {
				if string(node["operation"]) == `"Tensor.mul"` {
					var operands []int
					if err := json.Unmarshal(node["operand_nodes"], &operands); err != nil {
						t.Fatal(err)
					}
					operands[0] = 1
					var err error
					node["operand_nodes"], err = json.Marshal(operands)
					if err != nil {
						t.Fatal(err)
					}
					changed = true
				}
			}
			if !changed {
				t.Fatal("missing multiplication node")
			}
			var err error
			plan["nodes"], err = json.Marshal(nodes)
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: test.name, source: source, number: 5, transform: func(t *testing.T, module []byte) []byte {
			return rewriteLLVMMetadata(t, module, func(metadata map[string]json.RawMessage) {
				var plans []map[string]json.RawMessage
				if err := json.Unmarshal(metadata["derivatives"], &plans); err != nil {
					t.Fatal(err)
				}
				if len(plans) != 1 {
					t.Fatal("expected one derivative plan")
				}
				test.edit(t, plans[0])
				var err error
				metadata["derivatives"], err = json.Marshal(plans)
				if err != nil {
					t.Fatal(err)
				}
			})
		}}})
	}
}

func rewriteLLVMMetadata(t *testing.T, module []byte, edit func(map[string]json.RawMessage)) []byte {
	t.Helper()
	declaration := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`)
	match := declaration.FindSubmatch(module)
	if match == nil {
		t.Fatal("missing emitted metadata")
	}
	data, err := decodeLLVMBytes(match[1])
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	edit(metadata)
	data, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	for _, item := range data {
		fmt.Fprintf(&encoded, `\%02X`, item)
	}
	replacement := []byte(fmt.Sprintf(`@lo_program_metadata = constant [%d x i8] c"%s"`, len(data), encoded.String()))
	module = bytes.Replace(module, match[0], replacement, 1)
	size := regexp.MustCompile(`@lo_program_metadata_size = constant i64 [0-9]+`)
	return size.ReplaceAll(module, []byte(fmt.Sprintf("@lo_program_metadata_size = constant i64 %d", len(data))))
}
