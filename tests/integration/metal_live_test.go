//go:build metal && darwin && arm64 && cgo

package integration_test

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/device"
)

func TestMetalLivePrimitives(t *testing.T)  { metalPrimitives(t, device.DefaultFactory) }
func TestMetalLiveDerivatives(t *testing.T) { metalDerivatives(t, device.DefaultFactory) }

func TestMetalLiveTransfers(t *testing.T) {
	bits := []uint32{0, 0x80000000, 1, 0x80000001, 0x007fffff, 0x807fffff, 0x00800000, 0x80800000, 0x7f7fffff, 0xff7fffff}
	state := uint32(127)
	for len(bits) < 513 {
		state = state*1664525 + 1013904223
		if state&0x7f800000 != 0x7f800000 {
			bits = append(bits, state)
		}
	}
	values := make([]string, len(bits))
	for i, pattern := range bits {
		text := strconv.FormatFloat(float64(math.Float32frombits(pattern)), 'g', -1, 32)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		values[i] = text
	}
	for _, tc := range []struct{ name, values, shape string }{
		{"finite edges and samples", strings.Join(values, ","), "513"},
		{"empty", "", "2,0,3"},
		{"scalar", "-0.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := `flow Main()->Tensor<FP32>! {a=Tensor.new(List.new(` + tc.values + `),List.new(` + tc.shape + `));b=Tensor.to(a,Device.new("metal:0"));same=Tensor.to(b,Device.new("metal:0"));return Tensor.to(same,Device.new("cpu"))};return Main()`
			path := sourceFile(t, text)
			actual := runExecutable(t, filepath.Join(repository, "bin", "lo-metal"), "run", path, "--allow-device", "metal:0", "--format", "json")
			cpu := runExecutable(t, binary, "run", sourceFile(t, strings.ReplaceAll(text, "metal:0", "cpu")), "--format", "json")
			if actual.status != 0 || actual.stderr != "" || cpu.status != 0 || actual.stdout != cpu.stdout {
				t.Fatalf("Metal/CPU transfer mismatch: actual=%+v expected=%+v", actual, cpu)
			}
			got, receipts := runWithDevices(t, context.Background(), text, device.DefaultFactory(), "--allow-device", "metal:0")
			if got.status != 0 || got.stdout != cpu.stdout {
				t.Fatalf("observed Metal execution: %+v", got)
			}
			transfers, opens := 0, 0
			var peak uint64
			for _, r := range receipts {
				peak = max(peak, r.Live)
				if r.Event == device.EventTransfer {
					transfers++
				}
				if r.Event == device.EventOpen {
					opens++
					if r.Backend == nil || r.Backend.Profile != device.MetalProfile || r.Backend.Name == "" || r.Backend.RegistryID == "" || r.Backend.System == "" || len(r.Backend.KernelSHA256) != 64 {
						t.Fatalf("missing device identity: %+v", r)
					}
					identity, _ := json.Marshal(r.Backend)
					t.Logf("Metal identity: %s", identity)
				}
				if r.Event == device.EventAllocate && r.Device == device.Metal && tc.name == "finite edges and samples" && r.Bytes < 2*uint64(len(bits))*4 {
					t.Fatalf("staging storage not charged: %+v", r)
				}
			}
			if transfers != 2 || opens != 1 {
				t.Fatalf("unexpected transfers=%d opens=%d", transfers, opens)
			}
			t.Logf("transfers=%d peak_reserved_bytes=%d", transfers, peak)
			assertReleased(t, receipts)
		})
	}
}

func TestMetalLiveLifetime(t *testing.T) {
	text := `flow Counter(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.add(state,1))}
flow Step(state:Tensor<FP32>,index:Int)->Tensor<FP32>! {remote=Tensor.to(state,Device.new("metal:0"));return Tensor.to(remote,Device.new("cpu"))}
flow Main()->Tensor<FP32>!{a=Tensor.new(List.new(1.25,-0.0,1.401298464324817e-45,-2.5),List.new(4));steps=List.unfold(0,100,Counter);return List.fold(steps,a,Step)};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, device.DefaultFactory(), "--allow-device", "metal:0")
	if got.status != 0 || !strings.Contains(got.stdout, `"values":["1.25","-0","1e-45","-2.5"]`) {
		t.Fatalf("transfer loop: %+v", got)
	}
	transfers := 0
	var peak uint64
	for _, r := range receipts {
		peak = max(peak, r.Live)
		if r.Event == device.EventTransfer {
			transfers++
		}
	}
	if transfers != 200 || peak > 65536 {
		t.Fatalf("transfer loop lifetime: count=%d peak=%d", transfers, peak)
	}
	t.Logf("100 steps: transfers=%d peak_reserved_bytes=%d", transfers, peak)
	assertReleased(t, receipts)
}

func TestMetalLiveOptimizers(t *testing.T) { metalOptimizers(t, device.DefaultFactory) }
func TestMetalLiveProfile(t *testing.T)    { metalNumericProfile(t, device.DefaultFactory) }

func TestMetalLiveTraining(t *testing.T)        { metalTraining(t, device.DefaultFactory) }
func TestMetalLiveNumericFailures(t *testing.T) { metalNumericFailures(t, device.DefaultFactory) }

func TestMetalLiveCLIComputation(t *testing.T) {
	path := filepath.Join(repository, "examples", "metal_arithmetic.lo")
	for _, command := range []string{"check", "inspect", "run"} {
		args := []string{command, path, "--format", "json"}
		if command == "run" {
			args = append(args, "--allow-device", "metal:0")
		}
		got := runExecutable(t, filepath.Join(repository, "bin", "lo-metal"), args...)
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("Metal CLI %s: %+v", command, got)
		}
		if command == "run" && !strings.Contains(got.stdout, `"values":["0.8","-1.6"]`) {
			t.Fatalf("Metal CLI result: %s", got.stdout)
		}
	}
}
