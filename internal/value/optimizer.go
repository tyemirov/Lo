package value

import (
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"math"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

type OptimizerConfig interface {
	Value
	Contents() Record
	Rate() float32
	configurationValue()
}
type SGDConfig interface {
	OptimizerConfig
	sgdConfig()
}
type AdamWConfig interface {
	OptimizerConfig
	Beta1() float32
	Beta2() float32
	Epsilon() float32
	Decay() float32
	adamwConfig()
}
type optimizerConfig struct {
	base
	contents Record
	rate     float32
}

func (optimizerConfig) configurationValue()            {}
func (v optimizerConfig) Contents() Record             { return v.contents }
func (v optimizerConfig) Rate() float32                { return v.rate }
func (v optimizerConfig) MarshalJSON() ([]byte, error) { return encode(v, recordValues(v.contents)) }

type sgdConfig struct{ optimizerConfig }

func (sgdConfig) sgdConfig() {}

type adamwConfig struct {
	optimizerConfig
	beta1, beta2, epsilon, decay float32
}

func (adamwConfig) adamwConfig()       {}
func (c adamwConfig) Beta1() float32   { return c.beta1 }
func (c adamwConfig) Beta2() float32   { return c.beta2 }
func (c adamwConfig) Epsilon() float32 { return c.epsilon }
func (c adamwConfig) Decay() float32   { return c.decay }
func recordValues(r Record) map[string]Value {
	out := map[string]Value{}
	for _, f := range r.Fields() {
		out[f.Name] = f.Value
	}
	return out
}
func configRecord(names []string, values []float64) (Record, []float32, error) {
	fields := make([]Field, len(names))
	numbers := make([]float32, len(names))
	for i, name := range names {
		n := float32(values[i])
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return nil, nil, fmt.Errorf("optimizer %s must convert to finite FP32", name)
		}
		numbers[i] = n
		fields[i] = Field{Name: name, Value: must(NewFloat(float64(n)))}
	}
	return must(NewRecord(fields)), numbers, nil
}
func NewSGDConfig(rate float64) (SGDConfig, error) {
	r, n, err := configRecord([]string{types.FieldLearningRate}, []float64{rate})
	if err != nil {
		return nil, err
	}
	if n[0] <= 0 {
		return nil, fmt.Errorf("optimizer learning_rate must be positive after FP32 conversion")
	}
	return sgdConfig{optimizerConfig{newBase(types.SGDConfig(), r.Weight()), r, n[0]}}, nil
}
func NewAdamWConfig(rate, beta1, beta2, epsilon, decay float64) (AdamWConfig, error) {
	r, n, err := configRecord([]string{types.FieldLearningRate, types.FieldBeta1, types.FieldBeta2, types.FieldEpsilon, types.FieldWeightDecay}, []float64{rate, beta1, beta2, epsilon, decay})
	if err != nil {
		return nil, err
	}
	if n[0] <= 0 || n[3] <= 0 || n[1] < 0 || n[1] >= 1 || n[2] < 0 || n[2] >= 1 || n[4] < 0 {
		return nil, fmt.Errorf("AdamW requires positive learning_rate and epsilon, betas in [0,1), and nonnegative weight_decay after FP32 conversion")
	}
	product := float32(n[0] * n[4])
	if math.IsInf(float64(product), 0) {
		return nil, fmt.Errorf("AdamW learning_rate times weight_decay must be finite FP32")
	}
	return adamwConfig{optimizerConfig{newBase(types.AdamWConfig(), r.Weight()), r, n[0]}, n[1], n[2], n[3], n[4]}, nil
}

type OptimizerState interface {
	Value
	Parameters() TensorBundle
	Step() Integer
	Config() OptimizerConfig
	Contents() Record
	stateValue()
}
type AdamWState interface {
	OptimizerState
	FirstMoment() Record
	SecondMoment() Record
	Beta1Power() Tensor
	Beta2Power() Tensor
	adamwState()
}
type optimizerState struct {
	base
	contents   Record
	parameters TensorBundle
	step       Integer
	config     OptimizerConfig
}

func (optimizerState) stateValue()                    {}
func (s optimizerState) Parameters() TensorBundle     { return s.parameters }
func (s optimizerState) Step() Integer                { return s.step }
func (s optimizerState) Config() OptimizerConfig      { return s.config }
func (s optimizerState) Contents() Record             { return s.contents }
func (s optimizerState) MarshalJSON() ([]byte, error) { return encode(s, recordValues(s.contents)) }

type adamwState struct{ optimizerState }

func (adamwState) adamwState()            {}
func (s adamwState) FirstMoment() Record  { return s.contents.Field(types.FieldFirstMoment).(Record) }
func (s adamwState) SecondMoment() Record { return s.contents.Field(types.FieldSecondMoment).(Record) }
func (s adamwState) Beta1Power() Tensor   { return s.contents.Field(types.FieldBeta1Power).(Tensor) }
func (s adamwState) Beta2Power() Tensor   { return s.contents.Field(types.FieldBeta2Power).(Tensor) }
func stateBase(kind types.Kind, p TensorBundle, step Integer, config OptimizerConfig, extra []Field) (optimizerState, error) {
	if p == nil || p.Type().Kind() != types.ParametersKind || step == nil || step.Big().Sign() < 0 || config == nil {
		return optimizerState{}, fmt.Errorf("optimizer state requires Parameters, nonnegative step, and validated config")
	}
	t, err := types.NewOptimizerState(kind, p.Type().(types.TensorBundle).Schema())
	if err != nil {
		return optimizerState{}, err
	}
	fields := append([]Field{{Name: types.FieldParameters, Value: p}, {Name: types.FieldStep, Value: step}, {Name: types.FieldConfig, Value: config}}, extra...)
	contents := must(NewRecord(fields))
	return optimizerState{newBase(t, contents.Weight()), contents, p, step, config}, nil
}
func NewSGDState(p TensorBundle, step Integer, config SGDConfig) (OptimizerState, error) {
	s, err := stateBase(types.SGDStateKind, p, step, config, nil)
	if err != nil {
		return nil, err
	}
	return s, nil
}
func NewAdamWState(p TensorBundle, step Integer, config AdamWConfig, first, second Record, beta1, beta2 Tensor) (AdamWState, error) {
	if p == nil || first == nil || second == nil || beta1 == nil || beta2 == nil {
		return nil, fmt.Errorf("AdamW state requires Parameters, moments, and beta powers")
	}
	for _, moment := range []Record{first, second} {
		if err := MatchTensorSchema(p.Contents(), moment); err != nil {
			return nil, fmt.Errorf("AdamW moment: %w", err)
		}
	}
	for _, power := range []Tensor{beta1, beta2} {
		if power.Data().Location() != device.CPU || power.Data().Shape().Rank() != 0 || power.Data().(tensor.HostData).At(0) < 0 || power.Data().(tensor.HostData).At(0) > 1 {
			return nil, fmt.Errorf("AdamW beta power must be a rank-zero value in [0,1]")
		}
	}
	s, err := stateBase(types.AdamWStateKind, p, step, config, []Field{{Name: types.FieldFirstMoment, Value: first}, {Name: types.FieldSecondMoment, Value: second}, {Name: types.FieldBeta1Power, Value: beta1}, {Name: types.FieldBeta2Power, Value: beta2}})
	if err != nil {
		return nil, err
	}
	return adamwState{s}, nil
}
