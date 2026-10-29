package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// FileObserver supplies metadata observations through an authorized File boundary.
type FileObserver interface {
	Directory(context.Context, value.Text) (value.Value, error)
	Scan(context.Context, value.Text) (value.Value, error)
}

// RemovalGuardEvaluator invokes a checked program's pure removal predicate.
type RemovalGuardEvaluator func(value.RemovalGuard, value.FileEntry, value.FileEntry) (bool, error)

type removalGuardEvaluatorKey struct{}

// WithRemovalGuardEvaluator attaches the evaluator for one activation context.
func WithRemovalGuardEvaluator(ctx context.Context, evaluator RemovalGuardEvaluator) (context.Context, error) {
	if ctx == nil || evaluator == nil {
		return nil, fmt.Errorf("removal guard requires a context and evaluator")
	}
	return context.WithValue(ctx, removalGuardEvaluatorKey{}, evaluator), nil
}

// EvaluateRemovalGuard evaluates the predicate through the owning program.
func EvaluateRemovalGuard(ctx context.Context, guard value.RemovalGuard, original, current value.FileEntry, phase string, index int) (bool, error) {
	evaluator, ok := ctx.Value(removalGuardEvaluatorKey{}).(RemovalGuardEvaluator)
	if !ok {
		return false, HostFailure("evaluate removal guard", fmt.Sprintf("%s[%d]", phase, index), errors.New("missing checked program evaluator"))
	}
	return evaluator(guard, original, current)
}

type guardRejection struct {
	predicate string
	phase     string
	index     int
}

func (rejection guardRejection) Error() string { return "removal predicate returned false" }

// NewGuardRejection records a false predicate without inventing an application reason.
func NewGuardRejection(guard value.RemovalGuard, phase string, index int) error {
	return guardRejection{predicate: string(guard.Flow()), phase: phase, index: index}
}

// GuardRejectionDetails returns the public guard rejection evidence in an error chain.
func GuardRejectionDetails(err error) (value.Record, error) {
	var rejection guardRejection
	if !errors.As(err, &rejection) {
		return nil, nil
	}
	fields := []value.Field{{Name: "index", Value: value.Int64(int64(rejection.index))}}
	for _, field := range []struct{ name, input string }{
		{"predicate", rejection.predicate}, {"phase", rejection.phase}, {"condition", "predicate-false"},
	} {
		text, err := value.NewText(types.String(), field.input)
		if err != nil {
			return nil, fmt.Errorf("describe removal guard %s: %w", field.name, err)
		}
		fields = append(fields, value.Field{Name: field.name, Value: text})
	}
	return value.NewRecord(fields)
}
