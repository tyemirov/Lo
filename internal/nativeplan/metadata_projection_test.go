package nativeplan_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

// These fingerprints are frozen from the ABI1 producer before metadata compaction.
// A wire-schema change must not replace them with new producer observations.
const metadataProjectionLocationsSHA256 = "414590974e125c985537e4726308d1918107b1e36a3e0f267e027e8ac8a63a18"
const metadataProjectionTablesSHA256 = "4d0c5528af7b6df9d6716ad13a9b1697e2181e0cbea2807163a73745f0ac9c14"
const metadataProjectionArtifact = `{"arbitrary":{"nested":[null,true,18446744073709551615,1.25]},"source":{"path":"exact/λ.lo"},"unknown":"retained"}`

type metadataProjectionPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}
type metadataProjectionArgument struct {
	Kind string `json:"kind"`
	Type uint64 `json:"type"`
	Flow uint64 `json:"flow"`
}

// Keep an independent, explicit projection of all seventeen original fields.
type metadataProjectionLocation struct {
	ID              uint64                       `json:"id"`
	File            string                       `json:"file"`
	Start           metadataProjectionPosition   `json:"start"`
	End             metadataProjectionPosition   `json:"end"`
	Action          string                       `json:"action"`
	Type            uint64                       `json:"type"`
	Text            string                       `json:"text"`
	FloatBits       string                       `json:"float_bits"`
	Field           string                       `json:"field"`
	Flow            uint64                       `json:"flow"`
	Operation       string                       `json:"operation"`
	Labels          []string                     `json:"labels"`
	Owner           string                       `json:"owner"`
	Arguments       []metadataProjectionArgument `json:"arguments"`
	DerivativePlan  uint64                       `json:"derivative_plan"`
	DerivativeNode  uint64                       `json:"derivative_node"`
	DerivativeFrame uint64                       `json:"derivative_frame"`
}
type metadataProjectionFixture struct {
	name, text string
	conversion *struct {
		kind   ir.ConversionKind
		output types.Type
	}
}

func metadataProjectionFixtures(t *testing.T) []metadataProjectionFixture {
	t.Helper()
	texts := []string{
		"return 1\n", "value=1\n",
		`flow Later(item:Int)->Bool{return Math.eq(item,1)}; return Match.new(1,1:7,Math.add(2,3):8,when Later:9,_:10)`,
		`flow Difference(left:Int,right:Int)->Int{return Math.sub(left,right)}; return Difference(right:Math.mul(2,3),left:Math.add(10,1))`,
		`flow Double(item:Int)->Int{return Math.mul(item,2)}; return List.map(List.new(1,2),Double)`,
		`flow Add(total:Int,item:Int)->Int{return Math.add(total,item)}; return Flow.fold(List.new(1,2),0,Add)`,
		`record=Record.new(x:1); return record.x`,
		`target=File.Write.new(File.Path.new("out"),Bytes.new("data")); commit target; return 1`,
		"flow Divide(x:Int)->Int!{return Math.div(8,x)};return Divide(2)",
		`integer=-2; positive=3; floating=-0.0; text="λ🙂"; truth=true; lie=false; return integer`,
		`flow Square(x:Tensor<FP32>)->Tensor<FP32>!{return Tensor.mul(x,x)};flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{first=Square(p.x);second=Square(p.x);return Tensor.sum(Tensor.add(first,second),List.new(0))};flow Run()->Tensor<FP32>!{x=Tensor.new(List.new(2.0,3.0),List.new(2));first=Autodiff.grad(Record.new(x:x),Loss);second=Autodiff.grad(Record.new(x:x),Loss);return Tensor.add(first.gradients.x,second.gradients.x)};return Run()`,
	}
	fixtures := []metadataProjectionFixture{}
	for index, text := range texts {
		fixtures = append(fixtures, metadataProjectionFixture{name: fmt.Sprintf("source-%02d", index), text: text})
	}
	fallibleInt, err := types.NewFallible(types.Int())
	if err != nil {
		t.Fatal(err)
	}
	scalarList, err := types.NewList(types.Scalar())
	if err != nil {
		t.Fatal(err)
	}
	fallibleRecord, err := types.NewFallible(types.AnyRecord())
	if err != nil {
		t.Fatal(err)
	}
	for _, conversion := range []struct {
		name, text string
		kind       ir.ConversionKind
		output     types.Type
	}{
		{"success", "return 1", ir.Success, fallibleInt},
		{"failure", `return Error.new("code","message",Record.new())`, ir.Failure, fallibleInt},
		{"propagate", "return Math.div(8,2)", ir.Propagate, types.Int()},
		{"record_view", "return Record.new(x:1)", ir.RecordView, types.AnyRecord()},
		{"scalar_list_view", `return "hello"`, ir.ScalarListView, scalarList},
		{"fallible_record_view", `flow RecordFlow()->Record{x:Int}!{return Record.new(x:1)};return RecordFlow()`, ir.FallibleRecordView, fallibleRecord},
	} {
		fixtures = append(fixtures, metadataProjectionFixture{name: "conversion-" + conversion.name, text: conversion.text, conversion: &struct {
			kind   ir.ConversionKind
			output types.Type
		}{conversion.kind, conversion.output}})
	}
	return fixtures
}
func metadataProjectionJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
func metadataProjectionNormalize(t *testing.T, encoded []byte) json.RawMessage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return metadataProjectionJSON(t, value)
}
func metadataProjectionRows(locations []nativeplan.Location) []metadataProjectionLocation {
	rows := make([]metadataProjectionLocation, len(locations))
	for index, location := range locations {
		arguments := make([]metadataProjectionArgument, len(location.Arguments))
		for argumentIndex, argument := range location.Arguments {
			arguments[argumentIndex] = metadataProjectionArgument{argument.Kind, argument.Type, argument.Flow}
		}
		rows[index] = metadataProjectionLocation{
			ID: location.ID, File: location.File,
			Start:  metadataProjectionPosition{location.Start.Offset, location.Start.Line, location.Start.Column},
			End:    metadataProjectionPosition{location.End.Offset, location.End.Line, location.End.Column},
			Action: location.Action, Type: location.Type, Text: location.Text, FloatBits: location.FloatBits, Field: location.Field, Flow: location.Flow, Operation: location.Operation,
			Labels: append([]string{}, location.Labels...), Owner: location.Owner, Arguments: arguments,
			DerivativePlan: location.DerivativePlan, DerivativeNode: location.DerivativeNode, DerivativeFrame: location.DerivativeFrame,
		}
	}
	return rows
}
func metadataProjectionEmitted(t *testing.T, module []byte) map[string]json.RawMessage {
	t.Helper()
	match := regexp.MustCompile(`(?m)^@lo_program_metadata = constant \[[0-9]+ x i8\] c"((?:\\[0-9A-Fa-f]{2}|[^"\\])*)"`).FindSubmatch(module)
	if match == nil {
		t.Fatal("missing program metadata")
	}
	encoded := match[1]
	decoded := []byte{}
	for index := 0; index < len(encoded); index++ {
		if encoded[index] != '\\' {
			decoded = append(decoded, encoded[index])
			continue
		}
		if index+2 >= len(encoded) {
			t.Fatal("incomplete LLVM metadata escape")
		}
		escaped, err := hex.DecodeString(string(encoded[index+1 : index+3]))
		if err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, escaped[0])
		index += 2
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &root); err != nil {
		t.Fatal(err)
	}
	return root
}
func metadataProjectionTables(t *testing.T, root map[string]json.RawMessage) json.RawMessage {
	t.Helper()
	tables := map[string]json.RawMessage{}
	for _, key := range []string{"source", "types", "flows", "derivatives", "has_result"} {
		value, exists := root[key]
		if !exists {
			t.Fatalf("missing original table %s", key)
		}
		tables[key] = value
	}
	return metadataProjectionNormalize(t, metadataProjectionJSON(t, tables))
}
func TestMetadataProjectionBeforeCompaction(t *testing.T) {
	type rowFixture struct {
		Name string
		Rows []metadataProjectionLocation
	}
	type tableFixture struct {
		Name   string
		Tables json.RawMessage
	}
	allRows := []rowFixture{}
	allTables := []tableFixture{}
	actions := map[string]bool{}
	bodyPlanOne := false
	for _, fixture := range metadataProjectionFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			file, err := source.New("metadata-projection/"+fixture.name+".lo", fixture.text)
			if err != nil {
				t.Fatal(err)
			}
			program, diagnostic := compiler.Check(file)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			if fixture.conversion != nil {
				body := program.Body()
				ret := body[len(body)-1].(ir.Return)
				converted, err := ir.NewConversion(fixture.conversion.output, ret.Value(), fixture.conversion.kind)
				if err != nil {
					t.Fatal(err)
				}
				body[len(body)-1], err = ir.NewReturn(converted, ret.Span())
				if err != nil {
					t.Fatal(err)
				}
				program, err = ir.NewProgram(file, body, program.Flows())
				if err != nil {
					t.Fatal(err)
				}
			}
			plan, err := nativeplan.Lower(context.Background(), program)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := plan.Snapshot()
			rows := metadataProjectionRows(snapshot.Locations)
			var originalRoot map[string]json.RawMessage
			if err := json.Unmarshal(snapshot.Metadata, &originalRoot); err != nil {
				t.Fatal(err)
			}
			tables := metadataProjectionTables(t, originalRoot)
			allRows = append(allRows, rowFixture{fixture.name, rows})
			allTables = append(allTables, tableFixture{fixture.name, tables})
			for _, row := range rows {
				actions[row.Action] = true
				if row.Action == "body" && row.Owner == "<derivative>" && row.DerivativePlan == 1 {
					bodyPlanOne = true
				}
			}
			var firstMetadata []byte
			for _, target := range []string{"darwin/arm64", "linux/arm64"} {
				var previous []byte
				for repeat := 0; repeat < 2; repeat++ {
					module, err := llvmemit.Emit(context.Background(), plan, target)
					if err != nil {
						t.Fatal(err)
					}
					root := metadataProjectionEmitted(t, module)
					if _, exists := root["artifact"]; exists {
						t.Fatal("Emit without artifact added artifact metadata")
					}
					wireRows := metadataProjectionDecode(t, root)
					if !bytes.Equal(metadataProjectionJSON(t, wireRows), metadataProjectionJSON(t, rows)) {
						t.Fatal("wire rows differ from complete typed location projection")
					}
					if !bytes.Equal(metadataProjectionTables(t, root), tables) {
						t.Fatal("emission changed original source/type/flow/derivative tables")
					}
					current := metadataProjectionJSON(t, root)
					if firstMetadata == nil {
						firstMetadata = current
					} else if !bytes.Equal(firstMetadata, current) {
						t.Fatal("targets changed canonical program metadata")
					}
					if repeat > 0 && !bytes.Equal(previous, current) {
						t.Fatal("metadata emission is nondeterministic")
					}
					previous = current
				}
				module, err := llvmemit.EmitArtifact(context.Background(), plan, target, json.RawMessage(metadataProjectionArtifact))
				if err != nil {
					t.Fatal(err)
				}
				root := metadataProjectionEmitted(t, module)
				if !bytes.Equal(metadataProjectionNormalize(t, root["artifact"]), metadataProjectionNormalize(t, []byte(metadataProjectionArtifact))) {
					t.Fatal("original artifact JSON value changed")
				}
				if !bytes.Equal(metadataProjectionTables(t, root), tables) {
					t.Fatal("artifact emission changed original tables")
				}
			}
			t.Logf("locations=%d", len(rows))
		})
	}
	expected := strings.Fields("body case cause charge_value commit convert_failure convert_fallible_record_view convert_propagate convert_record_view convert_scalar_list_view convert_success derivative_call derivative_enter derivative_failure derivative_finish derivative_host_failure derivative_loss derivative_node derivative_retain derivative_reverse derivative_seed derivative_snapshot derivative_zero expression_step flow flow_call flow_enter flow_ref literal match match_result operation parameter predicate project reference scope_enter statement_step")
	actual := []string{}
	for action := range actions {
		actual = append(actual, action)
	}
	slices.Sort(actual)
	if !slices.Equal(actual, expected) {
		t.Errorf("canonical action union=%v; want %v", actual, expected)
	}
	if !bodyPlanOne {
		t.Error("missing second synthetic derivative Body plan provenance")
	}
	for _, projection := range []struct {
		name     string
		value    any
		expected string
	}{
		{"locations", allRows, metadataProjectionLocationsSHA256}, {"tables", allTables, metadataProjectionTablesSHA256},
	} {
		observed := fmt.Sprintf("%x", sha256.Sum256(metadataProjectionJSON(t, projection.value)))
		if observed != projection.expected {
			t.Errorf("frozen %s SHA256=%s; want %s", projection.name, observed, projection.expected)
		}
	}
}

func metadataProjectionDecode(t *testing.T, root map[string]json.RawMessage) []metadataProjectionLocation {
	t.Helper()
	if string(root["abi"]) != `"lo-native-2"` {
		t.Fatal("projection requires canonical ABI2")
	}
	var files []string
	var spans []struct {
		FileIndex  uint64 `json:"file_index"`
		Start, End metadataProjectionPosition
	}
	var rows []struct {
		Span    uint64
		Action  string
		Type    uint64
		Owner   string
		Flow    uint64
		Payload json.RawMessage
	}
	for _, field := range []struct {
		data json.RawMessage
		out  any
	}{{root["files"], &files}, {root["spans"], &spans}, {root["locations"], &rows}} {
		if err := json.Unmarshal(field.data, field.out); err != nil {
			t.Fatal(err)
		}
	}
	result := make([]metadataProjectionLocation, len(rows))
	for index, row := range rows {
		if row.Span >= uint64(len(spans)) || spans[row.Span].FileIndex >= uint64(len(files)) {
			t.Fatal("invalid canonical span reference")
		}
		span := spans[row.Span]
		projected := metadataProjectionLocation{ID: uint64(index), File: files[span.FileIndex], Start: span.Start, End: span.End, Action: row.Action, Type: row.Type, Owner: row.Owner, Flow: row.Flow, Labels: []string{}, Arguments: []metadataProjectionArgument{}}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		read := func(key string, out any) {
			value, exists := payload[key]
			if !exists {
				t.Fatalf("missing canonical payload field %s for %s", key, row.Action)
			}
			if err := json.Unmarshal(value, out); err != nil {
				t.Fatal(err)
			}
		}
		switch {
		case row.Action == "literal":
			read("text", &projected.Text)
			if value, exists := payload["float_bits"]; exists {
				if err := json.Unmarshal(value, &projected.FloatBits); err != nil {
					t.Fatal(err)
				}
			}
		case row.Action == "project":
			read("field", &projected.Field)
		case row.Action == "operation":
			read("operation", &projected.Operation)
			read("labels", &projected.Labels)
			read("arguments", &projected.Arguments)
		case row.Action == "body" || strings.HasPrefix(row.Action, "derivative_"):
			read("derivative_plan", &projected.DerivativePlan)
			read("derivative_node", &projected.DerivativeNode)
			read("derivative_frame", &projected.DerivativeFrame)
			if row.Action != "body" {
				read("operation", &projected.Operation)
				read("labels", &projected.Labels)
				read("arguments", &projected.Arguments)
			}
		}
		result[index] = projected
	}
	return result
}
