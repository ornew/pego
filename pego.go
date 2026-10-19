// Package pego is the public API of the PEGO parser framework.
//
// A grammar is written in the PEGO grammar language (see ParseGrammar), built as an AST with the grammar
// package, or decoded from JSON. Compile turns it into a Parser, and Parser.Parse parses input with it.
package pego

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// Node is a value of a parse result: a node of the input with a range and a string, tuple or list
// value, an error value of recovery, or a value built by an action. It is opaque; its methods
// read it (Kind tells which of them apply). Nodes can be encoded to JSON with encoding/json.
type Node = engine.Node

// NodeKind is what a Node stands for (Node.Kind).
type NodeKind = engine.Kind

// The kinds of nodes.
const (
	KindString     = engine.KindString     // a node whose value is a string: Text
	KindTuple      = engine.KindTuple      // a node whose value is a tuple: Len and Elem
	KindList       = engine.KindList       // a node whose value is a list: Len and Elem
	KindError      = engine.KindError      // an error value of recovery: Text is its message
	KindStruct     = engine.KindStruct     // a struct built by an action: Type and Field
	KindTupleValue = engine.KindTupleValue // a tuple built by an action, without a range
	KindListValue  = engine.KindListValue  // a list built by an action, without a range
	KindValue      = engine.KindValue      // another value built by an action: Value
)

// SyntaxError reports that the input does not match the grammar.
type SyntaxError = engine.SyntaxError

// MissingTokenLimitError reports exhaustion of a #missing insertion budget.
type MissingTokenLimitError = engine.MissingTokenLimitError

// SyntaxErrors lists the syntax errors recovered with #recover or #missing.
type SyntaxErrors = engine.SyntaxErrors

// TruncatedSyntaxErrors reports a bounded tail of recovered stream errors.
// Total and Dropped are lower bounds when CountsSaturated is true.
type TruncatedSyntaxErrors = engine.TruncatedSyntaxErrors

// ParseGrammar parses PEGO source code into a grammar AST.
func ParseGrammar(src string) (*grammar.Grammar, error) {
	return syntax.Parse(src)
}

// Parser is a compiled grammar. It is safe for concurrent use by multiple goroutines.
type Parser struct {
	prog  *engine.Program
	start string
}

// CompileOption configures compilation.
type CompileOption func(*engine.Options)

// WithSourceInfo records source information in parse results: the rule that produced each node
// and where its captures are. Node.Rule, Node.Field, the rule and capture names of JSON and
// S-expression output, and rule constraints in queries need it. It costs time and memory, so
// parsers omit it by default.
func WithSourceInfo() CompileOption {
	return func(o *engine.Options) { o.SourceInfo = true }
}

// WithDefaultValue makes every rule produce the default value of its expression. Value actions
// (=>) and the declared types of rules are dropped, so the parser returns the default node tree
// even for grammars with actions, and nothing is checked against the types the actions would
// have made. Predicates still run, because they decide the match, and the rules that
// make the values predicates read keep their actions and types. It is a property of the
// parser: a compiled grammar written from it keeps it.
func WithDefaultValue() CompileOption {
	return func(o *engine.Options) { o.DefaultValue = true }
}

// Compile compiles the grammar g into a parser that starts parsing at the rule start.
func Compile(g *grammar.Grammar, start string, opts ...CompileOption) (*Parser, error) {
	var o engine.Options
	for _, opt := range opts {
		opt(&o)
	}
	prog, err := engine.Compile(g, o)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(prog.Rules(), start) {
		return nil, fmt.Errorf("start rule %s is not defined", start)
	}
	return &Parser{prog: prog, start: start}, nil
}

// DefaultValue reports whether the parser produces default values (WithDefaultValue).
func (p *Parser) DefaultValue() bool { return p.prog.DefaultValue() }

// SourceInfo reports whether the parser records source information (WithSourceInfo).
func (p *Parser) SourceInfo() bool { return p.prog.SourceInfo() }

// CompileSource compiles PEGO source code into a parser that starts parsing at the rule start.
func CompileSource(src, start string, opts ...CompileOption) (*Parser, error) {
	g, err := ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return Compile(g, start, opts...)
}

// PositionUnit is the unit of input positions: node Start and End (also in actions), syntax error
// positions and columns, and the len builtin.
type PositionUnit = engine.PositionUnit

const (
	// CodePoints counts Unicode code points. It is the default.
	CodePoints = engine.CodePoints
	// Bytes counts UTF-8 bytes.
	Bytes = engine.Bytes
)

// ParseOption configures a single parse.
type ParseOption func(*engine.ParseOptions)

// WorkLimits configures cumulative engine work for a parse invocation.
// Zero limits are unlimited. Counts depend on backend, input unit and version.
type WorkLimits = engine.WorkLimits

// WorkLimitError describes a reservation that would exceed a work limit.
type WorkLimitError = engine.WorkLimitError

// WorkLimitKind identifies the exhausted counter.
type WorkLimitKind = engine.WorkLimitKind

const (
	InputWorkLimit = engine.InputWorkLimit
	StepWorkLimit  = engine.StepWorkLimit
)

// ErrWorkLimit identifies engine work-limit failures through errors.Is.
var ErrWorkLimit = engine.ErrWorkLimit

// WithContext selects invocation-local cooperative cancellation. Nil disables it.
func WithContext(ctx context.Context) ParseOption {
	return func(o *engine.ParseOptions) { o.Context = ctx }
}

// WithWorkLimits sets both invocation-local work limits. Zero means unlimited.
func WithWorkLimits(limits WorkLimits) ParseOption {
	return func(o *engine.ParseOptions) { o.WorkLimits = limits }
}

// WithMissingTokens bounds attempted #missing literal insertions in one parse.
// Zero disables insertion; negative limits are invalid. Only Closure whole-input
// parsing and recognition support insertion. Positive limits are invalid with
// ParsePrefix, even without #missing in the grammar. ParsePrefix also rejects
// grammars containing #missing when insertion is disabled. Options resolve in order.
func WithMissingTokens(limit int) ParseOption {
	return func(o *engine.ParseOptions) { o.MissingTokens = limit }
}

// WithUnit selects the position unit. Matching always proceeds by code point, whatever the unit.
func WithUnit(u PositionUnit) ParseOption {
	return func(o *engine.ParseOptions) { o.Unit = u }
}

// Backend selects how a grammar is executed. All backends produce identical results.
type Backend = engine.Backend

const (
	// DefaultBackend uses Closure when the grammar AST is available and Bytecode otherwise.
	DefaultBackend = engine.Default
	// Closure compiles parsing expressions into Go closures.
	Closure = engine.Closure
	// Bytecode runs language-independent bytecode (see spec/bytecode.md) on a VM that uses Go
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
// The operand of a prefix operator and the right operand of an infix operator of an Operators Rule expression
// nest like rule calls.
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
// function is called in order. A nil f is ignored.
//
// Tracing works with every backend, with Parse, ParsePrefix, RecognizeOnly, ParseStream and Document (whose
// option applies to every Document.Parse), but not with generated Go parsers.
func WithTrace(f func(TraceEvent)) ParseOption {
	return func(o *engine.ParseOptions) {
		if f == nil {
			return
		}
		if prev := o.Trace; prev != nil {
			o.Trace = engine.ComposeTrace(prev, f)
			return
		}
		o.Trace = f
	}
}

// WithDiagnosticCallback reports each committed #recover error during ParseStream or Iterate,
// immediately before the corresponding stream element. Speculative recoveries are not reported.
// The callback receives an independent copy; returning an error stops parsing and suppresses
// that element. A nil callback disables delivery. Non-nil callbacks are invalid with Parse,
// ParsePrefix and Document. Recoveries in a successful suffix are reported before the terminal result. With or
// without this option, recovered errors remain available at stream completion.
func WithDiagnosticCallback(f func(*SyntaxError) error) ParseOption {
	return func(o *engine.ParseOptions) { o.DiagnosticCallback = f }
}

// WithDiagnosticLimit retains at most n committed stream recoveries, keeping the latest errors.
// Zero retains every error, as in an unconfigured stream parse. Negative values are invalid.
// This option is valid only with ParseStream and Iterate, including when n is zero.
// When errors are dropped, the terminal error is *TruncatedSyntaxErrors.
func WithDiagnosticLimit(n int) ParseOption {
	return func(o *engine.ParseOptions) {
		o.DiagnosticLimit = n
		o.DiagnosticLimitSet = true
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
// several times slower, and the times in prof are meaningful relative to each other only. A nil prof is
// ignored.
func WithProfile(prof *Profile) ParseOption {
	if prof == nil {
		return WithTrace(nil)
	}
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
// WithContext and WithWorkLimits apply to this invocation. Cancellation cannot interrupt a blocked
// reader or callback; the caller must arrange for it to return. A returned reader or emit error wins
// over cancellation first observed after that call. Delivered elements remain caller-owned, and
// retrying an interrupted stream requires caller-managed input replay.
// WithDiagnosticCallback delivers committed recoveries immediately before their elements; its
// error stops parsing before that element is emitted. Recovered errors are still returned at EOF.
// WithDiagnosticLimit bounds the retained recovery tail and explicitly marks a truncated result.
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

// DocumentVersion identifies one text revision of a Document. Its zero value
// is invalid; tokens are comparable and do not retain document contents.
type DocumentVersion = engine.DocumentVersion

// TextEdit replaces [Start, End) in the document's position unit. EditBatch
// interprets every range in the supplied base version's coordinates.
type TextEdit = engine.TextEdit

// Version returns the current text revision, initially sequence one.
func (d *Document) Version() DocumentVersion { return d.doc.Version() }

// Unit returns the position unit selected when the document was constructed.
func (d *Document) Unit() PositionUnit { return d.doc.Unit() }

// EditBatch atomically applies ordered, nonoverlapping edits in base coordinates.
// It rejects stale or foreign tokens, repeated starts, invalid ranges and byte
// boundaries. An empty batch validates base but changes nothing. A nonempty
// batch advances one revision and clears parse caches without parsing.
func (d *Document) EditBatch(base DocumentVersion, edits ...TextEdit) error {
	return d.doc.EditBatch(base, edits...)
}

// Snapshot is an immutable document parse outcome.
type Snapshot = engine.Snapshot

// SnapshotNode is a comparable read-only node handle; its zero value is nil.
type SnapshotNode = engine.SnapshotNode

// SnapshotField stores a read-only node handle or immutable scalar.
type SnapshotField = engine.SnapshotField

// SnapshotCaptureError reports unsupported mutable or custom field values.
type SnapshotCaptureError = engine.SnapshotCaptureError

// Snapshot parses and freezes this revision using construction defaults plus
// opts. Cancellation and work limits cover parsing, text, tree and diagnostics;
// interruption returns nil. Ordinary syntax errors retain the failed version.
func (d *Document) Snapshot(opts ...ParseOption) (*Snapshot, error) {
	o := d.doc.ParseDefaults()
	for _, opt := range opts {
		opt(&o)
	}
	return d.doc.SnapshotWith(o)
}

// ParseStats counts rule evaluations and reused results in a Document.Parse.
type ParseStats = engine.Stats

// NewDocument creates a Document for text. opts select its fixed unit and backend
// and the defaults for later Parse calls. Construction and each Parse use fresh
// work counters. Edit uses the same unit and is outside parse work controls.
// It returns an error if the backend is unavailable (Closure without the AST).
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
// Each invalid UTF-8 byte is a separate character; edits may join bytes into
// a valid UTF-8 character.
func (d *Document) Edit(start, end int, text string) error { return d.doc.Edit(start, end, text) }

// Parse parses the current text. opts apply once over construction defaults and
// may change context, work limits, depth and trace, but not unit, effective backend
// or recognition mode. Overrides do not change future defaults. Pre-execution
// interruption preserves caches and statistics; execution or publication
// interruption discards reusable state while preserving text for retry.
// Parser-produced syntax diagnostics belong to that call: mutating them does
// not affect later parses. Returned Nodes may share the mutable Document graph.
func (d *Document) Parse(opts ...ParseOption) (*Node, error) {
	o := d.doc.ParseDefaults()
	for _, opt := range opts {
		opt(&o)
	}
	return d.doc.ParseWith(o)
}

// Stats returns the evaluation and reuse counts of the last Parse.
func (d *Document) Stats() ParseStats { return d.doc.Stats() }

// GenOption configures code generation.
type GenOption func(*engine.GenOptions)

// WithTypes generates declarations for the grammar's types and typed results.
// Go adds ParseAST for the start rule and ParseRuleAST for named rules.
// TypeScript adds parseAST and parseASTWithOptions, converting the completed
// Node result into plain objects and arrays. Declared aliases preserve the target value type.
// Names colliding with runtime declarations gain underscores.
func WithTypes() GenOption {
	return func(o *engine.GenOptions) { o.Types = true }
}

// WithGeneratedDefaultValue makes the generated parser produce default values, as WithDefaultValue
// does for a compiled parser. It cannot be combined with WithTypes.
func WithGeneratedDefaultValue() GenOption {
	return func(o *engine.GenOptions) { o.DefaultValue = true }
}

// WithGeneratedSourceInfo makes the generated parser record source information, as WithSourceInfo
// does for a compiled parser.
func WithGeneratedSourceInfo() GenOption {
	return func(o *engine.GenOptions) { o.SourceInfo = true }
}

// WithDocument generates a mutable incremental Document for Go. Combined with
// WithTypes, it also generates Document.ParseAST with direct incremental matching
// for eligible grammars and Node conversion otherwise. TypeScript generation does
// not support Documents.
func WithDocument() GenOption {
	return func(o *engine.GenOptions) { o.Document = true }
}

// WithStream generates ParseStream for Go, which reads the input from an
// io.Reader and passes each committed element of the start rule's #stream
// repetition to a callback, as Parser.ParseStream does. The start rule must
// have a #stream repetition. With WithTypes it also generates ParseStreamAST
// and IterateAST, which pass the typed value of each element. TypeScript
// generation does not support it.
func WithStream() GenOption {
	return func(o *engine.GenOptions) { o.Stream = true }
}

// WithGeneratedObservability emits rule tracing and profiling for Go batch
// parsers and Documents, and for TypeScript batch parsers. Both include typed
// entry points when WithTypes is enabled. Collection is selected per invocation.
func WithGeneratedObservability() GenOption {
	return func(o *engine.GenOptions) { o.Observability = true }
}

// WithRecognize also generates a function Recognize, which checks the input against the start rule
// without building a tree, like Parse with RecognizeOnly.
func WithRecognize() GenOption {
	return func(o *engine.GenOptions) { o.Recognize = true }
}

// WithGeneratedMaxDepth sets the generated parser's default nested rule-call limit.
// Zero selects 100,000; negative values fail generation. TypeScript additionally
// requires an exact JavaScript integer. Individual invocations may override it.
func WithGeneratedMaxDepth(n int) GenOption {
	return func(o *engine.GenOptions) { o.MaxDepth = n }
}

// WithoutPackageDoc leaves the package comment out of a generated Go parser, for a package that has
// its own documentation in another file.
func WithoutPackageDoc() GenOption {
	return func(o *engine.GenOptions) { o.OmitPackageDoc = true }
}

// GenerateGo generates the source code of a standalone Go parser for g in package pkg. The generated code
// depends only on the standard library; its Parse(input) parses from the rule start.
// WithDocument adds incremental Node parsing; combined with WithTypes it also
// generates typed Document.ParseAST. WithStream adds Node stream parsing.
func GenerateGo(g *grammar.Grammar, pkg, start string, opts ...GenOption) ([]byte, error) {
	o := engine.GenOptions{Package: pkg, Start: start}
	for _, opt := range opts {
		opt(&o)
	}
	return engine.Generate(g, o)
}

// GenerateTypeScript generates the source code of a standalone TypeScript parser for g: a single ES
// module without dependencies, whose parse(input) parses from the rule start and returns the same
// trees and errors as the engine. WithRecognize also generates recognize; WithTypes adds typed
// declarations and configured-start parseAST/parseASTWithOptions. It does not
// support stream or incremental parsing.
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
