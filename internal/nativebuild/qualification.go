//go:build nativequalification

package nativebuild

import (
	"context"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
)

// BuildRace is available only to the explicit repository qualification harness.
func BuildRace(ctx context.Context, program ir.Program, output, target string) (Receipt, diagnostic.Diagnostic) {
	return build(ctx, program, output, target, raceProfile)
}
