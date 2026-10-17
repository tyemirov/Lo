package catalog

import (
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const (
	CheckpointEncode         = "Checkpoint.encode"
	CheckpointDecode         = "Checkpoint.decode"
	ParametersTo             = "Parameters.to"
	SGDStateTo               = "Optimizer.SGD.State.to"
	AdamWStateTo             = "Optimizer.AdamW.State.to"
	CheckpointInvalidData    = "Checkpoint.InvalidData"
	CheckpointSchemaMismatch = "Checkpoint.SchemaMismatch"
	ParamData                = "data"
	ParamTemplate            = "template"
)

func addCheckpoints(operations map[string]Operation) {
	operations[CheckpointEncode] = operation{name: CheckpointEncode, rule: CheckpointOperation, parameters: []types.Parameter{{Name: ParamValue}}, errors: []string{HostObservationRequired}}
	operations[CheckpointDecode] = operation{name: CheckpointDecode, rule: CheckpointOperation, parameters: []types.Parameter{{Name: ParamData, Type: types.Bytes()}, {Name: ParamTemplate}}, errors: []string{CheckpointInvalidData, CheckpointSchemaMismatch, HostObservationRequired, device.AllocationFailed}}
	for _, name := range []string{ParametersTo, SGDStateTo, AdamWStateTo} {
		parameter := ParamState
		if name == ParametersTo {
			parameter = ParamParameters
		}
		operations[name] = operation{name: name, rule: CheckpointOperation, parameters: []types.Parameter{{Name: parameter}, {Name: ParamDevice, Type: types.Device()}}, errors: []string{device.Mismatch, device.Unavailable, device.AllocationFailed, device.TransferFailed, device.UnsupportedProfile, device.LibraryFailed, device.Lost}}
	}
}

// IsTransfer identifies explicit tensor and training-container transfers.
func IsTransfer(name string) bool {
	return name == TensorTo || name == ParametersTo || name == SGDStateTo || name == AdamWStateTo
}
