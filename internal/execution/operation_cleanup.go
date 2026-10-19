package execution

import "errors"

type operationCleanup struct {
	primary error
	cleanup error
}

func (e operationCleanup) Error() string {
	return e.primary.Error() + "; cleanup: " + e.cleanup.Error()
}
func (e operationCleanup) Unwrap() []error { return []error{e.primary, e.cleanup} }

// WithOperationCleanup preserves the first failure and separate cleanup evidence.
func WithOperationCleanup(primary, cleanup error) error {
	if primary == nil {
		return cleanup
	}
	if cleanup == nil {
		return primary
	}
	return operationCleanup{primary, cleanup}
}

// CleanupDetails returns cleanup failures without changing the primary class.
func CleanupDetails(err error) []string {
	var details []string
	var visit func(error)
	visit = func(err error) {
		switch e := err.(type) {
		case interface{ CleanupEvidence() []string }:
			details = append(details, e.CleanupEvidence()...)
		case operationCleanup:
			visit(e.primary)
			details = append(details, e.cleanup.Error())
		case interface{ Unwrap() error }:
			visit(e.Unwrap())
		case interface{ Unwrap() []error }:
			for _, child := range e.Unwrap() {
				visit(child)
			}
		}
	}
	visit(err)
	return details
}

func primaryBoundaryFailure(err error) error {
	switch e := err.(type) {
	case operationCleanup:
		return primaryBoundaryFailure(e.primary)
	case HostError:
		return hostError{e.Code(), e.Error(), primaryBoundaryFailure(e.Unwrap())}
	case interface{ Unwrap() error }:
		return primaryBoundaryFailure(e.Unwrap())
	case interface{ Unwrap() []error }:
		children := e.Unwrap()
		primary := make([]error, len(children))
		for index, child := range children {
			primary[index] = primaryBoundaryFailure(child)
		}
		return errors.Join(primary...)
	}
	return err
}
