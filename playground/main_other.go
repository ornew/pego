//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "playground: build this program with GOOS=js GOARCH=wasm (see site/build.sh)")
	os.Exit(2)
}
