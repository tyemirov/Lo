package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

// MaxProcessOutputBytes is the maximum captured byte count for each output stream.
const MaxProcessOutputBytes = 1 << 20

// ProcessInvocation contains only the explicit command inputs. Getters copy data.
type ProcessInvocation interface {
	Executable() string
	Argv() []string
	Environment() []string
	Directory() string
	Stdin() []byte
	processInvocation()
}
type processInvocation struct {
	executable, directory string
	argv, environment     []string
	stdin                 []byte
}

func (processInvocation) processInvocation()      {}
func (v processInvocation) Executable() string    { return v.executable }
func (v processInvocation) Directory() string     { return v.directory }
func (v processInvocation) Argv() []string        { return slices.Clone(v.argv) }
func (v processInvocation) Environment() []string { return slices.Clone(v.environment) }
func (v processInvocation) Stdin() []byte         { return slices.Clone(v.stdin) }
func processInputs(command value.ProcessCommand) ProcessInvocation {
	v := processInvocation{executable: command.Executable().Path().Text(), directory: command.Directory().Text(), stdin: command.Stdin().Bytes()}
	v.argv = []string{v.executable}
	for _, argument := range command.Arguments().Items() {
		v.argv = append(v.argv, argument.(value.Text).Text())
	}
	v.environment = []string{}
	for _, entry := range command.Environment().Entries() {
		v.environment = append(v.environment, entry.Key.(value.Text).Text()+"="+entry.Value.(value.Text).Text())
	}
	sort.Strings(v.environment)
	return v
}

// ProcessDriver is a trusted injected OS boundary. This package provides no OS driver.
// Start must honor its context. Every returned child transfers ownership, even on
// error. A nil child with an error guarantees that no owned resources remain.
type ProcessDriver interface {
	Start(context.Context, ProcessInvocation) (ProcessChild, error)
}

// ProcessChild owns a primary process, its descendants, and private handles.
// Pipe Close returns promptly and unblocks concurrent IO, including after an error.
// Stop is idempotent and covers descendants.
// Wait runs once and reports primary termination separately from observation errors.
// Stop, Wait, and Release must honor their contexts. Release succeeds only after
// pending creation is settled, no future child can appear, all private handles
// are released, and no primary or descendants survive. On failure, retain
// enough ownership for Stop and another Release. These are trusted-driver duties,
// not native confinement guarantees established by the lifecycle core.
type ProcessChild interface {
	// StartConfirmed reports that the owned OS primary was created, including
	// late creation. It does not confirm target exec or an execution permit.
	// The getter is prompt, concurrency-safe, monotonic, and valid after Release.
	// False means unconfirmed, not proof that creation cannot still occur.
	StartConfirmed() bool
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait(context.Context) (value.ProcessTermination, error)
	Stop(context.Context) error
	Release(context.Context) error
}

// ProcessLimits bounds captured bytes per stream, execution time, and cleanup time.
type ProcessLimits interface {
	OutputBytes() int
	Timeout() time.Duration
	CleanupTimeout() time.Duration
	processLimits()
}
type processLimits struct {
	output           int
	timeout, cleanup time.Duration
}

func (processLimits) processLimits()                  {}
func (l processLimits) OutputBytes() int              { return l.output }
func (l processLimits) Timeout() time.Duration        { return l.timeout }
func (l processLimits) CleanupTimeout() time.Duration { return l.cleanup }

// NewProcessLimits validates capture and lifecycle bounds for a trusted embedding.
func NewProcessLimits(outputBytes int, timeout, cleanup time.Duration) (ProcessLimits, error) {
	if outputBytes <= 0 || outputBytes > MaxProcessOutputBytes || timeout <= 0 || cleanup <= 0 {
		return nil, fmt.Errorf("Process limits require 1..%d output bytes and positive durations", MaxProcessOutputBytes)
	}
	return processLimits{outputBytes, timeout, cleanup}, nil
}

// DefaultProcessLimits permits one MiB per stream, five seconds of execution,
// and a separate one-second cleanup interval.
func DefaultProcessLimits() ProcessLimits {
	return processLimits{MaxProcessOutputBytes, 5 * time.Second, time.Second}
}

// ProcessLifecycle executes the shared protocol through a trusted injected driver.
// Constructing this executor neither qualifies nor enables a native backend.
type ProcessLifecycle interface {
	Execute(context.Context, value.ProcessCommand) (value.ProcessResult, error)
	processLifecycle()
}
type processLifecycle struct {
	driver ProcessDriver
	limits ProcessLimits
}

func (*processLifecycle) processLifecycle() {}

// NewProcessLifecycle binds the shared protocol to a trusted injected driver.
func NewProcessLifecycle(driver ProcessDriver, limits ProcessLimits) (ProcessLifecycle, error) {
	if driver == nil || limits == nil {
		return nil, fmt.Errorf("Process lifecycle requires a driver and limits")
	}
	return &processLifecycle{driver, limits}, nil
}

type processLifecycleBoundary struct {
	execution.Boundary
	lifecycle ProcessLifecycle
}

// WithProcessLifecycle supplies an executor to a trusted embedding. Standard CLI
// execution continues to install WithProcess, which rejects every Process command.
func WithProcessLifecycle(boundary execution.Boundary, lifecycle ProcessLifecycle) (execution.Boundary, error) {
	if boundary == nil || lifecycle == nil {
		return nil, fmt.Errorf("Process lifecycle boundary requires dependencies")
	}
	return &processLifecycleBoundary{boundary, lifecycle}, nil
}
func (b *processLifecycleBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	if command, ok := target.(value.ProcessCommand); ok {
		return b.lifecycle.Execute(ctx, command)
	}
	return b.Boundary.Commit(ctx, target)
}

type processCloser struct {
	io.Closer
	once sync.Once
	err  error
}

func (c *processCloser) close() error {
	c.once.Do(func() {
		if c.Closer != nil {
			c.err = c.Closer.Close()
		}
	})
	return c.err
}

type processEvent struct {
	stage         string
	count         int
	data          []byte
	termination   value.ProcessTermination
	err, closeErr error
}

func processCapture(stage string, reader io.Reader, closer *processCloser, limit int, events chan<- processEvent) {
	event := processEvent{stage: stage}
	buffer := make([]byte, 4096)
	emptyReads := 0
	for {
		remaining := limit - len(event.data)
		size := min(len(buffer), remaining+1)
		n, err := reader.Read(buffer[:size])
		if n < 0 || n > size {
			event.err = execution.HostFailure("Process capture", stage, errors.New("invalid driver read count"))
			break
		}
		if n > 0 {
			emptyReads = 0
			event.data = append(event.data, buffer[:min(n, remaining)]...)
			if n > remaining {
				event.err = execution.Limit("Process " + stage + " bytes")
				break
			}
		} else {
			emptyReads++
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				event.err = err
			}
			break
		}
		if emptyReads >= 100 {
			event.err = io.ErrNoProgress
			break
		}
	}
	event.closeErr = closer.close()
	events <- event
}
func processInput(writer io.Writer, closer *processCloser, input []byte, events chan<- processEvent) {
	event := processEvent{stage: processStdin}
	for event.count < len(input) {
		n, err := writer.Write(input[event.count:])
		if n < 0 || n > len(input)-event.count {
			event.err = execution.HostFailure("Process input", "stdin", errors.New("invalid driver write count"))
			break
		}
		event.count += n
		if err != nil {
			event.err = err
			break
		}
		if n == 0 {
			event.err = io.ErrShortWrite
			break
		}
	}
	event.closeErr = closer.close()
	events <- event
}

func (l *processLifecycle) Execute(parent context.Context, command value.ProcessCommand) (value.ProcessResult, error) {
	if command == nil {
		return nil, execution.HostFailure("Process execution", "command", errors.New("missing checked command"))
	}
	ctx, cancel := context.WithTimeout(parent, l.limits.Timeout())
	defer cancel()
	progress := &processFailure{stage: processStart, primaryClass: processExecution}
	if err := ctx.Err(); err != nil {
		progress.setPrimary(processStart, err)
		progress.cleanupComplete = true
		return nil, progress
	}
	invocation := processInputs(command)
	child, startErr := l.driver.Start(ctx, invocation)
	if child == nil {
		if startErr == nil {
			startErr = newProcessUnresolvedStartError()
		}
		progress.setPrimary(processStart, startErr)
		var unresolved *processUnresolvedStartError
		progress.containmentFailure = errors.As(startErr, &unresolved)
		progress.cleanupComplete = !progress.containmentFailure
		return nil, progress.finalFailure()
	}
	progress.startConfirmed = child.StartConfirmed()
	if ctx.Err() != nil {
		progress.setPrimary(processCancel, ctx.Err())
	}
	stdin, stdout, stderr := child.Stdin(), child.Stdout(), child.Stderr()
	closers := []*processCloser{{Closer: stdin}, {Closer: stdout}, {Closer: stderr}}
	if startErr != nil {
		progress.setPrimary(processStart, startErr)
	}
	if stdin == nil || stdout == nil || stderr == nil {
		progress.setPrimary(processStart, execution.HostFailure("Process start", "driver", errors.New("driver returned missing pipes")))
	}
	supervisor, stopSupervisor := context.WithCancel(context.WithoutCancel(parent))
	defer stopSupervisor()
	events := make(chan processEvent, 4)
	workers := 1
	go func() {
		termination, err := child.Wait(supervisor)
		events <- processEvent{stage: processWait, termination: termination, err: err}
	}()
	if stdin != nil {
		workers++
		if progress.cause == nil {
			go processInput(stdin, closers[0], invocation.Stdin(), events)
		} else {
			events <- processEvent{stage: processStdin, closeErr: closers[0].close()}
		}
	}
	if stdout != nil {
		workers++
		go processCapture(processStdout, stdout, closers[1], l.limits.OutputBytes(), events)
	}
	if stderr != nil {
		workers++
		go processCapture(processStderr, stderr, closers[2], l.limits.OutputBytes(), events)
	}
	var cleanup context.Context
	var cancelCleanup context.CancelFunc
	var stopDeadline func() bool
	beginCleanup := func() {
		if cleanup != nil {
			return
		}
		cleanup, cancelCleanup = context.WithTimeout(context.WithoutCancel(parent), l.limits.CleanupTimeout())
		stopDeadline = context.AfterFunc(cleanup, stopSupervisor)
		// Closing pipes unblocks workers before Stop waits for process resources.
		for _, closer := range closers {
			if err := closer.close(); err != nil {
				progress.pipeReleaseFailure()
			}
		}
		if err := child.Stop(cleanup); err != nil {
			progress.cleanup = append(progress.cleanup, "Process descendant termination failed")
		}
	}
	defer func() {
		if stopDeadline != nil {
			stopDeadline()
		}
		if cancelCleanup != nil {
			cancelCleanup()
		}
	}()
	if progress.cause != nil {
		beginCleanup()
	}
	for workers > 0 {
		var event processEvent
		if progress.cause != nil {
			event = <-events
		} else {
			select {
			case event = <-events:
			case <-ctx.Done():
				progress.setPrimary(processCancel, ctx.Err())
				beginCleanup()
				continue
			}
		}
		workers--
		switch event.stage {
		case processStdin:
			progress.stdinWritten = event.count
		case processStdout:
			progress.stdout = event.data
		case processStderr:
			progress.stderr = event.data
		case processWait:
			progress.termination = event.termination
			if event.termination != nil {
				progress.startConfirmed = true
			}
			if event.termination == nil && event.err == nil {
				event.err = execution.HostFailure("Process wait", "driver", errors.New("driver returned no termination"))
			}
		}
		if event.err != nil {
			progress.setPrimary(event.stage, event.err)
		}
		if event.closeErr != nil {
			progress.pipeReleaseFailure()
			if progress.cause == nil {
				progress.setPrimary(event.stage, event.closeErr)
			}
		}
		if progress.cause != nil {
			beginCleanup()
		}
	}
	if ctx.Err() != nil {
		progress.setPrimary(processCancel, ctx.Err())
		beginCleanup()
	}
	if cleanup == nil {
		var cancelNormalCleanup context.CancelFunc
		cleanup, cancelNormalCleanup = context.WithTimeout(context.WithoutCancel(parent), l.limits.CleanupTimeout())
		defer cancelNormalCleanup()
	}
	releaseErr := child.Release(cleanup)
	if releaseErr != nil {
		progress.setPrimary(processRelease, releaseErr)
		progress.hostCause = execution.HostFailure("Process resource release", "driver", errors.New("Process resource release failed"))
		// A failed normal finalization can expose survivors. Stop them, but never
		// convert this operation back to success after the cleanup attempt.
		if stopDeadline == nil {
			stopDeadline = context.AfterFunc(cleanup, stopSupervisor)
			if err := child.Stop(cleanup); err != nil {
				progress.cleanup = append(progress.cleanup, "Process descendant termination failed")
			}
			releaseErr = child.Release(cleanup)
		}
		progress.cleanup = append(progress.cleanup, "Process resource release failed")
	}
	progress.startConfirmed = progress.startConfirmed || child.StartConfirmed()
	progress.cleanupComplete = releaseErr == nil && !progress.pipeReleaseFailed
	progress.containmentFailure = !progress.cleanupComplete
	if progress.termination == nil {
		progress.containmentFailure = progress.containmentFailure || progress.startConfirmed
		progress.setPrimary(processWait, errors.New("Process termination was not confirmed"))
	}
	if ctx.Err() != nil {
		progress.setPrimary(processCancel, ctx.Err())
	}
	if progress.cause != nil {
		return nil, progress.finalFailure()
	}
	return value.NewProcessResult(progress.termination, value.NewBytes(progress.stdout), value.NewBytes(progress.stderr))
}
