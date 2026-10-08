// Package pego is the public API of the PEGO parser framework.
//
// A grammar is written in the PEGO grammar language (see ParseGrammar), built as an AST with the grammar
// package, or decoded from JSON. Compile turns it into a Parser, and Parser.Parse parses input with it.
package pego

import (
	"fmt"
	"io"
	"slices"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// Node is a node of a parse result. Nodes can be encoded to JSON with encoding/json.
type Node = engine.Node

// SyntaxError reports that the input does not match the grammar.
type SyntaxError = engine.SyntaxError

// SyntaxErrors lists the syntax errors recovered from with #recover.
type SyntaxErrors = engine.SyntaxErrors

// ParseGrammar parses PEGO source code into a grammar AST.
func ParseGrammar(src string) (*grammar.Grammar, error) {
	return syntax.Parse(src)
}

// Parser is a compiled grammar. It is safe for concurrent use by multiple goroutines.
type Parser struct {
	prog  *engine.Program
	start string
}

// Compile compiles the grammar g into a parser that starts parsing at the rule start.
func Compile(g *grammar.Grammar, start string) (*Parser, error) {
	prog, err := engine.Compile(g, engine.Options{})
	if err != nil {
		return nil, err
	}
	if !slices.Contains(prog.Rules(), start) {
		return nil, fmt.Errorf("start rule %s is not defined", start)
	}
	return &Parser{prog: prog, start: start}, nil
}

// CompileSource compiles PEGO source code into a parser that starts parsing at the rule start.
func CompileSource(src, start string) (*Parser, error) {
	g, err := ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return Compile(g, start)
}

// Unit is the unit of input positions: node Start and End, startPos and endPos in actions, syntax error
// positions and columns, and the len builtin.
type Unit = engine.Unit

const (
	// CodePoints counts Unicode code points. It is the default.
	CodePoints = engine.CodePoints
	// Bytes counts UTF-8 bytes.
	Bytes = engine.Bytes
)

// ParseOption configures a single parse.
type ParseOption func(*engine.ParseOptions)

// WithUnit selects the position unit. Matching always proceeds by code point, whatever the unit.
func WithUnit(u Unit) ParseOption {
	return func(o *engine.ParseOptions) { o.Unit = u }
}

// Backend selects how a grammar is executed. All backends produce identical results.
type Backend = engine.Backend

const (
	// DefaultBackend uses Closure when the grammar AST is available and Bytecode otherwise.
	DefaultBackend = engine.Default
	// Closure compiles parsing expressions into Go closures.
	Closure = engine.Closure
	// Bytecode runs language-independent bytecode (see docs/bytecode.md) on a VM that uses Go
	// recursion for rule calls.
	Bytecode = engine.Bytecode
	// BytecodeIterative runs the bytecode on a VM that keeps rule calls on its own stack, so deeply
	// nested input does not consume the Go stack.
	BytecodeIterative = engine.BytecodeIterative
)

// WithBackend selects the backend.
func WithBackend(b Backend) ParseOption {
	return func(o *engine.ParseOptions) { o.Backend = b }
}

// RecognizeOnly checks whether the input matches the grammar without building a tree. Parse then returns a
// nil node and the same syntax errors as a full parse. Actions are not evaluated, so runtime errors in
// actions are not reported; captures that predicates read are still built. Recognition is faster and
// allocates less than a full parse. It cannot be used with ParseStream or Document.
func RecognizeOnly() ParseOption {
	return func(o *engine.ParseOptions) { o.Recognize = true }
}

// WithMaxDepth limits the nesting of rule calls to n; deeper nesting makes the parse fail with an error.
// The default is 100,000 (10,000,000 for BytecodeIterative). The limit keeps runaway recursion from
// exhausting the stack or memory. The Closure and Bytecode backends nest rule calls on the goroutine
// stack, so raising the limit far above the default can exceed Go's maximum stack size, which aborts
// the program; use BytecodeIterative for deeper nesting. A call answered from the memo does not nest,
// so whether a parse that comes close to the limit succeeds can depend on memoization.
func WithMaxDepth(n int) ParseOption {
	return func(o *engine.ParseOptions) { o.MaxDepth = n }
}

func parseOptions(opts []ParseOption) engine.ParseOptions {
	var o engine.ParseOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Parse parses the whole input and returns the value of the start rule.
// If the input does not match, it returns a *SyntaxError. If the parse recovered from errors with
// #recover, it returns both the node and the SyntaxErrors.
func (p *Parser) Parse(input string, opts ...ParseOption) (*Node, error) {
	return p.prog.ParseWith(p.start, input, parseOptions(opts))
}

// ParseStream parses input read from r and passes each element of the #stream repetition at the top level
// of the start rule to emit as soon as it matches. Emitted elements and the input before them are not
// retained, so the input never has to fit in memory. If emit returns an error, parsing stops and
// ParseStream returns that error. An element without a value (for example, one that is discarded with -)
// is passed as nil. As in any repetition, an element that matches without consuming input ends it.
func (p *Parser) ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error {
	return p.prog.ParseStreamWith(p.start, r, emit, parseOptions(opts))
}

// Document is a text that is edited repeatedly. Parsing after an edit reuses the results of the previous
// parse that the edit did not affect (incremental parsing), as needed by editors. A Document is not safe
// for concurrent use.
type Document struct {
	doc *engine.Document
}

// ParseStats counts rule evaluations and reused results in a Document.Parse.
type ParseStats = engine.Stats

// NewDocument creates a Document for text. opts select the position unit, which Edit also uses, and the
// backend. It returns an error if the backend is unavailable (Closure on a parser saved without the AST).
func (p *Parser) NewDocument(text string, opts ...ParseOption) (*Document, error) {
	d, err := p.prog.NewDocumentWith(p.start, text, parseOptions(opts))
	if err != nil {
		return nil, err
	}
	return &Document{doc: d}, nil
}

// Text returns the current text.
func (d *Document) Text() string { return d.doc.Text() }

// Edit replaces the range [start, end) of the text, in the document's position unit, with text.
// With byte positions, start and end must be on character boundaries.
func (d *Document) Edit(start, end int, text string) error { return d.doc.Edit(start, end, text) }

// Parse parses the current text. Its results and errors are those of Parser.Parse.
func (d *Document) Parse() (*Node, error) { return d.doc.Parse() }

// Stats returns the evaluation and reuse counts of the last Parse.
func (d *Document) Stats() ParseStats { return d.doc.Stats() }

// GenerateGo generates the source code of a standalone Go parser for g in package pkg. The generated code
// depends only on the standard library; its Parse(input) parses from the rule start. It does not support
// stream or incremental parsing.
func GenerateGo(g *grammar.Grammar, pkg, start string) ([]byte, error) {
	return engine.Generate(g, engine.GenOptions{Package: pkg, Start: start})
}

// MarshalBinary encodes the parser (bytecode, grammar AST and start rule) in the .pegoc format.
// LoadParser restores it without parsing, analyzing, type checking or compiling the grammar again.
func (p *Parser) MarshalBinary() ([]byte, error) {
	return p.prog.MarshalBinary(p.start)
}

// MarshalOption configures Marshal.
type MarshalOption func(*engine.MarshalOptions)

// WithoutAST omits the grammar AST. The file becomes smaller and holds only the bytecode needed to run,
// but a parser loaded from it can only use the bytecode backends, and its Grammar method returns nil.
func WithoutAST() MarshalOption {
	return func(o *engine.MarshalOptions) { o.OmitAST = true }
}

// Marshal encodes the parser like MarshalBinary, with options.
func (p *Parser) Marshal(opts ...MarshalOption) ([]byte, error) {
	var o engine.MarshalOptions
	for _, opt := range opts {
		opt(&o)
	}
	return p.prog.MarshalBinaryWith(p.start, o)
}

// IsCompiled reports whether data is a compiled grammar written by MarshalBinary or Marshal.
func IsCompiled(data []byte) bool { return engine.IsCompiled(data) }

// LoadParser loads a parser saved by MarshalBinary or Marshal. Corruption is detected by a checksum and
// structural validation, but types are not checked again: load only data from trusted sources, such as
// the output of pego compile or MarshalBinary.
func LoadParser(data []byte) (*Parser, error) {
	prog, start, err := engine.LoadProgram(data, engine.Options{})
	if err != nil {
		return nil, err
	}
	if start == "" {
		return nil, fmt.Errorf("compiled grammar has no start rule")
	}
	return &Parser{prog: prog, start: start}, nil
}

// WithStart returns a parser that starts at the rule name. The compiled grammar is shared.
func (p *Parser) WithStart(name string) (*Parser, error) {
	if !slices.Contains(p.prog.Rules(), name) {
		return nil, fmt.Errorf("start rule %s is not defined", name)
	}
	return &Parser{prog: p.prog, start: name}, nil
}

// Start returns the name of the parser's start rule.
func (p *Parser) Start() string { return p.start }

// Grammar returns the parser's grammar. The grammar of a loaded parser has no source positions, and it is
// nil for a parser saved without the AST.
func (p *Parser) Grammar() *grammar.Grammar { return p.prog.Grammar }
