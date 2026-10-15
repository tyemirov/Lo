package benchmark_test

import (
	"context"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
	"testing"
)

func BenchmarkNativeBaseline(b *testing.B) {
	for _, fixture := range corpus.Cases() {
		b.Run(fixture.Name, func(b *testing.B) {
			file, err := source.New(fixture.Name+".lo", fixture.Source)
			if err != nil {
				b.Fatal(err)
			}
			p, d := compiler.Check(file)
			if d != nil {
				b.Fatal(d)
			}
			budget, err := execution.NewBudget(10000000)
			if err != nil {
				b.Fatal(err)
			}
			result, err := eval.Run(context.Background(), p, noBoundaries{}, budget, device.DefaultConfig())
			if err != nil || value.TextResult(result) != fixture.Expected {
				b.Fatalf("result %v error %v, want %s", result, err, fixture.Expected)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := eval.Run(context.Background(), p, noBoundaries{}, budget, device.DefaultConfig())
				if err != nil {
					b.Fatal(err)
				}
				valueSink = result
			}
		})
	}
}
