// Package gen holds what the Go and TypeScript code generators share: the generation options, the
// runtime mode of generated Go parsers, generated files and the description of expressions in
// generated messages.
package gen

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// GenOptions configures code generation.
type GenOptions struct {
	// TypedOnly generates only the typed entry points (ParseAST, ParseRuleAST and their
	// siblings): no Parse, ParseRule or Recognize, and no function that builds an untyped Node.
	// It needs Types and excludes Recognize, Document and Stream.
	TypedOnly bool
	// WorkControls emits the work-controlled form of the generated functions, which charges
	// cancellation and the input and step limits (WithContext, WithWorkLimits; signal, maxInputWork
	// and maxSteps in TypeScript), and re-exports those options. Without it the functions have no charges (the ordinary form, or with Document
	// the form that serves a Document), and a parse with a control option fails with an error.
	WorkControls bool
	// Observability emits opt-in tracing and profiling for generated Go batch
	// and Document parsing, and for TypeScript batch parsing.
	Observability bool
	// Document includes the generated Go Document runner. With Types, eligible
	// outputs use private typed memoization; other outputs use Node conversion.
	Document bool
	// Stream generates ParseStream, which reads the input from an io.Reader
	// and passes the committed elements of the start rule's #stream
	// repetition to a callback. The start rule must have one.
	Stream bool
	// SourceInfo records source information in the values: the rule that produced each node and
	// where its captures are (Options.SourceInfo).
	SourceInfo bool
	// DefaultValue drops value actions and declared rule types, so the parser returns the default
	// node tree (Options.DefaultValue). It cannot be combined with Types.
	DefaultValue bool
	// Module is the import path of the generated package (required for Go output). The embedded
	// runtime is imported from its internal/runtime directory.
	Module string
	// Package is the package name of the generated code. It defaults to the last element of Module,
	// without a major version suffix (DefaultPackageName).
	Package string
	// Runtime selects how the generated Go code reaches the runtime: embedded in the package
	// (the default), imported from this module, or generated alone.
	Runtime RuntimeMode
	// RuntimeModule is the specifier from which TypeScript parsers import the runtime with RuntimeImport,
	// such as "@scope/pego-runtime" or "../shared/runtime.ts".
	RuntimeModule string
	// MaxDepth is the generated default rule-call limit. Zero selects 100,000.
	MaxDepth int
	// Start is the start rule of the generated Parse function, a rule marked @start. Empty
	// selects the default start rule.
	Start string
	// Types generates Go types, ParseAST for the start rule, and ParseRuleAST
	// for a named rule as values of those types (see gogen/types.go).
	Types bool
	// Recognize also generates Recognize, which checks the input without building a tree (as the
	// engine's ParseOptions.Recognize does).
	Recognize bool
	// OmitPackageDoc leaves out the package comment, for a package that documents itself in another
	// file.
	OmitPackageDoc bool
	// ConvertTypes makes typed entry points convert Node results even when the
	// typed runtime could build them (for tests).
	ConvertTypes bool
	// LeftRecursionBindings compiles with the compiler option leftRecursionBindings (for tests).
	LeftRecursionBindings bool
	// KeepNodes keeps the untyped Node rule functions of a typed-only parser. generate sets it
	// itself, when a typed entry point converts the Node result.
	KeepNodes bool
	// KeepUnreachable emits the bodies of the rules that no entry point or generated call reaches
	// (gogen/reach.go), as before they were left out, for differential tests and size comparisons.
	KeepUnreachable bool
	// KeepComments keeps the comments of unexported generated code, which tests read as route markers.
	KeepComments bool
	// DisableKeepNodes leaves the Node rule functions out of a typed-only parser that needs them
	// (a negative control for tests).
	DisableKeepNodes bool
	// DisableMethodSharing and DisableLiteralSharing retain duplicate generated
	// helpers and read-only tables for independent generation-time controls.
	// Neither adds a selector to the generated parser.
	DisableMethodSharing  bool
	DisableLiteralSharing bool
	// EnableTSLiteralSharing retains a generation-only experiment. Sharing
	// literal tables can hurt TypeScript warm-up; production shares helpers
	// only. An explicit DisableLiteralSharing takes precedence.
	EnableTSLiteralSharing bool
	// DisableLiteralTextDispatch keeps literal text disjunctions on the general
	// expression path for differential tests and same-generator measurements.
	DisableLiteralTextDispatch bool
	// DisableTerminalPrograms keeps value-free terminal expressions of lean typed parsers as
	// straight-line code (gogen/terminal.go) for differential tests and measurements.
	DisableTerminalPrograms bool
	// DisableGuardedClasses makes the first class of an alternative read and test the character
	// that the guard of its choice has accepted (core.Alt.GuardedClass), for differential tests.
	DisableGuardedClasses bool
	// BoxedComparisons evaluates comparisons of fields as other binary operations, for tests.
	BoxedComparisons bool
	// Independent generation-only controls for typed object and list slabs.
	// Reference parsers allocate each object or list separately.
	DisableTypedObjectSlabs bool
	DisableTypedListSlabs   bool
	// DisableTypedOutputBootstrap retains the original slab first allocation
	// for same-generator retention and throughput comparisons.
	DisableTypedOutputBootstrap bool
	// DisableSparseMemo keeps the dense first-call bitset in TypeScript output
	// for same-generator comparisons without a parser-time selector.
	DisableSparseMemo bool
}

// ValidateDefaultValue rejects typed output with default values: the types come from the actions and
// declarations that default values drop.
func (opts GenOptions) ValidateDefaultValue() error {
	if opts.DefaultValue && opts.Types {
		return fmt.Errorf("typed output is not available with default Values: the types come from the actions and declared types that default values drop")
	}
	return nil
}

func (o GenOptions) ValidateMaxDepth(ts bool) error {
	if o.MaxDepth < 0 {
		return fmt.Errorf("generated max depth must be non-negative: %d", o.MaxDepth)
	}
	if ts && uint64(o.MaxDepth) > 9007199254740991 {
		return fmt.Errorf("generated max depth must be a safe JavaScript integer: %d", o.MaxDepth)
	}
	return nil
}

// RuntimeMode selects how a generated Go parser reaches the runtime.
type RuntimeMode int

const (
	// RuntimeEmbed writes a copy of the runtime into the generated package's internal/runtime
	// directory, so that the generated code depends only on the standard library.
	RuntimeEmbed RuntimeMode = iota
	// RuntimeImport imports the runtime from this module.
	RuntimeImport
	// RuntimeOnly generates only the embedded copy of the runtime, for refreshing a package
	// generated with RuntimeEmbed without regenerating its parser.
	RuntimeOnly
)

func (m RuntimeMode) String() string {
	switch m {
	case RuntimeEmbed:
		return "embed"
	case RuntimeImport:
		return "import"
	case RuntimeOnly:
		return "only"
	}
	return fmt.Sprintf("RuntimeMode(%d)", int(m))
}

// ParseRuntimeMode returns the mode named s: embed, import or only.
func ParseRuntimeMode(s string) (RuntimeMode, error) {
	switch s {
	case "embed":
		return RuntimeEmbed, nil
	case "import":
		return RuntimeImport, nil
	case "only":
		return RuntimeOnly, nil
	}
	return 0, fmt.Errorf("unknown runtime mode %q: want embed, import or only", s)
}

// GeneratedFile is a file of generated code. Path is relative to the directory of the generated
// package and uses slashes.
type GeneratedFile struct {
	Path    string
	Content []byte
}

// Describe returns the source form of the expression e for messages, or "" when it spans lines
// or is longer than 100 bytes.
func Describe(e grammar.Expr) string {
	s := grammar.FormatExpr(e)
	if strings.Contains(s, "\n") || len(s) > 100 {
		return ""
	}
	return s
}

// Compiler compiles a grammar for a generator. It returns the compiled program and a function
// that returns the program of its recognizer (the program in which no rule builds a value),
// which it compiles on first use.
type Compiler func(g *grammar.Grammar, opts compile.Options) (prog *compile.Program, recognizer func() (*compile.Program, error), err error)
