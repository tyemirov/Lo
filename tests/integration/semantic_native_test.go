package integration_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/codegen"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

type semanticNativeResult struct {
	Result      runtimeValue
	Reads       []string
	Commits     int
	HostCode    string
	ErrorKind   string
	Cancelled   bool
	LiveBuffers int
	Receipts    []device.Receipt
}

const semanticNativeHarness = `package main
import("context";"encoding/json";"errors";"fmt";"os";"github.com/MarcoPoloResearchLab/Lo/internal/device";"github.com/MarcoPoloResearchLab/Lo/internal/execution";"github.com/MarcoPoloResearchLab/Lo/internal/value")
type boundary struct{execution.Boundary;reads []string;commits int;mode string}
func(b *boundary)Read(_ context.Context,path value.Text)(value.Bytes,error){b.reads=append(b.reads,path.Text());if b.mode=="read-error"{return nil,fmt.Errorf("read %s: injected failure",path.Text())};text:=path.Text();if b.mode=="order"{text=fmt.Sprintf("%s:%d;",text,len(b.reads))};return value.NewBytes([]byte(text)),nil}
func(b *boundary)Commit(context.Context,value.Target)(value.Value,error){b.commits++;return nil,fmt.Errorf("unexpected activation")}
type trackedFactory struct{model *metalModel;live int}
func(f *trackedFactory)Open(ctx context.Context,selector string)(device.Backend,error){backend,err:=f.model.Open(ctx,selector);if err!=nil{return nil,err};return &trackedBackend{Backend:backend,factory:f},nil}
type trackedBackend struct{device.Backend;factory *trackedFactory}
func(b *trackedBackend)Allocate(ctx context.Context,size uint64)(device.Buffer,error){buffer,err:=b.Backend.Allocate(ctx,size);if err!=nil{return nil,err};b.factory.live++;return &trackedBuffer{Buffer:buffer,factory:b.factory},nil}
func untracked(buffer device.Buffer)device.Buffer{if buffer==nil{return nil};return buffer.(*trackedBuffer).Buffer}
func(b *trackedBackend)Compute(ctx context.Context,op device.Operation,inputs []device.Buffer,output,status device.Buffer)error{raw:=make([]device.Buffer,len(inputs));for i,input:=range inputs{raw[i]=untracked(input)};return b.Backend.Compute(ctx,op,raw,untracked(output),untracked(status))}
type trackedBuffer struct{device.Buffer;factory *trackedFactory}
func(b *trackedBuffer)Close()error{b.factory.live--;return b.Buffer.Close()}
func main(){
program,err:=New();if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
ctx,cancel:=context.WithCancel(context.Background());defer cancel();mode:=os.Args[1];if mode=="cancel"{cancel()}
model:=&metalModel{};if mode=="device-cancel"{model.before=func(device.Operation)error{cancel();return ctx.Err()}}
factory:=&trackedFactory{model:model};var receipts []device.Receipt;config,err:=device.NewConfig(device.Metal,factory,func(r device.Receipt){receipts=append(receipts,r)});if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
boundary:=&boundary{mode:mode};result,runErr:=program.Run(ctx,boundary,execution.DefaultBudget(),config)
out:=struct{Result value.Value;Reads []string;Commits int;HostCode,ErrorKind string;Cancelled bool;LiveBuffers int;Receipts []device.Receipt}{Result:result,Reads:boundary.reads,Commits:boundary.commits,Cancelled:errors.Is(runErr,context.Canceled),LiveBuffers:factory.live,Receipts:receipts}
if runErr!=nil{var host execution.HostError;var failure execution.Failure;if errors.As(runErr,&host){out.HostCode=string(host.Code())}else if errors.As(runErr,&failure){out.ErrorKind=failure.Value().Kind();out.Result=failure.Value()}else{fmt.Fprintln(os.Stderr,runErr);os.Exit(1)}}
if err:=json.NewEncoder(os.Stdout).Encode(out);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}`

func semanticNativeRun(t *testing.T, text, mode string) semanticNativeResult {
	t.Helper()
	directory := t.TempDir()
	resolvedDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	directory = resolvedDirectory
	relative, err := filepath.Rel(repository, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("native semantic fixture outside repository: %s", directory)
	}
	file, err := source.New(filepath.Join(directory, "program.lo"), text)
	if err != nil {
		t.Fatal(err)
	}
	checked, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	generated, err := codegen.Generate(checked, "main")
	if err != nil {
		t.Fatal(err)
	}
	modelPath, err := filepath.EvalSymlinks(filepath.Join(repository, "tests", "integration", "metal_executor_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	modelRelative, err := filepath.Rel(repository, modelPath)
	if err != nil || strings.HasPrefix(modelRelative, "..") {
		t.Fatalf("Metal model outside repository: %s", modelPath)
	}
	model, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	model = []byte(strings.Replace(string(model), "package integration_test", "package main", 1))
	for name, data := range map[string][]byte{
		"program.go": generated,
		"main.go":    []byte(semanticNativeHarness),
		"model.go":   model,
	} {
		output, err := os.Create(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := output.Write(data)
		closeErr := output.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("write native fixture %s: %v %v", name, writeErr, closeErr)
		}
	}
	artifact := filepath.Join(directory, "semantic-native")
	command := exec.Command("go", "build", "-o", artifact, ".")
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build native semantic fixture: %v\n%s", err, output)
	}
	got := runExecutable(t, artifact, mode)
	if got.status != 0 || got.stderr != "" {
		t.Fatalf("native semantic fixture: %+v", got)
	}
	var result semanticNativeResult
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSemanticNativeReadOrder(t *testing.T) {
	got := semanticNativeRun(t, `flow Read(path: String) -> String! { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
flow Join(a: String,b: String) -> String { return String.concat(a,b) }
flow Main() -> String! { unused=Read("unused");first=Read("repeat");second=Read("repeat");ordered=Join(b:Read("b"),a:Read("a"));return String.concat(first,second,ordered) };return Main()`, "order")
	valueJSON(t, successfulValue(t, got.Result), `"repeat:2;repeat:3;a:5;b:4;"`)
	if !reflect.DeepEqual(got.Reads, []string{"unused", "repeat", "repeat", "b", "a"}) || got.Commits != 0 || got.HostCode != "" || got.ErrorKind != "" {
		t.Fatalf("native read order: %+v", got)
	}
}

func TestSemanticNativeMatchRegions(t *testing.T) {
	got := semanticNativeRun(t, `flow Handle(error: Error) -> String { return "failed" }
flow Read(path: String) -> String onError Handle { bytes=File.read(File.Path.new(path)); return UTF8.String.new(bytes) }
return Match.new(Read("selector"),"miss":Read("unselected"),"selector":Read("selected"),"unreached":Read("unreached-body"),_:Read("default"))`, "match")
	valueJSON(t, got.Result, `"selected"`)
	if !reflect.DeepEqual(got.Reads, []string{"selector", "selected"}) || got.Commits != 0 || got.HostCode != "" || got.ErrorKind != "" {
		t.Fatalf("native match regions: %+v", got)
	}
}

func TestSemanticNativeErrorPrecedence(t *testing.T) {
	got := semanticNativeRun(t, `flow Handle(error: Error) -> Error { return error }
flow Main() -> Error onError Handle { first=File.read(File.Path.new("first"));later=File.read(File.Path.new("later"));return Error.new("missed","",Record.new()) };return Main()`, "read-error")
	if got.Result.Kind != "File.ReadFailed" || len(got.Result.Trace) != 1 || !reflect.DeepEqual(got.Reads, []string{"first"}) || got.Commits != 0 || got.HostCode != "" || got.ErrorKind != "" {
		t.Fatalf("native first failure: %+v", got)
	}
	if got.Result.Input == nil {
		t.Fatal("missing failed read input")
	}
	valueJSON(t, recordField(t, *got.Result.Input, "file_path"), `"first"`)
}

func TestSemanticNativeCancellation(t *testing.T) {
	got := semanticNativeRun(t, `flow Handle(error: Error)->Int{return 99}
flow Main()->Int onError Handle { bytes=File.read(File.Path.new("unread"));target=File.Write.new(File.Path.new("unwritten"),bytes);commit target;return 1 };return Main()`, "cancel")
	if got.HostCode != "runtime.cancelled" || !got.Cancelled || len(got.Reads) != 0 || got.Commits != 0 || got.Result.Type != "" {
		t.Fatalf("native cancellation: %+v", got)
	}
}

func TestSemanticNativeMetalProfile(t *testing.T) {
	got := semanticNativeRun(t, `flow Main()->Record{arithmetic:Tensor<FP32>,layout:Tensor<FP32>,reduction:Tensor<FP32>}!{
 metal=Device.new("metal:0");cpu=Device.new("cpu")
 a=Tensor.to(Tensor.new(List.new(-0.0,1.401298464324817e-45,-1.401298464324817e-45),List.new(3)),metal)
 b=Tensor.to(Tensor.new(List.new(0.0),List.new()),metal)
 arithmetic=Tensor.add(a,b);layout=Tensor.transpose(a,List.new(0))
 values=Tensor.to(Tensor.new(List.new(16777216.0,1.0,-16777216.0),List.new(3)),metal);reduction=Tensor.sum(values,List.new(0))
 return Record.new(arithmetic:Tensor.to(arithmetic,cpu),layout:Tensor.to(layout,cpu),reduction:Tensor.to(reduction,cpu)) };return Main()`, "metal")
	result := successfulValue(t, got.Result)
	for field, expected := range map[string][]uint32{
		"arithmetic": {0, 0, 0},
		"layout":     {0x80000000, 1, 0x80000001},
		"reduction":  {0},
	} {
		numbers := tensorNumbers(t, recordField(t, result, field))
		if len(numbers) != len(expected) {
			t.Fatalf("%s length: %d", field, len(numbers))
		}
		for index, number := range numbers {
			if actual := math.Float32bits(float32(number)); actual != expected[index] {
				t.Fatalf("%s[%d] bits %08x want %08x", field, index, actual, expected[index])
			}
		}
	}
	assertSemanticNativeTransfers(t, got, 6)
}

func assertSemanticNativeTransfers(t *testing.T, got semanticNativeResult, expected int) {
	t.Helper()
	if got.HostCode != "" || got.ErrorKind != "" || got.Commits != 0 || got.LiveBuffers != 0 {
		t.Fatalf("native Metal result: %+v", got)
	}
	transfers := 0
	for _, receipt := range got.Receipts {
		if receipt.Event == device.EventTransfer {
			transfers++
		}
	}
	if transfers != expected {
		t.Fatalf("native explicit transfers: %d want %d", transfers, expected)
	}
	assertReleased(t, got.Receipts)
}

func TestSemanticNativeMetalCheckpointResume(t *testing.T) {
	for _, fixture := range []struct {
		optimizer, config, first, second string
		transfers                        int
	}{
		{"SGD", `Optimizer.SGD.Config.new(0.25)`, semanticOptimizerCheckpoint("SGD", "1.5", "1", "", "", ""), semanticOptimizerCheckpoint("SGD", "1", "2", "", "", ""), 5},
		{"AdamW", `Optimizer.AdamW.Config.new(0.25,0.5,0.5,2.0,0.5)`, semanticOptimizerCheckpoint("AdamW", "1.625", "1", "1", "2", "0.5"), semanticOptimizerCheckpoint("AdamW", "1.296875", "2", "1.5", "3", "0.25"), 13},
	} {
		t.Run(fixture.optimizer, func(t *testing.T) {
			text := fmt.Sprintf(`flow Main()->Bytes!{
 metal=Device.new("metal:0");cpu=Device.new("cpu");x=Tensor.new(List.new(2.0),List.new());p=Parameters.new(Record.new(x:x))
 mp=Parameters.to(p,metal);cp=Parameters.to(mp,cpu);template=Optimizer.%s.State.new(cp,%s)
 restored=Checkpoint.decode(Bytes.new(%q),template);moved=Optimizer.%s.State.to(restored,metal)
 gradient=Tensor.to(x,metal);g=Gradients.new(moved.parameters,Record.new(x:gradient));resumed=Optimizer.%s.step(moved,g)
 host=Optimizer.%s.State.to(resumed,cpu);return Checkpoint.encode(host) };return Main()`, fixture.optimizer, fixture.config, fixture.first, fixture.optimizer, fixture.optimizer, fixture.optimizer)
			got := semanticNativeRun(t, text, "metal")
			assertSemanticCheckpoint(t, got.Result, fixture.second)
			assertSemanticNativeTransfers(t, got, fixture.transfers)
		})
	}
}

func TestSemanticNativeMetalFailureCleanup(t *testing.T) {
	const text = `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(-1.0),List.new()),Device.new("metal:0"));result=Tensor.sqrt(a);target=File.Write.new(File.Path.new("never"),Bytes.new("bad"));commit target;return result};return Main()`
	for _, mode := range []string{"numeric-error", "device-cancel"} {
		t.Run(mode, func(t *testing.T) {
			got := semanticNativeRun(t, text, mode)
			if got.Commits != 0 || len(got.Reads) != 0 || got.LiveBuffers != 0 {
				t.Fatalf("native failure reached effects: %+v", got)
			}
			if mode == "numeric-error" {
				if got.ErrorKind != "Numeric.Domain" || got.HostCode != "" {
					t.Fatalf("native numeric failure: %+v", got)
				}
			} else if got.HostCode != "runtime.cancelled" || !got.Cancelled || got.ErrorKind != "" {
				t.Fatalf("native dispatch cancellation: %+v", got)
			}
			assertReleased(t, got.Receipts)
		})
	}
}
