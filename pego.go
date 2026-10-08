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

// TraceEvent describes the start (TraceEnter) or the end (TraceExit) of a rule call. See WithTrace.
type TraceEvent = engine.TraceEvent

// TraceKind tells whether a TraceEvent starts or ends a rule call.
type TraceKind = engine.TraceKind

const (
	// TraceEnter is reported when a rule is called, before the memo is consulted.
	TraceEnter = engine.TraceEnter
	// TraceExit is reported when a rule call returns.
	TraceExit = engine.TraceExit
)

// WithTrace calls f at the start and at the end of every rule call of the parse, in order, so that the
// events nest like the calls (the depth of an event is in TraceEvent.Depth). It reports the calls the
// backend makes: a call skipped because the next character cannot start the rule (first-character
// dispatch) is not reported, and the backends do not skip the same calls. Tracing does not change the
// result, but it makes the parse several times slower. The event's methods (LineCol, Text, Failure) read
// the parser's state, so call them from f; its fields can be kept. With several WithTrace options, each
// function is called in order.
//
// Tracing works with every backend, with Parse, RecognizeOnly, ParseStream and Document (whose
// option applies to every Document.Parse), but not with generated Go parsers.
func WithTrace(f func(TraceEvent)) ParseOption {
	return func(o *engine.ParseOptions) {
		if prev := o.Trace; prev != nil {
			o.Trace = func(e TraceEvent) {
				prev(e)
				f(e)
			}
			return
		}
		o.Trace = f
	}
}

// Profile accumulates the cost of each rule over the parses given WithProfile: calls, body evaluations,
// memo hits, matches and failures, the input consumed and the input examined by failed calls, repeated
// evaluations at a position, and time. Hints summarizes where to look. A Profile is not safe for
// concurrent use.
type Profile = engine.Profile

// RuleProfile is the cost of one rule in a Profile.
type RuleProfile = engine.RuleProfile

// Location is a position with its line and column.
type Location = engine.Location

// WithProfile adds the cost of the parse to prof. It traces the parse (see WithTrace), so the parse is
// several times slower, and the times in prof are meaningful relative to each other only.
func WithProfile(prof *Profile) ParseOption {
	return WithTrace(prof.Trace)
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
//
// Reused results are shared with the trees returned by earlier parses, and the nodes after an edit are
// moved to their new positions in place: an earlier tree changes when Parse runs again, and is then a
// mix of nodes at new positions (those reused) and at old ones. Parse writes it, so it must not be read
// concurrently with Parse. Use Node.Clone to keep a tree as it was.
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

// GenOption configures code generation.
type GenOption func(*engine.GenOptions)

// WithTypes also generates a Go type for each type of the grammar and a function ParseAST, which
// returns the result of the start rule as values of those types instead of *Node.
func WithTypes() GenOption {
	return func(o *engine.GenOptions) { o.Types = true }
}

// WithRecognize also generates a function Recognize, which checks the input against the start rule
// without building a tree, like Parse with RecognizeOnly.
func WithRecognize() GenOption {
	return func(o *engine.GenOptions) { o.Recognize = true }
}

// GenerateGo generates the source code of a standalone Go parser for g in package pkg. The generated code
// depends only on the standard library; its Parse(input) parses from the rule start. It does not support
// stream or incremental parsing.
func GenerateGo(g *grammar.Grammar, pkg, start string, opts ...GenOption) ([]byte, error) {
	o := engine.GenOptions{Package: pkg, Start: start}
	for _, opt := range opts {
		opt(&o)
	}
	return engine.Generate(g, o)
}

// GenerateTypeScript generates the source code of a standalone TypeScript parser for g: a single ES
// module without dependencies, whose parse(input) parses from the rule start and returns the same
// trees and errors as the engine. WithRecognize also generates recognize; WithTypes is not
// supported. Like GenerateGo, it does not support stream or incremental parsing.
func GenerateTypeScript(g *grammar.Grammar, start string, opts ...GenOption) ([]byte, error) {
	o := engine.GenOptions{Start: start}
	for _, opt := range opts {
		opt(&o)
	}
	return engine.GenerateTS(g, o)
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
