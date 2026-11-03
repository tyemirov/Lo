package integration_test

import (
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkpointXORSource(t *testing.T, stage, backend, directory string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repository, "examples", "xor_"+stage+"_"+backend+".lo"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, name := range []string{"xor-state.json", "xor-parameters.json"} {
		text = strings.ReplaceAll(text, fmt.Sprintf("%q", "bin/"+name), fmt.Sprintf("%q", filepath.Join(directory, name)))
	}
	return text
}
