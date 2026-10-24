package llvmemit_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

// TestEmitLiteralSequenceBoundedCode exercises the public checked-program path.
// Metadata and descriptor bytes may grow with N; executable instructions, CFG,
// and stack allocation instructions must not grow with literal count.
func TestEmitLiteralSequenceBoundedCode(t *testing.T) {
	type size struct{ instructions, blocks, allocas int }
	for _, target := range []string{"darwin/arm64", "linux/arm64"} {
		t.Run(target, func(t *testing.T) {
			var first size
			for _, count := range []int{1, 1000, 10000} {
				text := "return List.new(" + strings.Repeat("11,", count-1) + "11)"
				file, err := source.New("literal-size.lo", text)
				if err != nil {
					t.Fatal(err)
				}
				checked, diagnostic := compiler.Check(file)
				if diagnostic != nil {
					t.Fatal(diagnostic)
				}
				plan, err := nativeplan.Lower(context.Background(), checked)
				if err != nil {
					t.Fatal(err)
				}
				module, err := llvmemit.Emit(context.Background(), plan, target)
				if err != nil {
					t.Fatal(err)
				}
				text = string(module)
				if strings.Count(text, "define internal void @lo_literals_") != 1 || !strings.Contains(text, fmt.Sprintf("alloca [%d x ptr]", 3*count)) || !strings.Contains(text, fmt.Sprintf("alloca [%d x ptr]", count)) {
					t.Fatalf("N=%d: missing single helper and grouped owned/argument storage", count)
				}
				if !strings.Contains(text, fmt.Sprintf("i8 0, i64 %d, i1 false)", uint64(count)*24)) || strings.Contains(text, fmt.Sprintf("store [%d x ptr] zeroinitializer", 3*count)) || strings.Count(text, "declare void @llvm.memset.p0.i64(") != 1 {
					t.Fatalf("N=%d: group initialization must use one bounded memset intrinsic", count)
				}
				if strings.Count(text, "!llvm.loop !") != 3 || strings.Count(text, "!\"llvm.loop.unroll.disable\"") != 1 {
					t.Fatalf("N=%d: helper and both ReleaseAll loops must disable unrolling", count)
				}
				loops := regexp.MustCompile(`(?m)^!([0-9]+) = distinct !\{!([0-9]+), !([0-9]+)\}$`).FindAllStringSubmatch(text, -1)
				if len(loops) != 3 {
					t.Fatalf("N=%d: missing closed loop metadata", count)
				}
				for _, loop := range loops {
					if loop[1] != loop[2] || strings.Count(text, "!llvm.loop !"+loop[1]+",") != 1 || !strings.Contains(text, "!"+loop[3]+" = !{!\"llvm.loop.unroll.disable\"}") {
						t.Fatalf("N=%d: loop metadata must be self-referential and disable unrolling: %v", count, loop)
					}
				}
				var actual size
				inFunction := false
				for _, line := range strings.Split(string(module), "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "define ") {
						inFunction = true
						continue
					}
					if line == "}" {
						inFunction = false
						continue
					}
					if !inFunction || line == "" || strings.HasPrefix(line, ";") {
						continue
					}
					if strings.HasSuffix(line, ":") {
						actual.blocks++
						continue
					}
					// Switch alternatives are table entries, not instructions.
					if line == "]" || strings.HasPrefix(line, "i32 ") {
						continue
					}
					actual.instructions++
					if strings.Contains(line, " = alloca ") {
						actual.allocas++
					}
				}
				t.Logf("N=%d module_bytes=%d instructions=%d blocks=%d allocas=%d", count, len(module), actual.instructions, actual.blocks, actual.allocas)
				if count == 1 {
					first = actual
				}
				if actual.instructions > first.instructions+24 || actual.blocks > first.blocks+8 || actual.allocas > first.allocas+2 {
					t.Errorf("N=%d generated code grows with literal count: got %+v, N=1 %+v", count, actual, first)
				}
			}
		})
	}
}
