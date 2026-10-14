package main

import (
	"encoding/json"
	"github.com/MarcoPoloResearchLab/Lo/tests/corpus"
	"os"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(corpus.Cases()); err != nil {
		panic(err)
	}
}
