package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLLVMRuntimeErrorDepthBudget(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	for _, count := range []int{256, 512, 768, 1024} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := sourceFile(t, fmt.Sprintf(`flow Items(state:Int)->Record{next_state:Int,value:Int}{return Record.new(next_state:state,value:state)}
flow Nest(state:Error,item:Int)->Error{return Error.new("Layer","",Record.new(previous:state))}
return List.fold(List.unfold(0,%d,Items),Error.new("Root","",Record.new()),Nest)`, count))
			expected := runExecutable(t, binary, "run", source, "--format", "json")
			actual := run(t, "run", source, "--format", "json")
			if expected.status != 0 && expected.status != 2 {
				t.Fatalf("interpreter depth %d: %+v", count, expected)
			}
			if expected.status == 2 && !strings.Contains(expected.stderr, `"code":"runtime.limit"`) {
				t.Fatalf("interpreter depth %d did not stop at a logical budget: %+v", count, expected)
			}
			if actual.status != expected.status {
				t.Fatalf("depth %d status: native %d stderr %.500s; interpreter %d stderr %.500s", count, actual.status, actual.stderr, expected.status, expected.stderr)
			}
			if expected.status == 0 {
				if !json.Valid([]byte(actual.stdout)) {
					t.Fatalf("native depth %d returned invalid JSON", count)
				}
				if actual.stdout != expected.stdout || actual.stderr != expected.stderr {
					t.Fatalf("depth %d output differs: native stdout length %d stderr %.500s; interpreter stdout length %d stderr %.500s", count, len(actual.stdout), actual.stderr, len(expected.stdout), expected.stderr)
				}
			} else if actual.stderr != expected.stderr {
				t.Fatalf("depth %d budget differs: native %s; interpreter %s", count, actual.stderr, expected.stderr)
			}
			t.Logf("depth=%d status=%d serialized_bytes=%d", count, actual.status, len(actual.stdout))
		})
	}
}
