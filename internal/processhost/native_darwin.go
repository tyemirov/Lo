//go:build darwin

package processhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"golang.org/x/sys/unix"
)

const nativeFrameLimit = 8192
const nativeEntryLimit = 1024
const nativeEntryCount = 16

func nativeFrame(invocation adapter.ProcessInvocation) ([]byte, error) {
	if invocation == nil {
		return nil, failure(Input)
	}
	arguments, environment := invocation.Argv(), invocation.Environment()
	if len(arguments) == 0 || len(arguments) > nativeEntryCount || len(environment) > nativeEntryCount || arguments[0] != invocation.Executable() {
		return nil, failure(Input)
	}
	var body bytes.Buffer
	entries := []string{invocation.Executable(), invocation.Directory()}
	entries = append(entries, arguments...)
	entries = append(entries, environment...)
	for _, entry := range entries {
		if len(entry) > nativeEntryLimit || !utf8.ValidString(entry) || strings.ContainsRune(entry, 0) {
			return nil, failure(Input)
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(entry)))
		body.Write(length[:])
		body.WriteString(entry)
	}
	if body.Len() > nativeFrameLimit {
		return nil, failure(Input)
	}
	header := make([]byte, 16, 16+body.Len())
	copy(header, "LOP1")
	binary.BigEndian.PutUint32(header[4:8], uint32(body.Len()))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(arguments)))
	binary.BigEndian.PutUint32(header[12:16], uint32(len(environment)))
	return append(header, body.Bytes()...), nil
}

// nativeSetup injects only local acquisition/close/start boundaries. Native
// defaults remain the standard APIs; this is not host qualification policy.
type nativeSetup struct {
	pipe  func() (*os.File, *os.File, error)
	close func(*os.File) error
	start func(*exec.Cmd) error
}

func (setup *nativeSetup) openPipe() (*os.File, *os.File, error) {
	if setup != nil && setup.pipe != nil {
		return setup.pipe()
	}
	return os.Pipe()
}
func (setup *nativeSetup) closeFile(file *os.File) error {
	if setup != nil && setup.close != nil {
		return setup.close(file)
	}
	return file.Close()
}
func (setup *nativeSetup) startCommand(command *exec.Cmd) error {
	if setup != nil && setup.start != nil {
		return setup.start(command)
	}
	return command.Start()
}

type nativeCommand struct {
	arm  bool
	done chan error
}

type nativePort struct {
	*os.File
	once      sync.Once
	err       error
	closeFile func(*os.File) error
}

func (port *nativePort) Close() error {
	port.once.Do(func() {
		if port.closeFile != nil {
			port.err = port.closeFile(port.File)
		} else {
			port.err = port.File.Close()
		}
	})
	return port.err
}

type nativeDescriptor struct {
	fd   int
	once sync.Once
	err  error
}

func (descriptor *nativeDescriptor) Close() error {
	descriptor.once.Do(func() { descriptor.err = unix.Close(descriptor.fd) })
	return descriptor.err
}

func closeNativeInventory(resources []io.Closer) error {
	failed := false
	for _, resource := range resources {
		if resource.Close() != nil {
			failed = true
		}
	}
	if failed {
		return host("native resource release failed")
	}
	return nil
}

type resourceOnlyPrimary struct {
	resources          []io.Closer
	events             chan Event
	input              io.WriteCloser
	output, diagnostic io.ReadCloser
}
type resourceOnlyInput struct{}

func (resourceOnlyInput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (resourceOnlyInput) Close() error              { return nil }
func newResourceOnlyPrimary(resources []io.Closer) *resourceOnlyPrimary {
	events := make(chan Event)
	close(events)
	return &resourceOnlyPrimary{resources: resources, events: events, input: resourceOnlyInput{}, output: io.NopCloser(bytes.NewReader(nil)), diagnostic: io.NopCloser(bytes.NewReader(nil))}
}
func (*resourceOnlyPrimary) StartConfirmed() bool                 { return false }
func (primary *resourceOnlyPrimary) Stdin() io.WriteCloser        { return primary.input }
func (primary *resourceOnlyPrimary) Stdout() io.ReadCloser        { return primary.output }
func (primary *resourceOnlyPrimary) Stderr() io.ReadCloser        { return primary.diagnostic }
func (primary *resourceOnlyPrimary) Events() <-chan Event         { return primary.events }
func (*resourceOnlyPrimary) ArmExecution(context.Context) error   { return failure(Witness) }
func (*resourceOnlyPrimary) Permit(context.Context) (bool, error) { return false, failure(Permission) }
func (*resourceOnlyPrimary) Wait(context.Context) (value.ProcessTermination, error) {
	return nil, host("native startup resources retained")
}
func (*resourceOnlyPrimary) Stop(context.Context) error { return nil }
func (primary *resourceOnlyPrimary) Release(context.Context) error {
	return closeNativeInventory(primary.resources)
}

type darwinPrimary struct {
	pid                                          int
	process                                      *os.Process
	input, output, diagnostic                    *nativePort
	frame, status, permit                        *nativePort
	configuration                                *nativePort
	wakeRead, wakeWrite                          *nativePort
	wakeFD                                       int
	wakeMu                                       sync.Mutex
	wakeClosed                                   bool
	kqueue                                       int
	kqueueResource                               *nativeDescriptor
	events                                       chan Event
	commands                                     chan nativeCommand
	stopRequest                                  chan struct{}
	stopOnce                                     sync.Once
	signalAttempted, signalFailed                atomic.Bool
	reaped, frameJoined, statusJoined, waitReady chan struct{}
	termination                                  value.ProcessTermination
	waitErr, processReleaseErr                   error
	armed, exited, executed, stopping            atomic.Bool
	permitMu                                     sync.Mutex
	permitFD                                     int
	permitClosed, permitted                      bool
	permitErr                                    error
	releaseGate                                  chan struct{}
	kqueueClosed                                 bool
	released                                     bool
	releaseErr                                   error
	resourcePorts                                map[*os.File]*nativePort
	transferred                                  []*nativePort
}

func (c *darwinCreator) Create(ctx context.Context, invocation adapter.ProcessInvocation) (Primary, error) {
	if err := c.authorize(ctx, invocation); err != nil {
		return nil, err
	}
	frame, err := nativeFrame(invocation)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var resources []io.Closer
	ports := make(map[*os.File]*nativePort)
	ownPort := func(file *os.File) {
		port := &nativePort{File: file, closeFile: c.setup.closeFile}
		ports[file] = port
		resources = append(resources, port)
	}
	pipe := func() (*os.File, *os.File, error) {
		read, write, err := c.setup.openPipe()
		// Every transferred handle is owned, including partial acquisition.
		if read != nil {
			ownPort(read)
		}
		if write != nil {
			ownPort(write)
		}
		return read, write, err
	}
	setupFailure := func(cause error) (Primary, error) {
		if cleanupErr := closeNativeInventory(resources); cleanupErr != nil {
			return newResourceOnlyPrimary(resources), errors.Join(cause, cleanupErr)
		}
		return nil, cause
	}
	inRead, inWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	outRead, outWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	errRead, errWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	frameRead, frameWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	statusRead, statusWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	permitRead, permitWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	configurationRead, configurationWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	wakeRead, wakeWrite, err := pipe()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	wakeFD, wakeReadFD, permitFD := int(wakeWrite.Fd()), int(wakeRead.Fd()), int(permitWrite.Fd())
	for _, fd := range []int{wakeFD, wakeReadFD, permitFD} {
		if err := unix.SetNonblock(fd, true); err != nil {
			return setupFailure(failure(Creation))
		}
	}
	kqueue, err := unix.Kqueue()
	if err != nil {
		return setupFailure(failure(Creation))
	}
	queueResource := &nativeDescriptor{fd: kqueue}
	resources = append(resources, queueResource)
	unix.CloseOnExec(kqueue)
	wakeEvent := unix.Kevent_t{Ident: uint64(wakeReadFD), Filter: unix.EVFILT_READ, Flags: unix.EV_ADD | unix.EV_ENABLE}
	if _, err := unix.Kevent(kqueue, []unix.Kevent_t{wakeEvent}, nil, nil); err != nil {
		return setupFailure(failure(Creation))
	}
	if err := ctx.Err(); err != nil {
		return setupFailure(err)
	}
	command := exec.Command(c.launcher)
	command.Env = []string{"CI=1", "TMPDIR=" + c.directory}
	command.Dir = c.directory
	command.Stdin, command.Stdout, command.Stderr = inRead, outWrite, errWrite
	command.ExtraFiles = []*os.File{frameRead, statusWrite, permitRead, configurationRead}
	// Supervisor already owns pending creation. This call is not claimed to be
	// independently interruptible; a late returned primary still transfers.
	if err := c.setup.startCommand(command); err != nil {
		return setupFailure(failure(Creation))
	}
	p := &darwinPrimary{resourcePorts: ports, configuration: ports[configurationWrite], pid: command.Process.Pid, process: command.Process,
		input: ports[inWrite], output: ports[outRead], diagnostic: ports[errRead], frame: ports[frameWrite], status: ports[statusRead], permit: ports[permitWrite],
		wakeRead: ports[wakeRead], wakeWrite: ports[wakeWrite], wakeFD: wakeFD, kqueue: kqueue, kqueueResource: queueResource, permitFD: permitFD, releaseGate: make(chan struct{}, 1),
		events: make(chan Event, 16), commands: make(chan nativeCommand, 1), stopRequest: make(chan struct{}),
		reaped: make(chan struct{}), frameJoined: make(chan struct{}), statusJoined: make(chan struct{}), waitReady: make(chan struct{})}
	transferErr := c.closeTransferred(p, []*os.File{inRead, outWrite, errWrite, frameRead, statusWrite, permitRead, configurationRead})
	initial := unix.Kevent_t{Ident: uint64(p.pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_EXIT}
	_, watchErr := unix.Kevent(kqueue, []unix.Kevent_t{initial}, nil, nil)
	go p.observe(wakeReadFD, watchErr)
	go p.readStatus()
	go func() {
		defer close(p.frameJoined)
		_, configurationErr := io.Copy(p.configuration, bytes.NewReader(c.configuration))
		configurationCloseErr := p.configuration.Close()
		_, writeErr := io.Copy(p.frame, bytes.NewReader(frame))
		closeErr := p.frame.Close()
		if (configurationErr != nil || configurationCloseErr != nil || writeErr != nil || closeErr != nil) && !p.stopping.Load() {
			p.emit(Event{Kind: Failed, Stage: Input})
		}
	}()
	go func() {
		<-p.reaped
		<-p.frameJoined
		<-p.statusJoined
		close(p.events)
		close(p.waitReady)
	}()
	if transferErr != nil {
		return p, transferErr
	}
	if watchErr != nil {
		return p, failure(Witness)
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	return p, nil
}

func (*darwinPrimary) StartConfirmed() bool    { return true }
func (p *darwinPrimary) Stdin() io.WriteCloser { return p.input }
func (p *darwinPrimary) Stdout() io.ReadCloser { return p.output }
func (p *darwinPrimary) Stderr() io.ReadCloser { return p.diagnostic }
func (p *darwinPrimary) Events() <-chan Event  { return p.events }

func (p *darwinPrimary) emit(event Event) {
	// Native evidence has a fixed finite budget. No observer ever waits for an
	// output consumer or for space in a control-event queue.
	select {
	case p.events <- event:
	default:
		p.stopping.Store(true)
		p.stopOnce.Do(func() { close(p.stopRequest) })
		p.wake()
	}
}

func (p *darwinPrimary) wake() {
	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	if !p.wakeClosed {
		_, _ = unix.Write(p.wakeFD, []byte{1})
	}
}

func (p *darwinPrimary) readStatus() {
	defer close(p.statusJoined)
	ready := false
	for count := 0; count < 16; count++ {
		var packet [6]byte
		_, err := io.ReadFull(p.status, packet[:])
		if err == io.EOF {
			return
		} // EOF is never target execution evidence.
		if err != nil {
			if !p.stopping.Load() {
				p.emit(Event{Kind: Failed, Stage: Protocol})
			}
			return
		}
		if packet[0] == 'R' && packet[1] == 0 && binary.BigEndian.Uint32(packet[2:]) == 0 && !ready {
			ready = true
			p.emit(Event{Kind: Ready})
			continue
		}
		if packet[0] == 'F' {
			stages := map[byte]Stage{1: Confinement, 2: Input, 3: Permission, 4: Execution, 5: Protocol}
			stage, ok := stages[packet[1]]
			if !ok {
				stage = Protocol
			}
			p.emit(Event{Kind: Failed, Stage: stage})
			return
		}
		p.emit(Event{Kind: Failed, Stage: Protocol})
		return
	}
	p.emit(Event{Kind: Failed, Stage: Protocol})
}

func (p *darwinPrimary) ArmExecution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request := nativeCommand{arm: true, done: make(chan error, 1)}
	select {
	case p.commands <- request:
		p.wake()
	case <-ctx.Done():
		return ctx.Err()
	case <-p.reaped:
		return failure(Witness)
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-p.reaped:
		return failure(Witness)
	}
}

func (p *darwinPrimary) Permit(ctx context.Context) (bool, error) {
	p.permitMu.Lock()
	defer p.permitMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !p.armed.Load() || p.exited.Load() || p.stopping.Load() || p.permitted || p.permitClosed {
		return false, failure(Permission)
	}
	n, err := unix.Write(p.permitFD, []byte{'P'})
	p.permitted = n == 1
	p.permitClosed = true
	p.permitErr = p.permit.Close()
	if err != nil || n != 1 || p.permitErr != nil {
		return p.permitted, failure(Permission)
	}
	return true, nil
}

// ClosePermit is a candidate transport seam for the explicit EOF rejection test.
func (p *darwinPrimary) ClosePermit() error {
	p.permitMu.Lock()
	defer p.permitMu.Unlock()
	if !p.permitClosed {
		p.permitClosed = true
		p.permitErr = p.permit.Close()
	}
	return p.permitErr
}

func (p *darwinPrimary) observe(wakeReadFD int, initialErr error) {
	defer close(p.reaped)
	// Only a consumed NOTE_EXIT permits reaping a registered primary. The
	// initial-registration failure path cannot publish a permit and separately
	// owns cleanup without an execution witness.
	exitObserved := initialErr != nil
	stopHandled := false
	if initialErr != nil {
		p.emit(Event{Kind: Failed, Stage: Witness})
	}
	stop := func() {
		if stopHandled {
			return
		}
		stopHandled = true
		if !p.exited.Load() {
			p.signalAttempted.Store(true)
			if err := unix.Kill(p.pid, unix.SIGKILL); err != nil {
				p.signalFailed.Store(true)
			}
		}
	}
	if initialErr != nil {
		p.stopping.Store(true)
		stop()
	}
	var flags uint32
	drain := func() {
		var queued [8]unix.Kevent_t
		zero := unix.Timespec{}
		for {
			count, err := unix.Kevent(p.kqueue, nil, queued[:], &zero)
			if err == unix.EINTR {
				continue
			}
			if err != nil || count == 0 {
				break
			}
			for _, event := range queued[:count] {
				if event.Filter == unix.EVFILT_PROC && event.Ident == uint64(p.pid) {
					flags |= event.Fflags
				}
				if event.Filter == unix.EVFILT_READ {
					var buffer [64]byte
					for {
						if count, err := unix.Read(wakeReadFD, buffer[:]); err != nil || count == 0 {
							break
						}
					}
				}
			}
		}
		if flags&unix.NOTE_EXEC != 0 && p.executed.CompareAndSwap(false, true) {
			p.emit(Event{Kind: Executed})
		}
	}
	for {
		select {
		case <-p.stopRequest:
			stop()
		default:
		}
		select {
		case request := <-p.commands:
			if !request.arm || exitObserved || p.stopping.Load() || p.armed.Load() {
				request.done <- failure(Witness)
			} else {
				event := unix.Kevent_t{Ident: uint64(p.pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_EXEC | unix.NOTE_EXIT}
				_, err := unix.Kevent(p.kqueue, []unix.Kevent_t{event}, nil, nil)
				if err != nil {
					p.emit(Event{Kind: Failed, Stage: Witness})
					request.done <- failure(Witness)
				} else {
					p.armed.Store(true)
					request.done <- nil
				}
			}
		default:
		}
		if exitObserved {
			drain()
			var status unix.WaitStatus
			pid, err := unix.Wait4(p.pid, &status, unix.WNOHANG, nil)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				p.waitErr = failure(Execution)
				p.exited.Store(true)
				return
			}
			if pid == p.pid {
				p.exited.Store(true)
				if status.Exited() {
					p.termination, p.waitErr = value.NewProcessExited(value.Int64(int64(status.ExitStatus())))
				} else if status.Signaled() {
					p.termination, p.waitErr = value.NewProcessSignaled(value.Int64(int64(status.Signal())))
				} else {
					p.waitErr = failure(Execution)
				}
				p.processReleaseErr = p.process.Release()
				return
			}
		}
		var events [8]unix.Kevent_t
		var timeout *unix.Timespec
		if exitObserved {
			short := unix.NsecToTimespec(int64(10 * time.Millisecond))
			timeout = &short
		}
		n, err := unix.Kevent(p.kqueue, nil, events[:], timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			p.emit(Event{Kind: Failed, Stage: Witness})
			p.stopping.Store(true)
			stop()
			continue
		}
		for _, event := range events[:n] {
			if event.Filter == unix.EVFILT_READ {
				var bytes [64]byte
				for {
					if count, err := unix.Read(wakeReadFD, bytes[:]); err != nil || count == 0 {
						break
					}
				}
				continue
			}
			if event.Filter == unix.EVFILT_PROC && event.Ident == uint64(p.pid) {
				if event.Flags&unix.EV_ERROR != 0 {
					p.emit(Event{Kind: Failed, Stage: Witness})
					p.stopping.Store(true)
					stop()
					continue
				}
				flags |= event.Fflags
			}
		}
		if flags&unix.NOTE_EXIT != 0 {
			// Preserve coalesced or already-queued NOTE_EXEC before the only reap.
			drain()
			exitObserved = true
		}
		if flags&unix.NOTE_EXEC != 0 && p.executed.CompareAndSwap(false, true) {
			p.emit(Event{Kind: Executed})
		}
	}
}

func (p *darwinPrimary) Wait(ctx context.Context) (value.ProcessTermination, error) {
	select {
	case <-p.waitReady:
		return p.termination, p.waitErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *darwinPrimary) Stop(ctx context.Context) error {
	// Failed closes of transferred child-side endpoints can keep protocol IO
	// alive after primary termination. Break every owned protocol dependency
	// before a reaped fast path or join. Release retains all close outcomes.
	p.stopping.Store(true)
	_ = p.status.Close()
	_ = p.ClosePermit()
	_ = p.frame.Close()
	_ = p.configuration.Close()
	completed := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.termination == nil || p.waitErr != nil {
			return failure(Execution)
		}
		return nil
	}
	select {
	case <-p.reaped:
		return completed()
	default:
	}
	p.stopOnce.Do(func() { close(p.stopRequest) })
	p.wake()
	select {
	case <-p.reaped:
		return completed()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StopSignalEvidence retains the signal attempt independently of the observed
// termination that decides Stop success. It is safe after Release.
func (p *darwinPrimary) StopSignalEvidence() (attempted, failed bool) {
	return p.signalAttempted.Load(), p.signalFailed.Load()
}

func (p *darwinPrimary) Release(ctx context.Context) error {
	select {
	case p.releaseGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.releaseGate }()
	if p.released {
		return p.releaseErr
	}
	if err := p.Stop(ctx); err != nil {
		return err
	}
	select {
	case <-p.waitReady:
	case <-ctx.Done():
		return ctx.Err()
	}
	var failures []error
	for _, file := range []*nativePort{p.input, p.output, p.diagnostic, p.frame, p.configuration, p.status, p.wakeRead} {
		failures = append(failures, file.Close())
	}
	p.wakeMu.Lock()
	p.wakeClosed = true
	failures = append(failures, p.wakeWrite.Close())
	p.wakeMu.Unlock()
	if p.kqueueResource == nil {
		p.kqueueResource = &nativeDescriptor{fd: p.kqueue}
	}
	p.kqueueClosed = true
	failures = append(failures, p.kqueueResource.Close())
	for _, port := range p.transferred {
		failures = append(failures, port.Close())
	}
	failures = append(failures, p.ClosePermit(), p.processReleaseErr)
	if errors.Join(failures...) != nil || p.termination == nil || p.waitErr != nil {
		p.releaseErr = failure(Execution)
	}
	p.released = p.releaseErr == nil
	return p.releaseErr
}

// closeTransferred releases local child-side copies after ownership transfer.
func (c *darwinCreator) closeTransferred(primary *darwinPrimary, files []*os.File) error {
	var resources []io.Closer
	for _, file := range files {
		port := primary.resourcePorts[file]
		if port == nil {
			port = &nativePort{File: file, closeFile: c.setup.closeFile}
		}
		primary.transferred = append(primary.transferred, port)
		resources = append(resources, port)
	}
	return closeNativeInventory(resources)
}
