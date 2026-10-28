package adapter

import (
	"context"
	"errors"
	"slices"

	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	processStart     = "start"
	processStdin     = "stdin"
	processStdout    = "stdout"
	processStderr    = "stderr"
	processWait      = "wait"
	processRelease   = "release"
	processCancel    = "cancel"
	processExecution = "execution"
)

// processUnresolvedStartError preserves ownership uncertainty through trusted
// driver wrappers. No handle exists through which cleanup can be confirmed.
type processUnresolvedStartError struct{ cause error }

func (*processUnresolvedStartError) Error() string {
	return "Process start left unresolved child ownership"
}
func (e *processUnresolvedStartError) Unwrap() error { return e.cause }
func newProcessUnresolvedStartError() error {
	return &processUnresolvedStartError{cause: execution.HostFailure("Process start", "driver", errors.New("driver returned no child and no error"))}
}

// ProcessFailure retains bounded captured prefixes for trusted Go callers only.
// ProcessProgress and Error deliberately expose no command, output, or driver payload.
type ProcessFailure interface {
	error
	Unwrap() error
	Stdout() []byte
	Stderr() []byte
	ProcessProgress() value.Record
}
type processFailure struct {
	cause                                               error
	hostCause                                           error
	stage, primaryClass                                 string
	startConfirmed, cleanupComplete, containmentFailure bool
	pipeReleaseFailed                                   bool
	stdinWritten                                        int
	stdout, stderr                                      []byte
	termination                                         value.ProcessTermination
	cleanup                                             []string
}

func (p *processFailure) Error() string { return "Process execution failed during " + p.stage }
func (p *processFailure) Unwrap() error {
	if p.hostCause != nil {
		return p.hostCause
	}
	return p.cause
}
func (p *processFailure) Stdout() []byte            { return slices.Clone(p.stdout) }
func (p *processFailure) Stderr() []byte            { return slices.Clone(p.stderr) }
func (p *processFailure) CleanupEvidence() []string { return slices.Clone(p.cleanup) }
func (p *processFailure) pipeReleaseFailure() {
	if !p.pipeReleaseFailed {
		p.cleanup = append(p.cleanup, "Process pipe release failed")
	}
	p.pipeReleaseFailed = true
}
func (p *processFailure) ProcessProgress() value.Record {
	fields := []value.Field{{Name: "stage", Value: text(p.stage)}, {Name: "start_confirmed", Value: value.Bool(p.startConfirmed)}, {Name: "stdin_written", Value: value.Int64(int64(p.stdinWritten))}, {Name: "stdout_bytes", Value: value.Int64(int64(len(p.stdout)))}, {Name: "stderr_bytes", Value: value.Int64(int64(len(p.stderr)))}, {Name: "cleanup_complete", Value: value.Bool(p.cleanupComplete)}, {Name: "primary_class", Value: text(p.primaryClass)}}
	if p.termination != nil {
		fields = append(fields, value.Field{Name: "termination", Value: p.termination})
	}
	return must(value.NewRecord(fields))
}
func (p *processFailure) setPrimary(stage string, cause error) {
	var creation interface{ ProcessCreationFailure() }
	if cause != nil && errors.As(cause, &creation) {
		// Creation errors take precedence over the channel failures that they cause.
		stage = processStart
		p.cause, p.hostCause = nil, nil
	}
	if cause != nil && p.hostCause == nil {
		if execution.BoundaryHostError(cause) != nil {
			p.hostCause = cause
		}
	}
	if p.cause != nil || cause == nil {
		return
	}
	p.cause = cause
	p.stage = stage
	p.primaryClass = processExecution
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		p.primaryClass = "cancelled"
		return
	}
	var host execution.HostError
	if errors.As(cause, &host) {
		p.primaryClass = "host"
		if host.Code() == diagnostic.RuntimeLimit {
			p.primaryClass = "limit"
		}
	}
}
func (p *processFailure) finalFailure() error {
	if p.containmentFailure {
		p.hostCause = execution.HostFailure("Process cleanup", "termination or containment", errors.New("Process cleanup was not verified"))
	}
	return p
}
