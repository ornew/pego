package main

// Shared flags of the debugging commands (trace, profile, explain).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego"
)

// debugFlags are the flags that select the grammar, the input and how to parse it.
type debugFlags struct {
	fs      *flag.FlagSet
	grammar *string
	start   *string
	input   *string
	unit    *string
	backend *string
}

func newDebugFlags(name string) *debugFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &debugFlags{
		fs:      fs,
		grammar: fs.String("g", "", "grammar file (.pego, .json, or .pegoc)"),
		start:   fs.String("s", "", "start rule name (default: the one saved in a .pegoc, otherwise main)"),
		input:   fs.String("i", "", "input string (default: standard input)"),
		unit:    fs.String("unit", "codepoints", "unit of positions: codepoints or bytes"),
		backend: fs.String("backend", "", "execution backend: closure, bytecode, or bytecode-iterative (default: chosen for the grammar)"),
	}
}

// load parses args and returns the parser, the input and the parse options the flags select.
func (f *debugFlags) load(args []string, stdin io.Reader) (*pego.Parser, string, []pego.ParseOption, error) {
	if err := f.fs.Parse(args); err != nil {
		return nil, "", nil, err
	}
	if f.fs.NArg() > 0 {
		return nil, "", nil, fmt.Errorf("unexpected argument %q", f.fs.Arg(0))
	}
	if *f.grammar == "" {
		return nil, "", nil, fmt.Errorf("-g is required")
	}
	var opts []pego.ParseOption
	switch *f.unit {
	case "codepoints":
		opts = append(opts, pego.WithUnit(pego.CodePoints))
	case "bytes":
		opts = append(opts, pego.WithUnit(pego.Bytes))
	default:
		return nil, "", nil, fmt.Errorf("unknown unit %q", *f.unit)
	}
	switch *f.backend {
	case "":
	case "closure":
		opts = append(opts, pego.WithBackend(pego.Closure))
	case "bytecode":
		opts = append(opts, pego.WithBackend(pego.Bytecode))
	case "bytecode-iterative":
		opts = append(opts, pego.WithBackend(pego.BytecodeIterative))
	default:
		return nil, "", nil, fmt.Errorf("unknown backend %q", *f.backend)
	}
	p, err := loadParser(*f.grammar, *f.start)
	if err != nil {
		return nil, "", nil, err
	}
	src := *f.input
	if !isFlagSet(f.fs, "i") {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", nil, err
		}
		src = string(b)
	}
	return p, src, opts, nil
}

// stringsFlag is a flag that can be repeated, each value possibly a comma-separated list.
type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ",") }

func (s *stringsFlag) Set(v string) error {
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			*s = append(*s, x)
		}
	}
	return nil
}

// snippet quotes text for display, shortened to about n characters.
func snippet(text string, n int) string {
	if utf8.RuneCountInString(text) > n {
		text = string([]rune(text)[:n-3]) + "..."
	}
	return strconv.Quote(text)
}
