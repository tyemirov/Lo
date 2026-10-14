package catalog

import "github.com/MarcoPoloResearchLab/Lo/internal/types"

const (
	ParametersNew          = "Parameters.new"
	ParametersValues       = "Parameters.values"
	GradientsNew           = "Gradients.new"
	GradientsValues        = "Gradients.values"
	GradientsZero          = "Gradients.zero"
	GradientsAdd           = "Gradients.add"
	GradientsScale         = "Gradients.scale"
	GradientSchemaMismatch = "Gradient.SchemaMismatch"
	ParamParameters        = "parameters"
	ParamGradients         = "gradients"
	ParamFactor            = "factor"
)

func addParameters(m map[string]Operation) {
	add := func(name string, params ...string) {
		parameters := make([]types.Parameter, len(params))
		for i, p := range params {
			parameters[i] = types.Parameter{Name: p}
		}
		errors := []string{}
		if name != ParametersValues && name != GradientsValues {
			errors = append(errors, GradientSchemaMismatch)
		}
		if name == GradientsAdd || name == GradientsScale {
			errors = append(errors, NumericNonFinite)
		}
		m[name] = operation{name: name, rule: TensorBundleOperation, parameters: parameters, errors: errors}
	}
	add(ParametersNew, ParamValues)
	add(ParametersValues, ParamParameters)
	add(GradientsNew, ParamParameters, ParamValues)
	add(GradientsValues, ParamGradients)
	add(GradientsZero, ParamParameters)
	add(GradientsAdd, ParamLeftOperand, ParamRightOperand)
	add(GradientsScale, ParamGradients, ParamFactor)
	m[AutodiffStop] = operation{name: AutodiffStop, rule: Fixed, parameters: []types.Parameter{{Name: ParamValue, Type: types.Tensor()}}, output: types.Tensor()}
}
