package integration_test

import (
	"encoding/json"
	"fmt"

	"testing"
)

func TestLLVMRuntimeErrorDepthBudget(t *testing.T) {
	for _, count := range []int{256, 512, 768, 1024} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`flow Items(state:Int)->Record{next_state:Int,value:Int}{return Record.new(next_state:state,value:state)}
flow Nest(state:Error,item:Int)->Error{return Error.new("Layer","",Record.new(previous:state))}
return List.fold(List.unfold(0,%d,Items),Error.new("Root","",Record.new()),Nest)`, count))
			actual := runProgram(t, source, "run", "--format", "json")
			nativeBudgetOrSuccess(t, actual)
			if actual.status == 0 {
				value := nativeValue(t, actual)
				for layer := 0; layer < count; layer++ {
					if value.Type != "Error" || value.Kind != "Layer" || value.Input == nil {
						t.Fatalf("depth %d lost error layer %d: %+v", count, layer, value)
					}
					var fields map[string]runtimeValue
					if err := json.Unmarshal(value.Input.Value, &fields); err != nil {
						t.Fatal(err)
					}
					value = fields["previous"]
				}
				if value.Kind != "Root" {
					t.Fatalf("depth %d lost root: %+v", count, value)
				}
			}
			t.Logf("depth=%d status=%d serialized_bytes=%d", count, actual.status, len(actual.stdout))
		})
	}
}
