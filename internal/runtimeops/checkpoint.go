package runtimeops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/identity"
	"github.com/MarcoPoloResearchLab/Lo/internal/tensor"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type checkpointFault struct{ kind, message string }

func (f checkpointFault) Error() string { return f.message }

func invalidCheckpoint(message string) error {
	return checkpointFault{catalog.CheckpointInvalidData, message}
}
func checkpointMismatch(message string) error {
	return checkpointFault{catalog.CheckpointSchemaMismatch, message}
}

// Checkpoint executes the checked codec and explicit container transfers.
func Checkpoint(session execution.Session, call Call, args []value.Value, _ map[int]identity.FlowID) (output value.Value, resultErr error) {
	m := &machine{session}
	scope := m.Scope()
	defer func() { scope.Close(output) }()
	var result value.Value
	var err error
	switch call.Operation().Name() {
	case catalog.CheckpointEncode:
		err = checkpointOnCPU(args[0])
		if err == nil {
			err = m.TensorBudget(args[0].Weight(), args[0].Weight())
		}
		if err == nil {
			var encoded []byte
			encoded, err = json.Marshal(args[0])
			if err == nil {
				err = m.TensorBudget(0, uint64(len(encoded)))
			}
			if err == nil {
				result = value.NewBytes(encoded)
			}
		}
	case catalog.CheckpointDecode:
		err = checkpointOnCPU(args[1])
		if err == nil {
			data := args[0].(value.Bytes).Bytes()
			err = m.TensorBudget(uint64(len(data)), uint64(len(data)))
			if err == nil {
				result, err = m.decodeCheckpoint(data, args[1])
			}
			if err == nil {
				var encoded []byte
				encoded, err = json.Marshal(result)
				if err == nil && !bytes.Equal(data, encoded) {
					err = invalidCheckpoint("checkpoint must use the canonical encoding")
				}
			}
		}
	default:
		result, err = m.transferTraining(args[0], args[1].(value.Device).Selector(), call.Operation().Name())
	}
	if err == nil {
		if adam, ok := result.(value.AdamWState); ok {
			err = m.Resources().Classify(value.Allocations(adam.FirstMoment(), adam.SecondMoment(), adam.Beta1Power(), adam.Beta2Power()), device.OptimizerStorage)
		}
	}
	if err == nil {
		if err := m.Context().Err(); err != nil {
			return nil, execution.BoundaryHostError(err)
		}
		return Success(call.Type(), result), nil
	}
	if host := execution.BoundaryHostError(err); host != nil {
		return nil, host
	}
	var fault checkpointFault
	if errors.As(err, &fault) {
		return OperationFailure(call, args, fault.kind, call.Operation().Name()+": "+err.Error()), nil
	}
	var backend device.Fault
	if errors.As(err, &backend) {
		return OperationFailure(call, args, backend.Kind(), call.Operation().Name()+": "+err.Error()), nil
	}
	return nil, fmt.Errorf("%s: %w", call.Operation().Name(), err)
}

func checkpointOnCPU(v value.Value) error {
	switch x := v.(type) {
	case value.Tensor:
		if x.Data().Location() != device.CPU {
			return checkpointFault{catalog.HostObservationRequired, "transfer all checkpoint tensors to CPU explicitly"}
		}
	case value.TensorBundle:
		return checkpointOnCPU(x.Contents())
	case value.OptimizerState:
		return checkpointOnCPU(x.Contents())
	case value.Record:
		for _, field := range x.Fields() {
			if err := checkpointOnCPU(field.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkpointJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return invalidCheckpoint(err.Error())
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return invalidCheckpoint("checkpoint contains trailing data")
	}
	return nil
}

func (m *machine) decodeCheckpoint(data []byte, template value.Value) (value.Value, error) {
	if err := m.Step(); err != nil {
		return nil, err
	}
	var envelope struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := checkpointJSON(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Type != template.Type().String() {
		return nil, checkpointMismatch("expected " + template.Type().String() + ", got " + envelope.Type)
	}
	switch x := template.(type) {
	case value.Tensor:
		return m.decodeCheckpointTensor(envelope.Value, x)
	case value.Record:
		return m.decodeCheckpointFields(envelope.Value, x)
	case value.TensorBundle:
		fields, err := m.decodeCheckpointFields(envelope.Value, x.Contents())
		if err != nil {
			return nil, err
		}
		return value.NewTensorBundle(x.Type().(types.TensorBundle), fields)
	case value.OptimizerConfig:
		fields, err := m.decodeCheckpointFields(envelope.Value, x.Contents())
		if err != nil {
			return nil, err
		}
		number := func(name string) float64 { return fields.Field(name).(value.Float).Number() }
		var config value.OptimizerConfig
		if x.Type().Kind() == types.SGDConfigKind {
			config, err = value.NewSGDConfig(number(types.FieldLearningRate))
		} else {
			config, err = value.NewAdamWConfig(number(types.FieldLearningRate), number(types.FieldBeta1), number(types.FieldBeta2), number(types.FieldEpsilon), number(types.FieldWeightDecay))
		}
		if err != nil {
			return nil, invalidCheckpoint(err.Error())
		}
		return config, nil
	case value.OptimizerState:
		fields, err := m.decodeCheckpointFields(envelope.Value, x.Contents())
		if err != nil {
			return nil, err
		}
		state, err := checkpointState(x.Type().Kind(), fields)
		if err != nil {
			return nil, invalidCheckpoint(err.Error())
		}
		if adam, ok := state.(value.AdamWState); ok {
			for _, field := range adam.SecondMoment().Fields() {
				data := field.Value.(value.Tensor).Data().(tensor.HostData)
				for index := 0; index < int(data.Shape().Count()); index++ {
					if index%1024 == 0 {
						if err := m.Context().Err(); err != nil {
							return nil, err
						}
					}
					if data.At(index) < 0 {
						return nil, invalidCheckpoint("AdamW second moments must be nonnegative")
					}
				}
			}
			for _, power := range []value.Tensor{adam.Beta1Power(), adam.Beta2Power()} {
				number := power.Data().(tensor.HostData).At(0)
				if number < 0 || number > 1 {
					return nil, invalidCheckpoint("AdamW beta powers must be in [0,1]")
				}
			}
		}
		return state, nil
	case value.Float:
		var text string
		if err := checkpointJSON(envelope.Value, &text); err != nil {
			return nil, err
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, invalidCheckpoint("checkpoint Float must be finite")
		}
		return value.NewFloat(number)
	case value.Integer:
		var text string
		if err := checkpointJSON(envelope.Value, &text); err != nil {
			return nil, err
		}
		if len(text) > execution.MaxIntegerBits/3+1 {
			return nil, execution.Limit("checkpoint integer bits")
		}
		number, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, invalidCheckpoint("invalid checkpoint integer")
		}
		return Integer(number)
	default:
		return nil, fmt.Errorf("unsupported checked checkpoint component %s", template.Type())
	}
}

func (m *machine) decodeCheckpointFields(data []byte, template value.Record) (value.Record, error) {
	var object map[string]json.RawMessage
	if err := checkpointJSON(data, &object); err != nil {
		return nil, err
	}
	fields := template.Fields()
	if len(object) != len(fields) {
		return nil, checkpointMismatch("checkpoint field set differs from template")
	}
	for index, field := range fields {
		encoded, ok := object[field.Name]
		if !ok {
			return nil, checkpointMismatch("checkpoint is missing field " + field.Name)
		}
		decoded, err := m.decodeCheckpoint(encoded, field.Value)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		fields[index].Value = decoded
	}
	return value.NewRecord(fields)
}

func (m *machine) decodeCheckpointTensor(encoded []byte, template value.Tensor) (value.Value, error) {
	var data struct {
		Dtype  string   `json:"dtype"`
		Shape  []string `json:"shape"`
		Device string   `json:"device"`
		Values []string `json:"values"`
	}
	if err := checkpointJSON(encoded, &data); err != nil {
		return nil, err
	}
	shape := template.Data().Shape()
	if data.Dtype != tensor.Dtype || data.Device != device.CPU || len(data.Shape) != shape.Rank() {
		return nil, checkpointMismatch("checkpoint tensor requires CPU FP32 and the template shape")
	}
	for index, dimension := range shape.Dims() {
		if data.Shape[index] != strconv.FormatInt(dimension, 10) {
			return nil, checkpointMismatch("checkpoint tensor shape differs from template")
		}
	}
	if uint64(len(data.Values)) != shape.Count() {
		return nil, checkpointMismatch("checkpoint tensor element count differs from shape")
	}
	if err := m.TensorBudget(shape.Count(), tensor.Weight(shape, device.CPU)); err != nil {
		return nil, err
	}
	numbers := make([]float64, len(data.Values))
	for index, text := range data.Values {
		if index%1024 == 0 {
			if err := m.Context().Err(); err != nil {
				return nil, err
			}
		}
		number, err := strconv.ParseFloat(text, 32)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, invalidCheckpoint("checkpoint tensor elements must be finite FP32")
		}
		numbers[index] = number
	}
	result, err := tensor.Construct(m.Context(), m.Resources(), shape, func(index int) float64 { return numbers[index] })
	if err != nil {
		return nil, err
	}
	return value.NewTensor(result)
}

func checkpointState(kind types.Kind, fields value.Record) (value.OptimizerState, error) {
	parameters := fields.Field(types.FieldParameters).(value.TensorBundle)
	step := fields.Field(types.FieldStep).(value.Integer)
	if kind == types.SGDStateKind {
		return value.NewSGDState(parameters, step, fields.Field(types.FieldConfig).(value.SGDConfig))
	}
	return value.NewAdamWState(parameters, step, fields.Field(types.FieldConfig).(value.AdamWConfig), fields.Field(types.FieldFirstMoment).(value.Record), fields.Field(types.FieldSecondMoment).(value.Record), fields.Field(types.FieldBeta1Power).(value.Tensor), fields.Field(types.FieldBeta2Power).(value.Tensor))
}

func (m *machine) transferTraining(v value.Value, destination, operation string) (value.Value, error) {
	if err := m.Step(); err != nil {
		return nil, err
	}
	switch x := v.(type) {
	case value.Tensor:
		data := x.Data()
		for _, selector := range []string{data.Location(), destination} {
			if !m.Resources().Granted(selector) {
				return nil, execution.Denied(operation, selector)
			}
		}
		if data.Location() != device.CPU && destination != device.CPU && data.Location() != destination {
			return nil, checkpointFault{device.Mismatch, "transfers require CPU and one compute location"}
		}
		steps := uint64(0)
		if data.Location() != destination {
			steps = data.Shape().Count()
		}
		if err := m.TensorBudget(steps, tensor.Weight(data.Shape(), destination)); err != nil {
			return nil, err
		}
		transferred, err := tensor.Transfer(m.Context(), m.Resources(), data, destination)
		if err != nil {
			return nil, err
		}
		return value.NewTensor(transferred)
	case value.Record:
		fields := x.Fields()
		for index, field := range fields {
			moved, err := m.transferTraining(field.Value, destination, operation)
			if err != nil {
				return nil, err
			}
			fields[index].Value = moved
		}
		return value.NewRecord(fields)
	case value.TensorBundle:
		moved, err := m.transferTraining(x.Contents(), destination, operation)
		if err != nil {
			return nil, err
		}
		return value.NewTensorBundle(x.Type().(types.TensorBundle), moved.(value.Record))
	case value.OptimizerState:
		moved, err := m.transferTraining(x.Contents(), destination, operation)
		if err != nil {
			return nil, err
		}
		return checkpointState(x.Type().Kind(), moved.(value.Record))
	case value.Integer, value.OptimizerConfig:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported checked transfer component %s", v.Type())
	}
}
