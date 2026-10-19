package value

import (
	"fmt"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const (
	ProcessExited   = "exited"
	ProcessSignaled = "signaled"
)

// ProcessTermination describes exactly one completed process outcome.
type ProcessTermination interface {
	Value
	Kind() Text
	ExitCode() (Integer, error)
	Signal() (Integer, error)
	processTermination()
}

type processTermination struct {
	base
	kind    Text
	payload Integer
}

func (processTermination) processTermination() {}
func (v processTermination) Kind() Text        { return v.kind }
func (v processTermination) ExitCode() (Integer, error) {
	if v.kind.Text() != ProcessExited {
		return nil, fmt.Errorf("process termination is not exited")
	}
	return v.payload, nil
}
func (v processTermination) Signal() (Integer, error) {
	if v.kind.Text() != ProcessSignaled {
		return nil, fmt.Errorf("process termination is not signaled")
	}
	return v.payload, nil
}
func (v processTermination) MarshalJSON() ([]byte, error) {
	if v.kind.Text() == ProcessExited {
		return encode(v, struct {
			Kind     Text    `json:"kind"`
			ExitCode Integer `json:"exit_code"`
		}{v.kind, v.payload})
	}
	return encode(v, struct {
		Kind   Text    `json:"kind"`
		Signal Integer `json:"signal"`
	}{v.kind, v.payload})
}

// NewProcessExited validates a nonnegative exit code from a trusted driver.
func NewProcessExited(code Integer) (ProcessTermination, error) {
	if code == nil || !types.Equal(code.Type(), types.Int()) || code.Big().Sign() < 0 {
		return nil, fmt.Errorf("process exit code must be a nonnegative Int")
	}
	return newProcessTermination(ProcessExited, code), nil
}

// NewProcessSignaled validates a positive signal number from a trusted driver.
func NewProcessSignaled(signal Integer) (ProcessTermination, error) {
	if signal == nil || !types.Equal(signal.Type(), types.Int()) || signal.Big().Sign() <= 0 {
		return nil, fmt.Errorf("process signal must be a positive Int")
	}
	return newProcessTermination(ProcessSignaled, signal), nil
}
func newProcessTermination(kind string, payload Integer) ProcessTermination {
	text := must(NewText(types.String(), kind))
	return processTermination{newBase(types.ProcessTermination(), addWeight(text.Weight(), payload.Weight())), text, payload}
}

// ProcessResult contains a completed termination and captured output bytes.
type ProcessResult interface {
	Value
	Termination() ProcessTermination
	Stdout() Bytes
	Stderr() Bytes
	processResult()
}
type processResult struct {
	base
	termination    ProcessTermination
	stdout, stderr Bytes
}

func (processResult) processResult()                    {}
func (v processResult) Termination() ProcessTermination { return v.termination }
func (v processResult) Stdout() Bytes                   { return v.stdout }
func (v processResult) Stderr() Bytes                   { return v.stderr }
func (v processResult) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Termination ProcessTermination `json:"termination"`
		Stdout      Bytes              `json:"stdout"`
		Stderr      Bytes              `json:"stderr"`
	}{v.termination, v.stdout, v.stderr})
}

// NewProcessResult validates completed process data supplied by a trusted driver.
func NewProcessResult(termination ProcessTermination, stdout, stderr Bytes) (ProcessResult, error) {
	if termination == nil || stdout == nil || stderr == nil || !types.Equal(termination.Type(), types.ProcessTermination()) || !types.Equal(stdout.Type(), types.Bytes()) || !types.Equal(stderr.Type(), types.Bytes()) {
		return nil, fmt.Errorf("process result requires termination and captured Bytes")
	}
	weight := addWeight(termination.Weight(), addWeight(stdout.Weight(), stderr.Weight()))
	return processResult{newBase(types.ProcessResult(), weight), termination, stdout, stderr}, nil
}
