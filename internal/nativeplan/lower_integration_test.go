package nativeplan_test

import (
	"context"
	"errors"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"strings"
	"testing"
)

func TestCheckedProgramsLower(t *testing.T) {
	for _, text := range []string{"return 1\n", "value = 1\n",
		`flow Later(item:Int)->Bool{return Math.eq(item,1)}; return Match.new(1,1:7,Math.add(2,3):8,when Later:9,_:10)`,
		`flow Difference(left:Int,right:Int)->Int{return Math.sub(left,right)}; return Difference(right:Math.mul(2,3),left:Math.add(10,1))`,
		`flow Double(item:Int)->Int{return Math.mul(item,2)}; return List.map(List.new(1,2),Double)`,
		`flow Add(total:Int,item:Int)->Int{return Math.add(total,item)}; return Flow.fold(List.new(1,2),0,Add)`,
		`flow Step(state:Int,item:Int)->Record{state:Int,items:List<Int>,stop:Bool}{return Record.new(state:Math.add(state,item),items:List.new(),stop:false)}; return Flow.worklist(List.new(1,2),0,Step)`,
		`record=Record.new(x:1); return record.x`,
		`target=File.Write.new(File.Path.new("out"),Bytes.new("data")); commit target; return 1`, "flow Divide(x: Int) -> Int! { return Math.div(8, x) }\nreturn Divide(2)\n"} {
		t.Run(text, func(t *testing.T) {
			file, err := source.New("fixture.lo", text)
			if err != nil {
				t.Fatal(err)
			}
			program, diagnostic := compiler.Check(file)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			plan, err := nativeplan.Lower(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"darwin/arm64", "linux/arm64"} {
				if _, err := llvmemit.Emit(context.Background(), plan, target); err != nil {
					t.Fatal(err)
				}
			}
			if len(plan.Snapshot().Functions) == 0 {
				t.Fatal("missing entry")
			}
		})
	}
}

func TestAllConversions(t *testing.T) {
	fallibleInt, _ := types.NewFallible(types.Int())
	listScalar, _ := types.NewList(types.Scalar())
	fallibleRecord, _ := types.NewFallible(types.AnyRecord())
	for _, tc := range []struct {
		name, text string
		kind       ir.ConversionKind
		out        types.Type
	}{
		{"success", "return 1", ir.Success, fallibleInt},
		{"failure", `return Error.new("code","message",Record.new())`, ir.Failure, fallibleInt},
		{"propagate", "return Math.div(8,2)", ir.Propagate, types.Int()},
		{"record_view", "return Record.new(x:1)", ir.RecordView, types.AnyRecord()},
		{"scalar_list_view", `return "hello"`, ir.ScalarListView, listScalar},
		{"fallible_record_view", `flow RecordFlow()->Record{x:Int}!{return Record.new(x:1)}; return RecordFlow()`, ir.FallibleRecordView, fallibleRecord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, _ := source.New("conversion.lo", tc.text)
			program, diagnostic := compiler.Check(file)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			body := program.Body()
			ret := body[len(body)-1].(ir.Return)
			conversion, err := ir.NewConversion(tc.out, ret.Value(), tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			body[len(body)-1], err = ir.NewReturn(conversion, ret.Span())
			if err != nil {
				t.Fatal(err)
			}
			program, err = ir.NewProgram(file, body, program.Flows())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := nativeplan.Lower(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := llvmemit.Emit(context.Background(), plan, "linux/arm64"); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, location := range plan.Snapshot().Locations {
				if location.Action == "convert_"+tc.name {
					found = true
				}
			}
			if !found {
				t.Fatal("conversion omitted")
			}
		})
	}
}

func TestPlanMetadataIsImmutable(t *testing.T) {
	file, _ := source.New("snapshot.lo", `return Record.new(x:1)`)
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := nativeplan.Lower(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := plan.Snapshot()
	for i := range snapshot.Locations {
		if len(snapshot.Locations[i].Labels) > 0 {
			snapshot.Locations[i].Labels[0] = "corrupted"
		}
		if len(snapshot.Locations[i].Arguments) > 0 {
			snapshot.Locations[i].Arguments[0].Kind = "corrupted"
		}
	}
	for _, location := range plan.Snapshot().Locations {
		for _, label := range location.Labels {
			if label == "corrupted" {
				t.Fatal("snapshot mutated sealed labels")
			}
		}
		for _, argument := range location.Arguments {
			if argument.Kind == "corrupted" {
				t.Fatal("snapshot mutated sealed argument metadata")
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := nativeplan.Lower(ctx, program); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	seen := map[uint64]bool{}
	for _, location := range plan.Snapshot().Locations {
		if seen[location.ID] || location.File != "snapshot.lo" || location.Start.Line < 1 {
			t.Fatal("invalid unique source site")
		}
		seen[location.ID] = true
	}
	if !strings.Contains(string(plan.Snapshot().Metadata), "lo-native-3") {
		t.Fatal("missing ABI")
	}
}

func TestMalformedCheckedReferenceRejected(t *testing.T) {
	file, _ := source.New("invalid.lo", "return missing")
	ref, err := ir.NewReference(types.Int(), "missing", file.Span(0, 14))
	if err != nil {
		t.Fatal(err)
	}
	ret, err := ir.NewReturn(ref, ref.Span())
	if err != nil {
		t.Fatal(err)
	}
	program, err := ir.NewProgram(file, []ir.Statement{ret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativeplan.Lower(context.Background(), program); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("invalid checked reference accepted: %v", err)
	}
}

func TestNamedArgumentSourceOrder(t *testing.T) {
	file, _ := source.New("order.lo", `flow F(left:Int,right:Int)->Int{return Math.sub(left,right)}; return F(right:Math.mul(2,3),left:Math.add(10,1))`)
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := nativeplan.Lower(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	literals := []string{}
	for _, location := range plan.Snapshot().Locations {
		if location.Owner == "<top>" && location.Action == "literal" {
			literals = append(literals, location.Text)
		}
	}
	if strings.Join(literals, ",") != "2,3,10,1" {
		t.Fatalf("argument source order changed: %v", literals)
	}
}
