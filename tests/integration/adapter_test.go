package integration_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/device"
	"github.com/MarcoPoloResearchLab/Lo/internal/eval"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/ir"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MarcoPoloResearchLab/Lo/internal/adapter"
	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
	"sync"
)

func TestFileReadCancellation(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "input")
	if output, err := exec.Command("mkfifo", name).CombinedOutput(); err != nil {
		t.Fatalf("create local pipe: %v, %s", err, output)
	}
	files, err := adapter.OpenFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	}()
	path, err := value.NewText(types.Path(), name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	done := make(chan error, 1)
	go func() { _, err := files.Read(ctx, path); done <- err }()
	// More than one local pipe buffer proves that the adapter started its read.
	if _, err := writer.WriteString(strings.Repeat("x", 1<<20)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("read cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled file read remained blocked")
	}
}

func TestFileWriteCancellation(t *testing.T) {
	for _, operation := range []string{catalog.FileWrite, catalog.FileAppend} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "output")
			if output, err := exec.Command("mkfifo", name).CombinedOutput(); err != nil {
				t.Fatalf("create local pipe: %v, %s", err, output)
			}
			files, err := adapter.OpenFiles(nil, []string{dir})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := files.Close(); err != nil {
					t.Error(err)
				}
			}()
			reader, err := os.OpenFile(name, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			path, err := value.NewText(types.Path(), name)
			if err != nil {
				t.Fatal(err)
			}
			var content value.Value = value.NewBytes([]byte(strings.Repeat("x", 1<<20)))
			if operation == catalog.FileAppend {
				content, err = value.NewText(types.String(), strings.Repeat("x", 1<<20))
				if err != nil {
					t.Fatal(err)
				}
			}
			op, _ := catalog.Lookup(operation)
			target, err := value.NewEffect(op, path, content)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- files.Commit(ctx, target) }()
			var first [1]byte
			if _, err := reader.Read(first[:]); err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("write cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled file write remained blocked")
			}
		})
	}
}

func TestFileConcurrentAppendCreation(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "output")
	files, err := adapter.OpenFiles(nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	}()
	path, err := value.NewText(types.Path(), name)
	if err != nil {
		t.Fatal(err)
	}
	content, err := value.NewText(types.String(), "one\n")
	if err != nil {
		t.Fatal(err)
	}
	op, _ := catalog.Lookup(catalog.FileAppend)
	target, err := value.NewEffect(op, path, content)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := files.Commit(context.Background(), target); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(name)
	if err != nil || string(data) != strings.Repeat("one\n", 32) {
		t.Fatalf("concurrent append lost content: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "output" {
		t.Fatalf("append left unexpected entries: count %d", len(entries))
	}
}

func TestFileOpenDoesNotWaitForPipePeer(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "input")
	if output, err := exec.Command("mkfifo", name).CombinedOutput(); err != nil {
		t.Fatalf("create local pipe: %v, %s", err, output)
	}
	files, err := adapter.OpenFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	path, err := value.NewText(types.Path(), name)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		v, err := files.Read(context.Background(), path)
		if err == nil && len(v.Bytes()) != 0 {
			err = errors.New("pipe without a writer returned bytes")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		// Release a blocked open so the failed test does not leave an operation behind.
		writer, err := os.OpenFile(name, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		<-done
		t.Fatal("file open waited for an external pipe producer")
	}
}

func TestServerPartialStartupCleanup(t *testing.T) {
	src, err := source.New("partial.lo", `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "bad") }; first = HTTP.Server.new(0,H,E); second = HTTP.Server.new(-1,H,E); commit first; commit second`)
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(src)
	if d != nil {
		t.Fatal(d)
	}
	files, err := adapter.OpenFiles(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	policy, err := adapter.NewListenPolicy([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := adapter.NewServers(executable(t, p), files, policy, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, err = servers.Start(context.Background())
	var failure execution.Failure
	if !errors.As(err, &failure) || failure.Value().Kind() != "HTTP.ServerStartFailed" {
		t.Fatalf("startup failure: %v", err)
	}
	active := servers.Snapshot()
	if len(active) != 1 {
		t.Fatalf("first activation did not occur: %v", active)
	}
	conn, err := net.DialTimeout("tcp", active[0].Address, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("failed startup left its first listener open")
	}
}

func startAdapter(t *testing.T, text string) adapter.Servers {
	t.Helper()
	src, err := source.New("server.lo", text)
	if err != nil {
		t.Fatal(err)
	}
	p, d := compiler.Check(src)
	if d != nil {
		t.Fatal(d)
	}
	files, err := adapter.OpenFiles(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	policy, err := adapter.NewListenPolicy([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := adapter.NewServers(executable(t, p), files, policy, execution.DefaultBudget(), device.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = servers.Shutdown(ctx)
	})
	if _, err := servers.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return servers
}

func TestServerActivationInRequest(t *testing.T) {
	servers := startAdapter(t, `flow ErrorResponse(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }
flow Inner(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "inner") }
flow Outer(r: HTTP.Request) -> HTTP.Response! { s = HTTP.Server.new(0, Inner, ErrorResponse); commit s; return HTTP.Response.Text.new(200, "outer") }
s = HTTP.Server.new(0, Outer, ErrorResponse); commit s`)
	client := &http.Client{Timeout: time.Second}
	for i, want := range []string{"outer", "inner"} {
		active := servers.Snapshot()
		if len(active) != i+1 {
			t.Fatalf("server activations: %v", active)
		}
		r, err := client.Get("http://" + active[i].Address + "/")
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r.Body)
		closeErr := r.Body.Close()
		if err != nil || closeErr != nil || r.StatusCode != 200 || string(data) != want {
			t.Fatalf("nested server: %d %q %v %v", r.StatusCode, data, err, closeErr)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := servers.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, server := range servers.Snapshot() {
		conn, err := net.DialTimeout("tcp", server.Address, time.Second)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("nested listener remained open: %s", server.Address)
		}
	}
}

func TestServerForcedShutdown(t *testing.T) {
	servers := startAdapter(t, `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Bytes.new(200, r.body) }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, e.kind) }; s = HTTP.Server.new(0,H,E); commit s`)
	conn, err := net.DialTimeout("tcp", servers.Snapshot()[0].Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1\r\nExpect: 100-continue\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	interim, err := http.ReadResponse(reader, nil)
	if err != nil || interim.StatusCode != 100 {
		t.Fatalf("active request handshake: %v, %v", interim, err)
	}
	if err := interim.Body.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- servers.Shutdown(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("forced shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("forced shutdown retained an active request")
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("forced shutdown left request connected")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("forced shutdown did not close request")
	}
}

func TestServerDisconnectedErrorResponse(t *testing.T) {
	program := fmt.Sprintf(`flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400, %q) }; s = HTTP.Server.new(0,H,E); commit s`, strings.Repeat("x", 8<<20))
	servers := startAdapter(t, program)
	conn, err := net.DialTimeout("tcp", servers.Snapshot()[0].Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\nshort"); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 400 {
		t.Fatalf("error response status: %d", response.StatusCode)
	}
	var first [1024]byte
	if _, err := io.ReadFull(response.Body, first[:]); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).SetLinger(0); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := servers.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-servers.Failures():
		t.Fatalf("client disconnection became a service failure: %v", err)
	default:
	}
}

func executable(t *testing.T, p ir.Program) execution.Program {
	t.Helper()
	result, err := eval.Program(p)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
