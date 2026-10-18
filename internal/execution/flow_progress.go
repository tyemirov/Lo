package execution

import (
	"math/bits"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FlowProgress retains a completed fold prefix without executable provenance.
type FlowProgress interface {
	Record() value.Record
	flowProgress()
}

type flowProgress struct{ record value.Record }

func (*flowProgress) flowProgress()          {}
func (p *flowProgress) Record() value.Record { return p.record }

type flowError struct {
	error
	progress FlowProgress
}

func (e flowError) Unwrap() error { return e.error }

// WithFlowProgress preserves the original failure and one completed fold prefix.
func WithFlowProgress(err error, progress FlowProgress) error {
	if err == nil || progress == nil {
		return err
	}
	return flowError{err, progress}
}

// FlowDetails obtains nested progress from outermost to innermost fold.
func FlowDetails(err error) []value.Record {
	var records []value.Record
	seen := map[FlowProgress]bool{}
	var visit func(error)
	visit = func(err error) {
		if err == nil {
			return
		}
		if e, ok := err.(flowError); ok && !seen[e.progress] {
			seen[e.progress] = true
			records = append(records, e.progress.Record())
		}
		switch e := err.(type) {
		case interface{ Unwrap() []error }:
			for _, cause := range e.Unwrap() {
				visit(cause)
			}
		case interface{ Unwrap() error }:
			visit(e.Unwrap())
		}
	}
	visit(err)
	return records
}

func (s *session) RecordFlowProgress(total, completed int, state value.Value) (FlowProgress, error) {
	const fixedWeight = uint64(16 + len("operation") + 16 + len(catalog.FlowFold) + len("total_count") + len("completed_count") + len("next_index") + len("state"))
	weight := fixedWeight + value.SnapshotWeight(state)
	weight += uint64(16+(bits.Len64(uint64(total))+7)/8) + 2*uint64(16+(bits.Len64(uint64(completed))+7)/8)
	if err := s.reserveFlowProgress(weight); err != nil {
		return nil, err
	}
	snapshot := value.Snapshot(state)
	operation, err := value.NewText(types.String(), catalog.FlowFold)
	if err != nil {
		return nil, err
	}
	record, err := value.NewRecord([]value.Field{
		{Name: "operation", Value: operation},
		{Name: "total_count", Value: value.Int64(int64(total))},
		{Name: "completed_count", Value: value.Int64(int64(completed))},
		{Name: "next_index", Value: value.Int64(int64(completed))},
		{Name: "state", Value: snapshot},
	})
	if err != nil {
		return nil, err
	}
	s.weight += weight
	progress := &flowProgress{record}
	s.flowProgress = progress
	return progress, nil
}

func (s *session) reserveFlowProgress(weight uint64) error {
	if err := s.ctx.Err(); err != nil {
		return BoundaryHostError(err)
	}
	if weight > MaxValueWeight {
		return Limit("expanded flow progress bytes")
	}
	if weight > MaxEvaluationWeight-s.weight {
		return Limit("evaluation flow progress bytes")
	}
	return nil
}
