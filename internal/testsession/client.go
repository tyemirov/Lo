package testsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

type connectionLease struct {
	connection net.Conn
	stop       func() bool
	once       sync.Once
}

func (lease *connectionLease) Close() error {
	var failure error
	lease.once.Do(func() {
		lease.stop()
		failure = lease.connection.Close()
		if errors.Is(failure, net.ErrClosed) {
			failure = nil
		}
	})
	return failure
}

func exchange(ctx context.Context, address string, selected request) (response, *connectionLease, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", address)
	if err != nil {
		return response{}, nil, fmt.Errorf("connect to fixture session: %w", err)
	}
	lease := &connectionLease{connection: connection}
	lease.stop = context.AfterFunc(ctx, func() { _ = connection.Close() })
	if err := json.NewEncoder(connection).Encode(selected); err != nil {
		_ = lease.Close()
		return response{}, nil, fmt.Errorf("send fixture session request: %w", err)
	}
	var result response
	decoder := json.NewDecoder(io.LimitReader(connection, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		_ = lease.Close()
		if ctx.Err() != nil {
			return response{}, nil, ctx.Err()
		}
		return response{}, nil, fmt.Errorf("receive fixture session result: %w", err)
	}
	if result.Error != "" {
		_ = lease.Close()
		if result.Failure != nil {
			return response{}, nil, result.Failure
		}
		return response{}, nil, fmt.Errorf("fixture session: %s", result.Error)
	}
	return result, lease, nil
}

// Compile prepares an unretained fixture that later registry pressure can evict.
func Compile(ctx context.Context, address string, selected CompileRequest) (CompileResult, error) {
	result, lease, err := exchange(ctx, address, request{Operation: compileOperation, Compile: selected})
	if err != nil {
		return CompileResult{}, err
	}
	return result.Compile, lease.Close()
}

// CompileRetained keeps an executable alive until Close or worker disconnection.
// Its context controls preparation only. The caller must close the returned lease.
func CompileRetained(ctx context.Context, address string, selected CompileRequest) (CompileResult, io.Closer, error) {
	result, lease, err := exchange(ctx, address, request{Operation: retainedOperation, Compile: selected})
	if err != nil {
		return CompileResult{}, nil, err
	}
	lease.stop()
	if err := ctx.Err(); err != nil {
		_ = lease.Close()
		return CompileResult{}, nil, err
	}
	return result.Compile, lease, nil
}

// Source returns a session-owned immutable source file for exact source bytes.
func Source(ctx context.Context, address, text string) (string, error) {
	result, lease, err := exchange(ctx, address, request{Operation: sourceOperation, Source: text})
	if err != nil {
		return "", err
	}
	return result.Source, lease.Close()
}

// Inputs returns the private distribution admitted once for the whole session.
func Inputs(ctx context.Context, address string) (InputsResult, error) {
	result, lease, err := exchange(ctx, address, request{Operation: inputsOperation})
	if err != nil {
		return InputsResult{}, err
	}
	return result.Inputs, lease.Close()
}

// Acquire reserves one global backend slot until Close or worker disconnection.
func Acquire(ctx context.Context, address string) (io.Closer, error) {
	_, lease, err := exchange(ctx, address, request{Operation: leaseOperation})
	return lease, err
}
