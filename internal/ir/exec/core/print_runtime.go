//go:build ignore

// print_runtime writes the printed compositions of the Go runtime to runtime/call_exec.go. It runs
// from go generate in the directory of package core.
package main

import (
	"log"
	"os"

	"github.com/ornew/pego/internal/ir/exec/core"
)

func main() {
	src, err := core.GoRuntime()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(core.GoRuntimeFile, []byte(src), 0o644); err != nil {
		log.Fatal(err)
	}
}
