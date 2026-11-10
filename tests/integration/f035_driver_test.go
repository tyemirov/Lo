package integration_test

import (
	"fmt"
	"testing"
)

const f035DriverCommand = `context=Process.Context.new(Map.new(),File.Path.new("/work"));executable=Process.Executable.new(File.Path.new("/command"));command=Process.Command.new(context,executable,List.new(),Bytes.new(""));`
const f035Require = `flow Require(value:Bool)->Int!{return Match.new(value,true:0,_:Error.new("Fixture.Failed","invalid result",Record.new()))};`

func TestF035DriverLifecycle(t *testing.T) { runLLVMRuntimeScenarios(t, f035DriverScenarios()) }
func TestF035LinuxDriverLifecycle(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f035DriverScenarios())
}

func TestF035SessionInstallation(t *testing.T) {
	runLLVMRuntimeScenarios(t, f035SessionScenarios())
}
func TestF035LinuxSessionInstallation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f035SessionScenarios())
}
func f035SessionScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{
		{name: "driver-after-peer-evaluation", source: `return 0`, number: 28, harness: "testdata/native-runtime/process-command-driver.c"},
		{name: "limits-after-peer-evaluation", source: `return 0`, number: 29, harness: "testdata/native-runtime/process-command-driver.c"},
	}
}

func TestF035NativeDeadlineBoundaries(t *testing.T) {
	var cases []llvmRuntimeScenario
	for index, duration := range []int{30000, 60000, 180000, 1800000} {
		cases = append(cases, llvmRuntimeScenario{name: fmt.Sprintf("deadline-%d", duration), source: `return 1`, number: index, harness: "testdata/native-runtime/process-deadline.c"})
	}
	runLLVMRuntimeTargetScenarios(t, "darwin/arm64", cases)
}
func TestF035MacCleanupRunMilliseconds(t *testing.T) {
	source := cleanupFlows(t, "Run") + `flow Main()->Int!{context=Process.Context.new(Map.new(),File.Path.new("/work"));executable=Process.Executable.new(File.Path.new("/command"));result=Run(context,executable,List.new(),"capture",30);return Process.Termination.exit_code(result.termination)};return Main()`
	runLLVMRuntimeScenarios(t, []llvmRuntimeScenario{{name: "application-seconds-to-milliseconds", source: source, number: 19, harness: "testdata/native-runtime/process-command-driver.c"}})
}
func f035DriverScenarios() []llvmRuntimeScenario {
	var cases []llvmRuntimeScenario
	for index, duration := range []int{30000, 60000, 180000, 1800000} {
		body := f035DriverCommand + fmt.Sprintf(`timed=Process.Command.with_timeout(command,%d);result=commit timed;return Require(Math.eq(result.stdout_bytes,0))`, duration)
		cases = append(cases, llvmRuntimeScenario{name: fmt.Sprintf("duration-%d", duration), source: f035Require + processContextProgram("Int", body), number: index, harness: "testdata/native-runtime/process-command-driver.c"})
	}
	for _, test := range []struct {
		name    string
		number  int
		forward bool
	}{
		{"excessive-request-before-driver", 4, false},
		{"timeout-after-cleanup-permits-next", 5, false},
		{"timeout-unknown-cleanup-terminal", 6, false},
		{"cancelled-output-terminal", 7, true},
		{"forwarded-binary-owner-thread", 8, true},
		{"partial-sink-failure-terminal", 9, true},
		{"backpressure-then-success", 10, true},
		{"backpressure-deadline-after-cleanup", 11, true},
		{"process-output-limit-terminal", 12, false},
		{"ungranted-destination-before-driver", 13, true},
		{"console-output-limit-terminal", 15, true},
		{"cancelled-after-confirmed-prefix", 16, true},
		{"invalid-driver-output-chunk", 17, true},
		{"ignored-invalid-output-chunk", 20, true},
		{"ignored-output-limit", 21, true},
		{"ignored-invalid-output-stream", 22, true},
		{"output-rejection-overrides-recoverable-receipt", 23, true},
		{"confirmed-output-before-cleanup-expiry", 24, true},
		{"joined-output-producers", 25, true},
		{"rejected-output-starts-cleanup-wait", 26, true},
		{"joined-producers-share-stream-limit", 27, true},
	} {
		output := "capture"
		if test.forward {
			output = "forward"
		}
		timeout := 1000
		if test.number == 4 {
			timeout = 1800001
		}
		if test.number == 11 {
			timeout = 10
		}
		if test.number == 26 {
			timeout = 1800000
		}
		prelude := f035Require + `flow Recover(error:Error)->Bool{return String.eq(error.kind,"Process.Timeout")};flow Attempt(command:Process.Command)->Bool onError Recover{result=commit command;return false};`
		body := f035DriverCommand + fmt.Sprintf(`timed=Process.Command.with_timeout(command,%d);configured=Process.Command.with_output(timed,%q);`, timeout, output)
		if test.number == 5 || test.number == 11 {
			body += `timed_out=Attempt(configured);result=commit command;return Require(timed_out)`
		} else if test.number == 8 || test.number == 10 || test.number == 25 {
			body += `result=commit configured;return Require(Math.eq(Math.add(result.stdout_bytes,result.stderr_bytes),6))`
		} else {
			body += `caught=Attempt(configured);later=commit command;return Require(false)`
		}
		cases = append(cases, llvmRuntimeScenario{name: test.name, number: test.number, source: prelude + processContextProgram("Int", body), harness: "testdata/native-runtime/process-command-driver.c"})
	}
	return cases
}
