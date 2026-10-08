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
	// Clients also pass flags of their own (such as --clientProcessId=N), and the server has no
	// options, so every argument but a request for help is ignored.
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			return fs.Parse([]string{a})
		}
	}
	version := "(unknown)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	return lsp.Serve(stdin, stdout, version)
}
