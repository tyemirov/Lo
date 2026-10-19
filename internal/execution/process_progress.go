package execution

import (
	"errors"

	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type processProgress interface {
	error
	ProcessProgress() value.Record
}

// ProcessProgress returns counts and lifecycle status, without captured output.
func ProcessProgress(err error) value.Record {
	var progress processProgress
	if errors.As(err, &progress) {
		return progress.ProcessProgress()
	}
	return nil
}
