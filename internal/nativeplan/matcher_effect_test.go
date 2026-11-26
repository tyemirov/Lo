package nativeplan_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestMatcherReadConversionLowers(t *testing.T) {
	text := `
flow Positive(value: Int) -> Bool { return Math.gt(value, 0) }
flow Keep(value: Int) -> Int { return value }
flow Case() -> Match.Case<Int, Int, Read> { return Match.Case.new(Positive, Keep) }
flow Build() -> Match<Int, Int, Read> { return Match.new(List.new(), Keep) }
flow Apply(value: Int, matcher: Match<Int, Int, Read>) -> Int { return value |> Match.apply(matcher) }
return Apply(1, Build())
`
	file, err := source.New("matcher-read.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := nativeplan.Lower(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	conversions := 0
	for _, location := range plan.Snapshot().Locations {
		if location.Action == "convert_matcher_read_view" {
			conversions++
		}
	}
	if conversions != 2 {
		t.Fatalf("matcher/case effect widening sites = %d, want 2", conversions)
	}
	for _, target := range []string{"darwin/arm64", "linux/arm64"} {
		module, err := llvmemit.Emit(context.Background(), plan, target)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(module), "@lo_convert_matcher_read_view(") {
			t.Fatal("matcher effect conversion is absent from emitted module")
		}
	}
}
