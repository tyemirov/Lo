package nativeplan

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestVerifierRejectsInvalidOwnershipAndABI(t *testing.T) {
	file, _ := source.New("verify.lo", "value=1; return value")
	program, diagnostic := compiler.Check(file)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	plan, err := Lower(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(*Module)
	}{
		{"body cleanup alias", "operand use after owned cleanup", func(m *Module) {
			f := &m.Functions[0]
			available := ValueID(0)
			for _, block := range f.Blocks {
				for _, instruction := range block.Instructions {
					if instruction.Kind == Call && instruction.Symbol == ScopeEnterSymbol {
						available = instruction.Value
					}
				}
			}
			for bi := range f.Blocks {
				b := &f.Blocks[bi]
				for _, i := range b.Instructions {
					if i.Kind == ScopeLeave {
						b.Instructions = append(b.Instructions, Instruction{Kind: Alias, Site: f.Site, Value: ValueID(len(f.Values) + 1), Inputs: []ValueID{available}})
						f.Values = append(f.Values, true)
						return
					}
				}
			}
		}},
		{"body cleanup boolean", "body cleanup terminator uses released value", func(m *Module) {
			f := &m.Functions[0]
			available := ValueID(0)
			for _, block := range f.Blocks {
				for _, instruction := range block.Instructions {
					if instruction.Kind == Call && instruction.Symbol == ScopeEnterSymbol {
						available = instruction.Value
					}
				}
			}
			for bi := range f.Blocks {
				b := &f.Blocks[bi]
				for _, i := range b.Instructions {
					if i.Kind == ScopeLeave {
						b.End.Kind = Boolean
						b.End.Value = available
						b.End.Targets[1] = b.End.Targets[0]
						return
					}
				}
			}
		}},
		{"unknown helper", "unknown runtime helper", func(m *Module) {
			for bi := range m.Functions[0].Blocks {
				for ii := range m.Functions[0].Blocks[bi].Instructions {
					i := &m.Functions[0].Blocks[bi].Instructions[ii]
					if i.Kind == Call && i.Symbol == LiteralSymbol {
						i.Symbol = "lo_arbitrary"
						return
					}
				}
			}
		}},
		{"host payload adoption", "host or unknown payload adopted", func(m *Module) {
			for bi := range m.Functions[0].Blocks {
				b := &m.Functions[0].Blocks[bi]
				if b.End.Kind == Status && b.End.Result != 0 {
					b.End.Targets[2] = b.End.Targets[0]
					return
				}
			}
		}},
		{"missing status branch", "call result must be status-tested", func(m *Module) {
			for bi := range m.Functions[0].Blocks {
				b := &m.Functions[0].Blocks[bi]
				if b.End.Kind == Status && b.End.Result != 0 {
					b.End.Kind = Jump
					return
				}
			}
		}},
		{"use after cleanup", "operand use after final owned cleanup", func(m *Module) {
			f := &m.Functions[0]
			for bi := range f.Blocks {
				b := &f.Blocks[bi]
				if b.End.Kind == Return {
					b.Instructions = append(b.Instructions, Instruction{Kind: Alias, Site: f.Site, Value: ValueID(len(f.Values) + 1), Inputs: []ValueID{1}})
					f.Values = append(f.Values, true)
					return
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := plan.Snapshot()
			tc.mutate(&m)
			err := verify(context.Background(), m)
			if err == nil {
				t.Fatal("malformed graph verified")
			}
			if tc.name != "use after cleanup" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong rejection: %v", err)
			}
		})
	}
}
