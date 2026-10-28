package integration_test

import (
	encodingbinary "encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestLLVMHostInvocationInjectedInputs(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmHostInvocationScenarios(t))
}

func TestLLVMLinuxHostInvocationInjectedInputs(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmHostInvocationScenarios(t))
}

func llvmHostInvocationScenarios(t *testing.T) []llvmRuntimeScenario {
	t.Helper()
	body := encodingbinary.BigEndian.AppendUint32(nil, 2)
	for _, input := range []string{"literal", "", repository} {
		body = encodingbinary.BigEndian.AppendUint32(body, uint32(len(input)+1))
		body = append(body, []byte(input)...)
		body = append(body, 0)
	}
	body = encodingbinary.BigEndian.AppendUint32(body, 123)
	body = encodingbinary.BigEndian.AppendUint32(body, 456)
	body = encodingbinary.BigEndian.AppendUint64(body, ^uint64(16))
	valid := fmt.Sprintf(`flow Join(state:String,item:String)->String{return String.concat(state,item)};flow Failed(error:Error)->Bool{return false};flow Main()->Bool onError Failed{initial=Host.Invocation.current();again=Host.Invocation.current();return Match.new(initial.real_uid,123:Match.new(initial.effective_uid,456:Match.new(initial.now_unix_ns,-17:Match.new(List.length(initial.arguments),2:Match.new(File.Path.eq(initial.home,File.Path.new(%q)),true:String.eq(List.fold(again.arguments,"",Join),"literal"),_:false),_:false),_:false),_:false),_:false)};return Main()`, repository)
	failed := `flow Failed(error:Error)->Bool{return String.eq(error.kind,"Host.InvocationFailed")};flow Main()->Bool onError Failed{initial=Host.Invocation.current();return false};return Main()`
	var scenarios []llvmRuntimeScenario
	for number, test := range []struct {
		name, source string
		body         []byte
	}{
		{"captured", valid, body},
		{"truncated", failed, body[:len(body)-1]},
		{"trailing", failed, append(append([]byte{}, body...), 0)},
		{"count-overflow", failed, []byte{0, 0, 16, 1}},
		{"invalid-utf8", failed, []byte{0, 0, 0, 1, 0, 0, 0, 2, 255, 0}},
		{"interior-nul", failed, []byte{0, 0, 0, 1, 0, 0, 0, 3, 0, 'x', 0}},
		{"unused-capture-error", `return true`, []byte{0, 0, 0, 1}},
	} {
		encoded := make([]string, len(test.body))
		for index, octet := range test.body {
			encoded[index] = fmt.Sprintf("%d", octet)
		}
		initializer := strings.Join(encoded, ",")
		scenarios = append(scenarios, llvmRuntimeScenario{name: test.name, source: test.source, number: number, harness: "testdata/native-runtime/host-invocation-input.c", harnessTransform: func(t *testing.T, source []byte) []byte {
			return []byte(strings.ReplaceAll(string(source), "@HOST_BODY@", initializer))
		}})
	}
	return scenarios
}
