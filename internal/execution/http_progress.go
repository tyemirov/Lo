package execution

import (
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type httpProgress interface {
	error
	Stage() string
	RequestWritten() bool
	Outcome() string
}

// HTTPProgress returns allowlisted exchange evidence without request or response payloads.
func HTTPProgress(err error) (value.Record, error) {
	var progress httpProgress
	if !errors.As(err, &progress) {
		return nil, nil
	}
	stage, stageErr := value.NewText(types.String(), progress.Stage())
	if stageErr != nil {
		return nil, fmt.Errorf("HTTP progress stage: %w", stageErr)
	}
	outcome, outcomeErr := value.NewText(types.String(), progress.Outcome())
	if outcomeErr != nil {
		return nil, fmt.Errorf("HTTP progress outcome: %w", outcomeErr)
	}
	return value.NewRecord([]value.Field{{Name: "stage", Value: stage}, {Name: "request_written", Value: value.Bool(progress.RequestWritten())}, {Name: "remote_outcome", Value: outcome}})
}
