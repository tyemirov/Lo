package adapter

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"golang.org/x/sys/unix"
)

const consoleWriterArgument = "--lo-console-writer"

type consoleImage struct {
	path     string
	registry *processRegistry
	grants   []ProcessExecutableGrant
}

func bindConsoleImage(ctx context.Context) (*consoleImage, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	image := &consoleImage{path: path}
	seen := map[string]bool{}
	for _, name := range []string{path, resolved} {
		if seen[name] {
			continue
		}
		seen[name] = true
		file, err := os.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		digest, hashErr := hashProcessFile(ctx, file, defaultProcessRegistryMaxBytes)
		if err := errors.Join(hashErr, file.Close()); err != nil {
			return nil, err
		}
		grant, err := NewProcessExecutableGrant(name, digest)
		if err != nil {
			return nil, err
		}
		image.grants = append(image.grants, grant)
	}
	registry, err := OpenProtectedProcessRegistry(ctx, nil, image.grants, DefaultProcessRegistryLimits())
	if err != nil {
		return nil, err
	}
	image.registry = registry.(*processRegistry)
	return image, nil
}

// OpenCLIConsole binds its worker image before evaluation and owns inherited
// destinations without changing their shared descriptor status flags.
func OpenCLIConsole(ctx context.Context, options ConsoleOptions, stdout, stderr *os.File) (*Console, error) {
	if stdout == nil || stderr == nil {
		return nil, execution.HostFailure("Console", "destinations", errors.New("missing CLI descriptors"))
	}
	image, err := bindConsoleImage(ctx)
	if err != nil {
		return nil, execution.HostFailure("Console", "writer image", errors.New("cannot bind writer image"))
	}
	options.Stdout = &consoleProcessSink{image: image, stdout: stdout, stderr: stderr, destination: 1}
	options.Stderr = &consoleProcessSink{image: image, stdout: stdout, stderr: stderr, destination: 2}
	console, err := OpenConsole(options)
	if err != nil {
		return nil, errors.Join(err, image.registry.Close())
	}
	console.image = image
	return console, nil
}

// ProtectedImages returns descriptions for protection-only host registration.
// These descriptions never grant Process activation.
func (c *Console) ProtectedImages() []ProcessExecutableGrant {
	if c == nil || c.image == nil {
		return nil
	}
	return append([]ProcessExecutableGrant(nil), c.image.grants...)
}

type consoleProcessSink struct {
	image          *consoleImage
	stdout, stderr *os.File
	destination    byte
}

func (*consoleProcessSink) Flush(ctx context.Context) error { return ctx.Err() }

func (s *consoleProcessSink) Write(ctx context.Context, data []byte) (int, error) {
	if len(data) > consoleBodyLimit {
		return 0, execution.Limit("Console encoded record bytes")
	}
	for _, grant := range s.image.grants {
		if err := s.image.registry.validateExecutable(ctx, grant.Path()); err != nil {
			return 0, err
		}
	}
	requestRead, requestWrite, err := os.Pipe()
	if err != nil {
		return 0, consoleWorkerFailure()
	}
	defer requestRead.Close()
	defer requestWrite.Close()
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		return 0, consoleWorkerFailure()
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	liveRead, liveWrite, err := os.Pipe()
	if err != nil {
		return 0, consoleWorkerFailure()
	}
	defer liveRead.Close()
	defer liveWrite.Close()
	command := exec.Command(s.image.path, consoleWriterArgument)
	command.Stdin = requestRead
	command.Stdout = s.stdout
	command.Stderr = s.stderr
	command.ExtraFiles = []*os.File{ackWrite, liveRead}
	if err := command.Start(); err != nil {
		return 0, consoleWorkerFailure()
	}
	requestRead.Close()
	ackWrite.Close()
	liveRead.Close()
	// Cancellation owns the process and its protocol pipes. The callback never
	// outlives this operation: it is joined before returning.
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = command.Process.Kill()
		_ = requestWrite.Close()
		_ = ackRead.Close()
		_ = liveWrite.Close()
		close(cancelDone)
	})
	defer func() {
		if !stop() {
			<-cancelDone
		}
	}()
	header := make([]byte, 9)
	copy(header, "LCW1")
	header[4] = s.destination
	binary.BigEndian.PutUint32(header[5:], uint32(len(data)))
	_, sendErr := writeFile(ctx, requestWrite, header)
	if sendErr == nil {
		_, sendErr = writeFile(ctx, requestWrite, data)
	}
	sendErr = errors.Join(sendErr, requestWrite.Close())
	confirmed := uint64(0)
	complete, failed := false, false
	var protocolErr error
	if sendErr == nil {
		for {
			var frame [10]byte
			_, readErr := io.ReadFull(ackRead, frame[:])
			if readErr != nil {
				protocolErr = readErr
				break
			}
			count := binary.BigEndian.Uint64(frame[1:9])
			if count < confirmed || count > uint64(len(data)) {
				protocolErr = errors.New("invalid progress")
				break
			}
			switch frame[0] {
			case 1:
				if frame[9] != 0 || count <= confirmed {
					protocolErr = errors.New("invalid progress")
				} else {
					confirmed = count
				}
			case 2:
				if frame[9] != 0 || count != uint64(len(data)) {
					protocolErr = errors.New("invalid completion")
				} else {
					confirmed = count
					complete = true
				}
			case 3:
				if frame[9] != 1 && frame[9] != 2 {
					protocolErr = errors.New("invalid failure")
				} else {
					confirmed = count
					failed = true
				}
			default:
				protocolErr = errors.New("invalid acknowledgment")
			}
			if protocolErr != nil || complete || failed {
				break
			}
		}
	}
	if sendErr != nil || protocolErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if err := ctx.Err(); err != nil {
		return int(confirmed), err
	}
	if sendErr != nil || protocolErr != nil || waitErr != nil || (!complete && !failed) {
		return int(confirmed), consoleWorkerFailure()
	}
	if failed {
		return int(confirmed), io.ErrClosedPipe
	}
	return int(confirmed), nil
}

func consoleWorkerFailure() error {
	return execution.HostFailure("Console", "writer ownership", errors.New("writer protocol or ownership failed"))
}

// HandleConsoleWriter recognizes the private bounded worker protocol. The helper
// has no command, filesystem mutation, or language evaluation interface.
func HandleConsoleWriter(arguments []string) bool {
	if len(arguments) == 0 || arguments[0] != consoleWriterArgument {
		return false
	}
	if len(arguments) != 1 {
		os.Exit(125)
	}
	signal.Ignore(syscall.SIGPIPE)
	liveness := os.NewFile(4, "Console parent liveness")
	go func() { var b [1]byte; _, _ = liveness.Read(b[:]); os.Exit(125) }()
	ack := os.NewFile(3, "Console acknowledgment")
	var header [9]byte
	if _, err := io.ReadFull(os.Stdin, header[:]); err != nil || string(header[:4]) != "LCW1" || (header[4] != 1 && header[4] != 2) {
		os.Exit(125)
	}
	length := binary.BigEndian.Uint32(header[5:])
	if length > consoleBodyLimit {
		os.Exit(125)
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(os.Stdin, data); err != nil {
		os.Exit(125)
	}
	var extra [1]byte
	n, err := os.Stdin.Read(extra[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		os.Exit(125)
	}
	fd := 1
	if header[4] == 2 {
		fd = 2
	}
	confirmed := 0
	send := func(kind, class byte) {
		var frame [10]byte
		frame[0] = kind
		binary.BigEndian.PutUint64(frame[1:9], uint64(confirmed))
		frame[9] = class
		if _, err := ack.Write(frame[:]); err != nil {
			os.Exit(125)
		}
	}
	for confirmed < len(data) {
		count, err := unix.Write(fd, data[confirmed:])
		if count > 0 {
			confirmed += count
			send(1, 0)
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || count == 0 {
			class := byte(2)
			if errors.Is(err, unix.EPIPE) {
				class = 1
			}
			send(3, class)
			return true
		}
	}
	send(2, 0)
	return true
}
