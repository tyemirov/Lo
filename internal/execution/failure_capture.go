package execution

import (
	"context"
	"fmt"
	"sync"
)

// FailureCapture preserves one invocation's receipts after its resources close.
type FailureCapture interface {
	Preserve(error) error
	failureCapture()
}

type failureCapture struct {
	mu      sync.Mutex
	session Session
}

func (*failureCapture) failureCapture() {}

type failureCaptureKey struct{}

// CaptureFailure creates a context and receipt capture for one invocation.
func CaptureFailure(ctx context.Context) (context.Context, FailureCapture, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("failure capture requires an execution context")
	}
	capture := &failureCapture{}
	return context.WithValue(ctx, failureCaptureKey{}, capture), capture, nil
}

func (c *failureCapture) bind(session Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return fmt.Errorf("failure capture already belongs to an invocation")
	}
	c.session = session
	return nil
}

func (c *failureCapture) Preserve(err error) error {
	if err == nil {
		return nil
	}
	c.mu.Lock()
	session := c.session
	c.mu.Unlock()
	if session == nil {
		return err
	}
	return session.preserveFailure(err)
}
