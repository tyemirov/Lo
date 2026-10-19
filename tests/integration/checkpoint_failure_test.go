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

func TestCheckpointOptimizerValidation(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "adam.json")
	original := successfulValue(t, runValue(t, checkpointWriteSource("AdamW", path), "--write-root", directory))
	checkNumbers(t, tensorNumbers(t, recordField(t, recordField(t, original, "parameters"), "x")), []float64{.9890000224113464, -1.9880000352859497}, 1e-7, 0)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new, kind string }{
		{"negative step", `"step":{"type":"Int","value":"1"}`, `"step":{"type":"Int","value":"-1"}`, "Checkpoint.InvalidData"},
		{"invalid rate", `"learning_rate":{"type":"Float","value":"0.009999999776482582"}`, `"learning_rate":{"type":"Float","value":"0"}`, "Checkpoint.InvalidData"},
		{"beta power", `"values":["0.9"]`, `"values":["2"]`, "Checkpoint.InvalidData"},
		{"second moment", `"values":["9.9998715e-06","3.9999486e-05"]`, `"values":["-1","3.9999486e-05"]`, "Checkpoint.InvalidData"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := strings.Replace(string(data), tc.old, tc.new, 1)
			if bad == string(data) {
				t.Fatalf("missing fixture %s in %s", tc.old, data)
			}
			setup, typ := checkpointSetup("AdamW")
			text := fmt.Sprintf(`flow Run()->%s!{%s return Checkpoint.decode(Bytes.new(%q),original)};return Run()`, typ, setup, bad)
			got, receipts := runWithDevices(t, context.Background(), text, device.SoftwareFactory{})
			if got.status != 1 || !strings.Contains(got.stderr, tc.kind) {
				t.Fatalf("invalid optimizer: %+v", got)
			}
			assertReleased(t, receipts)
		})
	}
	setup, typ := checkpointSetup("SGD")
	text := fmt.Sprintf(`flow Run()->%s!{%s return Checkpoint.decode(Bytes.new(%q),original)};return Run()`, typ, setup, string(data))
	got, receipts := runWithDevices(t, context.Background(), text, device.SoftwareFactory{})
	if got.status != 1 || !strings.Contains(got.stderr, "Checkpoint.SchemaMismatch") {
		t.Fatalf("wrong optimizer kind: %+v", got)
	}
	assertReleased(t, receipts)
}

func TestCheckpointTransferFailureCleanup(t *testing.T) {
	const text = `flow Main()->Bytes!{
 x=Tensor.new(List.new(1.0,-2.0),List.new(2)); p=Parameters.new(Record.new(x:x));
 state=Optimizer.AdamW.State.new(p,Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1));
 moved=Optimizer.AdamW.State.to(state,Device.new("metal:0"));
 restored=Optimizer.AdamW.State.to(moved,Device.new("cpu"));
 data=Checkpoint.encode(restored); effect=File.Write.new(File.Path.new("never"),data); commit effect; return data
 }; return Main()`
	for _, tc := range []struct {
		name, kind string
		status     int
		configure  func(*metalTestFactory, context.CancelFunc)
	}{
		{"partial upload", "Device.TransferFailed", 1, func(f *metalTestFactory, _ context.CancelFunc) {
			count := 0
			f.upload = func() {
				count++
				if count == 3 {
					f.uploadErr = errors.New("third state upload failed")
				}
			}
		}},
		{"download", "Device.TransferFailed", 1, func(f *metalTestFactory, _ context.CancelFunc) { f.downloadErr = errors.New("state download failed") }},
		{"cancel", "runtime.cancelled", 2, func(f *metalTestFactory, cancel context.CancelFunc) {
			count := 0
			f.upload = func() {
				count++
				if count == 3 {
					cancel()
				}
			}
		}},
		{"allocation budget", "runtime.limit", 2, func(f *metalTestFactory, _ context.CancelFunc) { f.reserved = device.MaxStorage }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			factory := &metalTestFactory{}
			tc.configure(factory, cancel)
			dir := t.TempDir()
			path := filepath.Join(dir, "never")
			program := strings.Replace(text, `"never"`, fmt.Sprintf("%q", path), 1)
			got, receipts := runWithDevices(t, ctx, program, factory, "--allow-device", "metal:0", "--write-root", dir)
			if got.status != tc.status || got.stdout != "" || !strings.Contains(got.stderr, tc.kind) {
				t.Fatalf("checkpoint failure: %+v", got)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("later commit executed: %v", err)
			}
			if factory.live != 0 {
				t.Fatalf("live buffers after failure: %d", factory.live)
			}
			assertReleased(t, receipts)
		})
	}
}

func TestCheckpointBitsAndWork(t *testing.T) {
	for _, tc := range []struct{ elements, shape string }{
		{"1.25,-0.0,1e-45,-2.5", "4"}, {"", "2,0,3"},
	} {
		text := fmt.Sprintf(`flow Run()->Record{before:Bytes,after:Bytes}!{x=Tensor.new(List.new(%s),List.new(%s));p=Parameters.new(Record.new(x:x));before=Checkpoint.encode(p);loaded=Checkpoint.decode(before,p);moved=Parameters.to(loaded,Device.new("metal:0"));returned=Parameters.to(moved,Device.new("cpu"));after=Checkpoint.encode(returned);return Record.new(before:before,after:after)};return Run()`, tc.elements, tc.shape)
		v, _, _ := metalResult(t, text, metalComputeFactory())
		if string(recordField(t, v, "before").Value) != string(recordField(t, v, "after").Value) {
			t.Fatal("checkpoint FP32 bits changed")
		}
	}
	program := func(count int) string {
		return fmt.Sprintf(`flow Run()->Parameters<Record{x:Tensor<FP32>}>!{x=Tensor.broadcast(Tensor.new(List.new(1.0),List.new()),List.new(%d));p=Parameters.new(Record.new(x:x));data=Checkpoint.encode(p);return Checkpoint.decode(data,p)};return Run()`, count)
	}
	small := minimumTensorSteps(t, program(1))
	large := minimumTensorSteps(t, program(100))
	if large <= small+100 {
		t.Fatalf("checkpoint work was not charged: small=%d large=%d", small, large)
	}
}

func TestCheckpointEncodingLimit(t *testing.T) {
	const text = `flow Main()->Bytes!{
 seed=Tensor.new(List.new(-3.4028234663852886e38),List.new());
 expanded=Tensor.broadcast(seed,List.new(1000000));
 parameters=Parameters.new(Record.new(x:expanded));
 return Checkpoint.encode(parameters)
 };return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, device.SoftwareFactory{}, "--max-steps", "10000000")
	if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") || !strings.Contains(got.stderr, "expanded tensor result bytes") {
		t.Fatalf("checkpoint byte limit: %+v", got)
	}
	assertReleased(t, receipts)
}
