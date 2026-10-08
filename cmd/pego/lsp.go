package main

import (
	"flag"
	"io"
	"runtime/debug"

	"github.com/ornew/pego/internal/lsp"
)

// lspCmd runs the language server on standard input and output until the client exits.
func lspCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("lsp", flag.ContinueOnError)
	// Language clients often pass --stdio; standard input and output are the only transport.
	fs.Bool("stdio", true, "communicate over standard input and output (the only transport)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	version := "(unknown)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	return lsp.Serve(stdin, stdout, version)
}
