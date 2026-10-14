// Command nativebench emits paired execution benchmarks from the fixed corpus.
package main

import (
	"flag"
	"fmt"
	"github.com/MarcoPoloResearchLab/Lo/internal/codegen"
	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
	"os"
	"path/filepath"
	"strings"
)

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
		file, err := source.New(fixture.Name+".lo", fixture.Source)
		if err != nil {
			return err
		}
		p, d := compiler.Check(file)
		if d != nil {
			return d
		}
		data, err := codegen.Generate(p, "fixture")
		if err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "program.go"), string(data)); err != nil {
			return err
		}
		test := fmt.Sprintf(`package fixture
import("context";"os";"testing";"github.com/MarcoPoloResearchLab/Lo/internal/compiler";"github.com/MarcoPoloResearchLab/Lo/internal/eval";"github.com/MarcoPoloResearchLab/Lo/internal/execution";"github.com/MarcoPoloResearchLab/Lo/internal/source";"github.com/MarcoPoloResearchLab/Lo/internal/value")
type boundary struct{}
func(boundary)Read(context.Context,value.Text)(value.Bytes,error){return nil,execution.Denied("read","benchmark")}
func(boundary)Commit(context.Context,value.Target)error{return execution.Denied("commit","benchmark")}
var sink value.Value
func BenchmarkExecution(b *testing.B){
 file,err:=source.New(%q,%q);if err!=nil{b.Fatal(err)};p,d:=compiler.Check(file);if d!=nil{b.Fatal(d)}
 interpreted,err:=eval.Program(p);if err!=nil{b.Fatal(err)}
 native,err:=New();if err!=nil{b.Fatal(err)};budget,err:=execution.NewBudget(10000000);if err!=nil{b.Fatal(err)}
 reference,err:=interpreted.Run(context.Background(),boundary{},budget);if err!=nil{b.Fatal(err)}
 expected:=value.TextResult(reference);if independent:=%q;independent!=""&&expected!=independent{b.Fatalf("reference %%s want %%s",expected,independent)}
 order:=[]string{"Interpreter","Native"};if os.Getenv("LO_BENCH_FIRST")=="native"{order=[]string{"Native","Interpreter"}}
 for _,backend:=range order{b.Run(backend,func(b *testing.B){
 program:=interpreted;if backend=="Native"{program=native}
 run:=func()(value.Value,error){return program.Run(context.Background(),boundary{},budget)}
 result,err:=run();if err!=nil{b.Fatal(err)};if value.TextResult(result)!=expected{b.Fatalf("result %%s want %%s",value.TextResult(result),expected)}
 b.ReportAllocs();b.ResetTimer();for b.Loop(){v,err:=run();if err!=nil{b.Fatal(err)};sink=v}
 })}
}
`, fixture.Name+".lo", fixture.Source, fixture.Expected)
		if err := write(filepath.Join(dir, "program_test.go"), test); err != nil {
			return err
		}
		if err := write(filepath.Join(dir, "source.lo"), fixture.Source); err != nil {
			return err
		}
		paths = append(paths, "./"+filepath.ToSlash(dir))
	}
	fmt.Println(strings.Join(paths, "\n"))
	return nil
}
