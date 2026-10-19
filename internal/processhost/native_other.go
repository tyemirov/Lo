//go:build !darwin

package processhost

import (
	"context"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
)

// NewDarwinCreator is unavailable on other host backends.
func NewDarwinCreator(context.Context, Config) (Creator, error) {
	return nil, execution.ProcessUnavailable()
}
