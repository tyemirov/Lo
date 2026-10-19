package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) != 0 {
		fmt.Fprintln(os.Stderr, "probe input must be closed")
		os.Exit(3)
	}
	if path := os.Getenv("LO_TEST_PROBE_RECORD"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			panic(err)
		}
		err = json.NewEncoder(file).Encode(os.Args[1:])
		if closeErr := file.Close(); err != nil || closeErr != nil {
			panic("record probe arguments")
		}
	}
	if name := os.Getenv("LO_TEST_PROBE_CHANGE"); name != "" {
		if len(os.Args) != 4 || filepath.Base(name) != name {
			panic("invalid fixture mutation")
		}
		path := filepath.Join(os.Args[3], name)
		if os.Getenv("LO_TEST_PROBE_ACTION") == "remove" {
			if err := os.Remove(path); err != nil {
				panic(err)
			}
		} else {
			file, err := os.Create(path)
			if err != nil {
				panic(err)
			}
			if _, err := file.WriteString("changed by probe fixture"); err != nil {
				panic(err)
			}
			if err := file.Close(); err != nil {
				panic(err)
			}
		}
	}
	if os.Getenv("LO_TEST_PROBE_LIST") == "1" {
		entries, err := os.ReadDir(".")
		if err != nil {
			panic(err)
		}
		for _, entry := range entries {
			fmt.Println("n./" + entry.Name())
		}
		os.Exit(0)
	}
	if os.Getenv("LO_TEST_PROBE_WAIT") == "1" {
		if ready := os.Getenv("LO_TEST_PROBE_READY"); ready != "" {
			file, err := os.Create(ready)
			if err != nil {
				panic(err)
			}
			if err := file.Close(); err != nil {
				panic(err)
			}
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if count, err := strconv.Atoi(os.Getenv("LO_TEST_PROBE_BYTES")); err == nil {
		fmt.Print(strings.Repeat("x", count))
	} else {
		fmt.Print(os.Getenv("LO_TEST_PROBE_STDOUT"))
	}
	fmt.Fprint(os.Stderr, os.Getenv("LO_TEST_PROBE_STDERR"))
	status, _ := strconv.Atoi(os.Getenv("LO_TEST_PROBE_EXIT"))
	os.Exit(status)
}
