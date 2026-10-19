package processhost

import (
	"context"
	"io"
	"sync"
)

type closeAttempt struct {
	done chan struct{}
	err  error
}

// pendingPort binds once and admits IO under the same lock that closes admission.
// IO and native Close run outside that lock. There is no buffering or IO worker.
type pendingPort struct {
	mu                     sync.Mutex
	ctx                    context.Context
	stopped                <-chan struct{}
	ready, closed, changed chan struct{}
	bound, closing         bool
	endpoint               io.Closer
	bindErr                error
	active                 int
	close                  *closeAttempt
}

func newPendingPort(ctx context.Context, stopped <-chan struct{}) *pendingPort {
	return &pendingPort{ctx: ctx, stopped: stopped, ready: make(chan struct{}), closed: make(chan struct{}), changed: make(chan struct{})}
}

func (p *pendingPort) stopError() error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	return io.ErrClosedPipe
}

func (p *pendingPort) stoppedNow() bool {
	select {
	case <-p.stopped:
		return true
	default:
		return false
	}
}

func (p *pendingPort) notify() { close(p.changed); p.changed = make(chan struct{}) }

func (p *pendingPort) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bound {
		return
	}
	p.bound, p.bindErr = true, err
	close(p.ready)
	p.notify()
}

func (p *pendingPort) beginClose() (*closeAttempt, io.Closer) {
	if !p.closing {
		p.closing = true
		close(p.closed)
		p.notify()
	}
	if p.endpoint == nil || p.close != nil {
		return p.close, nil
	}
	p.close = &closeAttempt{done: make(chan struct{})}
	return p.close, p.endpoint
}

func (p *pendingPort) completeClose(job *closeAttempt, endpoint io.Closer) {
	err := endpoint.Close()
	p.mu.Lock()
	job.err = err
	close(job.done)
	p.notify()
	p.mu.Unlock()
}

func (p *pendingPort) bind(endpoint io.Closer) {
	if endpoint == nil {
		p.fail(failure(Input))
		return
	}
	p.mu.Lock()
	p.endpoint, p.bound = endpoint, true
	close(p.ready)
	var job *closeAttempt
	var closeEndpoint io.Closer
	if p.closing || p.ctx.Err() != nil || p.stoppedNow() {
		job, closeEndpoint = p.beginClose()
	}
	p.notify()
	p.mu.Unlock()
	// A terminally closed pending port owns and closes a late endpoint before
	// binding completes. It never exposes that endpoint to a new IO operation.
	if closeEndpoint != nil {
		p.completeClose(job, closeEndpoint)
	}
}

func (p *pendingPort) admit() (io.Closer, error) {
	for {
		p.mu.Lock()
		if p.closing {
			p.mu.Unlock()
			return nil, io.ErrClosedPipe
		}
		if p.bound {
			if p.bindErr != nil {
				err := p.bindErr
				p.mu.Unlock()
				return nil, err
			}
			p.active++
			endpoint := p.endpoint
			p.mu.Unlock()
			return endpoint, nil
		}
		if err := p.ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if p.stoppedNow() {
			p.mu.Unlock()
			return nil, p.stopError()
		}
		p.mu.Unlock()
		select {
		case <-p.ready:
		case <-p.closed:
		case <-p.ctx.Done():
			return nil, p.ctx.Err()
		case <-p.stopped:
			return nil, p.stopError()
		}
	}
}

func (p *pendingPort) finishIO() {
	p.mu.Lock()
	p.active--
	p.notify()
	p.mu.Unlock()
}

func (p *pendingPort) Close() error {
	p.mu.Lock()
	job, endpoint := p.beginClose()
	p.mu.Unlock()
	if endpoint != nil {
		p.completeClose(job, endpoint)
	}
	if job == nil {
		return nil
	}
	<-job.done
	p.mu.Lock()
	err := job.err
	p.mu.Unlock()
	return err
}

func (p *pendingPort) join(ctx context.Context) error {
	for {
		p.mu.Lock()
		changed := p.changed
		if p.active == 0 {
			job := p.close
			if job == nil {
				p.mu.Unlock()
				return nil
			}
			select {
			case <-job.done:
				err := job.err
				p.mu.Unlock()
				return err
			default:
			}
		}
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type pendingRead struct{ *pendingPort }

func (p *pendingRead) Read(data []byte) (int, error) {
	endpoint, err := p.admit()
	if err != nil {
		return 0, err
	}
	defer p.finishIO()
	return endpoint.(io.Reader).Read(data)
}

type pendingWrite struct{ *pendingPort }

func (p *pendingWrite) Write(data []byte) (int, error) {
	endpoint, err := p.admit()
	if err != nil {
		return 0, err
	}
	defer p.finishIO()
	return endpoint.(io.Writer).Write(data)
}
