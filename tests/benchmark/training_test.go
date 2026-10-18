package benchmark_test

import (
	"context"
	"encoding/json"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type noBoundaries struct{}

func (noBoundaries) Read(context.Context, value.Text) (value.Bytes, error) {
	return nil, execution.Denied("File.read", "training benchmark")
}
func (noBoundaries) List(context.Context, value.Text) (value.List, error) {
	return nil, execution.Denied("File.list", "training benchmark")
}
func (noBoundaries) OpenPaths(context.Context, value.Text) (value.List, error) {
	return nil, execution.Denied("File.open_paths", "training benchmark")
}
func (noBoundaries) Commit(context.Context, value.Target) error {
	return execution.Denied("commit", "training benchmark")
}

var programSink ir.Program
var valueSink value.Value
var bytesSink []byte

func BenchmarkTraining(b *testing.B) {
	for _, fixture := range []struct{ name, path string }{
		{"MVP200", filepath.Join("..", "..", "examples", "training_cpu.lo")},
		{"Dense20", filepath.Join("testdata", "training_dense.lo")},
	} {
		text, err := os.ReadFile(fixture.path)
		if err != nil {
			b.Fatal(err)
		}
		file, err := source.New(fixture.path, string(text))
		if err != nil {
			b.Fatal(err)
		}
		program, d := compiler.Check(file)
		if d != nil {
			b.Fatal(d)
		}
		budget, err := execution.NewBudget(10_000_000)
		if err != nil {
			b.Fatal(err)
		}
		result, err := eval.Run(context.Background(), program, noBoundaries{}, budget, device.DefaultConfig())
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fixture.name+"/Check", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				p, d := compiler.Check(file)
				if d != nil {
					b.Fatal(d)
				}
				programSink = p
			}
		})
		b.Run(fixture.name+"/Execute", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v, err := eval.Run(context.Background(), program, noBoundaries{}, budget, device.DefaultConfig())
				if err != nil {
					b.Fatal(err)
				}
				valueSink = v
			}
		})
		b.Run(fixture.name+"/Serialize", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bytes, err := json.Marshal(result)
				if err != nil {
					b.Fatal(err)
				}
				bytesSink = bytes
			}
		})
	}
}
