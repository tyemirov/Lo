package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const (
	ConsoleStdout              = "stdout"
	ConsoleStderr              = "stderr"
	ConsoleText                = "text"
	ConsoleJSON                = "json"
	DefaultConsoleBytes uint64 = 1 << 20
	consoleBodyLimit           = 2 << 20
)

// ConsoleSink owns cooperative context-bound output. Write and Flush must return
// when their context ends; confirmed progress must never exceed the input length.
// The host never starts a detached goroutine around a supplied sink.
type ConsoleSink interface {
	Write(context.Context, []byte) (int, error)
	Flush(context.Context) error
}

type bufferConsoleSink struct{ buffer *bytes.Buffer }

func (s bufferConsoleSink) Write(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.buffer.Write(p)
}
func (bufferConsoleSink) Flush(ctx context.Context) error { return ctx.Err() }

// NewBufferConsoleSink supplies a concrete synchronous in-memory destination.
func NewBufferConsoleSink(buffer *bytes.Buffer) (ConsoleSink, error) {
	if buffer == nil {
		return nil, fmt.Errorf("Console buffer is missing")
	}
	return bufferConsoleSink{buffer}, nil
}

// SetRendering selects runner presentation before the host admits output.
func (c *Console) SetRendering(command, format string) error {
	if c == nil || c.ctx == nil {
		return fmt.Errorf("Console host is missing")
	}
	if format != ConsoleText && format != ConsoleJSON {
		return fmt.Errorf("Console output format is invalid")
	}
	if len(command) > 1024 {
		return fmt.Errorf("Console command metadata is too large")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != 0 || c.used != 0 || c.closing {
		return fmt.Errorf("Console presentation is already active")
	}
	c.options.Command, c.options.Format = command, format
	return nil
}

// ConsoleOptions supplies explicit destination authority and a logical-byte quota.
// MaxBytes zero disables the cumulative quota. Format must be selected explicitly.
type ConsoleOptions struct {
	Stdout, Stderr  ConsoleSink
	MaxBytes        uint64
	Command, Format string
}

// Console owns serialized output records and their retained cleanup lifetime.
type Console struct {
	options ConsoleOptions
	gate    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	used    uint64
	active  int
	closing bool
	done    chan struct{}
	cleanup error
	image   *consoleImage
}

// OpenConsole constructs an embedding host without enabling destinations implicitly.
func OpenConsole(options ConsoleOptions) (*Console, error) {
	if options.Format != ConsoleText && options.Format != ConsoleJSON {
		return nil, fmt.Errorf("Console requires an explicit output format")
	}
	if len(options.Command) > 1024 {
		return nil, fmt.Errorf("Console command metadata is too large")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Console{options: options, gate: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.gate <- struct{}{}
	return c, nil
}

type consoleBoundary struct {
	execution.Boundary
	console *Console
}

func (b *consoleBoundary) Directory(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileDirectory, path)
	if err != nil {
		return nil, err
	}
	return observer.Directory(ctx, path)
}
func (b *consoleBoundary) Scan(ctx context.Context, path value.Text) (value.Value, error) {
	observer, err := fileObserver(b.Boundary, catalog.FileScan, path)
	if err != nil {
		return nil, err
	}
	return observer.Scan(ctx, path)
}

// Wrap adds Console commit activation while preserving the underlying boundary.
func (c *Console) Wrap(boundary execution.Boundary) execution.Boundary {
	return &consoleBoundary{boundary, c}
}
func (b *consoleBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	line, ok := target.(value.ConsoleLine)
	if !ok {
		return b.Boundary.Commit(ctx, target)
	}
	data := append([]byte(line.Text()), '\n')
	_, err := b.console.Write(ctx, line.Stream(), data, line.Flush())
	if err != nil {
		return nil, err
	}
	return value.NewConsoleReceipt(line.Stream(), uint64(len(data)), line.Flush())
}

func (c *Console) begin(ctx context.Context) (context.Context, func(), error) {
	if c == nil || c.ctx == nil {
		return nil, nil, execution.HostFailure("Console", "host", errors.New("missing Console host"))
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return nil, nil, execution.HostFailure("Console", "host", errors.New("Console host is closing"))
	}
	c.active++
	c.mu.Unlock()
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	end := func() {
		stop()
		cancel()
		c.mu.Lock()
		c.active--
		c.finishLocked()
		c.mu.Unlock()
	}
	return child, end, nil
}

func (c *Console) reserve(count uint64) error {
	if c == nil || c.ctx == nil {
		return execution.HostFailure("Console", "host", errors.New("missing Console host"))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if count > math.MaxUint64-c.used || (c.options.MaxBytes != 0 && count > c.options.MaxBytes-c.used) {
		return execution.Limit("Console output bytes")
	}
	c.used += count
	return nil
}

func (c *Console) sink(stream string) (ConsoleSink, error) {
	var sink ConsoleSink
	switch stream {
	case ConsoleStdout:
		sink = c.options.Stdout
	case ConsoleStderr:
		sink = c.options.Stderr
	default:
		return nil, execution.HostFailure("Console", "stream", errors.New("invalid destination"))
	}
	if sink == nil {
		return nil, execution.Denied("Console.commit", stream)
	}
	return sink, nil
}

// Write serializes one logical output record and reports confirmed encoded bytes.
func (c *Console) Write(ctx context.Context, stream string, data []byte, flush bool) (int, error) {
	if len(data) > 1<<20 {
		return 0, execution.Limit("Console record bytes")
	}
	return c.writeRecord(ctx, stream, data, flush, true, true)
}

func (c *Console) writeRecord(ctx context.Context, stream string, data []byte, flush, event, charge bool) (int, error) {
	ctx, end, err := c.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer end()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.writeLocked(ctx, stream, data, flush, event, charge)
}

func (c *Console) writeLocked(ctx context.Context, stream string, data []byte, flush, event, charge bool) (int, error) {
	sink, err := c.sink(stream)
	if err != nil {
		return 0, err
	}
	if charge {
		if err := c.reserve(uint64(len(data))); err != nil {
			return 0, err
		}
	}
	encoded := data
	if event && c.options.Format == ConsoleJSON {
		encoded, err = json.Marshal(struct {
			Command  string `json:"command"`
			Event    string `json:"event"`
			Stream   string `json:"stream"`
			Encoding string `json:"encoding"`
			Data     string `json:"data"`
			Bytes    int    `json:"bytes"`
			Flush    bool   `json:"flush"`
		}{c.options.Command, "console", stream, "base64", base64.StdEncoding.EncodeToString(data), len(data), flush})
		if err != nil {
			return 0, execution.HostFailure("Console", "encoding", errors.New("encode output record"))
		}
		encoded = append(encoded, '\n')
	}
	if len(encoded) > consoleBodyLimit {
		return 0, execution.Limit("Console encoded record bytes")
	}
	total := 0
	for total < len(encoded) {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, writeErr := sink.Write(ctx, encoded[total:])
		if n < 0 || n > len(encoded)-total {
			return total, execution.HostFailure("Console", "sink", errors.New("invalid output progress"))
		}
		total += n
		if writeErr != nil {
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			if errors.Is(writeErr, context.Canceled) || errors.Is(writeErr, context.DeadlineExceeded) {
				return total, writeErr
			}
			var terminal execution.HostError
			if errors.As(writeErr, &terminal) {
				return total, writeErr
			}
			failure, _ := execution.NewConsoleWriteError(stream, uint64(total), "write")
			return total, failure
		}
		if n == 0 {
			failure, _ := execution.NewConsoleWriteError(stream, uint64(total), "write")
			return total, failure
		}
	}
	if flush {
		if err := sink.Flush(ctx); err != nil {
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return total, err
			}
			var terminal execution.HostError
			if errors.As(err, &terminal) {
				return total, err
			}
			failure, _ := execution.NewConsoleWriteError(stream, uint64(total), "flush")
			return total, failure
		}
	}
	if err := ctx.Err(); err != nil {
		return total, err
	}
	return total, nil
}

// WriteTerminal reserves both payloads before publishing either destination.
func (c *Console) WriteTerminal(ctx context.Context, stdout, stderr []byte) error {
	if len(stdout) > 1<<20 || len(stderr) > 1<<20-len(stdout) {
		return execution.Limit("terminal output bytes")
	}
	ctx, end, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer end()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, entry := range []struct {
		stream string
		data   []byte
	}{{ConsoleStdout, stdout}, {ConsoleStderr, stderr}} {
		if len(entry.data) != 0 {
			if _, err := c.sink(entry.stream); err != nil {
				return err
			}
		}
	}
	if err := c.reserve(uint64(len(stdout) + len(stderr))); err != nil {
		return err
	}
	for _, entry := range []struct {
		stream string
		data   []byte
	}{{ConsoleStdout, stdout}, {ConsoleStderr, stderr}} {
		if len(entry.data) != 0 {
			if _, err := c.writeLocked(ctx, entry.stream, entry.data, true, true, false); err != nil {
				return err
			}
		}
	}
	return nil
}

type consoleWriter struct {
	host   *Console
	ctx    context.Context
	stream string
}

func (w consoleWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) != 0 {
		size := len(p)
		if size > consoleBodyLimit {
			size = consoleBodyLimit
		}
		count, err := w.host.writeRecord(w.ctx, w.stream, p[:size], true, false, false)
		total += count
		if err != nil {
			return total, err
		}
		p = p[size:]
	}
	return total, nil
}

// Writer adapts final renderers to the same cancellable output owner, without
// charging diagnostic or envelope bytes against application output authority.
func (c *Console) Writer(ctx context.Context, stream string) io.Writer {
	return consoleWriter{c, ctx, stream}
}

func (c *Console) finishLocked() {
	if !c.closing || c.active != 0 {
		return
	}
	select {
	case <-c.done:
		return
	default:
	}
	if c.image != nil {
		c.cleanup = c.image.registry.Close()
	}
	close(c.done)
}

// Close cancels admitted callbacks and waits for the retained cleanup owner.
func (c *Console) Close(ctx context.Context) error {
	if c == nil || c.ctx == nil {
		return nil
	}
	c.mu.Lock()
	c.closing = true
	c.cancel()
	c.finishLocked()
	c.mu.Unlock()
	select {
	case <-c.done:
		return c.cleanup
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitRetained joins the same cleanup owner without restarting cleanup.
func (c *Console) WaitRetained() error {
	if c == nil || c.ctx == nil {
		return nil
	}
	<-c.done
	return c.cleanup
}
