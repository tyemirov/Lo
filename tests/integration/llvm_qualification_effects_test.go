package integration_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLLVMFailureStopsCommit(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	fixtures := []struct {
		name, source, kind string
		status             int
	}{
		{"autodiff-handled", `flow Loss(p:Record{x:Tensor<FP32>})->Tensor<FP32>!{return Tensor.sqrt(p.x)};flow Handle(e:Error)->String{return e.kind};flow Work()->String onError Handle{x=Tensor.new(List.new(0.0),List.new());r=Autodiff.grad(Record.new(x:x),Loss);effect=File.Write.new(File.Path.new("never"),Bytes.new("bad"));commit effect;return "unexpected"};return Work()`, "Autodiff.UndefinedDerivative", 0},
	}
	for _, optimizer := range []string{"SGD", "AdamW"} {
		config := "Optimizer.SGD.Config.new(2.0)"
		if optimizer == "AdamW" {
			config = "Optimizer.AdamW.Config.new(0.01,0.9,0.999,1e-8,0.1)"
		}
		for _, failure := range []struct{ name, shape, gradient, kind string }{{"schema", "List.new(1)", "1.0", "Gradient.SchemaMismatch"}, {"overflow", "List.new()", "3e38", "Numeric.NonFinite"}} {
			text := fmt.Sprintf(`flow Run()->Optimizer.%s.State<Record{x:Tensor<FP32>}>!{x=Tensor.new(List.new(1.0),List.new());p=Parameters.new(Record.new(x:x));state=Optimizer.%s.State.new(p,%s);g=Tensor.new(List.new(%s),%s);gp=Parameters.new(Record.new(x:g));gradients=Gradients.new(gp,Record.new(x:g));next=Optimizer.%s.step(state,gradients);effect=File.Write.new(File.Path.new("never"),Bytes.new("bad"));commit effect;return next};return Run()`, optimizer, optimizer, config, failure.gradient, failure.shape, optimizer)
			fixtures = append(fixtures, struct {
				name, source, kind string
				status             int
			}{optimizer + "-" + failure.name, text, failure.kind, 1})
		}
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			directory := t.TempDir()
			output := filepath.Join(directory, "never")
			path := sourceFile(t, strings.ReplaceAll(fixture.source, `"never"`, strconv.Quote(output)))
			artifact, built := buildForTest(t, path)
			if built.status != 0 {
				t.Fatalf("build: %+v", built)
			}
			expected := runExecutable(t, binary, "run", path, "--write-root", directory, "--format", "json")
			actual := runExecutable(t, artifact, "run", "--write-root", directory, "--format", "json")
			if actual != expected || actual.status != fixture.status || !strings.Contains(actual.stdout+actual.stderr, fixture.kind) {
				t.Fatalf("failure order: native=%+v interpreter=%+v", actual, expected)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("later commit reached: %v", err)
			}
		})
	}
}

func TestLLVMPartialServerStartupCleanup(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the LLVM runtime lane")
	}
	directory := t.TempDir()
	receipt := filepath.Join(directory, "started-port")
	text := fmt.Sprintf(`flow H(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow E(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,"bad")};first=HTTP.Server.new(0,H,E);started=commit first;record=File.Write.new(File.Path.new(%q),UTF8.Bytes.new(Int.to_string(started.port)));commit record;second=HTTP.Server.new(-1,H,E);commit second`, receipt)
	path := sourceFile(t, text)
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build: %+v", built)
	}
	for _, command := range [][]string{{binary, "serve", path}, {artifact, "serve"}} {
		got := runExecutable(t, command[0], append(command[1:], "--listen", "0", "--write-root", directory, "--format", "json")...)
		if got.status != 1 || got.stdout != "" || !strings.Contains(got.stderr, "HTTP.ServerStartFailed") {
			t.Fatalf("partial startup: %+v", got)
		}
		data, err := os.ReadFile(receipt)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.ParseUint(string(data), 10, 16)
		if err != nil || port == 0 {
			t.Fatalf("first server receipt: %q %v", data, err)
		}
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", string(data)), time.Second)
		if err == nil {
			connection.Close()
			t.Fatal("partial startup retained listener")
		}
		if err := os.Remove(receipt); err != nil {
			t.Fatal(err)
		}
	}
}
