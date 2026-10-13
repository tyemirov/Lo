package compiler

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/check"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/inspect"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/syntax"
)

func Parse(file source.File) (syntax.Program, diagnostic.Diagnostic) { return syntax.Parse(file) }
func Check(file source.File) (ir.Program, diagnostic.Diagnostic) {
	program, d := Parse(file)
	if d != nil {
		return nil, d
	}
	return check.Program(program)
}

func Inspect(file source.File) (inspect.Report, diagnostic.Diagnostic) {
	program, d := Check(file)
	if d != nil {
		return nil, d
	}
	return inspect.Program(program)
}
