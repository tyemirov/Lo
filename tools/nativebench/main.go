// Command nativebench emits native execution fixtures and independent oracle metadata.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
	"os"
	"path/filepath"
	"strings"
)

//go:embed harness.c.txt
var nativeHarness string

//go:embed allocation.c.txt
var nativeAllocationHarness string

func main() {
	out := flag.String("output", "", "owned benchmark directory")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func write(path, text string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = file.WriteString(text)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func run(out string) error {
	if out == "" {
		return fmt.Errorf("--output is required")
	}
	fixtures := corpus.Cases()
	for _, f := range []struct{ name, path string }{{"training_mvp", "examples/training_cpu.lo"}, {"training_dense", "tests/benchmark/testdata/training_dense.lo"}} {
		text, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		fixtures = append(fixtures, corpus.Case{Name: f.name, Source: string(text)})
	}
	paths := []string{}
	for _, fixture := range fixtures {
		dir := filepath.Join(out, fixture.Name)
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "benchmark.c"), nativeHarness); err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "allocation.c"), nativeAllocationHarness); err != nil {
			return err
		}
		oracle := map[string]string{"kind": "exact", "expected": fixture.Expected}
		if fixture.Name == "training_mvp" || fixture.Name == "training_dense" {
			oracle = map[string]string{"kind": fixture.Name}
		}
		data, err := json.Marshal(oracle)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "oracle.json"), string(data)+"\n"); err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "source.lo"), fixture.Source); err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(dir))
	}
	fmt.Println(strings.Join(paths, "\n"))
	return nil
}
