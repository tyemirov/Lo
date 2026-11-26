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
	valid := fmt.Sprintf(`flow Join(state:String,item:String)->String{return String.concat(state,item)};flow Failed(error:Error)->Bool{return false};flow Main()->Bool onError Failed{initial=Host.Invocation.current();again=Host.Invocation.current();
    flow MainChoiceTest1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Math.eq(request.selector, 123)
    }

    flow MainChoice2Test1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Math.eq(request.selector, 456)
    }

    flow MainChoice3Test1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Math.eq(request.selector, -17)
    }

    flow MainChoice4Test1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Math.eq(request.selector, 2)
    }

    flow MainChoice5Test1(request: Record{again: Host.Invocation, selector: Bool}) -> Bool {
        return request.selector
    }

    flow MainChoice5Result1(request: Record{again: Host.Invocation, selector: Bool}) -> Bool {
        return String.eq(List.fold(request.again.arguments,"",Join),"literal")
    }

    flow MainChoice5Default(request: Record{again: Host.Invocation, selector: Bool}) -> Bool {
        return false
    }

    flow MainChoice4Result1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Record.new(selector: File.Path.eq(request.initial.home,File.Path.new(%[1]q)), again: request.again) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoice5Test1, MainChoice5Result1)), MainChoice5Default))
    }

    flow MainChoice4Default(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return false
    }

    flow MainChoice3Result1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Record.new(selector: List.length(request.initial.arguments), again: request.again, initial: request.initial) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoice4Test1, MainChoice4Result1)), MainChoice4Default))
    }

    flow MainChoice3Default(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return false
    }

    flow MainChoice2Result1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Record.new(selector: request.initial.now_unix_ns, again: request.again, initial: request.initial) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoice3Test1, MainChoice3Result1)), MainChoice3Default))
    }

    flow MainChoice2Default(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return false
    }

    flow MainChoiceResult1(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return Record.new(selector: request.initial.effective_uid, again: request.again, initial: request.initial) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoice2Test1, MainChoice2Result1)), MainChoice2Default))
    }

    flow MainChoiceDefault(request: Record{again: Host.Invocation, initial: Host.Invocation, selector: Int}) -> Bool {
        return false
    }

    return Record.new(selector: initial.real_uid, again: again, initial: initial) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoiceTest1, MainChoiceResult1)), MainChoiceDefault))};return Main()`, repository)
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
		{"zero-string-length", failed, []byte{0, 0, 0, 1, 0, 0, 0, 0}},
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
