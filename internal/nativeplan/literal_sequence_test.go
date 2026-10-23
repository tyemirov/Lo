package nativeplan

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func literalPlan(t *testing.T, text string) Plan {
	t.Helper()
	file, err := source.New("literal-plan.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := Lower(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func firstSequence(t *testing.T, module *Module) (*Instruction, *Block) {
	t.Helper()
	for block := range module.Functions[0].Blocks {
		candidate := &module.Functions[0].Blocks[block]
		for index := range candidate.Instructions {
			if candidate.Instructions[index].Kind == LiteralSequence {
				return &candidate.Instructions[index], candidate
			}
		}
	}
	t.Fatal("missing checked literal sequence")
	return nil, nil
}

func TestLiteralSequenceEligibilityAndSnapshot(t *testing.T) {
	for _, scenario := range []struct {
		text  string
		count int
	}{
		{"return List.new(0,-1,999999999999999999999999999999)", 1},
		{"return List.new(List.new(1,2),List.new(3))", 2},
		{"item=1;return List.new(item,2)", 0},
		{"return List.new(1,Math.add(2,3))", 0},
		{"return List.new(1.0,2.0)", 0},
		{"flow Empty()->List<Int>{return List.new()};return Empty()", 0},
	} {
		plan := literalPlan(t, scenario.text)
		count := 0
		for _, function := range plan.Snapshot().Functions {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instructions {
					if instruction.Kind == LiteralSequence {
						count++
					}
				}
			}
		}
		if count != scenario.count {
			t.Fatalf("%s: got %d sequences, want %d", scenario.text, count, scenario.count)
		}
	}
	plan := literalPlan(t, "return List.new(1,2)")
	first := plan.Snapshot()
	sequence, _ := firstSequence(t, &first)
	original := sequence.Literals[0]
	sequence.Literals[0].Literal = math.MaxUint64
	first.Functions[0].Values[sequence.OwnedBase-1] = false
	second := plan.Snapshot()
	untouched, _ := firstSequence(t, &second)
	if untouched.Literals[0] != original || !second.Functions[0].Values[untouched.OwnedBase-1] {
		t.Fatal("snapshot changed sealed sequence descriptors or ownership")
	}
}

func TestVerifierRejectsInvalidLiteralSequences(t *testing.T) {
	plan := literalPlan(t, "return List.new(11,22,33)")
	for _, scenario := range []struct {
		name, want string
		mutate     func(*Module, *Instruction, *Block)
	}{
		{"empty", "ownership range", func(_ *Module, sequence *Instruction, _ *Block) { sequence.Literals = nil }},
		{"range overflow", "ownership range", func(_ *Module, sequence *Instruction, _ *Block) { sequence.OwnedBase = math.MaxUint32 }},
		{"range overlaps result", "ownership range", func(_ *Module, sequence *Instruction, _ *Block) { sequence.OwnedBase++ }},
		{"descriptor order", "site order", func(_ *Module, sequence *Instruction, _ *Block) {
			sequence.Literals[0], sequence.Literals[1] = sequence.Literals[1], sequence.Literals[0]
		}},
		{"unknown descriptor site", "site order", func(_ *Module, sequence *Instruction, _ *Block) { sequence.Literals[0].Literal = math.MaxUint64 }},
		{"borrowed slot", "borrowed slot", func(module *Module, sequence *Instruction, _ *Block) {
			module.Functions[0].Values[sequence.OwnedBase-1] = false
		}},
		{"wrong action", "canonical Int", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Literals[0].Literal].Action = "reference"
		}},
		{"wrong literal", "integer literal", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Literals[0].Literal].Text = "1.5"
		}},
		{"wrong coordinates", "source/type/owner", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Literals[0].Literal].Start.Column++
		}},
		{"wrong owner", "source/type/owner", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Literals[0].Step].Owner = "other"
		}},
		{"wrong operation", "operation metadata", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Site].Operation = "List.map"
		}},
		{"wrong argument type", "canonical Int", func(module *Module, sequence *Instruction, _ *Block) {
			module.Locations[sequence.Site].Arguments[0].Type = module.Locations[sequence.Site].Type
		}},
		{"ordinary input", "instruction shape", func(_ *Module, sequence *Instruction, _ *Block) { sequence.Inputs = []ValueID{sequence.OwnedBase} }},
		{"synthetic derivative", "synthetic derivative", func(module *Module, _ *Instruction, _ *Block) { module.Functions[0].Flow = -2 }},
		{"recoverable phase", "nonrecoverable", func(_ *Module, _ *Instruction, block *Block) { block.End.Targets[1] = block.End.Targets[0] }},
		{"wrong invalid site", "shared invalid-status", func(module *Module, sequence *Instruction, block *Block) {
			module.Functions[0].Blocks[block.End.Targets[3]].Instructions[0].Site = sequence.Site
		}},
		{"ordinary operand reads group", "ordinary operand", func(module *Module, sequence *Instruction, block *Block) {
			function := &module.Functions[0]
			value := ValueID(len(function.Values) + 1)
			function.Values = append(function.Values, true)
			block.Instructions = append(block.Instructions, Instruction{Kind: Alias, Value: value, Inputs: []ValueID{sequence.OwnedBase}, Site: sequence.Site})
		}},
		{"partial explicit release", "requires ReleaseAll", func(_ *Module, sequence *Instruction, block *Block) {
			block.Instructions = append(block.Instructions, Instruction{Kind: Release, Inputs: []ValueID{sequence.OwnedBase + 1}, Site: sequence.Site})
		}},
		{"whole explicit release", "requires ReleaseAll", func(_ *Module, sequence *Instruction, block *Block) {
			inputs := []ValueID{}
			for offset := 0; offset < 3*len(sequence.Literals); offset++ {
				inputs = append(inputs, sequence.OwnedBase+ValueID(offset))
			}
			block.Instructions = append(block.Instructions, Instruction{Kind: Release, Inputs: inputs, Site: sequence.Site})
		}},
		{"duplicate canonical type", "noncanonical native type", func(module *Module, _ *Instruction, _ *Block) {
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(module.Metadata, &metadata); err != nil {
				t.Fatal(err)
			}
			var records []typeMetadata
			if err := json.Unmarshal(metadata["types"], &records); err != nil {
				t.Fatal(err)
			}
			records = append(records, typeMetadata{ID: uint64(len(records)), Name: "Int", Kind: "Int"})
			metadata["types"], _ = json.Marshal(records)
			module.Metadata, _ = json.Marshal(metadata)
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			module := plan.Snapshot()
			sequence, block := firstSequence(t, &module)
			scenario.mutate(&module, sequence, block)
			if err := verify(context.Background(), module); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("want %q rejection, got %v", scenario.want, err)
			}
		})
	}
}
