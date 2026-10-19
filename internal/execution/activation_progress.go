package execution

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// ActivationProgress identifies the last completed activation in an invocation.
type ActivationProgress interface {
	json.Marshaler
	Operation() string
	Span() source.Span
	Receipt() value.Receipt
	Summary() string
	activationProgress()
}

type activationProgress struct {
	operation  string
	span       source.Span
	receipt    value.Receipt
	resultType string
	status     value.Integer
}

func (*activationProgress) activationProgress()      {}
func (p *activationProgress) Operation() string      { return p.operation }
func (p *activationProgress) Span() source.Span      { return p.span }
func (p *activationProgress) Receipt() value.Receipt { return p.receipt }
func (p *activationProgress) Summary() string {
	if p.receipt != nil {
		return value.TextResult(p.receipt)
	}
	return p.resultType + " status " + value.TextResult(p.status)
}
func (p *activationProgress) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Operation  string        `json:"operation"`
		Span       source.Span   `json:"span"`
		Receipt    value.Receipt `json:"receipt,omitempty"`
		ResultType string        `json:"result_type,omitempty"`
		Status     value.Integer `json:"status,omitempty"`
	}{p.operation, p.span, p.receipt, p.resultType, p.status})
}

type activationFailure struct {
	error
	progress ActivationProgress
}

func (e activationFailure) Unwrap() error { return e.error }

// WithActivationProgress retains a completed operation separately from a later failure.
func WithActivationProgress(err error, progress ActivationProgress) error {
	if err == nil || progress == nil {
		return err
	}
	return activationFailure{err, progress}
}

// ActivationDetails returns the most recent completed operation carried by a failure.
func ActivationDetails(err error) ActivationProgress {
	var failure activationFailure
	if errors.As(err, &failure) {
		return failure.progress
	}
	return nil
}

func (s *session) RecordActivation(operation string, span source.Span, result value.Value) error {
	progress := &activationProgress{operation: operation, span: span}
	switch result := result.(type) {
	case value.Receipt:
		progress.receipt = result
	case value.HTTPValue:
		if result.Type().Kind() != types.ResponseKind {
			return fmt.Errorf("unsupported HTTP completion type")
		}
		progress.resultType = result.Type().String()
		progress.status = result.Fields().Field("status").(value.Integer)
	default:
		return fmt.Errorf("unsupported activation completion type")
	}
	s.lastActivation = progress
	return nil
}
