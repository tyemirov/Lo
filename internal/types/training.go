package types

import "fmt"

const (
	ParametersKind  Kind = "Parameters"
	GradientsKind   Kind = "Gradients"
	SGDConfigKind   Kind = "Optimizer.SGD.Config"
	AdamWConfigKind Kind = "Optimizer.AdamW.Config"
	SGDStateKind    Kind = "Optimizer.SGD.State"
	AdamWStateKind  Kind = "Optimizer.AdamW.State"
)

const (
	FieldParameters   = "parameters"
	FieldStep         = "step"
	FieldConfig       = "config"
	FieldFirstMoment  = "first_moment"
	FieldSecondMoment = "second_moment"
	FieldBeta1Power   = "beta1_power"
	FieldBeta2Power   = "beta2_power"
	FieldLearningRate = "learning_rate"
	FieldBeta1        = "beta1"
	FieldBeta2        = "beta2"
	FieldEpsilon      = "epsilon"
	FieldWeightDecay  = "weight_decay"
)

func SGDConfig() Type   { return atom(SGDConfigKind) }
func AdamWConfig() Type { return atom(AdamWConfigKind) }

type OptimizerState interface {
	Type
	Schema() Record
	optimizerState()
}
type optimizerState struct {
	kind   Kind
	schema Record
}

func (optimizerState) loType()          {}
func (optimizerState) optimizerState()  {}
func (t optimizerState) Kind() Kind     { return t.kind }
func (t optimizerState) Schema() Record { return t.schema }
func (t optimizerState) String() string { return string(t.kind) + "<" + t.schema.String() + ">" }
func NewOptimizerState(kind Kind, schema Type) (OptimizerState, error) {
	if kind != SGDStateKind && kind != AdamWStateKind {
		return nil, fmt.Errorf("invalid optimizer state type %s", kind)
	}
	r, err := NewTensorBundle(ParametersKind, schema)
	if err != nil {
		return nil, err
	}
	return optimizerState{kind, r.Schema()}, nil
}
func ConfigContents(kind Kind) Record {
	fields := []Field{{Name: FieldLearningRate, Type: Float()}}
	if kind == AdamWConfigKind {
		for _, name := range []string{FieldBeta1, FieldBeta2, FieldEpsilon, FieldWeightDecay} {
			fields = append(fields, Field{Name: name, Type: Float()})
		}
	}
	r, err := NewRecord(fields)
	if err != nil {
		panic(err)
	}
	return r
}
func StateContents(state OptimizerState) Record {
	p, err := NewTensorBundle(ParametersKind, state.Schema())
	if err != nil {
		panic(err)
	}
	config := SGDConfig()
	if state.Kind() == AdamWStateKind {
		config = AdamWConfig()
	}
	fields := []Field{{Name: FieldParameters, Type: p}, {Name: FieldStep, Type: Int()}, {Name: FieldConfig, Type: config}}
	if state.Kind() == AdamWStateKind {
		fields = append(fields, Field{Name: FieldFirstMoment, Type: state.Schema()}, Field{Name: FieldSecondMoment, Type: state.Schema()}, Field{Name: FieldBeta1Power, Type: Tensor()}, Field{Name: FieldBeta2Power, Type: Tensor()})
	}
	r, err := NewRecord(fields)
	if err != nil {
		panic(err)
	}
	return r
}

type TensorBundle interface {
	Type
	Schema() Record
	tensorBundle()
}
type tensorBundle struct {
	kind   Kind
	schema Record
}

func (tensorBundle) loType()          {}
func (tensorBundle) tensorBundle()    {}
func (t tensorBundle) Kind() Kind     { return t.kind }
func (t tensorBundle) Schema() Record { return t.schema }
func (t tensorBundle) String() string { return string(t.kind) + "<" + t.schema.String() + ">" }
func NewTensorBundle(kind Kind, schema Type) (TensorBundle, error) {
	if kind != ParametersKind && kind != GradientsKind {
		return nil, fmt.Errorf("invalid tensor record type %s", kind)
	}
	r, ok := schema.(Record)
	if !ok || len(r.Fields()) == 0 {
		return nil, fmt.Errorf("%s requires a nonempty exact tensor record", kind)
	}
	for _, f := range r.Fields() {
		if !Equal(f.Type, Tensor()) {
			return nil, fmt.Errorf("%s field %s requires Tensor<FP32>", kind, f.Name)
		}
	}
	return tensorBundle{kind, r}, nil
}
