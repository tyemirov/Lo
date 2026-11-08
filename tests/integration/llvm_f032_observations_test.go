package integration_test

import (
	"strings"
	"testing"
)

func TestLLVMF032Observations(t *testing.T) {
	runLLVMRuntimeScenarios(t, llvmF032Scenarios())
}

func TestLLVMLinuxF032Observations(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", llvmF032Scenarios())
}

func TestLLVMF032SessionInstallation(t *testing.T) {
	runLLVMRuntimeScenarios(t, f032SessionScenarios())
}

func TestLLVMLinuxF032SessionInstallation(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, "linux/arm64", f032SessionScenarios())
}

func f032SessionScenarios() []llvmRuntimeScenario {
	return []llvmRuntimeScenario{{name: "shared-session-after-startup", source: `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)};s=HTTP.Server.new(34481,Handle,OnError);commit s`, harness: "testdata/native-runtime/host-observations-session.c", transform: func(t *testing.T, module []byte) []byte {
		t.Helper()
		const definition = "define void @lo_flow_0("
		if !strings.Contains(string(module), definition) {
			t.Fatal("missing request callback")
		}
		text := strings.Replace(string(module), definition, "define void @lo_observation_handler(", 1)
		return []byte(text + "\ndeclare void @lo_flow_0(ptr, ptr, i64, i64, ptr)\n")
	}}}
}

func llvmF032Scenarios() []llvmRuntimeScenario {
	const clock = `return Clock.now_unix_ns()`
	const volume = `return File.System.usage(File.Path.new("."))`
	const allocation = `flow Main()->Int!{directory=File.directory(File.Path.new("."));return directory.allocated_bytes};return Main()`
	var scenarios []llvmRuntimeScenario
	for index, fixture := range []struct{ name, source, expected string }{
		{"fresh-clock", `flow Main()->Record{first:Int,second:Int,captured:Bool}!{before=Host.Invocation.current();first=Clock.now_unix_ns();second=Clock.now_unix_ns();after=Host.Invocation.current();return Record.new(first:first,second:second,captured:Math.eq(before.now_unix_ns,after.now_unix_ns))};return Main()`, `"second":{"type":"Int","value":"2000000003"}`},
		{"clock-failure", clock, "Clock.Failed"},
		{"clock-invalid-provider", clock, "runtime.host-failure"},
		{"clock-range", clock, "Clock.Failed"},
		{"volume-reserved-blocks", volume, `"free_bytes":{"type":"Int","value":"4096"}`},
		{"volume-failure", volume, "File.System.UsageFailed"},
		{"volume-invalid-unit", volume, "runtime.host-failure"},
		{"volume-invalid-counts", volume, "runtime.host-failure"},
		{"volume-large-counts", volume, `"total_bytes":{"type":"Int","value":"9444732965739290426880"}`},
		{"allocation", allocation, `"value":"3584"`},
		{"allocation-invalid-provider", allocation, "runtime.host-failure"},
		{"allocation-large", allocation, `"value":"4722366482869645213184"`},
		{"clock-cancellation", clock, "runtime.cancelled"},
		{"volume-cancellation", volume, "runtime.cancelled"},
		{"volume-freshness", `flow Main()->List<Int>!{first=File.System.usage(File.Path.new("."));second=File.System.usage(File.Path.new("."));return List.new(first.free_bytes,second.free_bytes)};return Main()`, `[{"type":"Int","value":"4096"},{"type":"Int","value":"2048"}]`},
		{"logical-removal-receipt", `flow Keep(entry:File.Entry)->Bool{return String.eq(entry.name,"entry")};flow Main()->Int!{write=File.Write.new(File.Path.new("entry"),Bytes.new("abc"));commit write;entries=List.filter(File.list(File.Path.new(".")),Keep);target=File.Remove.new(entries);receipt=commit target;return receipt.observed_bytes};return Main()`, `"value":"3"`},
		{"volume-replaced-root", `return File.System.usage(File.Path.new("root"))`, "runtime.capability-denied"},
		{"uninstalled-late-provider", `return 0`, `"value":"0"`},
		{"foreign-owner", clock, `"value":"1000000003"`},
		{"clock-quota", clock, "runtime.limit"},
		{"volume-quota", volume, "runtime.limit"},
		{"volume-close-failure", volume, "runtime.host-failure"},
		{"negative-clock", clock, `"value":"-999999997"`},
	} {
		scenarios = append(scenarios, llvmRuntimeScenario{name: fixture.name, source: fixture.source, number: index, harness: "testdata/native-runtime/host-observations.c", transform: func(t *testing.T, module []byte) []byte {
			t.Helper()
			return []byte(strings.Replace(string(module), "define void @lo_entry(", "define void @lo_qualified_entry(", 1))
		}, verify: func(t *testing.T, actual result) {
			if actual.status != 0 || actual.stderr != "" || !strings.Contains(actual.stdout, fixture.expected) {
				t.Fatalf("F032 %s: %+v; expected %s", fixture.name, actual, fixture.expected)
			}
			if fixture.name == "fresh-clock" && (!strings.Contains(actual.stdout, `"first":{"type":"Int","value":"1000000003"}`) || !strings.Contains(actual.stdout, `"captured":{"type":"Bool","value":true}`)) {
				t.Fatalf("fresh clock changed invocation: %s", actual.stdout)
			}
			if fixture.name == "volume-reserved-blocks" && (!strings.Contains(actual.stdout, `"total_bytes":{"type":"Int","value":"20480"}`) || !strings.Contains(actual.stdout, `"used_bytes":{"type":"Int","value":"12288"}`)) {
				t.Fatalf("volume arithmetic: %s", actual.stdout)
			}
		}})
	}
	return scenarios
}
