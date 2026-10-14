package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
)

// Bound each generated function's control-flow graph without changing Lo costs.
const argumentChunkSize = 64

func (b *block) arguments(arguments []ir.ValueArgument, argumentCount int) string {
	result := b.g.fresh()
	b.line("%s:=make([]value.Value,%d)", result, argumentCount)
	bindings := make([]ir.BindingID, 0, len(b.bindings))
	for binding := range b.bindings {
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i] < bindings[j] })
	parameters, values := []string{}, []string{}
	helperBindings := map[ir.BindingID]string{}
	for i, binding := range bindings {
		name := fmt.Sprintf("input%d", i)
		parameters = append(parameters, name+" value.Value")
		values = append(values, b.bindings[binding])
		helperBindings[binding] = name
	}
	parameterSuffix, valueSuffix := "", ""
	if len(parameters) != 0 {
		parameterSuffix = "," + strings.Join(parameters, ",")
		valueSuffix = "," + strings.Join(values, ",")
	}
	for start := 0; start < len(arguments); start += argumentChunkSize {
		helper := &block{g: b.g, bindings: helperBindings, name: b.name}
		for _, argument := range arguments[start:min(start+argumentChunkSize, len(arguments))] {
			v := helper.expression(argument.Value())
			helper.line("arguments[%d]=%s", argument.Position(), v)
		}
		name := b.g.fresh()
		fmt.Fprintf(&b.g.functions, "func(p *program)%s(s execution.Session,arguments []value.Value%s)(value.Value,error){var err error;_ = err\n%s\nreturn nil,nil}\n", name, parameterSuffix, helper.out.String())
		b.line("_,err=p.%s(s,%s%s)", name, result, valueSuffix)
		b.check()
	}
	return result
}
