//go:build ignore

// copy_runtime replaces the copy of the runtime package in _runtime with the non-test Go files of
// ../runtime. It runs from go generate in the directory of package gen.
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := os.RemoveAll("_runtime"); err != nil {
		log.Fatal(err)
	}
	if err := os.Mkdir("_runtime", 0o755); err != nil {
		log.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "runtime", "*.go"))
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("_runtime", filepath.Base(f)), data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
