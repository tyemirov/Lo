package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/runner"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type observedDeviceFactory struct {
	opens, live, downloads                                 int
	openErr, allocateErr, uploadErr, downloadErr, closeErr error
	upload                                                 func()
	reserved                                               uint64
}

func (f *observedDeviceFactory) Open(ctx context.Context, selector string) (device.Backend, error) {
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	backend, err := (device.SoftwareFactory{}).Open(ctx, selector)
	if err != nil {
		return nil, err
	}
	return &observedDeviceBackend{Backend: backend, factory: f}, nil
}

type observedDeviceBackend struct {
	device.Backend
	factory *observedDeviceFactory
}

func (b *observedDeviceBackend) Allocate(ctx context.Context, size uint64) (device.Buffer, error) {
	if b.factory.allocateErr != nil {
		return nil, b.factory.allocateErr
	}
	buffer, err := b.Backend.Allocate(ctx, size)
	if err != nil {
		return nil, err
	}
	b.factory.live++
	return &observedDeviceBuffer{Buffer: buffer, factory: b.factory}, nil
}

type observedDeviceBuffer struct {
	device.Buffer
	factory *observedDeviceFactory
}

func (b *observedDeviceBuffer) Bytes() uint64 {
	if b.factory.reserved != 0 {
		return b.factory.reserved
	}
	return b.Buffer.Bytes()
}
func (b *observedDeviceBuffer) Upload(ctx context.Context, data []float32) error {
	if b.factory.upload != nil {
		b.factory.upload()
	}
	if b.factory.uploadErr != nil {
		return b.factory.uploadErr
	}
	return b.Buffer.Upload(ctx, data)
}
func (b *observedDeviceBuffer) Download(ctx context.Context, data []float32) error {
	b.factory.downloads++
	if b.factory.downloadErr != nil {
		return b.factory.downloadErr
	}
	return b.Buffer.Download(ctx, data)
}
func (b *observedDeviceBuffer) Close() error {
	b.factory.live--
	return errors.Join(b.Buffer.Close(), b.factory.closeErr)
}

func runWithDevices(t *testing.T, ctx context.Context, text string, factory device.Factory, options ...string) (result, []device.Receipt) {
	t.Helper()
	file, err := source.New("device-test.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	graph, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	program, err := eval.Program(graph)
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"run", "device-test.lo", "--format", "json"}, options...)
	invocation, err := runner.ParseCompiler(args)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	var receipts []device.Receipt
	status := runner.RunContext(ctx, invocation, program, factory, func(r device.Receipt) { receipts = append(receipts, r) }, &stdout, &stderr)
	return result{stdout.String(), stderr.String(), status}, receipts
}

func assertReleased(t *testing.T, receipts []device.Receipt) {
	t.Helper()
	live := map[uint64]bool{}
	for _, r := range receipts {
		switch r.Event {
		case "allocate":
			if live[r.ID] {
				t.Fatal("duplicate allocation")
			}
			live[r.ID] = true
		case "release":
			if !live[r.ID] {
				t.Fatalf("invalid release: %+v", r)
			}
			delete(live, r.ID)
		}
	}
	if len(live) != 0 || len(receipts) == 0 || receipts[len(receipts)-1].Live != 0 {
		t.Fatalf("resources not released: %+v", receipts)
	}
}

func TestDeviceInjectedFailures(t *testing.T) {
	injected := errors.New("controlled backend failure")
	for _, tc := range []struct {
		name, diagnostic string
		status           int
		configure        func(*observedDeviceFactory, context.CancelFunc)
	}{
		{"success", "", 0, func(*observedDeviceFactory, context.CancelFunc) {}},
		{"unavailable", "Device.Unavailable", 1, func(f *observedDeviceFactory, _ context.CancelFunc) { f.openErr = injected }},
		{"allocation", "Device.AllocationFailed", 1, func(f *observedDeviceFactory, _ context.CancelFunc) { f.allocateErr = injected }},
		{"upload", "Device.TransferFailed", 1, func(f *observedDeviceFactory, _ context.CancelFunc) { f.uploadErr = injected }},
		{"download", "Device.TransferFailed", 1, func(f *observedDeviceFactory, _ context.CancelFunc) { f.downloadErr = injected }},
		{"cancel", "runtime.cancelled", 2, func(f *observedDeviceFactory, cancel context.CancelFunc) { f.upload = cancel }},
		{"reserved budget", "runtime.limit", 2, func(f *observedDeviceFactory, _ context.CancelFunc) { f.reserved = device.MaxStorage }},
		{"cleanup", "runtime.host-failure", 2, func(f *observedDeviceFactory, _ context.CancelFunc) { f.closeErr = injected }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			factory := &observedDeviceFactory{}
			tc.configure(factory, cancel)
			text := deviceSource("cuda-sim:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`)
			got, receipts := runWithDevices(t, ctx, text, factory, "--allow-device", "cuda-sim:0")
			if got.status != tc.status || !strings.Contains(got.stderr, tc.diagnostic) {
				t.Fatalf("want %s status %d: %+v", tc.diagnostic, tc.status, got)
			}
			if tc.status != 0 && got.stdout != "" {
				t.Fatal("failure published a result")
			}
			if factory.live != 0 {
				t.Fatalf("backend retains %d buffers", factory.live)
			}
			assertReleased(t, receipts)
		})
	}
}

func TestDeviceDeniedBeforeInitialization(t *testing.T) {
	factory := &observedDeviceFactory{}
	got, receipts := runWithDevices(t, context.Background(), deviceSource("cuda-sim:0", "Device", "Tensor.device(transferred)"), factory)
	if got.status != 2 || !strings.Contains(got.stderr, "runtime.capability-denied") || factory.opens != 0 {
		t.Fatalf("authorization order: %+v opens=%d", got, factory.opens)
	}
	assertReleased(t, receipts)
}

func TestDeviceFailurePreventsCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output")
	text := fmt.Sprintf(`flow Main() -> Int! {
  source=Tensor.new(List.new(1.0),List.new())
  result=Tensor.to(source,Device.new("cuda-sim:0"))
  effect=File.Write.new(File.Path.new(%q),Bytes.new("published"))
  commit effect
  return 1
};return Main()`, path)
	got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{uploadErr: errors.New("transfer failed")}, "--allow-device", "cuda-sim:0", "--write-root", dir)
	if got.status != 1 || !strings.Contains(got.stderr, "Device.TransferFailed") {
		t.Fatalf("transfer failure: %+v", got)
	}
	if _, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failure allowed later commit: %v", err)
	}
	assertReleased(t, receipts)
}

func TestDeviceCallbackLifetime(t *testing.T) {
	text := `flow Counter(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.add(state,1))}
flow Step(state:Tensor<FP32>,index:Int)->Tensor<FP32>! {
  transferred=Tensor.to(state,Device.new("cuda-sim:0"))
  return Tensor.to(transferred,Device.new("cpu"))
}
flow Main()->Tensor<FP32>! {
  initial=Tensor.new(List.new(1.0,2.0,3.0,4.0),List.new(4))
  steps=List.unfold(0,100,Counter)
  return List.fold(steps,initial,Step)
};return Main()`
	actual := run(t, "run", sourceFile(t, text), "--allow-device", "cuda-sim:0", "--format", "json")
	if actual.status != 0 {
		t.Fatalf("CLI callbacks: %+v", actual)
	}
	factory := &observedDeviceFactory{}
	got, receipts := runWithDevices(t, context.Background(), text, factory, "--allow-device", "cuda-sim:0")
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("repeated transfers: %+v", got)
	}
	var peak uint64
	transfers := 0
	for _, r := range receipts {
		peak = max(peak, r.Live)
		if r.Event == "transfer" {
			transfers++
		}
	}
	if transfers != 200 || peak > 64 || factory.opens != 1 {
		t.Fatalf("unbounded callback storage or wrong transfers: count=%d peak=%d opens=%d", transfers, peak, factory.opens)
	}
	assertReleased(t, receipts)
}

func TestDeviceSharedStorageAndIdentity(t *testing.T) {
	text := `flow Main()->Record{a:Tensor<FP32>,b:Tensor<FP32>}! {
  a=Tensor.new(List.new(1.0,2.0,3.0,4.0),List.new(4))
  same=Tensor.to(a,Device.new("cpu"))
  b=Tensor.reshape(same,List.new(2,2))
  return Record.new(a:a,b:b)
};return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{})
	if got.status != 0 {
		t.Fatalf("shared storage: %+v", got)
	}
	allocations := 0
	for _, r := range receipts {
		if r.Event == "allocate" {
			allocations++
		}
		if r.Event == "transfer" {
			t.Fatal("identity transfer copied storage")
		}
	}
	if allocations != 1 {
		t.Fatalf("view allocated %d buffers", allocations)
	}
	assertReleased(t, receipts)
}

func TestDeviceDiagnosticsAndRecovery(t *testing.T) {
	text := `flow Handle(error:Error)->String{return error.kind}
flow Main()->String onError Handle {
 source=Tensor.new(List.new(-0.0),List.new())
 remote=Tensor.to(source,Device.new("cuda-sim:0"))
 observed=Tensor.to_float(remote)
 return "unexpected"
};return Main()`
	actual := run(t, "run", sourceFile(t, text), "--allow-device", "cuda-sim:0", "--format", "json")
	if actual.status != 0 {
		t.Fatalf("CLI callbacks: %+v", actual)
	}
	factory := &observedDeviceFactory{}
	got, receipts := runWithDevices(t, context.Background(), text, factory, "--allow-device", "cuda-sim:0")
	if got.status != 0 || !strings.Contains(got.stdout, "Tensor.HostObservationRequired") || factory.downloads != 0 {
		t.Fatalf("host-only recovery: %+v downloads=%d", got, factory.downloads)
	}
	assertReleased(t, receipts)
	failed := deviceSource("cuda-sim:0", "Tensor<FP32>", `Tensor.to(transferred,Device.new("cpu"))`)
	factory = &observedDeviceFactory{downloadErr: errors.New("controlled download failure")}
	got, receipts = runWithDevices(t, context.Background(), failed, factory, "--allow-device", "cuda-sim:0")
	if got.status != 1 || !strings.Contains(got.stderr, `"device":{"type":"String","value":"cuda-sim:0"}`) || strings.Contains(got.stderr, `"values":`) || factory.downloads != 1 {
		t.Fatalf("metadata diagnostic: %+v downloads=%d", got, factory.downloads)
	}
	assertReleased(t, receipts)
}

func TestDeviceParameterLocations(t *testing.T) {
	text := `flow Main()->Parameters<Record{a:Tensor<FP32>,b:Tensor<FP32>}>! {
 a=Tensor.new(List.new(1.0),List.new())
 b=Tensor.to(a,Device.new("cuda-sim:0"))
 return Parameters.new(Record.new(a:a,b:b))
 };return Main()`
	got := run(t, "run", sourceFile(t, text), "--allow-device", "cuda-sim:0", "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "Gradient.SchemaMismatch") {
		t.Fatalf("mixed parameter locations: %+v", got)
	}
}

func TestDeviceRetainedCallbackValues(t *testing.T) {
	text := `flow Step(state:Tensor<FP32>)->Record{value:Tensor<FP32>,next_state:Tensor<FP32>}! {
  host=Tensor.to(state,Device.new("cpu"))
  next=Tensor.to(host,Device.new("cuda-sim:0"))
  return Record.new(value:state,next_state:next)
 }
 flow Read(value:Tensor<FP32>)->Tensor<FP32>!{return Tensor.to(value,Device.new("cpu"))}
 flow Main()->List<Tensor<FP32>>!{
  host=Tensor.new(List.new(-0.0),List.new())
  initial=Tensor.to(host,Device.new("cuda-sim:0"))
  retained=List.unfold(initial,20,Step)
  return List.map(retained,Read)
 };return Main()`
	got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{}, "--allow-device", "cuda-sim:0")
	if got.status != 0 || strings.Count(got.stdout, `"values":["-0"]`) != 20 {
		t.Fatalf("retained callback values: %+v", got)
	}
	assertReleased(t, receipts)
	native := run(t, "run", sourceFile(t, text), "--allow-device", "cuda-sim:0", "--format", "json")
	if native.status != 0 || native.stdout != got.stdout {
		t.Fatalf("CLI retained values: %+v", native)
	}
}

func TestDeviceTrainingLifetime(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repository, "examples", "training_cpu.lo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, optimizer := range []string{"SGD", "AdamW"} {
		text := string(content)
		if optimizer == "AdamW" {
			text = strings.ReplaceAll(text, "Optimizer.SGD", "Optimizer.AdamW")
			text = strings.ReplaceAll(text, "Config.new(0.05)", "Config.new(0.01,0.9,0.999,0.00000001,0.0)")
		}
		got, receipts := runWithDevices(t, context.Background(), text, &observedDeviceFactory{})
		if got.status != 0 {
			t.Fatalf("%s training resources: %+v", optimizer, got)
		}
		var peak uint64
		var allocated int
		for _, r := range receipts {
			peak = max(peak, r.Live)
			if r.Event == device.EventAllocate {
				allocated++
			}
		}
		if peak > 4096 || allocated < 1000 {
			t.Fatalf("%s storage: peak=%d allocations=%d", optimizer, peak, allocated)
		}
		t.Logf("%s: peak=%d bytes allocations=%d", optimizer, peak, allocated)
		assertReleased(t, receipts)
	}
}

func TestDeviceStorageClasses(t *testing.T) {
	for _, test := range []struct {
		name  string
		class device.Class
		limit uint64
	}{{"tape", device.TapeStorage, device.MaxTape}, {"optimizer", device.OptimizerStorage, device.MaxOptimizer}, {"workspace", device.WorkspaceStorage, device.MaxWorkspace}} {
		t.Run(test.name, func(t *testing.T) {
			var receipts []device.Receipt
			factory := &observedDeviceFactory{reserved: test.limit}
			config, err := device.NewConfig(device.Simulation, factory, func(r device.Receipt) { receipts = append(receipts, r) })
			if err != nil {
				t.Fatal(err)
			}
			session, err := device.NewSession(config)
			if err != nil {
				t.Fatal(err)
			}
			allocation, err := session.Allocate(context.Background(), device.Simulation, 4)
			if err != nil {
				t.Fatal(err)
			}
			if err = session.Classify([]device.Allocation{allocation, allocation}, test.class); err != nil {
				t.Fatal(err)
			}
			if err = session.CheckBudget(test.class, 0); err != nil {
				t.Fatalf("duplicate storage counted twice: %v", err)
			}
			var budget device.BudgetError
			if err = session.CheckBudget(test.class, 4); !errors.As(err, &budget) {
				t.Fatalf("missing class budget: %v", err)
			}
			session.Release(allocation)
			if err = session.CheckBudget(test.class, test.limit); err != nil {
				t.Fatalf("released storage retains class budget: %v", err)
			}
			if err = session.Close(); err != nil {
				t.Fatal(err)
			}
			if factory.live != 0 {
				t.Fatal("backend allocation remains live")
			}
			assertReleased(t, receipts)
		})
	}
}
