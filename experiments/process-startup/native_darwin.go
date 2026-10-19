//go:build lo_process_native_qualification && darwin

package startup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

type darwinCreator struct{ launcher, directory string }

// NewDarwinCreator selects one sealed qualification launcher. It provides no
// standard-runtime integration and does not qualify the supplied image/profile.
func NewDarwinCreator(launcherPath string) (Creator, error) {
	if !filepath.IsAbs(launcherPath) || filepath.Clean(launcherPath) != launcherPath || filepath.Base(launcherPath) != "launcher" {
		return nil, failure(Creation)
	}
	resolved, err := filepath.EvalSymlinks(launcherPath)
	if err != nil || resolved != launcherPath {
		return nil, failure(Creation)
	}
	return &darwinCreator{launcherPath, filepath.Dir(launcherPath)}, nil
}

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

type nativeCommand struct {
	arm  bool
	done chan error
}

type nativePort struct {
	*os.File
	once sync.Once
	err  error
}

func (port *nativePort) Close() error {
	port.once.Do(func() { port.err = port.File.Close() })
	return port.err
}

type darwinPrimary struct {
	pid                                          int
	process                                      *os.Process
	input, output, diagnostic                    *nativePort
	frame, status, permit                        *nativePort
	wakeRead, wakeWrite                          *nativePort
	wakeFD                                       int
	wakeMu                                       sync.Mutex
	wakeClosed                                   bool
	kqueue                                       int
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
}

func (c *darwinCreator) Create(ctx context.Context, invocation adapter.ProcessInvocation) (Primary, error) {
	frame, err := nativeFrame(invocation)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var opened []*os.File
	pipe := func() (*os.File, *os.File, error) {
		read, write, err := os.Pipe()
		if err == nil {
			opened = append(opened, read, write)
		}
		return read, write, err
	}
	closeOpened := func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}
	inRead, inWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	outRead, outWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	errRead, errWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	frameRead, frameWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	statusRead, statusWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	permitRead, permitWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	wakeRead, wakeWrite, err := pipe()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	wakeFD, wakeReadFD, permitFD := int(wakeWrite.Fd()), int(wakeRead.Fd()), int(permitWrite.Fd())
	for _, fd := range []int{wakeFD, wakeReadFD, permitFD} {
		if err := unix.SetNonblock(fd, true); err != nil {
			closeOpened()
			return nil, failure(Creation)
		}
	}
	kqueue, err := unix.Kqueue()
	if err != nil {
		closeOpened()
		return nil, failure(Creation)
	}
	unix.CloseOnExec(kqueue)
	wakeEvent := unix.Kevent_t{Ident: uint64(wakeReadFD), Filter: unix.EVFILT_READ, Flags: unix.EV_ADD | unix.EV_ENABLE}
	if _, err := unix.Kevent(kqueue, []unix.Kevent_t{wakeEvent}, nil, nil); err != nil {
		_ = unix.Close(kqueue)
		closeOpened()
		return nil, failure(Creation)
	}
	if err := ctx.Err(); err != nil {
		_ = unix.Close(kqueue)
		closeOpened()
		return nil, err
	}
	command := exec.Command(c.launcher)
	command.Env = []string{"CI=1", "TMPDIR=" + c.directory}
	command.Dir = c.directory
	command.Stdin, command.Stdout, command.Stderr = inRead, outWrite, errWrite
	command.ExtraFiles = []*os.File{frameRead, statusWrite, permitRead}
	// Supervisor already owns pending creation. This call is not claimed to be
	// independently interruptible; a late returned primary still transfers.
	if err := command.Start(); err != nil {
		_ = unix.Close(kqueue)
		closeOpened()
		return nil, failure(Creation)
	}
	for _, file := range []*os.File{inRead, outWrite, errWrite, frameRead, statusWrite, permitRead} {
		_ = file.Close()
	}
	p := &darwinPrimary{pid: command.Process.Pid, process: command.Process,
		input: &nativePort{File: inWrite}, output: &nativePort{File: outRead}, diagnostic: &nativePort{File: errRead}, frame: &nativePort{File: frameWrite}, status: &nativePort{File: statusRead}, permit: &nativePort{File: permitWrite},
		wakeRead: &nativePort{File: wakeRead}, wakeWrite: &nativePort{File: wakeWrite}, wakeFD: wakeFD, kqueue: kqueue, permitFD: permitFD, releaseGate: make(chan struct{}, 1),
		events: make(chan Event, 16), commands: make(chan nativeCommand, 1), stopRequest: make(chan struct{}),
		reaped: make(chan struct{}), frameJoined: make(chan struct{}), statusJoined: make(chan struct{}), waitReady: make(chan struct{})}
	initial := unix.Kevent_t{Ident: uint64(p.pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_CLEAR, Fflags: unix.NOTE_EXIT}
	_, watchErr := unix.Kevent(kqueue, []unix.Kevent_t{initial}, nil, nil)
	go p.observe(wakeReadFD, watchErr)
	go p.readStatus()
	go func() {
		defer close(p.frameJoined)
		_, writeErr := io.Copy(p.frame, bytes.NewReader(frame))
		closeErr := p.frame.Close()
		if (writeErr != nil || closeErr != nil) && !p.stopping.Load() {
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
	p.stopping.Store(true)
	_ = p.ClosePermit()
	_ = p.frame.Close()
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
	for _, file := range []*nativePort{p.input, p.output, p.diagnostic, p.status, p.wakeRead} {
		failures = append(failures, file.Close())
	}
	p.wakeMu.Lock()
	p.wakeClosed = true
	failures = append(failures, p.wakeWrite.Close())
	p.wakeMu.Unlock()
	if !p.kqueueClosed {
		p.kqueueClosed = true
		failures = append(failures, unix.Close(p.kqueue))
	}
	failures = append(failures, p.ClosePermit(), p.processReleaseErr)
	if errors.Join(failures...) != nil || p.termination == nil || p.waitErr != nil {
		p.releaseErr = failure(Execution)
	}
	p.released = p.releaseErr == nil
	return p.releaseErr
}
