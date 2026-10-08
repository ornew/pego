// Command pego is a command-line tool for working with PEGO grammars.
//
// Usage:
//
//	pego parse -g grammar.pego [-s main] [-i input] [-f json|sexpr] [-stream] [-unit u] [-backend b]
//	pego fmt [-w] [-l] [grammar.pego ...]
//	pego convert [-to pego|json] [-o output] grammar.pego|grammar.json|grammar.pegoc
//	pego gen -g grammar.pego -pkg name [-s main] [-o parser.go] [-types] [-recognize]
//	pego gen -lang ts -g grammar.pego [-s main] [-o parser.ts] [-recognize]
//	pego compile -g grammar.pego [-s main] [-no-ast] -o grammar.pegoc
//	pego trace -g grammar.pego [-s main] [-i input] [-max-depth n] [-rule name] [-failures] [-f text|json]
//	pego profile -g grammar.pego [-s main] [-i input] [-sort column] [-n rows] [-f text|json]
//	pego explain -g grammar.pego [-s main] [-i input] [-n calls]
//	pego lint -g grammar.pego [-s main] [-f text|json] [-disable checks] [-strict] [-list]
//	pego sample -g grammar.pego [-s main] [-n 10] [-seed N] [-max-depth D] [-max-repeat R] [-max-len L] [-budget B]
//	            [-coverage] [-invalid] [-f lines|json]
//	pego lsp
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

const usage = `usage: pego <command> [flags] [arguments]

Commands:

  parse -g <grammar> [-s <rule>] [-i <input>] [-f json|sexpr] [-stream] [-check]
        [-unit codepoints|bytes] [-backend closure|bytecode|bytecode-iterative]
      Parse the input with a grammar and print the syntax tree. Without -i,
      the input is read from standard input.

  fmt [-w] [-l] [<file.pego> ...]
      Format PEGO source files, keeping comments. Without files, standard
      input is formatted. -w rewrites the files in place; -l lists the
      files whose formatting differs.

  convert [-to pego|json] [-o <file>] <grammar>
      Convert a grammar between PEGO source and JSON. -to defaults to the
      other format of the input and is required for compiled grammars.

  gen -g <grammar> -pkg <package> [-s <rule>] [-o <file>] [-types] [-recognize]
  gen -lang ts -g <grammar> [-s <rule>] [-o <file>] [-recognize]
      Generate a Go parser (or, with -lang ts, a TypeScript module) from a
      grammar. Without -o, the code is written to standard output. With
      -types, Go types for the grammar's types and ParseAST, which returns
      the result as values of those types, are generated too (Go only);
      with -recognize, Recognize (recognize in TypeScript), which checks
      input without building a tree.

  compile -g <grammar> [-s <rule>] [-no-ast] -o <file.pegoc>
      Compile a grammar and save it. The result can be used as <grammar>
      by the other commands. With -no-ast, the grammar AST is omitted; the
      result then runs only on the bytecode backends and cannot be
      converted back to a grammar.

  trace -g <grammar> [-s <rule>] [-i <input>] [-max-depth <n>] [-rule <name>]
        [-failures] [-f text|json] [-unit u] [-backend b]
      Parse the input and print every rule call as an indented call tree
      with positions, results and memo hits (-f json: one event per line).

  profile -g <grammar> [-s <rule>] [-i <input>] [-sort <column>] [-n <rows>]
        [-f text|json] [-unit u] [-backend b]
      Parse the input and print the cost of each rule, with hints on where
      the grammar does more work than it needs to.

  explain -g <grammar> [-s <rule>] [-i <input>] [-n <calls>] [-unit u] [-backend b]
      Parse the input and, for a syntax error, print the rule calls that
      recorded what it says was expected, each with the calls it was
      nested in.

  lint -g <grammar> [-s <rule>] [-f text|json] [-disable <checks>] [-strict]
       [-list]
      Report likely mistakes in a grammar: alternatives and expressions
      that can never match or have no effect, unused captures and rules,
      and shapes that slow down incremental parsing. Fails if an error is
      found (with -strict, also a warning). -list lists the checks.

  sample -g <grammar> [-s <rule>] [-n <count>] [-seed <n>] [-max-depth <d>]
         [-max-repeat <r>] [-max-len <bytes>] [-budget <steps>] [-coverage] [-invalid]
         [-f lines|json]
      Generate distinct inputs that the grammar accepts, for tests and fuzz
      corpora, each printed as a quoted string on its own line. -coverage
      prefers rules and alternatives not exercised yet and reports what was
      missed; -invalid generates near-miss inputs that the grammar rejects.

  lsp
      Run a Language Server Protocol server for .pego files on standard
      input and output, for editors: diagnostics, formatting, go to
      definition, references, hover, rename and completion.

A <grammar> is PEGO source (.pego), a grammar in JSON (.json), or a
grammar compiled with pego compile (.pegoc).

Run "pego <command> -h" for the flags of a command.`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pego:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "parse":
		return parse(args[1:], stdin, stdout)
	case "gen":
		return gen(args[1:], stdout)
	case "compile":
		return compileCmd(args[1:])
	case "fmt":
		return fmtCmd(args[1:], stdin, stdout)
	case "convert":
		return convertCmd(args[1:], stdout)
	case "trace":
		return traceCmd(args[1:], stdin, stdout)
	case "profile":
		return profileCmd(args[1:], stdin, stdout)
	case "explain":
		return explainCmd(args[1:], stdin, stdout)
	case "sample":
		return sampleCmd(args[1:], stdout)
	case "lsp":
		return lspCmd(args[1:], stdin, stdout)
	case "lint":
		return lintCmd(args[1:], stdout)
	}
	return fmt.Errorf("%s", usage)
}

// parseFlags parses args with fs, allowing flags after positional
// arguments, and returns the positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func fmtCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	write := fs.Bool("w", false, "write the result to the file instead of standard output")
	list := fs.Bool("l", false, "list the files whose formatting differs")
	files, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		if *write {
			return errors.New("fmt: cannot use -w with standard input")
		}
		src, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		return formatSource("<standard input>", src, false, *list, stdout)
	}
	var errs []error
	for _, path := range files {
		if err := formatFile(path, *write, *list, stdout); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func formatFile(path string, write, list bool, stdout io.Writer) error {
	if filepath.Ext(path) != ".pego" {
		return fmt.Errorf("%s: fmt formats only PEGO source files (.pego); use pego convert for JSON or compiled grammars", path)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return formatSource(path, src, write, list, stdout)
}

// formatSource formats the PEGO source src read from name. It rewrites
// name if write is set, lists name if list is set and the result differs
// from src, and otherwise writes the result to stdout.
func formatSource(name string, src []byte, write, list bool, stdout io.Writer) error {
	if pego.IsCompiled(src) {
		return fmt.Errorf("%s: a compiled grammar is not PEGO source; use pego convert -to pego to print its source", name)
	}
	if t := bytes.TrimSpace(src); len(t) > 0 && t[0] == '{' {
		return fmt.Errorf("%s: JSON is not PEGO source; use pego convert to convert it", name)
	}
	g, err := pego.ParseGrammar(string(src))
	if err != nil {
		return fileError(name, err)
	}
	out := grammar.Format(g)
	changed := out != string(src)
	if list && changed {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			return err
		}
	}
	if write {
		if !changed {
			return nil
		}
		info, err := os.Stat(name)
		if err != nil {
			return err
		}
		return os.WriteFile(name, []byte(out), info.Mode().Perm())
	}
	if list {
		return nil
	}
	_, err = io.WriteString(stdout, out)
	return err
}

func convertCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	to := fs.String("to", "", "output format, pego or json (default: the other format of the input)")
	output := fs.String("o", "", "output file (default: standard output)")
	paths, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return errors.New("convert: expected one grammar file")
	}
	path := paths[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	target := *to
	if target == "" {
		switch {
		case pego.IsCompiled(data):
			return fmt.Errorf("convert: %s is a compiled grammar; choose the output format with -to pego or -to json", path)
		case filepath.Ext(path) == ".json":
			target = "pego"
		default:
			target = "json"
		}
	}
	if target != "pego" && target != "json" {
		return fmt.Errorf("convert: unknown format %q (want pego or json)", target)
	}
	g, _, err := loadGrammar(path)
	if err != nil {
		return err
	}
	var out []byte
	if target == "pego" {
		out = []byte(grammar.Format(g))
	} else if out, err = grammar.MarshalJSON(g); err != nil {
		return err
	}
	if *output == "" {
		_, err = stdout.Write(out)
		return err
	}
	return os.WriteFile(*output, out, 0o644)
}

// fileError prefixes each line of err (one per error when there are several) with the name of
// the file it is about. Lines with a position ("line:col: ...") are joined to the name with a
// colon alone, like compiler messages.
func fileError(name string, err error) error {
	lines := strings.Split(err.Error(), "\n")
	for i, l := range lines {
		if l != "" && l[0] >= '0' && l[0] <= '9' {
			lines[i] = name + ":" + l
		} else {
			lines[i] = name + ": " + l
		}
	}
	return &wrappedError{msg: strings.Join(lines, "\n"), err: err}
}

// wrappedError is an error with a rewritten message that still unwraps to the original.
type wrappedError struct {
	msg string
	err error
}

func (e *wrappedError) Error() string { return e.msg }
func (e *wrappedError) Unwrap() error { return e.err }

// loadGrammar reads the grammar file at path. For a compiled grammar, it also returns the start
// rule saved in it.
func loadGrammar(path string) (g *grammar.Grammar, saved string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if pego.IsCompiled(data) {
		p, err := pego.LoadParser(data)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		if p.Grammar() == nil {
			return nil, "", fmt.Errorf("%s: the compiled grammar omits the AST", path)
		}
		return p.Grammar(), p.Start(), nil
	}
	if strings.HasSuffix(path, ".json") {
		g, err = grammar.UnmarshalJSON(data)
	} else {
		g, err = pego.ParseGrammar(string(data))
	}
	if err != nil {
		return nil, "", fileError(path, err)
	}
	return g, "", nil
}

func parse(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc)")
	start := fs.String("s", "", "start rule name (default: the one saved in a .pegoc, otherwise main)")
	input := fs.String("i", "", "input string (default: standard input)")
	format := fs.String("f", "json", "output format: json or sexpr")
	stream := fs.Bool("stream", false, "print each #stream element of the start rule on its own line as soon as it matches")
	unitName := fs.String("unit", "codepoints", "unit of positions: codepoints or bytes")
	check := fs.Bool("check", false, "only check that the input matches the grammar, without building a tree, and print ok if it does")
	backendName := fs.String("backend", "", "execution backend: closure, bytecode, or bytecode-iterative (default: chosen for the grammar)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *grammarPath == "" {
		return fmt.Errorf("-g is required")
	}
	p, err := loadParser(*grammarPath, *start)
	if err != nil {
		return err
	}
	if *format != "json" && *format != "sexpr" {
		return fmt.Errorf("unknown format %q", *format)
	}
	var unit pego.ParseOption
	switch *unitName {
	case "codepoints":
		unit = pego.WithUnit(pego.CodePoints)
	case "bytes":
		unit = pego.WithUnit(pego.Bytes)
	default:
		return fmt.Errorf("unknown unit %q", *unitName)
	}
	var backend pego.ParseOption
	switch *backendName {
	case "":
		backend = pego.WithBackend(pego.DefaultBackend)
	case "closure":
		backend = pego.WithBackend(pego.Closure)
	case "bytecode":
		backend = pego.WithBackend(pego.Bytecode)
	case "bytecode-iterative":
		backend = pego.WithBackend(pego.BytecodeIterative)
	default:
		return fmt.Errorf("unknown backend %q", *backendName)
	}
	if *stream {
		var r io.Reader = stdin
		if isFlagSet(fs, "i") {
			r = strings.NewReader(*input)
		}
		return p.ParseStream(r, func(n *pego.Node) error {
			if *format == "sexpr" {
				_, err := fmt.Fprintln(stdout, n)
				return err
			}
			return json.NewEncoder(stdout).Encode(n)
		}, unit, backend)
	}
	src := *input
	if !isFlagSet(fs, "i") {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		src = string(b)
	}
	if *check {
		if _, err := p.Parse(src, unit, backend, pego.RecognizeOnly()); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "ok")
		return nil
	}
	node, perr := p.Parse(src, unit, backend)
	if node == nil {
		return perr
	}
	// Print the tree even if the parser recovered from errors, and then
	// return the error.
	switch *format {
	case "sexpr":
		_, err = fmt.Fprintln(stdout, node)
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(node)
	default:
		return fmt.Errorf("unknown format %q", *format)
	}
	if err != nil {
		return err
	}
	return perr
}

func isFlagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func gen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc)")
	lang := fs.String("lang", "go", "language of the generated code: go or ts (TypeScript)")
	pkg := fs.String("pkg", "", "package name of the generated code (Go)")
	start := fs.String("s", "", "start rule of the generated Parse function (default: the one saved in a .pegoc, otherwise main)")
	output := fs.String("o", "", "output file (default: standard output)")
	types := fs.Bool("types", false, "also generate Go types for the grammar's types and ParseAST (Go)")
	recognize := fs.Bool("recognize", false, "also generate Recognize, which checks input without building a tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *lang {
	case "go":
		if *grammarPath == "" || *pkg == "" {
			return fmt.Errorf("-g and -pkg are required")
		}
	case "ts":
		if *grammarPath == "" {
			return fmt.Errorf("-g is required")
		}
		if *pkg != "" || *types {
			return fmt.Errorf("-pkg and -types are not supported with -lang ts")
		}
	default:
		return fmt.Errorf("unknown language %q (want go or ts)", *lang)
	}
	g, saved, err := loadGrammar(*grammarPath)
	if err != nil {
		return err
	}
	if *start == "" {
		*start = cmp.Or(saved, "main")
	}
	var opts []pego.GenOption
	if *types {
		opts = append(opts, pego.WithTypes())
	}
	if *recognize {
		opts = append(opts, pego.WithRecognize())
	}
	var code []byte
	if *lang == "ts" {
		code, err = pego.GenerateTypeScript(g, *start, opts...)
	} else {
		code, err = pego.GenerateGo(g, *pkg, *start, opts...)
	}
	if err != nil {
		return fileError(*grammarPath, err)
	}
	if *output == "" {
		_, err = stdout.Write(code)
		return err
	}
	return os.WriteFile(*output, code, 0o644)
}

// loadParser creates a parser from the grammar file at path, starting at
// the rule start. A compiled grammar is loaded as is, without being
// compiled again; if start is empty, it keeps the start rule saved in it,
// and other grammars start at main.
func loadParser(path, start string) (*pego.Parser, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if pego.IsCompiled(data) {
		p, err := pego.LoadParser(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if start == "" {
			return p, nil
		}
		return p.WithStart(start)
	}
	g, _, err := loadGrammar(path)
	if err != nil {
		return nil, err
	}
	p, err := pego.Compile(g, cmp.Or(start, "main"))
	if err != nil {
		return nil, fileError(path, err)
	}
	return p, nil
}

func compileCmd(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	grammarPath := fs.String("g", "", "grammar file (.pego, .json, or .pegoc)")
	start := fs.String("s", "", "start rule saved as the default (default: the one saved in a .pegoc, otherwise main)")
	output := fs.String("o", "", "output file (.pegoc)")
	noAST := fs.Bool("no-ast", false, "omit the grammar AST (the result runs only on the bytecode backends)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *grammarPath == "" || *output == "" {
		return fmt.Errorf("-g and -o are required")
	}
	p, err := loadParser(*grammarPath, *start)
	if err != nil {
		return err
	}
	var opts []pego.MarshalOption
	if *noAST {
		opts = append(opts, pego.WithoutAST())
	}
	data, err := p.Marshal(opts...)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, data, 0o644)
}
