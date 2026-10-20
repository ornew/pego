package pego

import (
	"embed"
	"io/fs"

	"github.com/ornew/pego/internal/engine"
)

// runtimeFiles holds the files of the runtime package, which GenerateGo copies into packages
// generated with RuntimeEmbed and reads to qualify the names generated code uses from it.
//
//go:embed runtime/*.go
var runtimeFiles embed.FS

func init() {
	sub, err := fs.Sub(runtimeFiles, "runtime")
	if err != nil {
		panic(err)
	}
	engine.SetRuntimeSource(sub)
}
