package benchmark_test

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
	"testing"
)

var programSink ir.Program

// BenchmarkCompilerCheck measures frontend Go work, not application execution.
func BenchmarkCompilerCheck(b *testing.B) {
	for _, fixture := range corpus.Cases() {
		b.Run(fixture.Name, func(b *testing.B) {
			file, err := source.New(fixture.Name+".lo", fixture.Source)
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
