package integration_test

import (
	"bytes"

	"io"

	"sync/atomic"
)

type lifecycleReadPipe struct {
	data     []byte
	err      error
	closeErr error
	closes   atomic.Int32
}

func (p *lifecycleReadPipe) Read(buffer []byte) (int, error) {
	if len(p.data) == 0 {
		return 0, io.EOF
	}
	count := copy(buffer, p.data)
	p.data = p.data[count:]
	return count, p.err
}
func (p *lifecycleReadPipe) Close() error { p.closes.Add(1); return p.closeErr }

type lifecycleInputPipe struct {
	closes  atomic.Int32
	writes  atomic.Int32
	failure error
}

func (p *lifecycleInputPipe) Write(data []byte) (int, error) {
	p.writes.Add(1)
	if p.failure != nil {
		return 2, p.failure
	}
	return len(data), nil
}

func (p *lifecycleInputPipe) Close() error { p.closes.Add(1); return nil }

type lifecycleWriteCloser struct{ bytes.Buffer }

func (w *lifecycleWriteCloser) Close() error { return nil }
