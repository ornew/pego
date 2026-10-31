package main

// Shared flags of the debugging commands (trace, profile, explain).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/zeroparse"
)

// debugFlags are the flags that select the grammar, the input and how to parse it.
type debugFlags struct {
	fs      *flag.FlagSet
	grammar *string
	start   *string
	input   *string
	unit    *string
	backend *string
	limits  limitFlags
}

func newDebugFlags(name string) *debugFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	f := &debugFlags{
		fs:      fs,
		grammar: fs.String("g", "", "grammar file (.zp, .json, or .zpc)"),
		start:   fs.String("s", "", "start rule name (default: the one saved in a .zpc, otherwise the grammar's default start rule)"),
		input:   fs.String("i", "", "input string (default: standard input)"),
		unit:    fs.String("unit", "codepoints", "unit of positions: codepoints or bytes"),
		backend: fs.String("backend", "", "execution backend: closure, bytecode, or bytecode-iterative (default: chosen for the grammar)"),
	}
	f.limits.register(fs)
	return f
}

// load parses args and returns the parser, the input and the parse options the flags select.
func (f *debugFlags) load(args []string, stdin io.Reader) (*zeroparse.Parser, string, []zeroparse.ParseOption, error) {
	if err := f.fs.Parse(args); err != nil {
		return nil, "", nil, err
	}
	if f.fs.NArg() > 0 {
		return nil, "", nil, fmt.Errorf("unexpected argument %q", f.fs.Arg(0))
	}
	if *f.grammar == "" {
		return nil, "", nil, fmt.Errorf("-g is required")
	}
	// The commands show what the grammar expected, so the parse records it.
	opts := []zeroparse.ParseOption{zeroparse.WithExpectations()}
	switch *f.unit {
	case "codepoints":
		opts = append(opts, zeroparse.WithUnit(zeroparse.CodePoints))
	case "bytes":
		opts = append(opts, zeroparse.WithUnit(zeroparse.Bytes))
	default:
		return nil, "", nil, fmt.Errorf("unknown unit %q", *f.unit)
	}
	switch *f.backend {
	case "":
	case "closure":
		opts = append(opts, zeroparse.WithBackend(zeroparse.Closure))
	case "bytecode":
		opts = append(opts, zeroparse.WithBackend(zeroparse.Bytecode))
	case "bytecode-iterative":
		opts = append(opts, zeroparse.WithBackend(zeroparse.BytecodeIterative))
	default:
		return nil, "", nil, fmt.Errorf("unknown backend %q", *f.backend)
	}
	opts = append(opts, f.limits.option())
	p, err := loadParser(*f.grammar, *f.start, compileFlags{})
	if err != nil {
		return nil, "", nil, err
	}
	// These commands trace the parse, which a compiled grammar serves in document mode.
	if mode, loaded := p.CompiledMode(); loaded && mode != zeroparse.CompiledDocument {
		return nil, "", nil, fmt.Errorf("%s: the compiled grammar is in %s mode; %s traces the parse, which needs document mode: compile it with -mode document, or use the grammar source", *f.grammar, mode, f.fs.Name())
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
