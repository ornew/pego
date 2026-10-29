// Package gen generates the source code of parsers from PEGO grammars: Go packages and TypeScript
// modules. A generated parser parses without the grammar and without the pego package; it needs
// only the runtime, which it imports or carries a copy of (RuntimeMode).
//
// Go and TypeScript are configurations: a value holds the settings of one generation, and its
// Generate method generates a parser for a grammar. Both implement Generator.
package gen

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gen/tsgen"
)

// Generator generates the source code of parsers in one language.
type Generator interface {
	// Generate generates the files of a parser for g whose entry points parse from the rule
	// start, which must be a start rule (a rule marked @start); an empty start selects the
	// default start rule.
	Generate(g *grammar.Grammar, start string) ([]File, error)
	// GenerateRuntime generates only the runtime, which does not depend on a grammar.
	GenerateRuntime() ([]File, error)
}

var (
	_ Generator = Go{}
	_ Generator = TypeScript{}
)

// File is a file of generated code. Path is relative to the directory of the generated package
// and uses slashes.
type File = gen.GeneratedFile

// RuntimeMode selects how generated code reaches the runtime.
type RuntimeMode int

// The runtime modes.
const (
	// RuntimeEmbed writes a copy of the runtime next to the parser, so that the generated code
	// depends on nothing else: the package internal/runtime in Go, runtime.ts in TypeScript. It is
	// the default. GenerateRuntime refreshes the copy without regenerating the parser.
	RuntimeEmbed RuntimeMode = iota
	// RuntimeImport imports the runtime: github.com/ornew/pego/runtime in Go, the module of
	// TypeScript.RuntimeModule in TypeScript.
	RuntimeImport
)

// String returns the lowercase name of the mode, embed or import, and RuntimeMode(n) for a value
// that names no mode.
func (m RuntimeMode) String() string {
	switch m {
	case RuntimeEmbed:
		return "embed"
	case RuntimeImport:
		return "import"
	}
	return fmt.Sprintf("RuntimeMode(%d)", int(m))
}

// internal returns the mode of the generators for m, or an error for a value that names no mode.
func (m RuntimeMode) internal() (gen.RuntimeMode, error) {
	switch m {
	case RuntimeEmbed:
		return gen.RuntimeEmbed, nil
	case RuntimeImport:
		return gen.RuntimeImport, nil
	}
	return 0, fmt.Errorf("unknown runtime mode %v", m)
}

// Go configures the generation of a Go parser. The zero value generates an untyped parser with an
// embedded runtime; Module must be set for it.
type Go struct {
	// Module is the import path of the generated package. It is required, except with
	// RuntimeImport and a PackageName and without Types.
	Module string
	// PackageName is the package name of the generated code. It defaults to the last element of
	// Module, without a major version suffix (example.com/m/v2 gives m).
	PackageName string
	// Runtime selects how the generated code reaches the runtime (RuntimeEmbed by default).
	Runtime RuntimeMode
	// Types generates declarations for the grammar's types and typed results: ParseAST for the
	// start rule and ParseRuleAST for named rules. The types are declared in the package
	// Module/types, so Module is required.
	Types bool
	// DefaultValue makes the generated parser produce default values, as pego.WithDefaultValue
	// does for a compiled parser. It cannot be combined with Types.
	DefaultValue bool
	// SourceInfo makes the generated parser record source information, as pego.WithSourceInfo
	// does for a compiled parser.
	SourceInfo bool
	// Observability emits rule tracing and profiling for batch parsers and Documents, including
	// their typed entry points. Collection is selected per invocation.
	Observability bool
	// WorkControls generates the work-controlled form of the parser, which supports cancellation
	// and the input and step limits and re-exports the options WithContext and WithWorkLimits.
	// Without it the generated parser is smaller and faster to build, and a parse that passes a
	// control option returns an error. The controlled form is the only form of the parser: a
	// parse that sets no limit runs it too, and is slower than with a parser generated without
	// work controls.
	WorkControls bool
	// Recognize also generates a function Recognize, which checks the input against the start
	// rule without building a tree, like a parse with pego.RecognizeOnly.
	Recognize bool
	// MaxDepth is the generated parser's default nested rule-call limit. Zero selects 100,000;
	// negative values fail generation. Individual invocations may override it.
	MaxDepth int
	// Document generates a mutable incremental Document. With Types, it also generates
	// Document.ParseAST with direct incremental matching for eligible grammars and Node
	// conversion otherwise.
	Document bool
	// Stream generates ParseStream, which reads the input from an io.Reader and passes each
	// committed element of the start rule's #stream repetition to a callback, as
	// pego.Parser.ParseStream does. The start rule must have a #stream repetition. With Types it
	// also generates ParseStreamAST and IterateAST, which pass the typed value of each element.
	Stream bool
	// TypedOnly generates only the typed entry points of a parser: ParseAST and its siblings, and
	// no Parse, ParseRule or Recognize. The generated package is smaller and builds faster. It
	// needs Types and cannot be combined with Recognize, Document or Stream.
	TypedOnly bool
	// OmitPackageDoc leaves the package comment out of the generated parser, for a package that
	// has its own documentation in another file.
	OmitPackageDoc bool
}

// Generate generates the files of a Go parser for g in the package with import path c.Module:
// its Parse(input) parses from the rule start (see Generator). The files are parser.go, with the
// code generated from the grammar, api.go, which re-exports the runtime names the parser's users
// need, with Types types/types.go, the package Module/types that declares the Go types of the
// grammar, and, with RuntimeEmbed, the files of a copy of the runtime in internal/runtime.
func (c Go) Generate(g *grammar.Grammar, start string) ([]File, error) {
	runtime, err := c.Runtime.internal()
	if err != nil {
		return nil, err
	}
	return gogen.Generate(g, compileForGenerator, gen.GenOptions{
		Module:         c.Module,
		Package:        c.PackageName,
		Start:          start,
		Runtime:        runtime,
		Types:          c.Types,
		DefaultValue:   c.DefaultValue,
		SourceInfo:     c.SourceInfo,
		Observability:  c.Observability,
		WorkControls:   c.WorkControls,
		Recognize:      c.Recognize,
		MaxDepth:       c.MaxDepth,
		Document:       c.Document,
		Stream:         c.Stream,
		TypedOnly:      c.TypedOnly,
		OmitPackageDoc: c.OmitPackageDoc,
	})
}

// GenerateRuntime generates only the embedded copy of the runtime for the package with import
// path c.Module, to refresh a parser generated with RuntimeEmbed without regenerating it. The
// other settings are not used.
func (c Go) GenerateRuntime() ([]File, error) {
	return gogen.Generate(nil, compileForGenerator, gen.GenOptions{Module: c.Module, Runtime: gen.RuntimeOnly})
}

// TypeScript configures the generation of a TypeScript parser. The zero value generates an
// untyped parser with an embedded runtime.
type TypeScript struct {
	// Runtime selects how the generated code reaches the runtime (RuntimeEmbed by default).
	// RuntimeImport needs RuntimeModule.
	Runtime RuntimeMode
	// RuntimeModule is the module specifier from which a parser generated with RuntimeImport
	// imports its runtime, such as "@scope/pego-runtime" or "../shared/runtime.ts". The runtime
	// module is the runtime.ts of GenerateRuntime (or of any parser generated with RuntimeEmbed).
	// It is an error with another mode.
	RuntimeModule string
	// Types generates declarations for the grammar's types and the entry points parseAST and
	// parseASTWithOptions, which convert the completed Node result into plain objects and arrays.
	// Declared aliases preserve the target value type.
	Types bool
	// DefaultValue makes the generated parser produce default values, as pego.WithDefaultValue
	// does for a compiled parser. It cannot be combined with Types.
	DefaultValue bool
	// SourceInfo makes the generated parser record source information, as pego.WithSourceInfo
	// does for a compiled parser.
	SourceInfo bool
	// Observability emits rule tracing and profiling for batch parsing, including the typed entry
	// points. Collection is selected per invocation.
	Observability bool
	// WorkControls generates the work-controlled form of the parser, which supports the signal,
	// maxInputWork and maxSteps options. Without it the generated parser is smaller, and a parse
	// that passes a control option returns an error. The controlled form is the only form of the
	// parser: a parse that sets no limit runs it too, and is slower than with a parser generated
	// without work controls.
	WorkControls bool
	// Recognize also generates recognize, which checks the input against the start rule without
	// building a tree, like a parse with pego.RecognizeOnly.
	Recognize bool
	// MaxDepth is the generated parser's default nested rule-call limit. Zero selects 100,000;
	// negative values and values that are not exact JavaScript integers fail generation.
	// Individual invocations may override it.
	MaxDepth int
}

// Generate generates the files of a TypeScript parser for g: its parse(input) parses from the
// rule start (see Generator) and returns the same trees and errors as a pego.Parser. The files
// are
//
//   - runtime.ts, the runtime, which does not depend on the grammar (with RuntimeEmbed),
//   - parser.ts, the code generated from the grammar, which imports ./runtime.ts, or the
//     RuntimeModule with RuntimeImport, and
//   - types.ts with Types, the declarations of the grammar's types, which parser.ts imports.
//
// Generated TypeScript parsers have no stream or incremental parsing.
func (c TypeScript) Generate(g *grammar.Grammar, start string) ([]File, error) {
	runtime, err := c.Runtime.internal()
	if err != nil {
		return nil, err
	}
	return tsgen.Generate(g, compileForGenerator, gen.GenOptions{
		Start:         start,
		Runtime:       runtime,
		RuntimeModule: c.RuntimeModule,
		Types:         c.Types,
		DefaultValue:  c.DefaultValue,
		SourceInfo:    c.SourceInfo,
		Observability: c.Observability,
		WorkControls:  c.WorkControls,
		Recognize:     c.Recognize,
		MaxDepth:      c.MaxDepth,
	})
}

// GenerateRuntime generates only the runtime of TypeScript parsers, runtime.ts: the module that
// parsers generated with RuntimeImport import, or the copy of a parser generated with RuntimeEmbed.
// The settings are not used.
func (c TypeScript) GenerateRuntime() ([]File, error) {
	return tsgen.Generate(nil, compileForGenerator, gen.GenOptions{Runtime: gen.RuntimeOnly})
}

// compileForGenerator is the gen.Compiler that the generators compile grammars with: it compiles g
// with the engine and builds the recognizer of the program (engine.Program.Recognizer) on first
// use.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := engine.Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*compile.Program, error) {
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		return rec.Static(), nil
	}
	return prog.Static(), recognizer, nil
}

// runtimeFiles holds a copy of the files of the runtime package, which Go.Generate copies into
// packages generated with RuntimeEmbed and reads to qualify the names generated code uses from it.
// The copy lives in this package rather than in package pego so that only programs that generate
// code carry it; TestRuntimeCopy keeps it equal to the runtime, and go generate refreshes it.
//
//go:generate go run copy_runtime.go
//go:embed _runtime/*.go
var runtimeFiles embed.FS

func init() {
	sub, err := fs.Sub(runtimeFiles, "_runtime")
	if err != nil {
		panic(err)
	}
	gogen.SetRuntimeSource(sub)
}
