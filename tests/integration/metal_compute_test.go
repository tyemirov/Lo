package integration_test

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/targetconfig"

	"strings"
	"testing"
)

func TestMetalComputeStaticInspection(t *testing.T) {
	text := `flow Main()->Tensor<FP32>!{a=Tensor.to(Tensor.new(List.new(1.0),List.new()),Device.new("metal:0"));return Tensor.mul(a,a)};return Main()`
	got := run(t, "inspect", sourceFile(t, text), "--format", "json")
	if got.status != 0 {
		t.Fatalf("inspect: %+v", got)
	}
	for _, fragment := range []string{`"profile":"metal-fp32"`, `"execution":"synchronous"`, targetconfig.ExecutionFailed, targetconfig.UnsupportedProfile, targetconfig.Lost} {
		if !strings.Contains(got.stdout, fragment) {
			t.Fatalf("missing static compute metadata %s: %s", fragment, got.stdout)
		}
	}
}
