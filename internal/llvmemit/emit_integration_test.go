package llvmemit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/llvmemit"
	"github.com/MarcoPoloResearchLab/Lo/internal/nativeplan"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestEmitVerifiedModule(t *testing.T) {
	file, err := source.New("source maps.lo", `flow Add(left:Int,right:Int)->Int{return Math.add(left,right)}; return Add(right:2,left:1)`)
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
	for target, triple := range map[string]string{"darwin/arm64": "aarch64-apple-darwin", "linux/arm64": "aarch64-unknown-linux-gnu"} {
		t.Run(target, func(t *testing.T) {
			module, err := llvmemit.Emit(context.Background(), plan, target)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{triple, "define void @lo_entry(", "define void @lo_flow_0(", "@lo_program_metadata", "@lo_program_flows", "!DIFile(", "!DILocation(", "source maps.lo", "switch i32", "@lo_value_release", "@lo_scope_leave"} {
				if !strings.Contains(string(module), required) {
					t.Fatalf("missing %q", required)
				}
			}
			second, err := llvmemit.Emit(context.Background(), plan, target)
			if err != nil || string(second) != string(module) {
				t.Fatal("emission is not deterministic")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := llvmemit.Emit(ctx, plan, "linux/arm64"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := llvmemit.Emit(context.Background(), nil, "linux/arm64"); err == nil {
		t.Fatal("unverified input admitted")
	}
	if _, err := llvmemit.Emit(context.Background(), plan, "linux/amd64"); err == nil {
		t.Fatal("unsupported target admitted")
	}
}
