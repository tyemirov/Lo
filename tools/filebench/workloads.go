package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
	"github.com/MarcoPoloResearchLab/Lo/internal/execution"
	"github.com/MarcoPoloResearchLab/Lo/internal/types"
	"github.com/MarcoPoloResearchLab/Lo/internal/value"
)

type workload struct {
	name, directory, source, expected, pipeline string
	size                                        int
	args                                        []string
	payload                                     []byte
	directories                                 []string
	held                                        *os.File
	limitAfterCommit                            bool
}

func (w *workload) canonicalSource() string {
	text := w.source
	for index, path := range w.directories {
		text = strings.ReplaceAll(text, strconv.Quote(path), strconv.Quote(fmt.Sprintf("@directory-%d", index)))
	}
	escaped := strconv.Quote(w.directory)
	return strings.ReplaceAll(text, escaped[1:len(escaped)-1], "@fixture")
}

func newWorkload(parent, name string, size, appendBytes int) (*workload, error) {
	directory, err := os.MkdirTemp(parent, "fixture-")
	if err != nil {
		return nil, err
	}
	w := &workload{name: name, size: size, directory: directory, expected: strconv.Itoa(size)}
	w.args = []string{"--format", "json", "--max-steps", stepBudget}
	path := func(name string) string {
		return "File.Path.new(" + strconv.Quote(filepath.Join(directory, name)) + ")"
	}
	root := "File.Path.new(" + strconv.Quote(directory) + ")"
	readGrant := func() { w.args = append(w.args, "--read-root", directory) }
	writeGrant := func() { w.args = append(w.args, "--write-root", directory) }
	listGrant := func() { w.args = append(w.args, "--list-root", directory) }
	switch name {
	case "startup":
		w.source, w.expected, w.pipeline = "return 1", "1", "scalar startup control; measured independently without subtraction"
	case "list", "scan", "remove", "checked-remove":
		listGrant()
		operation := "entries = File.list(" + root + "); return List.length(entries)"
		w.pipeline = "File.list plus scalar List.length; complete materialized metadata and sorting"
		if name == "scan" {
			operation = "scan = File.scan(" + root + "); return List.length(scan.entries)"
			w.pipeline = "File.scan plus scalar List.length; complete materialized metadata and sorting"
		}
		if name == "remove" || name == "checked-remove" {
			w.args = append(w.args, "--remove-root", directory)
			constructor := "File.Remove.new(entries)"
			if name == "checked-remove" {
				constructor = "File.Remove.checked(entries, Guard)"
			}
			operation = "entries = File.list(" + root + "); target = " + constructor + "; receipt = commit target; return receipt.completed_count"
			w.pipeline = "File.list followed by batch removal and scalar receipt; checked guard returns true"
		}
		w.source = "flow Guard(original: File.Entry, current: File.Entry) -> Bool { return true }; flow Main() -> Int! { " + operation + " }; return Main()"
	case "read", "write", "append":
		readGrant()
		w.payload = bytes.Repeat([]byte("x"), size)
		operation := "data = File.read(" + path("input") + "); text = UTF8.String.new(data); return String.length(text)"
		w.pipeline = "File.read followed by UTF8.String.new and String.length; scalar output; includes UTF-8 validation/conversion"
		if name != "read" {
			writeGrant()
			constructor := "File.Write.new"
			if name == "append" {
				constructor = "File.Append.new"
			}
			operation = "data = File.read(" + path("input") + "); target = " + constructor + "(" + path("output") + ", data); receipt = commit target; return receipt.bytes_written"
			w.pipeline = "File.read payload staging plus " + constructor + " and commit; scalar receipt; not isolated write throughput"
		}
		w.source = "flow Main() -> Int! { " + operation + " }; return Main()"
		if err := w.expectPayload(); err != nil {
			return nil, err
		}
	case "append-many":
		readGrant()
		writeGrant()
		w.payload = bytes.Repeat([]byte("x"), appendBytes)
		w.expected = strconv.Itoa(size * appendBytes)
		w.pipeline = "one File.read payload staging followed by repeated File.Append.new commits; scalar accumulated byte receipts"
		w.source = fmt.Sprintf(`flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Append(data: Bytes, total: Int, item: Int) -> Int! { target = File.Append.new(%s, data); receipt = commit target; return Math.add(total, receipt.bytes_written) }; flow Main() -> Int! { data = File.read(%s); items = List.unfold(0, %d, Next); return Flow.fold(items, 0, data, Append) }; return Main()`, path("output"), path("input"), size)
	case "directories":
		var paths []string
		for range size {
			child, err := os.MkdirTemp(directory, "d-")
			if err != nil {
				return nil, err
			}
			w.directories = append(w.directories, child)
			paths = append(paths, "File.Path.new("+strconv.Quote(child)+")")
			w.args = append(w.args, "--list-root", child)
		}
		w.source = `flow Observe(total: Int, path: File.Path) -> Int! { observed = File.directory(path); return Math.add(total, 1) }; flow Main() -> Int! { return Flow.fold(List.new(` + strings.Join(paths, ",") + `), 0, Observe) }; return Main()`
		w.pipeline = "File.directory once per distinct granted directory; each observation releases after its callback"
	case "open-paths":
		w.args = append(w.args, "--open-files-root", directory)
		w.source = "flow Main() -> Int! { paths = File.open_paths(" + root + "); return List.length(paths) }; return Main()"
		w.expected, w.pipeline = "1", "actual configured lsof probe over one held fixture file; directory descriptors excluded from result"
	default:
		return nil, fmt.Errorf("unknown case %q", name)
	}
	return w, nil
}

func (w *workload) close() error {
	if w.held != nil {
		err := w.held.Close()
		w.held = nil
		return err
	}
	return nil
}

func (w *workload) prepare() error {
	if err := w.close(); err != nil {
		return err
	}
	if w.name == "directories" || w.name == "startup" {
		return nil
	}
	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(w.directory, entry.Name())); err != nil {
			return err
		}
	}
	switch w.name {
	case "list", "scan", "remove", "checked-remove":
		for index := range w.size {
			if err := write(filepath.Join(w.directory, fmt.Sprintf("entry-%05d", index)), []byte("x")); err != nil {
				return err
			}
		}
	case "read", "write", "append", "append-many":
		if err := write(filepath.Join(w.directory, "input"), w.payload); err != nil {
			return err
		}
		if w.name == "append" || w.name == "append-many" {
			return write(filepath.Join(w.directory, "output"), nil)
		}
	case "open-paths":
		path := filepath.Join(w.directory, "held")
		if err := write(path, []byte("x")); err != nil {
			return err
		}
		w.held, err = os.Open(path)
		return err
	}
	return nil
}

func (w *workload) verify(stdout, stderr []byte, status int) error {
	if w.expected == "runtime.limit" {
		var diagnostic struct{ Diagnostics []struct{ Code string } }
		if err := json.Unmarshal(stderr, &diagnostic); err != nil {
			return fmt.Errorf("decode limit diagnostic: %w; output=%s", err, stderr)
		}
		if status != 2 || len(diagnostic.Diagnostics) != 1 || diagnostic.Diagnostics[0].Code != w.expected {
			return fmt.Errorf("expected typed limit, got status=%d stdout=%s stderr=%s", status, stdout, stderr)
		}
		return w.verifyFiles(w.limitAfterCommit)
	}
	if status != 0 || len(stderr) != 0 {
		return fmt.Errorf("execution status=%d stdout=%s stderr=%s", status, stdout, stderr)
	}
	var result struct{ Result json.RawMessage }
	if err := json.Unmarshal(stdout, &result); err != nil {
		return err
	}
	var scalar struct {
		Type, Status string
		Value        json.RawMessage
	}
	if err := json.Unmarshal(result.Result, &scalar); err != nil {
		return err
	}
	if scalar.Status == "ok" {
		if err := json.Unmarshal(scalar.Value, &scalar); err != nil {
			return err
		}
	}
	var observed string
	if err := json.Unmarshal(scalar.Value, &observed); err != nil {
		return fmt.Errorf("scalar result: %w; stdout=%s", err, stdout)
	}
	if scalar.Type != "Int" || observed != w.expected {
		return fmt.Errorf("result type=%q value=%q want Int %q", scalar.Type, observed, w.expected)
	}
	return w.verifyFiles(true)
}

func (w *workload) verifyFiles(success bool) error {
	switch w.name {
	case "list", "scan", "remove", "checked-remove":
		entries, err := os.ReadDir(w.directory)
		if err != nil {
			return err
		}
		count := w.size
		if success && (w.name == "remove" || w.name == "checked-remove") {
			count = 0
		}
		if len(entries) != count {
			return fmt.Errorf("remaining entries=%d want=%d", len(entries), count)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(w.directory, entry.Name()))
			if err != nil {
				return err
			}
			if !bytes.Equal(data, []byte("x")) {
				return fmt.Errorf("entry payload changed: %q", entry.Name())
			}
		}
	case "read", "write", "append", "append-many":
		input, err := os.ReadFile(filepath.Join(w.directory, "input"))
		if err != nil {
			return err
		}
		if !bytes.Equal(input, w.payload) {
			return fmt.Errorf("input payload changed")
		}
		if !success && w.name == "write" {
			if _, err := os.Lstat(filepath.Join(w.directory, "output")); !os.IsNotExist(err) {
				return fmt.Errorf("rejected write created output: %v", err)
			}
		}
		if !success && w.name == "append" {
			output, err := os.ReadFile(filepath.Join(w.directory, "output"))
			if err != nil || len(output) != 0 {
				return fmt.Errorf("rejected append changed output: %v", err)
			}
		}
		if success && w.name != "read" {
			output, err := os.ReadFile(filepath.Join(w.directory, "output"))
			if err != nil {
				return err
			}
			expected := w.payload
			if w.name == "append-many" {
				expected = bytes.Repeat(w.payload, w.size)
			}
			if !bytes.Equal(output, expected) {
				return fmt.Errorf("output payload mismatch: got %d bytes want %d", len(output), len(expected))
			}
		}
	}
	return nil
}

// expectPayload models value charges for these fixed straight-line Lo programs.
func (w *workload) expectPayload() error {
	input, err := value.NewText(types.Path(), filepath.Join(w.directory, "input"))
	if err != nil {
		return err
	}
	empty := value.NewBytes(nil)
	fallibleBytes, err := types.NewFallible(types.Bytes())
	if err != nil {
		return err
	}
	wrapped, err := value.Success(fallibleBytes, empty)
	if err != nil {
		return err
	}
	bytesWeight := uint64(w.size) + empty.Weight()
	fallibleWeight := uint64(w.size) + wrapped.Weight()
	integer := value.Int64(int64(w.size))
	fallibleInt, err := types.NewFallible(types.Int())
	if err != nil {
		return err
	}
	wrappedInteger, err := value.Success(fallibleInt, integer)
	if err != nil {
		return err
	}
	weights := []uint64{input.Weight(), input.Weight(), fallibleWeight, bytesWeight}
	commitIndex := -1
	if w.name == "read" {
		// UTF-8 result, propagated string, scalar, and wrapped flow return.
		// References and the top-level result preserve an already charged identity.
		weights = append(weights, fallibleWeight, bytesWeight, integer.Weight(), wrappedInteger.Weight())
	} else {
		output, err := value.NewText(types.Path(), filepath.Join(w.directory, "output"))
		if err != nil {
			return err
		}
		operationName, receiptType := catalog.FileWrite, types.FileWriteReceipt()
		if w.name == "append" {
			operationName, receiptType = catalog.FileAppend, types.FileAppendReceipt()
		}
		operation, ok := catalog.Lookup(operationName)
		if !ok {
			return fmt.Errorf("missing File constructor %s", operationName)
		}
		target, err := value.NewEffect(operation, output, empty)
		if err != nil {
			return err
		}
		fields, err := value.NewRecord([]value.Field{{Name: "path", Value: output}, {Name: "bytes_written", Value: integer}})
		if err != nil {
			return err
		}
		receipt, err := value.NewReceipt(receiptType, fields)
		if err != nil {
			return err
		}
		targetWeight := uint64(w.size) + target.Weight()
		weights = append(weights, output.Weight(), output.Weight(), targetWeight)
		// The write completes before the receipt's first charge.
		commitIndex = len(weights)
		weights = append(weights, receipt.Weight(), integer.Weight(), wrappedInteger.Weight())
	}
	limits := execution.DefaultBudgetInput().Limits
	var total uint64
	for index, weight := range weights {
		if (limits.ValueBytes != 0 && weight > limits.ValueBytes) ||
			(limits.LiveBytes != 0 && (total > limits.LiveBytes || weight > limits.LiveBytes-total)) {
			w.expected = "runtime.limit"
			w.limitAfterCommit = commitIndex >= 0 && index >= commitIndex
			return nil
		}
		total += weight
	}
	return nil
}
