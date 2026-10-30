package execution

import (
	"math/bits"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// RecordWorklistProgress captures the last completed state and remaining work.
func (s *session) RecordWorklistProgress(completed, pending int, state value.Value) (FlowProgress, error) {
	const fixedWeight = uint64(16 + len("operation") + 16 + len(catalog.FlowWorklist) + len("completed_count") + len("pending_count") + len("state"))
	weight := fixedWeight + value.SnapshotWeight(state)
	weight += uint64(16+(bits.Len64(uint64(completed))+7)/8) + uint64(16+(bits.Len64(uint64(pending))+7)/8)
	if err := s.reserveFlowProgress(weight); err != nil {
		return nil, err
	}
	operation, err := value.NewText(types.String(), catalog.FlowWorklist)
	if err != nil {
		return nil, err
	}
	record, err := value.NewRecord([]value.Field{
		{Name: "operation", Value: operation},
		{Name: "completed_count", Value: value.Int64(int64(completed))},
		{Name: "pending_count", Value: value.Int64(int64(pending))},
		{Name: "state", Value: value.Snapshot(state)},
	})
	if err != nil {
		return nil, err
	}
	if err := s.chargeProgress(record); err != nil {
		return nil, err
	}
	progress := &flowProgress{record}
	s.flowProgress = progress
	return progress, nil
}
