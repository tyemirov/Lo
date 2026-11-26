package check_test

import (
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/compiler"
	"github.com/MarcoPoloResearchLab/Lo/internal/diagnostic"
	"github.com/MarcoPoloResearchLab/Lo/internal/source"
)

func TestTextEncodingConstructors(t *testing.T) {
	for _, test := range []struct {
		name       string
		program    string
		resultType string
		code       diagnostic.Code
	}{
		{"named encoder", `return Bytes.new(value: "é")`, "Bytes", ""},
		{"named decoder", `return String.FromBytes.new(bytes: Bytes.new("é"))`, "String!", ""},
		{"removed encoder", `return UTF8.Bytes.new("é")`, "", diagnostic.UnknownName},
		{"removed decoder", `return UTF8.String.new(Bytes.new("é"))`, "", diagnostic.UnknownName},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := source.New("text-encoding.lo", test.program)
			if err != nil {
				t.Fatal(err)
			}
			program, failure := compiler.Check(file)
			if test.code != "" {
				if failure == nil || failure.Data().Code != test.code {
					t.Fatalf("diagnostic %v, want %s", failure, test.code)
				}
				return
			}
			if failure != nil {
				t.Fatal(failure)
			}
			if got := program.ResultType().String(); got != test.resultType {
				t.Fatalf("result type %s, want %s", got, test.resultType)
			}
		})
	}
}
