package catalog

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const (
	SGDConfigNew           = "Optimizer.SGD.Config.new"
	AdamWConfigNew         = "Optimizer.AdamW.Config.new"
	SGDStateNew            = "Optimizer.SGD.State.new"
	AdamWStateNew          = "Optimizer.AdamW.State.new"
	SGDStep                = "Optimizer.SGD.step"
	AdamWStep              = "Optimizer.AdamW.step"
	InvalidOptimizerConfig = "Optimizer.InvalidConfig"
	ParamState             = "state"
)

func addOptimizers(m map[string]Operation) {
	for _, name := range []string{SGDConfigNew, AdamWConfigNew} {
		out := types.SGDConfig()
		names := []string{types.FieldLearningRate}
		if name == AdamWConfigNew {
			out = types.AdamWConfig()
			names = append(names, types.FieldBeta1, types.FieldBeta2, types.FieldEpsilon, types.FieldWeightDecay)
		}
		params := make([]types.Parameter, len(names))
		for i, n := range names {
			params[i] = types.Parameter{Name: n, Type: types.Float()}
		}
		m[name] = operation{name: name, rule: Fixed, parameters: params, output: must(types.NewFallible(out)), errors: []string{InvalidOptimizerConfig}}
	}
	for _, name := range []string{SGDStateNew, AdamWStateNew, SGDStep, AdamWStep} {
		params := []types.Parameter{{Name: ParamParameters}, {Name: types.FieldConfig}}
		if name == SGDStep || name == AdamWStep {
			params = []types.Parameter{{Name: ParamState}, {Name: ParamGradients}}
		}
		m[name] = operation{name: name, rule: OptimizerOperation, parameters: params, errors: []string{GradientSchemaMismatch, NumericNonFinite, NumericZero, NumericDomain, device.UnsupportedOperation}}
	}
}
