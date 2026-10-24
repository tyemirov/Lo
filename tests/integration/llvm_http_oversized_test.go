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

func TestLLVMHTTPRejectedBodyDrain(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires the explicit LLVM runtime lane")
	}
	server := startServer(t, `flow Handle(r:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"handler entered")};flow OnError(e:Error)->HTTP.Response{return HTTP.Response.Text.new(413,String.concat(e.kind,":",Int.to_string(List.length(e.trace))))}`+serveSuffix)
	var stalled *net.TCPConn
	defer func() {
		if stalled != nil {
			defer stalled.Close()
		}
		stopServer(t, server)
	}()
	const bodyBytes = (1 << 20) + 1
	parentTest := t
	connect := func(t *testing.T) (*net.TCPConn, *bufio.Reader) {
		t.Helper()
		connection, err := net.DialTimeout("tcp", server.address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		tcp := connection.(*net.TCPConn)
		parentTest.Cleanup(func() { _ = tcp.Close() })
		if err := tcp.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := tcp.SetWriteBuffer(4096); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(tcp, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", bodyBytes); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(tcp)
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("read rejection: %v", err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 413 || string(body) != "HTTP.RequestTooLarge:1" || !response.Close {
			t.Fatalf("rejection: status=%d body=%q close=%v read=%v cleanup=%v", response.StatusCode, body, response.Close, readErr, closeErr)
		}
		return tcp, reader
	}
	t.Run("upload-after-rejection", func(t *testing.T) {
		connection, reader := connect(t)
		defer connection.Close()
		// Constrain local send buffering during this finite rejected upload.
		// The server keeps its existing absolute drain deadline.
		written, err := io.WriteString(connection, strings.Repeat("a", bodyBytes))
		if err != nil || written != bodyBytes {
			t.Fatalf("rejected upload: sent=%d want=%d error=%v", written, bodyBytes, err)
		}
		if err := connection.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		if extra, err := reader.ReadByte(); err != io.EOF {
			t.Fatalf("after rejection: byte=%d error=%v; want EOF", extra, err)
		}
	})
	t.Run("stalled-rejected-peer", func(t *testing.T) {
		var reader *bufio.Reader
		stalled, reader = connect(t)
		// Keep the upload side open through graceful server shutdown. EOF here
		// observes the response half-close, not completion of the server drain.
		if extra, err := reader.ReadByte(); err != io.EOF {
			t.Fatalf("idle rejection: byte=%d error=%v; want EOF", extra, err)
		}
	})
	status, body := response(t, server, "GET", "/healthy", "")
	if status != 200 || body != "handler entered" {
		t.Fatalf("healthy connection after rejection: %d %q", status, body)
	}
}
