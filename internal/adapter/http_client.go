package adapter

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

const maxHTTPInterimResponses = 8
const remoteNotStarted = "not_started"
const remoteUnknown = "unknown"

// HTTPFailure exposes safe exchange progress without request payloads.
type HTTPFailure interface {
	error
	Stage() string
	RequestWritten() bool
	Outcome() string
	Unwrap() error
}
type httpFailure struct {
	cause   error
	stage   string
	written bool
	outcome string
}

func (e httpFailure) Error() string        { return catalog.HTTPExchange + " " + e.stage + " failed" }
func (e httpFailure) Unwrap() error        { return e.cause }
func (e httpFailure) Stage() string        { return e.stage }
func (e httpFailure) RequestWritten() bool { return e.written }
func (e httpFailure) Outcome() string      { return e.outcome }

// CleanupEvidence reports resource-release failures without boundary payloads.
func (e httpFailure) CleanupEvidence() []string {
	details := execution.CleanupDetails(e.cause)
	evidence := make([]string, len(details))
	for index := range evidence {
		evidence[index] = "HTTP resource release failed"
	}
	return evidence
}

type httpBoundary struct {
	execution.Boundary
	policy HTTPPolicy
}

// WithHTTP adds explicitly authorized HTTP client activation to a boundary.
func WithHTTP(boundary execution.Boundary, policy HTTPPolicy) (execution.Boundary, error) {
	if boundary == nil || policy == nil {
		return nil, fmt.Errorf("HTTP adapter requires a boundary and policy")
	}
	return &httpBoundary{boundary, policy}, nil
}
func (b *httpBoundary) Commit(ctx context.Context, target value.Target) (value.Value, error) {
	request, ok := target.(value.ClientRequest)
	if !ok {
		return b.Boundary.Commit(ctx, target)
	}
	return b.exchange(ctx, request)
}

type exchangeProgress struct {
	mu                sync.Mutex
	stage             string
	written, possible bool
	dialed, finalized bool
	connection        *httpResponseConnection
	limit             error
}

var httpStageOrder = map[string]int{"authorize": 0, "resolve": 1, "connect": 2, "tls": 3, "write-request": 4, "read-response": 5, "read-body": 6, "finalize": 7}

func (p *exchangeProgress) setStage(stage string) {
	p.mu.Lock()
	if httpStageOrder[stage] > httpStageOrder[p.stage] {
		p.stage = stage
	}
	p.mu.Unlock()
}
func (p *exchangeProgress) headerLimit(subject string) error {
	failure := execution.Limit(subject)
	p.mu.Lock()
	p.limit = failure
	p.stage = "read-response"
	p.mu.Unlock()
	return failure
}

func (p *exchangeProgress) failure(cause error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	outcome := remoteNotStarted
	if p.possible {
		outcome = remoteUnknown
	}
	if p.limit != nil && !errors.Is(cause, p.limit) {
		cause = errors.Join(p.limit, cause)
	}
	return httpFailure{cause, p.stage, p.written, outcome}
}
func (p *exchangeProgress) close() error {
	p.mu.Lock()
	if p.finalized || p.connection == nil {
		p.mu.Unlock()
		return nil
	}
	p.finalized = true
	connection := p.connection
	p.mu.Unlock()
	return connection.Close()
}

func (b *httpBoundary) exchange(parent context.Context, target value.ClientRequest) (output value.Value, resultErr error) {
	progress := &exchangeProgress{stage: "authorize"}
	body := target.Body().Bytes()
	if len(body) > MaxRequestBytes {
		return nil, progress.failure(execution.Limit("HTTP request body bytes"))
	}
	addresses, allowed := b.policy.addresses(target.Method().Text(), target.URL().Origin())
	if !allowed {
		return nil, progress.failure(execution.Denied(catalog.HTTPExchange, target.URL().Origin()))
	}
	ctx, cancel := context.WithTimeout(parent, IOTimeout)
	defer cancel()
	parsed := target.URL().Parsed()
	hostname := parsed.Hostname()
	progress.setStage("resolve")
	resolved := []netip.Addr{}
	if address, err := netip.ParseAddr(hostname); err == nil {
		resolved = append(resolved, address.Unmap())
	} else {
		var err error
		resolved, err = net.DefaultResolver.LookupNetIP(ctx, "ip", hostname)
		if err != nil {
			return nil, progress.failure(err)
		}
	}
	selected := []string{}
	for _, address := range resolved {
		address = address.Unmap()
		if _, ok := addresses[address]; ok {
			selected = append(selected, address.String())
		}
	}
	slices.Sort(selected)
	if len(selected) == 0 {
		return nil, progress.failure(execution.Denied(catalog.HTTPExchange, target.URL().Origin()))
	}
	dialAddress := net.JoinHostPort(selected[0], parsed.Port())
	dial := func(dialCtx context.Context, _ string, _ string) (net.Conn, error) {
		progress.mu.Lock()
		already := progress.dialed
		progress.dialed = true
		progress.mu.Unlock()
		if already {
			return nil, errors.New("HTTP exchange permits one connection attempt")
		}
		progress.setStage("connect")
		return (&net.Dialer{}).DialContext(dialCtx, "tcp", dialAddress)
	}
	wrap := func(connection net.Conn) *httpResponseConnection {
		monitored := &httpResponseConnection{Conn: connection, progress: progress, headers: true}
		progress.mu.Lock()
		progress.connection = monitored
		progress.mu.Unlock()
		return monitored
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, MaxResponseHeaderBytes: MaxHeaderBytes + 1, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	transport.DialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		connection, err := dial(dialCtx, network, address)
		if err != nil {
			return nil, err
		}
		return wrap(connection), nil
	}
	transport.DialTLSContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		connection, err := dial(dialCtx, network, address)
		if err != nil {
			return nil, err
		}
		progress.setStage("tls")
		secure := tls.Client(connection, &tls.Config{ServerName: hostname, NextProtos: []string{"http/1.1"}})
		if err := secure.HandshakeContext(dialCtx); err != nil {
			return nil, execution.WithOperationCleanup(err, connection.Close())
		}
		return wrap(secure), nil
	}
	defer transport.CloseIdleConnections()
	defer func() {
		if closeErr := progress.close(); closeErr != nil {
			output = nil
			resultErr = progress.failure(execution.WithOperationCleanup(resultErr, closeErr))
		}
	}()
	request, err := http.NewRequestWithContext(ctx, target.Method().Text(), target.URL().Text(), bytes.NewReader(body))
	if err != nil {
		return nil, progress.failure(errors.New("invalid checked HTTP request"))
	}
	request.GetBody = nil
	for _, entry := range target.Headers().Entries().Items() {
		fields := entry.(value.Record)
		request.Header.Add(fields.Field("name").(value.Text).Text(), string(fields.Field("value").(value.Bytes).Bytes()))
	}
	interimCount, interimBytes := 0, 0
	trace := &httptrace.ClientTrace{
		WroteHeaders: func() { progress.setStage("write-request") },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			progress.mu.Lock()
			progress.written = info.Err == nil
			if info.Err == nil && httpStageOrder[progress.stage] < httpStageOrder["read-response"] {
				progress.stage = "read-response"
			}
			progress.mu.Unlock()
		},
		Got1xxResponse: func(_ int, headers textproto.MIMEHeader) error {
			interimCount++
			interimBytes += httpHeaderSize(http.Header(headers))
			if interimCount > maxHTTPInterimResponses || interimBytes > MaxHeaderBytes {
				return progress.headerLimit("HTTP interim response headers")
			}
			return nil
		},
	}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	response, err := transport.RoundTrip(request)
	if err != nil {
		if response != nil {
			err = execution.WithOperationCleanup(err, response.Body.Close())
		}
		return nil, progress.failure(err)
	}
	progress.setStage("read-response")
	closeBody := func(primary error) error {
		return execution.WithOperationCleanup(primary, execution.WithOperationCleanup(response.Body.Close(), progress.close()))
	}
	if response.ProtoMajor != 1 || response.ProtoMinor != 1 {
		return nil, progress.failure(closeBody(errors.New("unsupported HTTP response version")))
	}
	if response.StatusCode < 200 || response.StatusCode > 599 {
		return nil, progress.failure(closeBody(errors.New("unsupported HTTP response status")))
	}
	if httpHeaderSize(response.Header) > MaxHeaderBytes {
		return nil, progress.failure(closeBody(execution.Limit("HTTP response header bytes")))
	}
	entries := []value.Value{}
	for name, contents := range response.Header {
		for _, content := range contents {
			entries = append(entries, must(value.NewRecord([]value.Field{{Name: "name", Value: text(name)}, {Name: "value", Value: value.NewBytes([]byte(content))}})))
		}
	}
	headers, err := value.NewHeaders(must(value.NewList(types.HeaderEntries(), entries)))
	if err != nil {
		return nil, progress.failure(closeBody(errors.New("invalid HTTP response headers")))
	}
	progress.setStage("read-body")
	content, readErr := io.ReadAll(io.LimitReader(response.Body, MaxRequestBytes+1))
	if readErr == nil && len(content) > MaxRequestBytes {
		readErr = execution.Limit("HTTP response body bytes")
	}
	if readErr == nil && response.StatusCode == http.StatusResetContent && len(content) > 0 {
		readErr = errors.New("HTTP response status prohibits a body")
	}
	if readErr != nil {
		return nil, progress.failure(closeBody(readErr))
	}
	progress.setStage("finalize")
	if err := closeBody(nil); err != nil {
		return nil, progress.failure(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, progress.failure(err)
	}
	fields := must(value.NewRecord([]value.Field{{Name: "status", Value: value.Int64(int64(response.StatusCode))}, {Name: "headers", Value: headers}, {Name: "body", Value: value.NewBytes(content)}}))
	return value.NewHTTP(types.Response(), fields)
}
func httpHeaderSize(headers http.Header) int {
	size := 2
	for name, values := range headers {
		for _, value := range values {
			size += len(name) + len(value) + 4
		}
	}
	return size
}

type httpResponseConnection struct {
	net.Conn
	progress                   *exchangeProgress
	headers                    bool
	section                    []byte
	interimCount, interimBytes int
	once                       sync.Once
	closeErr                   error
}

func (c *httpResponseConnection) Close() error {
	c.once.Do(func() { c.closeErr = c.Conn.Close() })
	return c.closeErr
}
func (c *httpResponseConnection) Write(content []byte) (int, error) {
	c.progress.mu.Lock()
	c.progress.possible = true
	if httpStageOrder[c.progress.stage] < httpStageOrder["write-request"] {
		c.progress.stage = "write-request"
	}
	c.progress.mu.Unlock()
	return c.Conn.Write(content)
}
func (c *httpResponseConnection) Read(content []byte) (int, error) {
	count, err := c.Conn.Read(content)
	if count > 0 {
		c.progress.setStage("read-response")
	}
	for _, character := range content[:count] {
		if !c.headers {
			break
		}
		c.section = append(c.section, character)
		length := len(c.section)
		if character == '\n' && (length < 2 || c.section[length-2] != '\r') || length > 1 && c.section[length-2] == '\r' && character != '\n' {
			return 0, errors.New("HTTP response requires CRLF framing")
		}
		if len(c.section) > MaxHeaderBytes {
			return 0, c.progress.headerLimit("HTTP response header bytes")
		}
		if !bytes.HasSuffix(c.section, []byte("\r\n\r\n")) {
			if len(c.section) == MaxHeaderBytes {
				return 0, c.progress.headerLimit("HTTP response header bytes")
			}
			continue
		}
		first, _, _ := bytes.Cut(c.section, []byte("\r\n"))
		fields := strings.Fields(string(first))
		if len(fields) < 2 || fields[0] != "HTTP/1.1" {
			return 0, errors.New("unsupported HTTP response version")
		}
		status := 0
		if len(fields) > 1 {
			status, _ = strconv.Atoi(fields[1])
		}
		if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
			c.interimCount++
			c.interimBytes += len(c.section)
			if c.interimCount > maxHTTPInterimResponses || c.interimBytes > MaxHeaderBytes {
				return 0, c.progress.headerLimit("HTTP interim response headers")
			}
			c.section = c.section[:0]
		} else {
			c.headers = false
			c.section = nil
		}
	}
	return count, err
}
