package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func deviceSource(selector, outputType, expression string) string {
	return fmt.Sprintf(`flow Main() -> %s! {
  source = Tensor.new(List.new(1.25, -0.0, 1.401298464324817e-45, -2.5), List.new(2, 2))
  selected = Device.new(%q)
  transferred = Tensor.to(source, selected)
  return %s
}; return Main()`, outputType, selector, expression)
}

func TestDeviceTransferRoundTrip(t *testing.T) {
	source := deviceSource("cuda-sim:0", "Record{source: Tensor<FP32>, result: Tensor<FP32>, location: Device}", `Record.new(source: source, result: Tensor.to(transferred, Device.new("cpu")), location: Tensor.device(transferred))`)
	got := run(t, "run", sourceFile(t, source), "--allow-device", "cuda-sim:0", "--format", "json")
	if got.status != 0 || got.stderr != "" || !json.Valid([]byte(got.stdout)) {
		t.Fatalf("transfer round trip: %+v", got)
	}
	if strings.Count(got.stdout, `"values":["1.25","-0","1e-45","-2.5"]`) != 2 || !strings.Contains(got.stdout, `"value":"cuda-sim:0"`) {
		t.Fatalf("transfer changed source, bits, or location: %s", got.stdout)
	}
}

func TestDeviceAuthorizationAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, selector, grant, typ, expression, diagnostic string
		status                                             int
	}{
		{"denied simulation", "cuda-sim:0", "", "Device", "Tensor.device(transferred)", "runtime.capability-denied", 2},
		{"denied CUDA", "cuda:0", "", "Device", "Tensor.device(transferred)", "runtime.capability-denied", 2},
		{"unavailable CUDA", "cuda:0", "cuda:0", "Device", "Tensor.device(transferred)", "Device.Unavailable", 1},
		{"different grant", "cuda:0", "cuda-sim:0", "Device", "Tensor.device(transferred)", "runtime.capability-denied", 2},
		{"observation", "cuda-sim:0", "cuda-sim:0", "List<Float>", "Tensor.values(transferred)", "Tensor.HostObservationRequired", 1},
		{"publication", "cuda-sim:0", "cuda-sim:0", "Tensor<FP32>", "transferred", "runtime.tensor-publication", 2},
		{"nested publication", "cuda-sim:0", "cuda-sim:0", "Record{items: List<Tensor<FP32>>}", "Record.new(items: List.new(transferred))", "runtime.tensor-publication", 2},
		{"arithmetic pending", "cuda-sim:0", "cuda-sim:0", "Tensor<FP32>", "Tensor.add(transferred, transferred)", "Device.UnsupportedOperation", 1},
		{"location mismatch", "cuda-sim:0", "cuda-sim:0", "Tensor<FP32>", "Tensor.add(source, transferred)", "Tensor.DeviceMismatch", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"run", sourceFile(t, deviceSource(tc.selector, tc.typ, tc.expression)), "--format", "json"}
			if tc.grant != "" {
				args = append(args, "--allow-device", tc.grant)
			}
			got := run(t, args...)
			if got.status != tc.status || got.stdout != "" || !strings.Contains(got.stderr, tc.diagnostic) {
				t.Fatalf("want %s status %d: %+v", tc.diagnostic, tc.status, got)
			}
		})
	}
}

func TestDeviceGrantArguments(t *testing.T) {
	for _, options := range [][]string{{"--allow-device"}, {"--allow-device", "cpu"}, {"--allow-device", "cuda:1"}, {"--allow-device", "cuda:0", "--allow-device", "cuda-sim:0"}, {"--allow-device", "cuda:0", "--allow-device", "cuda:0"}} {
		got := run(t, append([]string{"run", sourceFile(t, "return 1"), "--format", "json"}, options...)...)
		if got.status != 2 || !strings.Contains(got.stderr, "cli.invalid-invocation") {
			t.Fatalf("grant arguments: %+v", got)
		}
	}
}

func TestDeviceStaticInspection(t *testing.T) {
	source := deviceSource("cuda:0", "Tensor<FP32>", `Tensor.to(transferred, Device.new("cpu"))`)
	path := sourceFile(t, source)
	if got := run(t, "check", path); got.status != 0 {
		t.Fatalf("device-free check: %+v", got)
	}
	got := run(t, "inspect", path, "--format", "json")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("device-free inspection: %+v", got)
	}
	for _, field := range []string{`"device_resources"`, `"transfers"`, `"cuda:0"`, `"storage_bytes":16`, `"devices":true`} {
		if !strings.Contains(got.stdout, field) {
			t.Fatalf("inspection lacks %s: %s", field, got.stdout)
		}
	}
}

func TestDeviceCPUIdentity(t *testing.T) {
	source := deviceSource("cpu", "Tensor<FP32>", "transferred")
	got := run(t, "run", sourceFile(t, source), "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"device":"cpu"`) {
		t.Fatalf("CPU identity: %+v", got)
	}
}

func TestDeviceHTTPRequests(t *testing.T) {
	text := `flow Handle(request:HTTP.Request)->HTTP.Response! {
  source=Tensor.new(List.new(-0.0),List.new())
  remote=Tensor.to(source,Device.new("cuda-sim:0"))
  host=Tensor.to(remote,Device.new("cpu"))
  scalar=Tensor.to_float(host)
  return HTTP.Response.Text.new(200,Float.to_string(scalar))
 }
 flow OnError(error:Error)->HTTP.Response {return HTTP.Response.Text.new(500,error.kind)}
 ` + serveSuffix
	process := startServer(t, text, "--allow-device", "cuda-sim:0")
	for i := 0; i < 3; i++ {
		status, body := response(t, process, "GET", "/", "")
		if status != 200 || body != "-0" {
			t.Fatalf("device request: %d %q", status, body)
		}
	}
	stopServer(t, process)
}

func TestDeviceEmptyAndIdentity(t *testing.T) {
	text := `flow Main()->Tensor<FP32>! {
  source=Tensor.new(List.new(),List.new(2,0,3))
  remote=Tensor.to(source,Device.new("cuda-sim:0"))
  same=Tensor.to(remote,Device.new("cuda-sim:0"))
  return Tensor.to(same,Device.new("cpu"))
 };return Main()`
	got := run(t, "run", sourceFile(t, text), "--allow-device", "cuda-sim:0", "--format", "json")
	if got.status != 0 || !strings.Contains(got.stdout, `"shape":["2","0","3"]`) || !strings.Contains(got.stdout, `"values":[]`) {
		t.Fatalf("empty tensor transfer: %+v", got)
	}
}

func TestDeviceInspectionUnknownRequirements(t *testing.T) {
	text := `flow Transfer(value:Tensor<FP32>,selector:String)->Tensor<FP32>! {return Tensor.to(value,Device.new(selector))}
 flow Unused(value:Tensor<FP32>,selector:String)->Tensor<FP32>! {return Transfer(value,selector)}
 return 1`
	path := sourceFile(t, text)
	got := run(t, "inspect", path, "--format", "json")
	if got.status != 0 {
		t.Fatalf("static dynamic target: %+v", got)
	}
	var output struct {
		Report struct {
			Transfers []struct {
				StorageBytes *uint64 `json:"storage_bytes"`
				Target       struct{ Kind string }
			}
			Flows []struct {
				Name    string
				Effects struct{ Devices bool }
			}
		}
	}
	if err := json.Unmarshal([]byte(got.stdout), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Report.Transfers) == 0 {
		t.Fatal("missing unused transfer site")
	}
	for _, site := range output.Report.Transfers {
		if site.StorageBytes != nil || site.Target.Kind == "known" {
			t.Fatalf("dynamic transfer was reported as known: %+v", site)
		}
	}
	for _, flow := range output.Report.Flows {
		if !flow.Effects.Devices {
			t.Fatalf("missing transitive device summary for %s", flow.Name)
		}
	}
	plain := run(t, "inspect", path)
	if plain.status != 0 || strings.Contains(plain.stdout, "0x") || !strings.Contains(plain.stdout, "storage_bytes=unknown") {
		t.Fatalf("text resource metadata: %+v", plain)
	}
}
