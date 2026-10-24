package integration_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLLVMHTTPBodyPipeline(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	server := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response!{body=UTF8.String.new(r.body);return HTTP.Response.Text.new(200,String.concat(r.method,"|",r.path,"|",body))};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(500,e.kind)}`+serveSuffix)
	defer stopServer(t, server)
	boundaryBody := strings.Repeat("0123456789abcdef", 512) + "boundary-tail"
	for _, scenario := range []struct {
		name             string
		body             string
		chunked          bool
		continueExpected bool
	}{
		{name: "content-length", body: "first\r\nGET /body-only HTTP/1.1\r\n\r\n"},
		{name: "chunked-trailers", body: "chunk-onechunk-two", chunked: true},
		{name: "body-above-8192", body: boundaryBody},
		{name: "continue-then-body", body: "continued-body", continueExpected: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			connection, err := net.DialTimeout("tcp", server.address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(connection)
			head := "POST /first HTTP/1.1\r\nHost: localhost\r\n"
			wireBody := scenario.body
			if scenario.chunked {
				head += "Transfer-Encoding: chunked\r\nTrailer: X-Tail\r\n"
				wireBody = "9\r\nchunk-one\r\n9\r\nchunk-two\r\n0\r\nX-Tail: finished\r\n\r\n"
			} else {
				head += fmt.Sprintf("Content-Length: %d\r\n", len(scenario.body))
			}
			if scenario.continueExpected {
				head += "Expect: 100-continue\r\n"
			}
			head += "\r\n"
			if scenario.continueExpected {
				if _, err := io.WriteString(connection, head); err != nil {
					t.Fatal(err)
				}
				interim, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatalf("read continue: %v", err)
				}
				if interim.StatusCode != http.StatusContinue {
					t.Fatalf("continue status: %d", interim.StatusCode)
				}
				if err := interim.Body.Close(); err != nil {
					t.Fatal(err)
				}
				head = ""
			}
			const queued = "PUT /queued HTTP/1.1\r\nHost: localhost\r\nContent-Length: 11\r\n\r\nsecond-bodyGET /final HTTP/1.1\r\nHost: localhost\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
			// Queued bytes exercise message boundaries. A write does not determine recv fragmentation.
			if _, err := io.WriteString(connection, head+wireBody+queued); err != nil {
				t.Fatal(err)
			}
			for index, want := range []string{"POST|/first|" + scenario.body, "PUT|/queued|second-body", "GET|/final|"} {
				response, err := http.ReadResponse(reader, nil)
				if err != nil {
					t.Fatalf("response %d: %v", index, err)
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(body) != want {
					t.Fatalf("response %d: status=%d body=%q want=%q read=%v close=%v", index, response.StatusCode, body, want, readErr, closeErr)
				}
				if response.Close != (index == 2) {
					t.Fatalf("response %d close=%v", index, response.Close)
				}
			}
			if extra, err := reader.ReadByte(); err != io.EOF {
				t.Fatalf("after final response: byte=%d error=%v; want EOF", extra, err)
			}
		})
	}
}
