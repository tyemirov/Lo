// Command nativebench emits paired execution benchmarks from the fixed corpus.
package main

import (
	_ "embed"
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
		test := fmt.Sprintf(`package fixture
import("context";"testing";"github.com/MarcoPoloResearchLab/Lo/internal/compiler";"github.com/MarcoPoloResearchLab/Lo/internal/eval";"github.com/MarcoPoloResearchLab/Lo/internal/execution"
"github.com/MarcoPoloResearchLab/Lo/internal/device";"github.com/MarcoPoloResearchLab/Lo/internal/source";"github.com/MarcoPoloResearchLab/Lo/internal/value")
type boundary struct{execution.Boundary}
func(boundary)Read(context.Context,value.Text)(value.Bytes,error){return nil,execution.Denied("read","benchmark")}
func(boundary)Commit(context.Context,value.Target)(value.Value,error){return nil,execution.Denied("commit","benchmark")}
var sink value.Value
func BenchmarkExecution(b *testing.B){
 file,err:=source.New(%q,%q);if err!=nil{b.Fatal(err)};p,d:=compiler.Check(file);if d!=nil{b.Fatal(d)}
 interpreted,err:=eval.Program(p);if err!=nil{b.Fatal(err)}
 budget,err:=execution.NewBudget(10000000);if err!=nil{b.Fatal(err)}
 reference,err:=interpreted.Run(context.Background(),boundary{},budget,device.DefaultConfig());if err!=nil{b.Fatal(err)}
 expected:=value.TextResult(reference);if independent:=%q;independent!=""&&expected!=independent{b.Fatalf("reference %%s want %%s",expected,independent)}
 b.ReportAllocs();b.ResetTimer();for b.Loop(){v,err:=interpreted.Run(context.Background(),boundary{},budget,device.DefaultConfig());if err!=nil{b.Fatal(err)};sink=v}
}

`, fixture.Name+".lo", fixture.Source, fixture.Expected)
		if err := write(filepath.Join(dir, "program_test.go"), test); err != nil {
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
