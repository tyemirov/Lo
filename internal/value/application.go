package value

import (
	"fmt"
	"math/big"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const (
	MaxConsoleRecordBytes = 1 << 20
	ConsoleStdout         = "stdout"
	ConsoleStderr         = "stderr"
)

// ConsoleLine is an inert output target without a sink or execution authority.
type ConsoleLine interface {
	Target
	Stream() string
	Text() string
	Flush() bool
	consoleLine()
}

type consoleLine struct {
	base
	stream, text string
	flush        bool
	byteCount    uint64
}

func (consoleLine) target()          {}
func (consoleLine) consoleLine()     {}
func (v consoleLine) Stream() string { return v.stream }
func (v consoleLine) Text() string   { return v.text }
func (v consoleLine) Flush() bool    { return v.flush }
func (v consoleLine) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Stream   string `json:"stream"`
		Bytes    uint64 `json:"bytes"`
		Flush    bool   `json:"flush"`
		Redacted bool   `json:"redacted"`
	}{v.stream, v.byteCount, v.flush, true})
}

// NewConsoleLine validates destination and record size without output.
func NewConsoleLine(stream, text string, flush bool) (ConsoleLine, error) {
	if stream != ConsoleStdout && stream != ConsoleStderr {
		return nil, fmt.Errorf("Console destination must be stdout or stderr")
	}
	if !utf8.ValidString(text) || len(text) >= MaxConsoleRecordBytes {
		return nil, fmt.Errorf("Console line must be valid UTF-8 and fit the record limit")
	}
	return consoleLine{newBase(types.ConsoleLine(), uint64(len(stream)+len(text)+10)), stream, text, flush, uint64(len(text) + 1)}, nil
}

func redactedConsoleLine(v ConsoleLine) ConsoleLine {
	bytes := uint64(len(v.Text()) + 1)
	if original, ok := v.(consoleLine); ok {
		bytes = original.byteCount
	}
	return consoleLine{newBase(types.ConsoleLine(), uint64(len(v.Stream())+10)), v.Stream(), "", v.Flush(), bytes}
}

// NewConsoleReceipt publishes confirmed logical output after complete activation.
func NewConsoleReceipt(stream string, bytesWritten uint64, flushed bool) (Receipt, error) {
	if stream != ConsoleStdout && stream != ConsoleStderr || bytesWritten == 0 || bytesWritten > MaxConsoleRecordBytes {
		return nil, fmt.Errorf("invalid Console receipt")
	}
	fields := must(NewRecord([]Field{
		{Name: "stream", Value: must(NewText(types.String(), stream))},
		{Name: "bytes_written", Value: must(NewInt(new(big.Int).SetUint64(bytesWritten)))},
		{Name: "flushed", Value: Bool(flushed)},
	}))
	return NewReceipt(types.ConsoleReceipt(), fields)
}

// ProgramExit is an inert final-run description, distinct from an ordinary Int.
type ProgramExit interface {
	Value
	Code() int
	Stdout() Bytes
	Stderr() Bytes
	programExit()
}

type programExit struct {
	base
	code                   int
	stdout, stderr         Bytes
	stdoutSize, stderrSize int
}

func (programExit) programExit()    {}
func (v programExit) Code() int     { return v.code }
func (v programExit) Stdout() Bytes { return v.stdout }
func (v programExit) Stderr() Bytes { return v.stderr }
func (v programExit) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		Code        int  `json:"code"`
		StdoutBytes int  `json:"stdout_bytes"`
		StderrBytes int  `json:"stderr_bytes"`
		Redacted    bool `json:"redacted"`
	}{v.code, v.stdoutSize, v.stderrSize, true})
}

// NewProgramExit validates one final status and bounded trailing byte payloads.
func NewProgramExit(code Integer, stdout, stderr Bytes) (ProgramExit, error) {
	if code == nil || stdout == nil || stderr == nil || !code.Big().IsUint64() || code.Big().Uint64() > 255 {
		return nil, fmt.Errorf("Program exit status must be from 0 through 255")
	}
	if stdout.Len() > MaxConsoleRecordBytes || stderr.Len() > MaxConsoleRecordBytes-stdout.Len() {
		return nil, fmt.Errorf("Program terminal output exceeds the record limit")
	}
	return programExit{newBase(types.ProgramExit(), addWeight(8, addWeight(stdout.Weight(), stderr.Weight()))), int(code.Big().Int64()), stdout, stderr, stdout.Len(), stderr.Len()}, nil
}

func redactedProgramExit(v ProgramExit) ProgramExit {
	stdoutSize, stderrSize := v.Stdout().Len(), v.Stderr().Len()
	if original, ok := v.(programExit); ok {
		stdoutSize, stderrSize = original.stdoutSize, original.stderrSize
	}
	return programExit{newBase(types.ProgramExit(), 40), v.Code(), NewBytes(nil), NewBytes(nil), stdoutSize, stderrSize}
}

// CLIOptions contains selected option values and declared help without raw argv.
type CLIOptions interface {
	Value
	FieldsRecord() Record
	cliOptions()
}

type cliOptions struct {
	base
	fields Record
}

func (cliOptions) cliOptions()            {}
func (v cliOptions) FieldsRecord() Record { return v.fields }
func (v cliOptions) MarshalJSON() ([]byte, error) {
	fields := map[string]Value{}
	for _, field := range v.fields.Fields() {
		fields[field.Name] = field.Value
	}
	return encode(v, fields)
}

// NewCLIOptions validates the exact public nominal result schema.
func NewCLIOptions(fields Record) (CLIOptions, error) {
	if fields == nil || len(fields.Fields()) != 3 {
		return nil, fmt.Errorf("CLI options require the complete selected option result")
	}
	for _, field := range fields.Fields() {
		typ, ok := types.FieldType(types.CLIOptions(), field.Name)
		if !ok || !types.Equal(typ, field.Value.Type()) {
			return nil, fmt.Errorf("invalid CLI option field")
		}
	}
	return cliOptions{newBase(types.CLIOptions(), fields.Weight()), fields}, nil
}
