package catalog

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/targetconfig"
)

const (
	AutodiffGrad        = "Autodiff.grad"
	AutodiffStop        = "Autodiff.stop"
	InvalidLoss         = "Autodiff.InvalidLoss"
	UndefinedDerivative = "Autodiff.UndefinedDerivative"
	ParamTargets        = "targets"
	FieldGradients      = "gradients"
)

func addAutodiff(m map[string]Operation) {
	m[AutodiffGrad] = operation{name: AutodiffGrad, rule: Gradient, errors: []string{InvalidLoss, UndefinedDerivative, NumericNonFinite, NumericZero, NumericDomain, ShapeMismatch, InvalidShape, EmptyReduction, targetconfig.UnsupportedOperation}}
}
