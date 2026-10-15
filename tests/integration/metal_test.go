package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

// metalTestFactory models the Metal storage boundary without a GPU dependency.
type metalTestFactory struct {
	observedDeviceFactory
	selectors []string
}

func (f *metalTestFactory) Open(ctx context.Context, selector string) (device.Backend, error) {
	f.selectors = append(f.selectors, selector)
	if selector != "metal:0" {
		return nil, fmt.Errorf("unexpected selected backend %q", selector)
	}
	return f.observedDeviceFactory.Open(ctx, device.Simulation)
}

func TestMetalSelection(t *testing.T) {
	path := sourceFile(t, deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`))
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		kind   string
	}{
		{"denied", nil, 2, "runtime.capability-denied"},
		{"unavailable build", []string{"--allow-device", "metal:0"}, 1, "Device.Unavailable"},
		{"different grant", []string{"--allow-device", "cuda-sim:0"}, 2, "runtime.capability-denied"},
		{"duplicate", []string{"--allow-device", "metal:0", "--allow-device", "metal:0"}, 2, "cli.invalid-invocation"},
		{"multiple", []string{"--allow-device", "metal:0", "--allow-device", "cuda:0"}, 2, "cli.invalid-invocation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, append([]string{"run", path, "--format", "json"}, tc.args...)...)
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" {
				t.Fatalf("want %s/%d: %+v", tc.kind, tc.status, got)
			}
		})
	}
	for _, selector := range []string{"metal", "metal:1", "Metal:0", "mps:0", "auto"} {
		got := run(t, "run", sourceFile(t, fmt.Sprintf(`return Device.new(%q)`, selector)), "--format", "json")
		if got.status != 1 || !strings.Contains(got.stderr, "Device.InvalidSelector") {
			t.Fatalf("invalid selector %s: %+v", selector, got)
		}
	}
}

func TestMetalStaticInspection(t *testing.T) {
	path := sourceFile(t, deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`))
	for _, command := range []string{"parse", "check", "inspect"} {
		got := run(t, command, path, "--format", "json")
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("static %s: %+v", command, got)
		}
		if command == "inspect" {
			for _, fragment := range []string{`"metal:0"`, `"storage_bytes":16`, `"devices":true`, `metal-transfer-fp32`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`} {
				if !strings.Contains(got.stdout, fragment) {
					t.Fatalf("inspection lacks %s: %s", fragment, got.stdout)
				}
			}
		}
	}
}

func TestMetalTransferContract(t *testing.T) {
	for _, tc := range []struct{ name, text, expected string }{
		{"bits and source", deviceSource("metal:0", "Record{source:Tensor<FP32>,result:Tensor<FP32>,location:Device}", `Record.new(source:source,result:Tensor.to(transferred,Device.new("cpu")),location:Tensor.device(transferred))`), `"values":["1.25","-0","1e-45","-2.5"]`},
		{"empty", `flow Main()->Tensor<FP32>! {a=Tensor.new(List.new(),List.new(2,0,3)); b=Tensor.to(a,Device.new("metal:0")); c=Tensor.to(b,Device.new("metal:0"));return Tensor.to(c,Device.new("cpu"))};return Main()`, `"shape":["2","0","3"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := &metalTestFactory{}
			got, receipts := runWithDevices(t, context.Background(), tc.text, factory, "--allow-device", "metal:0")
			if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, tc.expected) {
				t.Fatalf("transfer: %+v", got)
			}
			if tc.name == "bits and source" && strings.Count(got.stdout, tc.expected) != 2 {
				t.Fatalf("source changed: %s", got.stdout)
			}
			transfers := 0
			for _, receipt := range receipts {
				if receipt.Event == device.EventTransfer {
					transfers++
				}
			}
			if transfers != 2 || factory.opens != 1 || factory.live != 0 {
				t.Fatalf("transfer lifetime: count=%d factory=%+v", transfers, factory)
			}
			assertReleased(t, receipts)
		})
	}
	denied := &metalTestFactory{}
	got, receipts := runWithDevices(t, context.Background(), deviceSource("metal:0", "Device", "Tensor.device(transferred)"), denied)
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") || len(denied.selectors) != 0 {
		t.Fatalf("authorization initialized backend: %+v %+v", got, denied)
	}
	assertReleased(t, receipts)
}

func TestMetalControlledFailures(t *testing.T) {
	injected := errors.New("controlled Metal failure")
	for _, tc := range []struct {
		name, kind string
		status     int
		configure  func(*metalTestFactory, context.CancelFunc)
	}{
		{"unavailable", "Device.Unavailable", 1, func(f *metalTestFactory, _ context.CancelFunc) { f.openErr = injected }},
		{"allocate", "Device.AllocationFailed", 1, func(f *metalTestFactory, _ context.CancelFunc) { f.allocateErr = injected }},
		{"upload", "Device.TransferFailed", 1, func(f *metalTestFactory, _ context.CancelFunc) { f.uploadErr = injected }},
		{"download", "Device.TransferFailed", 1, func(f *metalTestFactory, _ context.CancelFunc) { f.downloadErr = injected }},
		{"cancel", "runtime.cancelled", 2, func(f *metalTestFactory, cancel context.CancelFunc) { f.upload = cancel }},
		{"cancel and transfer failure", "runtime.cancelled", 2, func(f *metalTestFactory, cancel context.CancelFunc) { f.upload = cancel; f.uploadErr = injected }},
		{"reserved storage", "runtime.limit", 2, func(f *metalTestFactory, _ context.CancelFunc) { f.reserved = device.MaxStorage }},
		{"cleanup", "runtime.host-failure", 2, func(f *metalTestFactory, _ context.CancelFunc) { f.closeErr = injected }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			factory := &metalTestFactory{}
			tc.configure(factory, cancel)
			dir := t.TempDir()
			path := filepath.Join(dir, "output")
			text := deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`)
			// A transfer failure must prevent the following effect.
			text = strings.Replace(text, `return Tensor.to(transferred,Device.new("cpu"))`, fmt.Sprintf(`restored=Tensor.to(transferred,Device.new("cpu"));effect=File.write(File.Path.new(%q),Bytes.new("published"));commit effect;return restored`, path), 1)
			got, receipts := runWithDevices(t, ctx, text, factory, "--allow-device", "metal:0", "--write-root", dir)
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" {
				t.Fatalf("want %s: %+v", tc.kind, got)
			}
			if tc.name != "cleanup" {
				if _, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("later commit: %v", err)
				}
			}
			if factory.live != 0 || len(factory.selectors) != 1 || factory.selectors[0] != "metal:0" {
				t.Fatalf("backend lifecycle: %+v", factory)
			}
			assertReleased(t, receipts)
		})
	}
}

func TestMetalLostSession(t *testing.T) {
	failure, err := device.NewFault("Device.Lost", errors.New("device removed"))
	if err != nil {
		t.Fatal(err)
	}
	factory := &metalTestFactory{}
	factory.uploadErr = failure
	uploads := 0
	factory.upload = func() { uploads++ }
	text := `flow Stop(error:Error)->String{return error.kind}
flow Retry(error:Error)->String onError Stop {a=Tensor.new(List.new(1.0),List.new());b=Tensor.to(a,Device.new("metal:0"));return "unexpected"}
flow Main()->String onError Retry {a=Tensor.new(List.new(1.0),List.new());b=Tensor.to(a,Device.new("metal:0"));return "unexpected"};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, factory, "--allow-device", "metal:0")
	if got.status != 0 || !strings.Contains(got.stdout, "Device.Lost") || uploads != 1 || factory.opens != 1 {
		t.Fatalf("lost session submitted again: %+v uploads=%d", got, uploads)
	}
	assertReleased(t, receipts)
}

func TestMetalTensorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, typ, expression, kind string
		status                      int
	}{
		{"observation", "List<Float>", "Tensor.values(transferred)", "Tensor.HostObservationRequired", 1},
		{"publication", "Tensor<FP32>", "transferred", "runtime.tensor-publication", 2},
		{"arithmetic pending", "Tensor<FP32>", "Tensor.add(transferred,transferred)", "Device.UnsupportedOperation", 1},
		{"mismatched locations", "Tensor<FP32>", "Tensor.add(source,transferred)", "Tensor.DeviceMismatch", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := &metalTestFactory{}
			got, receipts := runWithDevices(t, context.Background(), deviceSource("metal:0", tc.typ, tc.expression), factory, "--allow-device", "metal:0")
			if got.status != tc.status || !strings.Contains(got.stderr, tc.kind) || got.stdout != "" || factory.downloads != 0 {
				t.Fatalf("tensor boundary: %+v downloads=%d", got, factory.downloads)
			}
			assertReleased(t, receipts)
		})
	}
}

func TestMetalBackendFaultKinds(t *testing.T) {
	for _, kind := range []string{device.UnsupportedProfile, device.LibraryFailed, device.Lost} {
		t.Run(kind, func(t *testing.T) {
			failure, err := device.NewFault(kind, errors.New("controlled backend initialization"))
			if err != nil {
				t.Fatal(err)
			}
			factory := &metalTestFactory{}
			factory.openErr = failure
			got, receipts := runWithDevices(t, context.Background(), deviceSource("metal:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`), factory, "--allow-device", "metal:0")
			if got.status != 1 || !strings.Contains(got.stderr, kind) || !strings.Contains(got.stderr, `"operation":{"type":"String","value":"Tensor.to"}`) || got.stdout != "" {
				t.Fatalf("backend fault: %+v", got)
			}
			if factory.opens != 1 || factory.downloads != 0 {
				t.Fatalf("backend substitution or diagnostic download: %+v", factory)
			}
			assertReleased(t, receipts)
		})
	}
}
