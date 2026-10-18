package adapter

import (
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

func (s *servers) request(target value.Server, w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		panic(http.ErrAbortHandler)
	}
	s.requests.Add(1)
	s.mu.Unlock()
	defer s.requests.Done()
	defer func() {
		if p := recover(); p != nil {
			if p != http.ErrAbortHandler {
				s.fail(execution.HostFailure(catalog.HTTPRequest, r.URL.Path, fmt.Errorf("runtime invariant: %v", p)))
			}
			panic(http.ErrAbortHandler)
		}
	}()
	ctx, cancel := context.WithTimeout(r.Context(), IOTimeout)
	defer cancel()
	request, cause := readRequest(w, r)
	if cause != nil && cause.Kind() == catalog.RequestReadFailed && r.Context().Err() != nil {
		// A peer can finish its request stream and still receive the error response.
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(s.requestContext, IOTimeout)
		defer stop()
	}
	invocation, err := s.program.NewInvocation(ctx, s, s.budget, s.devices)
	if err != nil {
		s.abort(err)
	}
	defer func() {
		if closeErr := invocation.Close(); closeErr != nil {
			s.abort(execution.PreserveCleanup(err, closeErr))
		}
	}()
	var output value.Value
	if cause == nil {
		output, err = invocation.Invoke(target.Handler(), []value.Value{request})
		if err != nil {
			var failure execution.Failure
			if errors.As(err, &failure) {
				cause = failure.Value()
			} else {
				s.requestAbort(ctx, err)
			}
		}
	}
	var response responseData
	if cause == nil {
		response, cause = responseValue(output)
	}
	if cause != nil {
		operation := catalog.HTTPRequest
		if cause.Kind() == catalog.InvalidResponse {
			operation = catalog.HTTPRespond
		}
		input := must(value.NewRecord([]value.Field{{Name: "port", Value: target.Port()}}))
		cause = value.WithFrame(cause, "", operation, input)
		output, err = invocation.Invoke(target.OnError(), []value.Value{cause})
		if err != nil {
			s.requestAbort(ctx, err)
		}
		var invalid value.Error
		response, invalid = responseValue(output)
		if invalid != nil {
			s.abort(execution.HostFailure(catalog.HTTPRespond, "onError", fmt.Errorf("%s: %s", invalid.Kind(), invalid.Message())))
		}
	}
	if closeErr := invocation.Close(); closeErr != nil {
		s.abort(execution.PreserveCleanup(nil, closeErr))
	}
	for name, v := range response.headers {
		w.Header().Set(name, v)
	}
	w.WriteHeader(response.status)
	if r.Method != http.MethodHead && len(response.body) > 0 {
		if _, err := w.Write(response.body); err != nil {
			s.requestAbort(r.Context(), execution.HostFailure(catalog.HTTPRespond, r.URL.Path, err))
		}
	}
}
func (s *servers) requestAbort(ctx context.Context, err error) {
	if _, _, ok := execution.RemovalDetails(err); ok {
		s.abort(err)
	}
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if ctx.Err() != nil || stopping && errors.Is(err, context.Canceled) {
		panic(http.ErrAbortHandler)
	}
	s.abort(err)
}
func (s *servers) abort(err error) { s.fail(err); panic(http.ErrAbortHandler) }

func readRequest(w http.ResponseWriter, r *http.Request) (value.HTTPValue, value.Error) {
	input := must(value.NewRecord([]value.Field{{Name: "method", Value: value.NewBytes([]byte(r.Method))}, {Name: "path", Value: value.NewBytes([]byte(r.URL.Path))}}))
	bad := func(kind, message string) (value.HTTPValue, value.Error) {
		return nil, must(value.NewError(kind, message, input))
	}
	if !utf8.ValidString(r.Method) || !utf8.ValidString(r.URL.Path) || !utf8.ValidString(r.Host) {
		return bad(catalog.InvalidRequest, "request text is not valid UTF-8")
	}
	if r.ContentLength > MaxRequestBytes {
		return bad(catalog.RequestTooLarge, "request body exceeds its byte limit")
	}
	entries := []value.Entry{{Key: text("Host"), Value: text(r.Host)}}
	names := make([]string, 0, len(r.Header))
	for name := range r.Header {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		values := r.Header[name]
		separator := ", "
		if name == "Cookie" {
			separator = "; "
		}
		joined := strings.Join(values, separator)
		if !utf8.ValidString(name) || !utf8.ValidString(joined) {
			return bad(catalog.InvalidRequest, "request header is not valid UTF-8")
		}
		entries = append(entries, value.Entry{Key: text(http.CanonicalHeaderKey(name)), Value: text(joined)})
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return bad(catalog.RequestTooLarge, "request body exceeds its byte limit")
		}
		return bad(catalog.RequestReadFailed, fmt.Sprintf("read request body: %v", err))
	}
	headers := must(value.NewMap(must(types.NewMap(types.String(), types.String())), entries))
	fields := must(value.NewRecord([]value.Field{{Name: "method", Value: text(r.Method)}, {Name: "path", Value: text(r.URL.Path)}, {Name: "headers", Value: headers}, {Name: "body", Value: value.NewBytes(body)}}))
	return must(value.NewHTTP(types.Request(), fields)), nil
}

type responseData struct {
	status  int
	headers map[string]string
	body    []byte
}

func responseValue(v value.Value) (responseData, value.Error) {
	if f, ok := v.(value.Fallible); ok {
		var failure value.Error
		v, failure = f.Outcome()
		if failure != nil {
			panic("unhandled adapter flow failure")
		}
	}
	fields := v.(value.HTTPValue).Fields()
	n := fields.Field("status").(value.Integer).Big()
	body := fields.Field("body").(value.Bytes).Bytes()
	bad := func(message string) (responseData, value.Error) {
		return responseData{}, must(value.NewError(catalog.InvalidResponse, message, fields))
	}
	if !n.IsInt64() || n.Int64() < 200 || n.Int64() > 599 {
		return bad("response status must be between 200 and 599")
	}
	status := int(n.Int64())
	if (status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified) && len(body) != 0 {
		return bad("response status prohibits a body")
	}
	headers := map[string]string{}
	for _, entry := range fields.Field("headers").(value.Map).Entries() {
		name, text := entry.Key.(value.Text).Text(), entry.Value.(value.Text).Text()
		if strings.ContainsAny(name+text, "\r\n") || name == "" {
			return bad("invalid response header")
		}
		headers[name] = text
	}
	return responseData{status, headers, body}, nil
}
func text(s string) value.Text { return must(value.NewText(types.String(), s)) }
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
