package execution

import (
	"math/bits"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FlowProgress retains completed processor state without executable provenance.
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

// WithFlowProgress preserves the original failure and one completed processor prefix.
func WithFlowProgress(err error, progress FlowProgress) error {
	if err == nil || progress == nil {
		return err
	}
	return flowError{err, progress}
}

func preserveValueFlowProgress(err error, rejected value.Value) error {
	cause, _ := rejected.(value.Error)
	if fallible, ok := rejected.(value.Fallible); ok {
		_, cause = fallible.Outcome()
	}
	if cause == nil {
		return err
	}
	progress := cause.FlowProgress()
	for index := len(progress) - 1; index >= 0; index-- {
		err = WithFlowProgress(err, &flowProgress{progress[index]})
	}
	return err
}

// FlowDetails obtains nested progress from outermost to innermost processor.
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
		if failure, ok := err.(Failure); ok {
			records = append(records, failure.Value().FlowProgress()...)
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
	if err := s.chargeProgress(record); err != nil {
		return nil, err
	}
	progress := &flowProgress{record}
	s.flowProgress = progress
	return progress, nil
}

func (s *session) reserveFlowProgress(weight uint64) error {
	// Diagnostic completion survives a resource cleanup failure after a callback.
	if err := s.contextError(); err != nil {
		return BoundaryHostError(err)
	}
	if exceeds(s.limits.ValueBytes, 0, weight) {
		return Limit("expanded flow progress bytes")
	}
	if exceeds(s.limits.LiveBytes, s.live-s.progressWeight, weight) {
		return Limit("live value bytes")
	}
	if exceeds(s.limits.DataBytes, s.data, weight) {
		return Limit("cumulative data bytes")
	}
	return nil
}
func (s *session) chargeProgress(record value.Record) error {
	weight := record.Weight()
	if err := s.reserveFlowProgress(weight); err != nil {
		return err
	}
	s.live = s.live - s.progressWeight + weight
	s.progressWeight = weight
	s.data += weight
	return nil
}
