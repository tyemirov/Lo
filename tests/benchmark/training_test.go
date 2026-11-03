package benchmark_test

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkTrainingCheck measures only frontend compiler checking.
func BenchmarkTrainingCheck(b *testing.B) {
	for _, fixture := range []struct{ name, path string }{{"MVP200", filepath.Join("..", "..", "examples", "training_cpu.lo")}, {"Dense20", filepath.Join("testdata", "training_dense.lo")}} {
		b.Run(fixture.name, func(b *testing.B) {
			text, err := os.ReadFile(fixture.path)
			if err != nil {
				b.Fatal(err)
			}
			file, err := source.New(fixture.path, string(text))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				program, problem := compiler.Check(file)
				if problem != nil {
					b.Fatal(problem)
				}
				programSink = program
			}
		})
	}
}
