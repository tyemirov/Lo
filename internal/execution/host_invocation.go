package execution

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type hostInvocationKey struct{}
type initialHostInvocation struct {
	value value.HostInvocation
	err   error
}

// WithHostInvocation supplies validated immutable inputs to an embedded runner.
func WithHostInvocation(ctx context.Context, initial value.HostInvocation) (context.Context, error) {
	if ctx == nil || initial == nil || len(initial.Transport()) == 0 {
		return nil, fmt.Errorf("initial host invocation requires a context and validated value")
	}
	return context.WithValue(ctx, hostInvocationKey{}, initialHostInvocation{value: initial}), nil
}

// WithHostInvocationTransport decodes an initial snapshot and defers capture failure until observation.
func WithHostInvocationTransport(ctx context.Context, body []byte) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("initial host invocation requires a context")
	}
	initial, err := value.DecodeHostInvocation(body)
	return context.WithValue(ctx, hostInvocationKey{}, initialHostInvocation{value: initial, err: err}), nil
}

// CaptureHostInvocation captures host inputs once and keeps an injected capture result unchanged.
func CaptureHostInvocation(ctx context.Context, arguments []string) context.Context {
	if _, ok := ctx.Value(hostInvocationKey{}).(initialHostInvocation); ok {
		return ctx
	}
	ctx = CaptureProcessContext(ctx)
	process := ctx.Value(processContextKey{}).(initialProcessContext)
	home, err := value.NewText(types.Path(), process.home)
	var initial value.HostInvocation
	if err != nil {
		err = fmt.Errorf("capture host home: invalid text")
	} else {
		now := time.Now()
		nanoseconds := now.UnixNano()
		if !time.Unix(0, nanoseconds).Equal(now) {
			err = fmt.Errorf("capture host clock: outside the signed 64-bit range")
		} else {
			initial, err = value.NewHostInvocation(arguments, home, value.Int64(int64(os.Getuid())), value.Int64(int64(os.Geteuid())), value.Int64(nanoseconds))
		}
	}
	return context.WithValue(ctx, hostInvocationKey{}, initialHostInvocation{value: initial, err: err})
}

// InheritHostInvocation attaches the same capture result to a new request lifetime.
func InheritHostInvocation(ctx, parent context.Context) (context.Context, error) {
	if ctx == nil || parent == nil {
		return nil, fmt.Errorf("host invocation inheritance requires two contexts")
	}
	parent = CaptureHostInvocation(parent, nil)
	return context.WithValue(ctx, hostInvocationKey{}, parent.Value(hostInvocationKey{})), nil
}

// InitialHostInvocation returns the original capture result without another host observation.
func InitialHostInvocation(ctx context.Context) (value.HostInvocation, error) {
	initial, ok := ctx.Value(hostInvocationKey{}).(initialHostInvocation)
	if !ok {
		return nil, fmt.Errorf("initial host invocation was not captured")
	}
	return initial.value, initial.err
}
