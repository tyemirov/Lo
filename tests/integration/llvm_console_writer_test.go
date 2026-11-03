package integration_test

import (
	"bytes"
	"context"
	encodingbinary "encoding/binary"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestNativeConsoleWriterFrames(t *testing.T) {
	artifact, built := buildForTest(t, sourceFile(t, "return 0"))
	if built.status != 0 {
		t.Fatalf("build writer fixture: %+v", built)
	}
	frame := func(destination byte, body []byte) []byte {
		request := make([]byte, 9, len(body)+9)
		copy(request, "LCW2")
		request[4] = destination
		encodingbinary.BigEndian.PutUint32(request[5:], uint32(len(body)))
		return append(request, body...)
	}
	large := bytes.Repeat([]byte("x"), 2<<20)
	oversized := frame(1, nil)
	encodingbinary.BigEndian.PutUint32(oversized[5:], 2<<20+1)
	truncated := frame(1, []byte("missing"))[:11]
	for _, test := range []struct {
		name           string
		request        []byte
		stdout, stderr []byte
		status         int
		completedBytes []uint64
	}{
		{"multiple frames", append(frame(1, large), frame(2, []byte("last"))...), large, []byte("last"), 0, []uint64{uint64(len(large)), 4}},
		{"oversized frame", oversized, nil, nil, 125, nil},
		{"truncated frame", truncated, nil, nil, 125, nil},
		{"truncated header", []byte("LCW2"), nil, nil, 125, nil},
		{"obsolete protocol", []byte{'L', 'C', 'W', '1', 1, 0, 0, 0, 0}, nil, nil, 125, nil},
		{"truncated next frame", append(frame(1, []byte("first")), truncated...), []byte("first"), nil, 125, []uint64{5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ackRead, ackWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer ackRead.Close()
			defer ackWrite.Close()
			liveRead, liveWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer liveRead.Close()
			defer liveWrite.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, artifact, "--lo-console-writer")
			command.Stdin = bytes.NewReader(test.request)
			command.ExtraFiles = []*os.File{ackWrite, liveRead}
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			ackWrite.Close()
			liveRead.Close()
			acknowledged := make(chan []byte, 1)
			go func() { data, _ := io.ReadAll(ackRead); acknowledged <- data }()
			_ = command.Wait()
			if command.ProcessState.ExitCode() != test.status || !bytes.Equal(stdout.Bytes(), test.stdout) || !bytes.Equal(stderr.Bytes(), test.stderr) {
				t.Fatalf("writer status=%d stdout bytes=%d stderr bytes=%d", command.ProcessState.ExitCode(), stdout.Len(), stderr.Len())
			}
			acks := <-acknowledged
			if len(acks)%10 != 0 {
				t.Fatalf("truncated acknowledgments: %d bytes", len(acks))
			}
			completed := 0
			for offset := 0; offset < len(acks); offset += 10 {
				kind := acks[offset]
				confirmed := encodingbinary.BigEndian.Uint64(acks[offset+1 : offset+9])
				if acks[offset+9] != 0 || (kind != 1 && kind != 2) {
					t.Fatalf("unexpected acknowledgment: %v", acks[offset:offset+10])
				}
				if completed >= len(test.completedBytes) || confirmed > test.completedBytes[completed] {
					t.Fatalf("acknowledged bytes=%d, completed=%d", confirmed, completed)
				}
				if kind == 2 {
					if confirmed != test.completedBytes[completed] {
						t.Fatalf("completion bytes=%d, want %d", confirmed, test.completedBytes[completed])
					}
					completed++
				}
			}
			if completed != len(test.completedBytes) {
				t.Fatalf("completed frames=%d, want %d", completed, len(test.completedBytes))
			}
		})
	}
}

func TestNativeConsoleWriterClosedPipe(t *testing.T) {
	artifact, built := buildForTest(t, sourceFile(t, "return 0"))
	if built.status != 0 {
		t.Fatalf("build writer fixture: %+v", built)
	}
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	liveRead, liveWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer liveRead.Close()
	defer liveWrite.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outRead.Close()
	defer outWrite.Close()
	if err := outRead.Close(); err != nil {
		t.Fatal(err)
	}
	body := []byte("protocol\n")
	request := make([]byte, 9)
	copy(request, "LCW2")
	request[4] = 1
	encodingbinary.BigEndian.PutUint32(request[5:], uint32(len(body)))
	request = append(request, body...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, artifact, "--lo-console-writer")
	command.Stdin = bytes.NewReader(request)
	command.Stdout = outWrite
	command.Stderr = outWrite
	command.ExtraFiles = []*os.File{ackWrite, liveRead}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	ackWrite.Close()
	liveRead.Close()
	var final [10]byte
	for {
		if _, err := io.ReadFull(ackRead, final[:]); err != nil {
			t.Fatal(err)
		}
		if final[0] != 1 {
			break
		}
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if final[0] != 3 || final[9] != 1 || encodingbinary.BigEndian.Uint64(final[1:9]) != 0 {
		t.Fatalf("failure frame=%v", final)
	}
}
