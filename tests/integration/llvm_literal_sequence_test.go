package integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

const literalSequenceGradientSource = `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{shaped=Tensor.reshape(p.x,List.new(1,1));summed=Tensor.sum(shaped,List.new(0,1));return Tensor.mul(summed,summed)};flow Run()->Tensor<FP32>!{x=Tensor.new(List.new(2.0),List.new());r=Autodiff.grad(Record.new(x:x),Loss);return r.gradients.x};return Run()`

func TestLLVMLiteralSequenceGradientRelease(t *testing.T) {
	file, err := source.New("literal-gradient.lo", literalSequenceGradientSource)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := nativeplan.Lower(context.Background(), checked)
	if err != nil {
		t.Fatal(err)
	}
	module := plan.Snapshot()
	groups := 0
	ordinaryGroups := 0
	retainedOutputs := 0
	for _, function := range module.Functions {
		if function.Flow != -2 {
			if function.Name == "Loss" {
				for _, block := range function.Blocks {
					for _, instruction := range block.Instructions {
						if instruction.Kind == nativeplan.LiteralSequence {
							ordinaryGroups++
						}
					}
				}
			}
			continue
		}
		aliases := map[nativeplan.ValueID]bool{}
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				if instruction.Kind == nativeplan.LiteralSequence {
					t.Fatal("synthetic derivative arguments must remain ordinary references")
				}
				if instruction.Kind == nativeplan.Alias {
					aliases[instruction.Value] = true
				}
			}
		}
		// Autodiff emits literal nodes separately and rebuilds List.new using
		// references. Its existing reference temporary Release stays ordinary.
		for _, block := range function.Blocks {
			var temporaryRelease []nativeplan.ValueID
			for _, instruction := range block.Instructions {
				if instruction.Kind == nativeplan.Release {
					temporaryRelease = instruction.Inputs
				}
				if instruction.Kind == nativeplan.Call && instruction.Symbol == "lo_derivative_retain" && len(temporaryRelease) != 0 {
					for _, released := range temporaryRelease {
						if released == instruction.Inputs[0] {
							t.Fatal("derivative node output released before tape retain")
						}
					}
					retainedOutputs++
				}
				if instruction.Kind != nativeplan.Call || instruction.Symbol != nativeplan.OperationSymbol("List.new") || len(instruction.Inputs) != 2 {
					continue
				}
				if !aliases[instruction.Inputs[0]] || !aliases[instruction.Inputs[1]] {
					t.Fatal("derivative List.new arguments must be reference aliases")
				}
				first, last := instruction.Inputs[0]-1, instruction.Inputs[1]+1
				if last-first+1 != 6 {
					t.Fatalf("noncontiguous shape literal temporary range: %d..%d", first, last)
				}
				found := false
				for _, releaseBlock := range function.Blocks {
					for _, release := range releaseBlock.Instructions {
						if release.Kind != nativeplan.Release {
							continue
						}
						for index, id := range release.Inputs {
							if id != first || index+6 > len(release.Inputs) {
								continue
							}
							for offset := 0; offset < 6; offset++ {
								if release.Inputs[index+offset] != first+nativeplan.ValueID(offset) {
									t.Fatalf("reordered derivative temporary group: %v", release.Inputs)
								}
							}
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("literal range %d..%d has no explicit derivative temporary Release", first, last)
				}
				groups++
			}
		}
	}
	if groups < 2 || ordinaryGroups < 2 || retainedOutputs == 0 {
		t.Fatalf("expected ordinary Loss literals, derivative reference groups and retained output: ordinary=%d reference=%d retained=%d", ordinaryGroups, groups, retainedOutputs)
	}
}

func TestLLVMLiteralSequenceGroupedLifetime(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	checkNumbers(t, tensorNumbers(t, successfulValue(t, runValue(t, literalSequenceGradientSource))), []float64{4}, 0, 0)
	const nested = `flow Run()->Tensor<FP32>!{dims=Match.new(true,true:List.new(1,1),_:List.new(2,2));nested=List.new(List.new(11,22),List.new(33));return Tensor.reshape(Tensor.new(List.new(2.0),List.new()),dims)};return Run()`
	checkNumbers(t, tensorNumbers(t, successfulValue(t, runValue(t, nested))), []float64{2}, 0, 0)
	// Compute a derivative first, then cancel during a later nested group's
	// literal prefix. The existing numerical harness checks repeated sessions,
	// tape/scope cleanup, escaped output lifetime, and zero live owners.
	partial := `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.mul(p.x,p.x)};flow Run()->Tensor<FP32>!{x=Tensor.new(List.new(2.0),List.new());r=Autodiff.grad(Record.new(x:x),Loss);nested=List.new(List.new(11,22),List.new(` + strings.Repeat("1,", 299) + `1));return r.gradients.x};return Run()`
	scenarios := []llvmRuntimeScenario{
		{name: "gradient-retained-node-output", source: literalSequenceGradientSource, number: 0},
		{name: "gradient-budget-cleanup", source: literalSequenceGradientSource, number: 2},
		{name: "nested-and-untouched-branch-groups", source: nested, number: 5},
		{name: "nested-partial-prefix-cancellation", source: partial, number: 3},
	}
	for index := range scenarios {
		scenarios[index].harness = "testdata/native-runtime/numerical.c"
	}
	var first, last uint64
	scenarios[3].transform = func(t *testing.T, module []byte) []byte {
		metadata := struct{ Locations []nativeplan.Location }{metadataLocations(t, locationMetadataBytes(t, module))}
		start := strings.Index(partial, strings.Repeat("1,", 299))
		count := 0
		for index, location := range metadata.Locations {
			if location.Action == "literal" && location.Text == "1" && location.Start.Offset >= start {
				if metadata.Locations[index-1].Action != "expression_step" || metadata.Locations[index+1].Action != "charge_value" {
					t.Fatal("literal prefix does not have its original step/literal/charge sites")
				}
				if count == 0 {
					first = location.ID - 1
				}
				last = location.ID + 1
				count++
			}
		}
		if count != 300 || last-first+1 != 900 {
			t.Fatalf("selected nested literal prefix: count=%d sites=%d..%d", count, first, last)
		}
		text := string(module)
		for _, symbol := range []string{"step", "literal", "charge_value"} {
			text = strings.ReplaceAll(text, "call void @lo_"+symbol+"(", "call void @lo_grouped_"+symbol+"(")
			text += "\ndeclare void @lo_grouped_" + symbol + "(ptr,ptr,i64,i64,ptr)\n"
		}
		return []byte(text)
	}
	scenarios[3].harnessTransform = func(t *testing.T, harness []byte) []byte {
		if first == 0 || last <= first {
			t.Fatal("missing selected literal prefix sites")
		}
		text := strings.Replace(string(harness), "int main(void) {", "int literal_sequence_numerical_main(void) {", 1)
		text += "\nstatic U64 first_host_site;\n"
		for _, symbol := range []string{"step", "literal", "charge_value"} {
			text += fmt.Sprintf(`
extern void lo_%[1]s(Context *, const Value *const *, U64, U64, ResultSlot *);
void lo_grouped_%[1]s(Context *c, const Value *const *args, U64 count, U64 site, ResultSlot *out) {
    lo_%[1]s(c,args,count,site,out);
    if (out->status == 2 && !first_host_site) first_host_site=site;
}
`, symbol)
		}
		text += fmt.Sprintf("\nint main(void) { int result=literal_sequence_numerical_main(); if (result) return result; return first_host_site >= %d && first_host_site <= %d ? 0 : 90; }\n", first, last)
		return []byte(text)
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func TestLLVMLiteralSequenceEventsAndCleanup(t *testing.T) {
	scenarios := []llvmRuntimeScenario{literalSequenceScenario([]string{"0", "1", "256"}, 0)}
	large := "1" + strings.Repeat("0", 315654)
	for index := 0; index < 3; index++ {
		items := []string{"0", "1", "256"}
		items[index] = large
		scenarios = append(scenarios, literalSequenceScenario(items, index+1))
	}
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), scenarios)
}

func literalSequenceScenario(items []string, number int) llvmRuntimeScenario {
	return llvmRuntimeScenario{
		name:           fmt.Sprintf("literal-prefix-%d", number),
		source:         "return List.new(\n  " + strings.Join(items, ",\n  ") + "\n)\n",
		number:         number,
		harness:        "testdata/native-qualification/literal-sequence.c",
		expectedStdout: "literal-sequence-prefixes-ok\n",
		transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			metadata := struct{ Locations []nativeplan.Location }{metadataLocations(t, locationMetadataBytes(t, module))}
			expected := []string{"scope_enter", "statement_step", "expression_step",
				"expression_step", "literal", "charge_value", "expression_step", "literal", "charge_value",
				"expression_step", "literal", "charge_value", "operation", "charge_value", "cause"}
			sites := []string{}
			literals := []string{}
			var body uint64
			literalIndex := 0
			literalOffset := len("return List.new(\n  ")
			for _, location := range metadata.Locations {
				if location.Action == "body" {
					body = location.ID
					continue
				}
				if len(sites) >= len(expected) || location.Action != expected[len(sites)] {
					t.Fatalf("literal sequence source action order: %+v", metadata.Locations)
				}
				if location.Action == "literal" {
					literals = append(literals, location.Text)
					if literalIndex >= len(items) || location.Text != items[literalIndex] || location.Start.Offset != literalOffset || location.Start.Line != literalIndex+2 || location.Start.Column != 3 {
						t.Fatalf("original literal runtime site lost text/order/coordinates at index%d", literalIndex)
					}
					literalOffset += len(items[literalIndex]) + len(",\n  ")
					literalIndex++
				}
				if location.Action == "operation" && location.Operation != "List.new" {
					t.Fatalf("unexpected operation: %+v", location)
				}
				sites = append(sites, fmt.Sprintf("i64 %d", location.ID))
			}
			if len(sites) != 15 || strings.Join(literals, ",") != strings.Join(items, ",") {
				t.Fatalf("literal sequence site count%d, literal count%d", len(sites), len(literals))
			}
			text := string(module)
			for _, symbol := range []string{"scope_enter", "step", "literal", "charge_value", "op_List_2Enew", "cause"} {
				wrapper := map[string]string{"scope_enter": "enter", "charge_value": "charge", "op_List_2Enew": "list"}[symbol]
				if wrapper == "" {
					wrapper = symbol
				}
				text = strings.ReplaceAll(text, "call void @lo_"+symbol+"(", "call void @lo_literal_sequence_"+wrapper+"(")
				text += "\ndeclare void @lo_literal_sequence_" + wrapper + "(ptr,ptr,i64,i64,ptr)\n"
			}
			text = strings.ReplaceAll(text, "call void @lo_scope_leave(", "call void @lo_literal_sequence_scope_leave(")
			text = strings.ReplaceAll(text, "call void @lo_value_release(", "call void @lo_literal_sequence_release(")
			text += "\ndeclare void @lo_literal_sequence_scope_leave(ptr,ptr,i64)\ndeclare void @lo_literal_sequence_release(ptr)\n"
			text += fmt.Sprintf("@lo_literal_sequence_sites = constant [15 x i64] [%s]\n@lo_literal_sequence_body_site = constant i64 %d\n", strings.Join(sites, ", "), body)
			return []byte(text)
		},
	}
}

func TestLLVMLiteralSequenceFailureLocations(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	source := "return List.new(\n  11,\n  22,\n  33\n)\n"
	path := sourceFile(t, source)
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build literal step boundaries: %+v", built)
	}
	for index, item := range []string{"11", "22", "33"} {
		t.Run("step-"+item, func(t *testing.T) {
			for lane, args := range map[string][]string{
				"interpreter": {"run", path, "--max-steps", fmt.Sprint(index + 2), "--format", "json"},
				"native":      {"run", "--max-steps", fmt.Sprint(index + 2), "--format", "json"},
			} {
				executable := binary
				if lane == "native" {
					executable = artifact
				}
				got := runExecutable(t, executable, args...)
				assertLiteralSequenceFailure(t, got, strings.Index(source, item), index+2, 3)
			}
		})
	}
	large := "1" + strings.Repeat("0", 315654)
	for index := 0; index < 3; index++ {
		t.Run(fmt.Sprintf("literal-%d", index), func(t *testing.T) {
			items := []string{"11", "22", "33"}
			items[index] = large
			source := "return List.new(\n  " + strings.Join(items, ",\n  ") + "\n)\n"
			path := sourceFile(t, source)
			artifact, built := buildForTest(t, path)
			if built.status != 0 {
				t.Fatalf("build admitted literal limit: %+v", built)
			}
			for lane, args := range map[string][]string{"interpreter": {"run", path, "--format", "json"}, "native": {"run", "--format", "json"}} {
				executable := binary
				if lane == "native" {
					executable = artifact
				}
				assertLiteralSequenceFailure(t, runExecutable(t, executable, args...), strings.Index(source, large), index+2, 3)
			}
		})
	}
}

func assertLiteralSequenceFailure(t *testing.T, got result, offset, line, column int) {
	t.Helper()
	if got.status != 2 || got.stdout != "" {
		t.Fatalf("literal failure: %+v", got)
	}
	diagnostic := diagnostics(t, got.stderr).Diagnostics[0]
	if diagnostic.Code != "runtime.limit" || diagnostic.Span != nil {
		t.Fatalf("terminal limit diagnostic shape changed: %+v (runtime sites separately verify offset%d line%d column%d)", diagnostic, offset, line, column)
	}
}
