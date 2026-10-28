package integration_test

import (
	"bytes"
	"context"
	wire "encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func processWireFrame(magic string, first, second uint32, entries ...string) []byte {
	var body bytes.Buffer
	for _, entry := range entries {
		var length [4]byte
		wire.BigEndian.PutUint32(length[:], uint32(len(entry)))
		body.Write(length[:])
		body.WriteString(entry)
	}
	header := make([]byte, 16)
	copy(header, magic)
	wire.BigEndian.PutUint32(header[4:8], uint32(body.Len()))
	wire.BigEndian.PutUint32(header[8:12], first)
	wire.BigEndian.PutUint32(header[12:16], second)
	return append(header, body.Bytes()...)
}

func processWireFile(t *testing.T, directory string, data []byte) *os.File {
	t.Helper()
	file, err := os.CreateTemp(directory, "frame-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestProcessLauncherRejectsMalformedFrames(t *testing.T) {
	fixture := newCleanupFixture(t)
	launcher := filepath.Join(repository, "bin", "lo-process-launcher")
	profile := `(version 1) (deny default (with no-log)) (allow file-read* (subpath ` + processLiteral(t, fixture.root) + `))`
	configuration := processWireFrame("LOC2", 2, 1, profile, filepath.Dir(launcher), fixture.executable, fixture.executable, launcher, fixture.root)
	valid := func(arguments, environment []string) []byte {
		entries := append([]string{fixture.executable, fixture.root}, arguments...)
		return processWireFrame("LOP1", uint32(len(arguments)), uint32(len(environment)), append(entries, environment...)...)
	}
	mutate := func(offset int, value uint32) []byte {
		frame := valid([]string{fixture.executable}, nil)
		wire.BigEndian.PutUint32(frame[offset:offset+4], value)
		return frame
	}
	for _, test := range []struct {
		name  string
		frame []byte
	}{
		{"valid-frame", valid([]string{fixture.executable}, nil)},
		{"header", []byte("LOP1")},
		{"magic", append([]byte("BAD!"), valid([]string{fixture.executable}, nil)[4:]...)},
		{"body-overflow", mutate(4, (1<<20)+1)},
		{"arguments-overflow", mutate(8, 257)},
		{"environment-overflow", mutate(12, 4097)},
		{"no-arguments", valid(nil, nil)},
		{"truncated-body", mutate(4, 4096)},
		{"entry-overflow", valid([]string{fixture.executable, strings.Repeat("x", (1<<16)+1)}, nil)},
		{"trailing-input", append(valid([]string{fixture.executable}, nil), 'x')},
		{"argv-zero", valid([]string{"wrong"}, nil)},
		{"invalid-utf8", valid([]string{fixture.executable, string([]byte{255})}, nil)},
		{"nul-entry", valid([]string{fixture.executable, "a\x00b"}, nil)},
		{"environment-name", valid([]string{fixture.executable}, []string{"=value"})},
		{"environment-equals", valid([]string{fixture.executable}, []string{"MISSING"})},
		{"environment-duplicate", valid([]string{fixture.executable}, []string{"NAME=first", "NAME=second"})},
		{"initial-scope-mismatch", processWireFrame("LOP1", 1, 0, launcher, fixture.root, launcher)},
	} {
		t.Run(test.name, func(t *testing.T) {
			frame, trusted, permit := processWireFile(t, fixture.root, test.frame), processWireFile(t, fixture.root, configuration), processWireFile(t, fixture.root, nil)
			status, statusWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer status.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, launcher)
			command.Dir = repository
			command.Env = []string{"CI=1", "TMPDIR=" + filepath.Dir(launcher)}
			command.ExtraFiles = []*os.File{frame, statusWriter, permit, trusted}
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err = command.Run()
			if closeErr := statusWriter.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			observed, readErr := io.ReadAll(status)
			if test.name == "valid-frame" {
				if err == nil || ctx.Err() != nil || readErr != nil || command.ProcessState == nil || command.ProcessState.ExitCode() != 67 || len(observed) != 12 || observed[0] != 'R' || observed[6] != 'F' || observed[7] != 3 {
					t.Fatalf("valid frame did not reach the permit boundary: %v %x", err, observed)
				}
				return
			}
			if err == nil || ctx.Err() != nil || readErr != nil || command.ProcessState == nil || command.ProcessState.ExitCode() != 66 || stdout.Len() != 0 || stderr.Len() != 0 || len(observed) != 6 || observed[0] != 'F' || observed[1] != 2 {
				t.Fatalf("malformed frame reached admission: error=%v status=%x read=%v stdout=%s stderr=%s", err, observed, readErr, &stdout, &stderr)
			}
		})
	}
}
