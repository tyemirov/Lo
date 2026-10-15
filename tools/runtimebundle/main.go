// Command runtimebundle packages the canonical native runtime sources.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var packages = []string{"adapter", "catalog", "derivative", "device", "diagnostic", "execution", "identity", "runner", "runtimeops", "source", "tensor", "types", "value"}

func main() {
	check := flag.Bool("check", false, "verify the bundle against canonical sources")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(check bool) error {
	files := map[string][]byte{}
	add := func(source, target string) error {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		files[target] = data
		return nil
	}
	for _, pkg := range packages {
		directory := filepath.Join("internal", pkg)
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if pkg == "device" && name == "metal_darwin.go" {
				continue
			}
			if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				if err := add(filepath.Join(directory, name), filepath.ToSlash(filepath.Join(directory, name))); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range []string{"go.mod", "go.sum", "LICENSE"} {
		if err := add(name, name); err != nil {
			return err
		}
	}
	download := exec.Command("go", "mod", "download", "golang.org/x/sys")
	if output, err := download.CombinedOutput(); err != nil {
		return fmt.Errorf("download pinned dependency: %w: %s", err, output)
	}
	command := exec.Command("go", "list", "-m", "-json", "golang.org/x/sys")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("locate pinned dependency: %w", err)
	}
	var dependency struct{ Dir, Path, Version, GoVersion string }
	if err := json.Unmarshal(output, &dependency); err != nil {
		return err
	}
	if dependency.Version != "v0.49.0" {
		return fmt.Errorf("unexpected dependency %s", dependency.Version)
	}
	for _, directory := range []string{"unix"} {
		err := filepath.WalkDir(filepath.Join(dependency.Dir, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			ext := filepath.Ext(name)
			if ext != ".go" && ext != ".s" && ext != ".h" {
				return nil
			}
			relative, err := filepath.Rel(dependency.Dir, path)
			if err != nil {
				return err
			}
			return add(path, "vendor/"+dependency.Path+"/"+filepath.ToSlash(relative))
		})
		if err != nil {
			return err
		}
	}
	for _, name := range []string{"LICENSE", "PATENTS"} {
		if err := add(filepath.Join(dependency.Dir, name), "vendor/"+dependency.Path+"/"+name); err != nil {
			return err
		}
	}
	files["vendor/modules.txt"] = []byte(fmt.Sprintf("# %s %s\n## explicit; go %s\n%s/unix\n", dependency.Path, dependency.Version, dependency.GoVersion, dependency.Path))
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	manifest := map[string]string{}
	for _, name := range names {
		file, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err = file.Write(files[name]); err != nil {
			return err
		}
		digest := sha256.Sum256(files[name])
		manifest[name] = hex.EncodeToString(digest[:])
	}
	if err := writer.Close(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	for name, data := range map[string][]byte{"runtime.zip": archive.Bytes(), "manifest.json": data} {
		path := filepath.Join("internal/runtimebundle", name)
		if check {
			existing, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(existing, data) {
				return fmt.Errorf("runtime bundle differs from canonical inputs: %s", name)
			}
			continue
		}
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
