package adapter

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const filePollMilliseconds = 50

type contextReader struct {
	ctx  context.Context
	file *os.File
}

func (r contextReader) Read(data []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := r.file.Read(data)
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
		if err := waitFile(r.ctx, r.file, unix.POLLIN); err != nil {
			return 0, err
		}
	}
}
func writeFile(ctx context.Context, file *os.File, data []byte) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var flagErr error
	if err := raw.Control(func(fd uintptr) { flagErr = unix.SetNonblock(int(fd), true) }); err != nil {
		return err
	}
	if flagErr != nil {
		return flagErr
	}
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Write(data)
		data = data[n:]
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			if err := waitFile(ctx, file, unix.POLLOUT); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func waitFile(ctx context.Context, file *os.File, event int16) error {
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var pollErr error
		var ready int16
		err := raw.Control(func(fd uintptr) {
			fds := []unix.PollFd{{Fd: int32(fd), Events: event}}
			_, pollErr = unix.Poll(fds, filePollMilliseconds)
			ready = fds[0].Revents
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if errors.Is(pollErr, unix.EINTR) {
			continue
		}
		if pollErr != nil {
			return pollErr
		}
		if ready != 0 {
			return nil
		}
	}
}
