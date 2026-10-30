package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	DefaultSteps        uint64 = 1_000_000
	MaxBytes                   = 16 << 20
	MaxIntegerBits             = 1_048_576
	MaxDepth                   = 256
	MaxValueWeight      uint64 = 16 << 20
	MaxEvaluationWeight uint64 = 64 << 20
)

type Boundary interface {
	Read(context.Context, value.Text) (value.Bytes, error)
	List(context.Context, value.Text) (value.List, error)
	OpenPaths(context.Context, value.Text) (value.List, error)
	Commit(context.Context, value.Target) (value.Value, error)
}

// Limits configures host quotas. Zero disables the corresponding quota.
type Limits struct {
	ValueBytes      uint64
	LiveBytes       uint64
	DataBytes       uint64
	ListItems       uint64
	FileEntries     uint64
	FileDirectories uint64
}

// BudgetInput declares work and memory quotas for one execution.
type BudgetInput struct {
	Steps  uint64
	Limits Limits
}

type Budget interface {
	Steps() uint64
	Limits() Limits
	budget()
}
type budget struct{ input BudgetInput }

func (budget) budget()          {}
func (b budget) Steps() uint64  { return b.input.Steps }
func (b budget) Limits() Limits { return b.input.Limits }

// NewBudget validates the execution policy at the host boundary.
func NewBudget(input BudgetInput) (Budget, error) {
	if input.Steps == 0 {
		return nil, fmt.Errorf("execution steps must be positive")
	}
	return budget{input}, nil
}

// DefaultBudgetInput returns the host's default execution policy.
func DefaultBudgetInput() BudgetInput {
	return BudgetInput{Steps: DefaultSteps, Limits: Limits{ValueBytes: MaxValueWeight, LiveBytes: MaxEvaluationWeight, FileDirectories: 128}}
}
func DefaultBudget() Budget { return budget{DefaultBudgetInput()} }

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

// ProcessUnavailable reports the absence of a qualified host execution profile.
func ProcessUnavailable() HostError {
	return hostError{diagnostic.ProcessUnavailable, "Process activation requires a configured, qualified host execution profile", nil}
}
func NoServer() HostError {
	return hostError{diagnostic.NoServer, "serve requires an activated HTTP server", nil}
}
func BoundaryHostError(err error) HostError {
	primary := primaryBoundaryFailure(err)
	var budget device.BudgetError
	if errors.As(primary, &budget) {
		return hostError{diagnostic.RuntimeLimit, budget.Error(), err}
	}
	if errors.Is(primary, context.Canceled) || errors.Is(primary, context.DeadlineExceeded) {
		return hostError{diagnostic.RuntimeCancelled, "execution cancelled", err}
	}
	var host HostError
	if errors.As(primary, &host) {
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
	if !value.HostPublishable(v) {
		return nil, hostError{diagnostic.TensorPublication, "result contains a tensor without an explicit CPU transfer", nil}
	}
	return v, nil
}
