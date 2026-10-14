package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	DefaultSteps        uint64 = 1_000_000
	MaxBytes                   = 16 << 20
	MaxItems                   = 1_000_000
	MaxIntegerBits             = 1_048_576
	MaxDepth                   = 256
	MaxValueWeight      uint64 = 16 << 20
	MaxEvaluationWeight uint64 = 64 << 20
)

type Boundary interface {
	Read(context.Context, value.Text) (value.Bytes, error)
	Commit(context.Context, value.Target) error
}
type Budget interface {
	Steps() uint64
	budget()
}
type budget uint64

func (budget) budget()         {}
func (b budget) Steps() uint64 { return uint64(b) }
func NewBudget(steps uint64) (Budget, error) {
	if steps == 0 {
		return nil, fmt.Errorf("execution steps must be positive")
	}
	return budget(steps), nil
}
func DefaultBudget() Budget { return budget(DefaultSteps) }

type Failure interface {
	error
	Value() value.Error
	failure()
}
type failure struct{ cause value.Error }

func (failure) failure()             {}
func (f failure) Value() value.Error { return f.cause }
func (f failure) Error() string      { return f.cause.Kind() + ": " + f.cause.Message() }

type HostError interface {
	error
	Code() diagnostic.Code
	Unwrap() error
	hostError()
}
type hostError struct {
	code    diagnostic.Code
	message string
	cause   error
}

func (hostError) hostError()              {}
func (e hostError) Code() diagnostic.Code { return e.code }
func (e hostError) Unwrap() error         { return e.cause }
func (e hostError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func Denied(operation, subject string) HostError {
	return hostError{diagnostic.CapabilityDenied, fmt.Sprintf("%s %q has no host capability", operation, subject), nil}
}
func Limit(subject string) HostError {
	return hostError{diagnostic.RuntimeLimit, "execution limit: " + subject, nil}
}
func HostFailure(operation, subject string, cause error) HostError {
	return hostError{diagnostic.RuntimeHost, fmt.Sprintf("%s %q", operation, subject), cause}
}
func ServerRequiresServe() HostError {
	return hostError{diagnostic.ServerRequiresServe, "HTTP server activation requires lo serve", nil}
}
func NoServer() HostError {
	return hostError{diagnostic.NoServer, "serve requires an activated HTTP server", nil}
}
func BoundaryHostError(err error) HostError {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	var host HostError
	if errors.As(err, &host) {
		return hostError{host.Code(), "boundary operation failed", err}
	}
	return nil
}

func Failed(cause value.Error) Failure { return failure{cause} }

func Published(v value.Value) (value.Value, error) {
	if f, ok := v.(value.Fallible); ok {
		_, cause := f.Outcome()
		if cause != nil {
			return nil, failure{cause}
		}
	}
	return v, nil
}
