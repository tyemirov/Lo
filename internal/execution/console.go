package execution

import "fmt"

// ConsoleWriteError reports confirmed output progress without retaining output.
type ConsoleWriteError interface {
	error
	Stream() string
	BytesWritten() uint64
	Stage() string
	consoleWriteError()
}

type consoleWriteError struct {
	stream  string
	written uint64
	stage   string
}

func (consoleWriteError) consoleWriteError()     {}
func (e consoleWriteError) Error() string        { return "Console output " + e.stage + " failed" }
func (e consoleWriteError) Stream() string       { return e.stream }
func (e consoleWriteError) BytesWritten() uint64 { return e.written }
func (e consoleWriteError) Stage() string        { return e.stage }

// NewConsoleWriteError validates safe, closed output progress metadata.
func NewConsoleWriteError(stream string, bytesWritten uint64, stage string) (ConsoleWriteError, error) {
	if stream != "stdout" && stream != "stderr" {
		return nil, fmt.Errorf("invalid Console stream")
	}
	if stage != "write" && stage != "flush" {
		return nil, fmt.Errorf("invalid Console failure stage")
	}
	return consoleWriteError{stream, bytesWritten, stage}, nil
}
