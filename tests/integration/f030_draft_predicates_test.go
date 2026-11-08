package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func f030DraftPredicates(t *testing.T) string {
	t.Helper()
	return cleanupFlows(t, "BuilderFirst", "BuilderSuffix", "BuilderValue", "GatewayDigit", "GatewayDigits", "GatewayName")
}

func TestF030DraftPredicateBounds(t *testing.T) {
	flows := f030DraftPredicates(t)
	for _, tc := range []struct{ name, expression, expected string }{
		{"short-name", `GatewayName("x")`, `false`},
		{"no-digits", fmt.Sprintf(`GatewayName(%q)`, strings.Repeat("x", 11)+".log"), `false`},
		{"one-digit", fmt.Sprintf(`GatewayName(%q)`, strings.Repeat("x", 11)+"1.log"), `true`},
		{"non-digit", fmt.Sprintf(`GatewayName(%q)`, strings.Repeat("x", 11)+"1a.log"), `false`},
		{"empty-builder-line", `BuilderValue("Driver", "")`, `{"pending":{"type":"Bool","value":false},"values":{"type":"List<String>","value":[]}}`},
		{"other-builder-key", `BuilderValue("Driver", "Other: x")`, `{"pending":{"type":"Bool","value":false},"values":{"type":"List<String>","value":[]}}`},
		{"empty-builder-value", `BuilderValue("Driver", "Driver:")`, `{"pending":{"type":"Bool","value":true},"values":{"type":"List<String>","value":[]}}`},
		{"missing-builder-space", `BuilderValue("Driver", "Driver:docker")`, `{"pending":{"type":"Bool","value":false},"values":{"type":"List<String>","value":[]}}`},
		{"first-builder-token", `BuilderValue("Driver", "Driver: docker remote")`, `{"pending":{"type":"Bool","value":false},"values":{"type":"List<String>","value":[{"type":"String","value":"docker"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valueJSON(t, f030Value(t, flows+"return "+tc.expression), tc.expected)
		})
	}
}

func TestF030FallibleElements(t *testing.T) {
	valueJSON(t, f030Value(t, `flow Main()->Int!{return List.at(List.new(Math.div(1,0),Math.div(4,2)),1)};return Main()`), `"2"`)
	valueJSON(t, f030Value(t, `flow Failed(error:Error)->String{return error.kind};flow Main()->String onError Failed{selected=List.at(List.new(Math.div(1,0),Math.div(4,2)),0);return "unexpected success"};return Main()`), `"Math.DivideByZero"`)
	result := runValue(t, `flow Compare(left:Int!,right:Int!)->Int{return 0};return List.sort(List.new(Math.div(1,0),Math.div(4,2)),Compare)`)
	if result.Type != "List<Int!>" {
		t.Fatalf("sort erased element failure tags: %+v", result)
	}
	var items []runtimeValue
	if err := json.Unmarshal(result.Value, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != "error" || items[0].Error == nil || items[0].Error.Kind != "Math.DivideByZero" {
		t.Fatalf("stable tagged elements: %+v", items)
	}
	valueJSON(t, successfulValue(t, items[1]), `"2"`)
}
