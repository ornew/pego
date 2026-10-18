package engine

import (
	_ "embed"
	"fmt"
	"go/format"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

//go:embed genrt/runtime.go
var runtimeSource string

//go:embed genrt/work.go
var workSource string

//go:embed genrt/typed.go
var typedSource string

//go:embed genrt/document.go
var documentSource string

//go:embed genrt/stream.go
var streamSource string

//go:embed genrt/fieldcheck.go
var fieldCheckSource string

//go:embed genrt/trace.go
var generatedTraceSource string

//go:embed genrt/profile.go
var generatedProfileSource string

// GenOptions configures code generation.
type GenOptions struct {
	// Observability emits opt-in tracing and profiling for generated Go batch
	// and Document parsing, and for TypeScript batch parsing.
	Observability bool
	// Document includes the generated Go Document runner. With Types, eligible
	// outputs use private typed memoization; other outputs use Node conversion.
	Document bool
	// disableDirectTypedDocument retains Node conversion for differential tests.
	disableDirectTypedDocument bool
	// Stream generates ParseStream, which reads the input from an io.Reader
	// and passes the committed elements of the start rule's #stream
	// repetition to a callback. The start rule must have one.
	Stream bool
	// referenceWorkControls emits a generation-time disabled control for tests.
	referenceWorkControls bool
	// referenceWorkControlLayout removes added invocation state and the new
	// control options from a private ordinary-call cost reference.
	referenceWorkControlLayout bool
	// disableOrdinaryWorkSpecialization retains charged-method nil checks for
	// same-generator tests and measurements without a new parser-time selector.
	disableOrdinaryWorkSpecialization bool
	// SourceInfo records source information in the values: the rule that produced each node and
	// where its captures are (Options.SourceInfo).
	SourceInfo bool
	// Package is the package name of the generated code.
	Package string
	// MaxDepth is the generated default rule-call limit. Zero selects 100,000.
	MaxDepth int
	// Start is the start rule of the generated Parse function.
	Start string
	// Types generates Go types, ParseAST for the start rule, and ParseRuleAST
	// for a named rule as values of those types (see gen_types.go).
	Types bool
	// Recognize also generates Recognize, which checks the input without building a tree (as the
	// engine's ParseOptions.Recognize does).
	Recognize bool
	// OmitPackageDoc leaves out the package comment, for a package that documents itself in another
	// file.
	OmitPackageDoc bool
	// convertTypes makes typed entry points convert Node results even when the
	// typed runtime could build them (for tests).
	convertTypes bool
	// disableTypedCuts keeps the general typed cut route for differential tests
	// and latest-generator performance controls. It adds no parser-time branch.
	disableTypedCuts bool
	// disableTypedLRBodies keeps per-expression bodies under the unchanged
	// left-recursion runtime, for same-revision tests and measurements.
	disableTypedLRBodies bool
	// disableTypedOperatorsRuleBodies retains per-expression Operators Rule line dispatch for
	// same-generator tests and measurements without a parser-time option.
	disableTypedOperatorsRuleBodies bool
	// disableTypedFramedCuts retains general cut-bearing LR and OperatorsRule bodies
	// for same-generator controls without a matching-time selector.
	disableTypedFramedCuts bool
	// disableTypedFrameLayouts retains structural capture discovery for
	// same-generator tests and measurements without a parser-time option.
	disableTypedFrameLayouts bool
	// disableTypedLocalLayouts retains structural discovery of Go-local captures
	// for same-generator controls without a matching-time selector.
	disableTypedLocalLayouts bool
	// disableTypedRecoveryBodies retains general recovery dispatch for
	// same-generator tests and measurements without a parser-time option.
	disableTypedRecoveryBodies bool
	// disableMethodSharing and disableLiteralSharing retain duplicate generated
	// helpers and read-only tables for independent generation-time controls.
	// Neither adds a selector to the generated parser.
	disableMethodSharing  bool
	disableLiteralSharing bool
	// enableTSLiteralSharing retains a generation-only experiment. Sharing
	// literal tables can hurt TypeScript warm-up; production shares helpers
	// only. An explicit disableLiteralSharing takes precedence.
	enableTSLiteralSharing bool
	// disableLiteralTextDispatch keeps literal text disjunctions on the general
	// expression path for differential tests and same-generator measurements.
	disableLiteralTextDispatch bool
	// disableNodeFrameScratch retains per-parse Node capture frames for
	// same-generator controls without a parser-time option.
	disableNodeFrameScratch bool
	// Independent generation-only controls for typed object and list slabs.
	// Reference parsers allocate each object or list separately.
	disableTypedObjectSlabs bool
	disableTypedListSlabs   bool
	// disableTypedOutputBootstrap retains the original slab first allocation
	// for same-generator retention and throughput comparisons.
	disableTypedOutputBootstrap bool
	// disableSparseMemo keeps the dense first-call bitset for same-generator
	// comparisons without a parser-time selector.
	disableSparseMemo bool
	// enableTypedOutputSweep selects a generation-only liveness prototype.
	// Default output retains the original allocation and publication paths.
	enableTypedOutputSweep bool
	// enableBoundedTypedOutputSweep selects a separate generation-only control.
	// It takes precedence when both private sweep controls are selected.
	enableBoundedTypedOutputSweep bool
}

// Generate generates the source code of a Go parser for a grammar.
//
// The generated code depends only on the standard library. Eligible rule bodies inline their expressions;
// other expressions become Go methods. Actions and predicates become Go expressions, and the embedded
// runtime (genrt) behaves exactly like the engine
// in this package. Without opts.Stream, #stream is a plain repetition.
func Generate(g *grammar.Grammar, opts GenOptions) ([]byte, error) {
	if err := opts.validateMaxDepth(false); err != nil {
		return nil, err
	}
	prog, err := Compile(g, Options{SourceInfo: opts.SourceInfo})
	if err != nil {
		return nil, err
	}
	if prog.hasMissing {
		return nil, missingUnsupported("generated parsers")
	}
	start := prog.byName[opts.Start]
	if start == nil {
		return nil, fmt.Errorf("start rule %s is not defined", opts.Start)
	}
	if opts.Package == "" {
		return nil, fmt.Errorf("package name is required")
	}
	if !token.IsIdentifier(opts.Package) || opts.Package == "_" {
		return nil, fmt.Errorf("invalid package name %q", opts.Package)
	}
	if opts.Stream {
		if !start.stream {
			return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", start.name)
		}
	}
	gen := &generator{observability: opts.Observability, disableDirectTypedDocument: opts.disableDirectTypedDocument, document: opts.Document, goWorkControls: !(opts.referenceWorkControls || opts.referenceWorkControlLayout), prog: prog, table: "rules", disableTypedCuts: opts.disableTypedCuts, disableTypedLRBodies: opts.disableTypedLRBodies, disableTypedOperatorsRuleBodies: opts.disableTypedOperatorsRuleBodies, disableTypedFramedCuts: opts.disableTypedFramedCuts, disableTypedFrameLayouts: opts.disableTypedFrameLayouts, disableTypedLocalLayouts: opts.disableTypedLocalLayouts, disableTypedRecoveryBodies: opts.disableTypedRecoveryBodies, disableMethodSharing: opts.disableMethodSharing, disableLiteralSharing: opts.disableLiteralSharing, disableLiteralTextDispatch: opts.disableLiteralTextDispatch, disableTypedObjectSlabs: opts.disableTypedObjectSlabs, disableTypedListSlabs: opts.disableTypedListSlabs, disableTypedOutputBootstrap: opts.disableTypedOutputBootstrap, enableTypedOutputSweep: opts.enableTypedOutputSweep, enableBoundedTypedOutputSweep: opts.enableBoundedTypedOutputSweep}
	if opts.Stream {
		gen.streamStart = start
	}
	gen.desc(fixedDescs[0]) // the fixed expectations come first
	gen.rules()
	if gen.streamErr != nil {
		return nil, gen.streamErr
	}
	var rec *Program
	if opts.Recognize {
		// The recognizer: the program in which no rule builds a value (Program.recognizer), in a
		// rule table of its own.
		if rec, err = prog.recognizer(); err != nil {
			return nil, err
		}
		gen.prog, gen.table = rec, "recRules"
		gen.rules()
		gen.prog, gen.table = prog, "rules"
	}
	fmt.Fprintf(&gen.init, "\tdescs = %s\n", goStrings(gen.descs))
	if prog.sourceInfo {
		gen.init.WriteString("\tsourceInfo = true\n")
	}

	var out strings.Builder
	out.WriteString("// Code generated by pego. DO NOT EDIT.\n\n")
	if !opts.OmitPackageDoc {
		fmt.Fprintf(&out, "// Package %s is a parser generated from a PEGO grammar.\n", opts.Package)
	}
	rt := runtimeSource[strings.Index(runtimeSource, "package genrt"):]
	rt = strings.Replace(rt, "package genrt", "package "+opts.Package, 1)
	// Existing parser packages may define their own typed-AST Walk helper. Keep
	// that API available by naming the generated generic Node walker WalkTree.
	rt = strings.Replace(rt, "func Walk(", "func WalkTree(", 1)
	rt = strings.Replace(rt, "// Walk visits", "// WalkTree visits", 1)
	rt = strings.Replace(rt, "return Walk(c.Root,", "return WalkTree(c.Root,", 1)
	rt = strings.Replace(rt, "const defaultMaxDepth = 100_000", fmt.Sprintf("const defaultMaxDepth = %d", opts.generatedMaxDepth()), 1)
	rt = strings.Replace(rt, "const documentEnabled = true", fmt.Sprintf("const documentEnabled = %t", opts.Document), 1)
	if opts.disableNodeFrameScratch {
		rt = strings.Replace(rt, "const nodeFrameScratch = true", "const nodeFrameScratch = false", 1)
	}

	if !opts.useSparseMemo(prog, rec) {
		rt = strings.Replace(rt, "const sparseSeen = true", "const sparseSeen = false", 1)
	}
	// The standalone output merges the work helper's standard-library imports.
	rt = strings.Replace(rt, "\"context\"", "\"context\"\n\t\"errors\"\n\t\"math\"\n\t\"slices\"", 1)
	if opts.Stream {
		rt = strings.Replace(rt, "\"bytes\"", "\"bufio\"\n\t\"bytes\"", 1)
	}
	if opts.Observability {
		rt = strings.Replace(rt, "\"sync\"", "\"sync\"\n\t\"time\"", 1)
		rt, err = generatedTraceRuntime(rt)
		if err != nil {
			return nil, err
		}
	}
	out.WriteString(rt)
	if opts.Observability {
		out.WriteString("\n// --- Opt-in generated rule tracing ---\n")
		out.WriteString(generatedTraceSource[strings.Index(generatedTraceSource, "package genrt")+len("package genrt"):])
		out.WriteString(generatedProfileSource[strings.Index(generatedProfileSource, "\n)\n")+len("\n)\n"):])
		out.WriteString(generatedTraceWrappers)
	}
	work := workSource[strings.Index(workSource, "\n)\n")+len("\n)\n"):]
	if opts.referenceWorkControls || opts.referenceWorkControlLayout {
		work = strings.Replace(work, "const workControlsEnabled = true", "const workControlsEnabled = false", 1)
	}
	out.WriteString("\n// --- Invocation work controls ---\n")
	out.WriteString(work)
	if opts.Document {
		doc := documentSource[strings.Index(documentSource, "\n)\n")+len("\n)\n"):]
		if opts.Observability {
			doc, err = generatedDocumentTraceRuntime(doc)
			if err != nil {
				return nil, err
			}
		}
		out.WriteString("\n// --- Incremental Node document ---\n")
		out.WriteString(doc)
	}
	if opts.Stream {
		stream := streamSource[strings.Index(streamSource, "\n)\n")+len("\n)\n"):]
		if opts.Observability {
			if stream, err = generatedStreamTraceRuntime(stream); err != nil {
				return nil, err
			}
		}
		out.WriteString("\n// --- Stream parsing ---\n")
		out.WriteString(stream)
	}
	out.WriteString("\n// --- Generated code ---\n\n")
	if gen.goWorkControls {
		fmt.Fprintf(&out, "// Parse parses the whole input with the rule %s. Options select position units, depth, cancellation and work limits.\n", start.name)
	} else {
		fmt.Fprintf(&out, "// Parse parses the whole input with the rule %s. Options select position units and the rule-call limit.\n", start.name)
	}
	out.WriteString("// If the parse recovered from errors with #recover, it returns the node together with SyntaxErrors.\n")
	fmt.Fprintf(&out, "func Parse(input string, opts ...ParseOption) (*Node, error) { return parse(rules[%d], nseen, input, opts) }\n\n", start.id)
	if opts.Stream {
		fmt.Fprintf(&out, "// ParseStream parses the input read from r with the rule %s, passing each committed element of\n", start.name)
		out.WriteString("// its #stream repetition to emit (nil if the element has no value) without retaining it. Committed\n// elements are final even if the parse fails later. A non-EOF error from r or an error returned by\n// emit ends the parse and is returned unchanged. A nil emit collects the repetition instead.\n")
		out.WriteString("func ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error {\n\treturn parseStream(streamRule, nseen, r, emit, opts)\n}\n\n")
		out.WriteString(`// Iterate returns an iterator over the elements that ParseStream would pass to emit, parsing only as
// far as each call to Next needs. Options are validated before it returns; r is first read by Next.
// The parser's lookahead and buffering may read ahead of the returned elements.
func Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error) {
	pull, err := iterateStream(opts, func(emit func(*Node) error) error { return parseStream(streamRule, nseen, r, emit, opts) })
	if err != nil {
		return nil, err
	}
	return &StreamIterator{pull}, nil
}

`)
	}
	if opts.Document {
		fmt.Fprintf(&out, "// NewDocument creates an incremental Node document for the start rule %s.\n", start.name)
		fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) { return makeDocument(rules[%d], text, opts) }\n\n", start.id)
	}
	if rec != nil {
		fmt.Fprintf(&out, "// Recognize checks that the whole input matches the rule %s without building a tree, and returns the\n", start.name)
		out.WriteString("// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover). Actions needed by\n// predicates are evaluated and can report runtime errors; other actions are skipped.\n")
		fmt.Fprintf(&out, "func Recognize(input string, opts ...ParseOption) error {\n\t_, err := parse(recRules[%d], recNseen, input, opts)\n\treturn err\n}\n\n", rec.byName[start.name].id)
		out.WriteString("// recRules is the rule table of Recognize, and recNseen its number of rules with rule.seen set.\nvar recRules []*rule\nvar recNseen int\n\n")
	}
	out.WriteString(`// ParseRule parses the whole input with the rule name.
func ParseRule(name, input string, opts ...ParseOption) (*Node, error) {
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	r := ruleByName(name)
	if r == nil {
		return nil, fmt.Errorf("rule %s is not defined", name)
	}
	return parseResolved(r, nseen, input, o)
}

`)
	if opts.Types {
		typed, span, err := gen.typedGo(start, opts.convertTypes)
		if err != nil {
			return nil, err
		}
		// The typed runtime's imports (fmt and sync) join those of the runtime.
		rt := typedSource[strings.Index(typedSource, "\n)\n")+len("\n)\n"):]
		if gen.directTypedDocument {
			rt, err = typedDocumentRuntime(rt)
			if err != nil {
				return nil, err
			}
		}
		if opts.enableTypedOutputSweep || opts.enableBoundedTypedOutputSweep {
			rt, err = typedOutputSweepRuntime(rt)
			if err != nil {
				return nil, err
			}
		}
		if opts.Observability {
			rt, err = generatedTypedTraceRuntime(rt)
			if err != nil {
				return nil, err
			}
		}
		if span != "Span" {
			rt = regexp.MustCompile(`\bSpan\b`).ReplaceAllString(rt, span)
		}
		out.WriteString("\n// --- Typed runtime ---\n")
		out.WriteString(rt)
		out.WriteString("\n")
		out.WriteString(typed)
		out.WriteString("\n")
	}
	if len(gen.fieldTypes) > 0 {
		checks := strings.TrimPrefix(fieldCheckSource, "package genrt\n")
		if !opts.Types {
			checks = fieldCheckTypedRegion.ReplaceAllString(checks, "")
		}
		out.WriteString("\n// --- Constructed field checks ---\n")
		out.WriteString(fieldCheckMarker.ReplaceAllString(checks, ""))
	}
	out.WriteString(gen.vars.String())
	out.WriteString("\nfunc init() {\n")
	out.WriteString(gen.init.String())
	out.WriteString("}\n\n")
	out.WriteString(gen.methods.String())
	src, err := format.Source([]byte(streamRegions(out.String(), opts.Stream)))
	if err != nil {
		return []byte(out.String()), fmt.Errorf("generated code does not compile: %w", err)
	}
	if opts.referenceWorkControlLayout {
		return removeGeneratedWorkState(src)
	}
	if gen.goWorkControls && !opts.disableOrdinaryWorkSpecialization {
		return specializeOrdinaryMethods(src, gen.methods.String())
	}
	return src, nil
}

var fieldCheckTypedRegion = regexp.MustCompile(`(?ms)^// fieldcheck:typed:begin\n.*?^// fieldcheck:typed:end\n`)
var fieldCheckMarker = regexp.MustCompile(`(?m)^// fieldcheck:typed:(begin|end)\n`)

// checkedField wraps the code of a constructed field's value in its run-time check when the
// value is not statically known to fit the field's declared type (Program.fieldCheckType).
func (g *generator) checkedField(spec *grammar.StructSpec, typ string, fi *grammar.FieldInit, code string) string {
	ft := g.prog.fieldCheckType(spec, fi)
	if ft == nil {
		return code
	}
	v := g.fieldTypeVar(fi, ft, func(v string, t *fieldType) string { return fmt.Sprintf("var %s = %s\n", v, goFieldType(t)) })
	return fmt.Sprintf("c.checkField(%s, %s, %q, %q)", code, v, fi.Name, typ)
}

// resultCheck returns the call that checks the value v of rule r, or of its Operators Rule line key,
// against the rule's declared type, or "" if the value needs no check (Program.resultCheckType).
func (g *generator) resultCheck(r *rule, key any, v string) string {
	if r.novalue {
		return ""
	}
	ft := g.prog.resultCheckType(key, r.def)
	if ft == nil {
		return ""
	}
	t := g.fieldTypeVar(key, ft, func(v string, t *fieldType) string { return fmt.Sprintf("var %s = %s\n", v, goFieldType(t)) })
	if g.table == "trules" {
		return fmt.Sprintf("p.tcheckResult(%s, %s, %q)", v, t, r.name)
	}
	return fmt.Sprintf("p.checkResult(%s, %s, %q)", v, t, r.name)
}

// resultCheckFunc returns the function that checks the values of rule r, or of its Operators Rule
// line key (resultCheck), or "" if they need no check.
func (g *generator) resultCheckFunc(r *rule, key any) string {
	check := g.resultCheck(r, key, "v")
	if check == "" {
		return ""
	}
	if g.table == "trules" {
		return "func(p *tparser, v any) { " + check + " }"
	}
	return "func(p *parser, v *Node) { " + check + " }"
}

// fieldTypeVar returns the variable holding check type ft of key, declaring it with decl the
// first time.
func (g *generator) fieldTypeVar(key any, ft ty, decl func(v string, t *fieldType) string) string {
	v, ok := g.fieldTypes[key]
	if !ok {
		if g.fieldTypes == nil {
			g.fieldTypes = map[any]string{}
		}
		v = fmt.Sprintf("fieldType%d", len(g.fieldTypes)+1)
		g.fieldTypes[key] = v
		g.fieldTypeKeys = append(g.fieldTypeKeys, key)
		g.vars.WriteString(decl(v, newFieldType(ft)))
	}
	return v
}

// goFieldType writes a field type as a generated fieldType value.
func goFieldType(t *fieldType) string {
	var b strings.Builder
	fmt.Fprintf(&b, "&fieldType{kind: %q, text: %q", rune(t.kind), t.text)
	if t.name != "" {
		fmt.Fprintf(&b, ", name: %q", t.name)
	}
	if t.terminal {
		b.WriteString(", terminal: true")
	}
	if t.elem != nil {
		b.WriteString(", elem: " + goFieldType(t.elem))
	}
	if len(t.alts) > 0 {
		alts := make([]string, len(t.alts))
		for i, a := range t.alts {
			alts[i] = goFieldType(a)
		}
		b.WriteString(", alts: []*fieldType{" + strings.Join(alts, ", ") + "}")
	}
	return b.String() + "}"
}

var streamRegion = regexp.MustCompile(`(?ms)^[ \t]*// stream:begin\n.*?^[ \t]*// stream:end\n`)
var streamMarker = regexp.MustCompile(`(?m)^[ \t]*// stream:(begin|end)\n`)

// streamRegions keeps or removes the runtime code between "// stream:begin"
// and "// stream:end" lines, which only stream parsing uses. Without streams,
// the runtime is the same as before stream parsing existed.
func streamRegions(src string, stream bool) string {
	if stream {
		return streamMarker.ReplaceAllString(src, "")
	}
	return streamRegion.ReplaceAllString(src, "")
}

// useSparseMemo removes the representation selector entirely when neither
// emitted rule table can cross the sparse-storage threshold.
func (opts GenOptions) useSparseMemo(prog, rec *Program) bool {
	return !opts.disableSparseMemo && (prog.nseen > 64 || rec != nil && rec.nseen > 64)
}

func (o GenOptions) generatedMaxDepth() int {
	if o.MaxDepth == 0 {
		return DefaultMaxDepth
	}
	return o.MaxDepth
}

func (o GenOptions) validateMaxDepth(ts bool) error {
	if o.MaxDepth < 0 {
		return fmt.Errorf("generated max depth must be non-negative: %d", o.MaxDepth)
	}
	if ts && uint64(o.MaxDepth) > 9007199254740991 {
		return fmt.Errorf("generated max depth must be a safe JavaScript integer: %d", o.MaxDepth)
	}
	return nil
}

type generator struct {
	observability                   bool
	directTypedDocument             bool
	disableDirectTypedDocument      bool
	document                        bool
	goWorkControls                  bool
	workClassNames                  map[string]string
	workClassKeys                   []string
	disableTypedCuts                bool
	disableTypedLRBodies            bool
	disableTypedOperatorsRuleBodies bool
	disableTypedFramedCuts          bool
	disableTypedFrameLayouts        bool
	disableTypedLocalLayouts        bool
	disableTypedRecoveryBodies      bool
	disableMethodSharing            bool
	disableLiteralSharing           bool
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames                   map[generatedMethod]string
	literalNames                  map[string]string
	methodKeys                    []generatedMethod
	literalKeys                   []string
	disableLiteralTextDispatch    bool
	disableTypedObjectSlabs       bool
	disableTypedListSlabs         bool
	disableTypedOutputBootstrap   bool
	enableTypedOutputSweep        bool
	enableBoundedTypedOutputSweep bool
	// nodeScopes records the frame layouts shared by typed rule metadata.
	nodeScopes           map[*rule][]string
	typedSpan            string          // generated helper name, avoiding grammar type and field names
	terminalConstructors map[string]bool // terminal helpers actually used by typed actions
	prog                 *Program
	table                string // the rule table being generated: rules, or recRules for Recognize
	cur                  *rule  // the rule being generated
	// proj holds the projected repetition captures of the rule being generated (projections).
	proj map[string]string
	// final is the struct constructor that makes an action's result in the typed runtime: its
	// range is the rule's (see tctx.result), so it computes none and is not recorded as created.
	final grammar.Term
	// caps maps capture names to the Go variables that hold them in a direct rule of the typed
	// runtime (see gen_direct.go), instead of frame slots.
	caps    map[string]string
	vars    strings.Builder
	init    strings.Builder
	methods strings.Builder
	n       int
	// descs is the expectation table of the generated parser (descs in genrt); descIDs maps a
	// description to its ID.
	descs   []string
	descIDs map[string]expID
	// streamStart is the start rule of ParseStream, whose separate body
	// (generated while streamBody is set) emits its #stream elements.
	streamStart *rule
	streamBody  bool
	streamErr   error
	// fieldTypes names the field type variable of each checked constructed field, result and cast (by
	// *grammar.FieldInit or checkInfo.results key), numbered separately so that checks do not
	// rename other generated names (genrt/fieldcheck.go).
	fieldTypes map[any]string
	// fieldTypeKeys are the keys of fieldTypes in the order of their variables, for rollback.
	fieldTypeKeys []any
}

type generatedMethod struct {
	signature string
	body      string
}

// methodName returns an existing exact helper or reserves the next name.
// The signature keeps Node, typed Go and TypeScript functions distinct.
func (g *generator) methodName(signature, body string) (string, bool) {
	if g.disableMethodSharing {
		return g.name("e"), false
	}
	key := generatedMethod{signature, body}
	if name, ok := g.methodNames[key]; ok {
		return name, true
	}
	if g.methodNames == nil {
		g.methodNames = make(map[generatedMethod]string)
	}
	name := g.name("e")
	g.methodNames[key] = name
	g.methodKeys = append(g.methodKeys, key)
	return name, false
}

// literalName shares immutable literal tables by decoded text. Descriptions
// belong to each matcher, so spellings with different expectations stay distinct.
func (g *generator) literalName(value string) (string, bool) {
	if g.disableLiteralSharing {
		return g.name("lit"), false
	}
	if name, ok := g.literalNames[value]; ok {
		return name, true
	}
	if g.literalNames == nil {
		g.literalNames = make(map[string]string)
	}
	name := g.name("lit")
	g.literalNames[value] = name
	g.literalKeys = append(g.literalKeys, value)
	return name, false
}

func (g *generator) literal(value string) string {
	name, shared := g.literalName(value)
	if !shared {
		fmt.Fprintf(&g.vars, "var %s = []rune(%q)\n", name, value)
	}
	return name
}

// desc returns the expectation ID of description s, adding it to the table.
func (g *generator) desc(s string) expID {
	if g.descIDs == nil {
		g.descIDs = map[string]expID{}
		for i, d := range fixedDescs {
			g.descs = append(g.descs, d)
			g.descIDs[d] = expID(i)
		}
	}
	if id, ok := g.descIDs[s]; ok {
		return id
	}
	id := expID(len(g.descs))
	g.descs = append(g.descs, s)
	g.descIDs[s] = id
	return id
}

func (g *generator) name(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func goStrings(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Quote(x)
	}
	return "[]string{" + strings.Join(q, ", ") + "}"
}

func (g *generator) rules() {
	init := &g.init
	bodies := map[*rule]string{}
	scopes := map[*rule]*scope{}
	directs := map[*rule]*direct{}
	all := append(append([]*rule(nil), g.prog.rules...), g.prog.twins...)
	for _, r := range all {
		s := newScope()
		scopes[r] = s
		if g.table == "trules" || directLeanOK(r) {
			// Typed direct rules need no separate body. Value-free Node rules retain
			// one for generic calls and external entry, whose caller owns depth.
			if d := g.directRule(r); d != nil {
				directs[r] = d
				if g.table != "trules" {
					bodies[r] = g.directLeanBody(r, d)
				}
				continue
			}
		}
		if _, ok := r.def.Expr.(*grammar.OperatorsRule); !ok {
			if g.table != "trules" {
				if body := g.directNodeBody(r, s); body != "" {
					bodies[r] = body
					continue
				}
			} else if body := g.directTypedFrameBody(r, s); body != "" {
				bodies[r] = body
				continue
			}
			g.cur, g.proj = r, g.projections(r)
			bodies[r] = g.expr(r.bodyExpr(), s, !r.lean)
			g.proj = nil
		}
	}
	if g.table == "rules" {
		g.nodeScopes = make(map[*rule][]string, len(all))
		for _, r := range all {
			g.nodeScopes[r] = append([]string(nil), scopes[r].names...)
		}
	}
	if g.table == "trules" {
		// The typed rules share the analysis results of the rules.
		init.WriteString("\ttrules = []*trule{\n")
		for _, r := range all {
			fmt.Fprintf(init, "\t\t{rule: rules[%d]},\n", r.id)
		}
		init.WriteString("\t}\n\tdefer tinit()\n")
	} else {
		fmt.Fprintf(init, "\t%s = []*rule{\n", g.table)
		for _, r := range all {
			extra := ""
			if g.document && (r.leader || r.memo) {
				extra = "docMemo: true, "
			}
			value := shaperLit(r.bodyExpr())
			if _, ok := r.def.Expr.(*grammar.OperatorsRule); ok {
				value = "shaper{}" // the lines build the rule's value
			}
			if r.textStruct && g.table != "trules" {
				// The typed runtime makes the struct with trules[i].term.
				extra += "textStruct: true, "
			}
			fmt.Fprintf(init, "\t\t{id: %d, name: %q, scope: %s, value: %s, terminalType: %q, memo: %v, leader: %v, %sseen: %d, novalue: %v, vars: %s},\n",
				r.id, r.name, goStrings(scopes[r].names), value, r.terminalType, r.memo && !r.transient, r.leader, extra, r.seen, r.novalue, goStrings(r.vars))
		}
		init.WriteString("\t}\n")
		fmt.Fprintf(init, "\tfor _, r := range %s {\n\t\tr.kinds = newRuleKinds(r.name, r.terminalType)\n\t}\n", g.table)
		fmt.Fprintf(init, "\t%s = %d\n", g.seenVar(), g.prog.nseen)
	}
	for _, r := range all {
		if pr, ok := r.def.Expr.(*grammar.OperatorsRule); ok {
			g.cur = r
			g.operatorsRule(r, pr)
			continue
		}
		if d := directs[r]; d != nil && g.table == "trules" {
			g.typedCall(r, "", scopes[r], d)
			continue
		}
		fmt.Fprintf(init, "\t%s[%d].body = func(p *%s, _ int) (%s, bool) { return p.%s() }\n", g.table, r.id, g.recv(), g.valType(), bodies[r])
		if r.action != nil && g.table == "trules" {
			// A function of its own, which typedCall calls directly.
			g.proj, g.final = g.projections(r), r.action
			fmt.Fprintf(&g.methods, "// The action of %s.\nfunc ta%d(c *tctx) any { return %s }\n\n", r.name, r.id, g.term(r.action, scopes[r], nil))
			g.proj, g.final = nil, nil
			fmt.Fprintf(init, "\ttrules[%d].action = ta%d\n", r.id, r.id)
		} else if r.action != nil {
			g.proj = g.projections(r)
			fmt.Fprintf(init, "\t%s[%d].action = func(c *%s) any { return %s }\n", g.table, r.id, g.ctxType(), g.term(r.action, scopes[r], nil))
			g.proj = nil
		}
		if check := g.resultCheckFunc(r, r.def); check != "" {
			fmt.Fprintf(init, "\t%s[%d].%s = %s\n", g.table, r.id, g.pick("check", "tcheck"), check)
		}
		if g.table == "trules" {
			g.typedCall(r, bodies[r], scopes[r], nil)
		} else if r.plain {
			if d := directs[r]; d != nil {
				g.directMethod(r, d, fmt.Sprintf("%s%d", g.plainPrefix(), r.id), "called as by invokePlain", true)
			} else {
				g.plainCall(r, bodies[r])
			}
		}
	}
	if r := g.streamStart; r != nil && g.table == "rules" {
		// ParseStream calls a copy of the start rule whose body emits the
		// #stream elements, generated last so that the batch code is the same
		// as without streams. Its captures have a layout of their own.
		g.cur, g.proj, g.streamBody = r, g.projections(r), true
		s := newScope()
		body := g.expr(r.bodyExpr(), s, !r.lean)
		g.proj, g.streamBody = nil, false
		if strings.Join(s.names, "\x00") != strings.Join(scopes[r].names, "\x00") {
			// The copied action addresses captures by the batch body's slots.
			g.streamErr = fmt.Errorf("internal error: the stream body of %s has a different capture layout", r.name)
		}
		fmt.Fprintf(init, "\tstreamRule = new(rule)\n\t*streamRule = *rules[%d]\n\tstreamRule.scope = %s\n\tstreamRule.body = func(p *parser, _ int) (*Node, bool) { return p.%s() }\n", r.id, goStrings(s.names), body)
		g.methods.WriteString("// streamRule is the start rule of ParseStream.\nvar streamRule *rule\n\n")
	}
	if g.table != "rules" {
		return // the recognizer shares the struct fields
	}
	// Struct fields, used to detect references to undeclared fields at run time.
	var names []string
	for name, spec := range g.prog.types {
		if _, ok := spec.(*grammar.StructSpec); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var fields []string
		for _, f := range g.prog.types[name].(*grammar.StructSpec).Fields {
			fields = append(fields, fmt.Sprintf("%q: true", f.Name))
		}
		fmt.Fprintf(init, "\tstructFields[%q] = map[string]bool{%s}\n", name, strings.Join(fields, ", "))
		fmt.Fprintf(init, "\ttypeKinds[%q] = kindOf(%q, \"\")\n", name, name)
	}
}

// typedCall writes the methods that call the rule r in the typed runtime: s<id> for a rule never
// memoized and without captures (what invokePlain does), and otherwise u<id> (what call does, the
// memo kept by callMemo) and i<id> (what invoke does). Each calls the body's method and the action
// directly and finishes the value as the rule's kind requires (what finish does), so a call goes
// through no rule table entry or function value.
//
// For a direct rule (d, see gen_direct.go), the method that runs the body (s<id>, v<id> or i<id>)
// has it inlined, with the action.
func (g *generator) typedCall(r *rule, body string, s *scope, d *direct) {
	if _, operatorsRule := r.def.Expr.(*grammar.OperatorsRule); operatorsRule || r.leader {
		return // the general call
	}
	m := &g.methods
	entry := fmt.Sprintf("u%d", r.id)
	if r.plain {
		entry = fmt.Sprintf("s%d", r.id)
	}
	fmt.Fprintf(&g.init, "\ttrules[%d].entry = (*tparser).%s\n", r.id, entry)
	ncaps := len(s.names)
	if d != nil {
		ncaps = len(r.scope.names)
	}
	finish := func(frame string) {
		switch {
		case r.novalue:
			m.WriteString("\t\tv = nil\n")
		case r.action != nil:
			fmt.Fprintf(m, "\t\tc := p.useCtx(tctx{p: p, f: %s, start: start, end: p.pos, cbase: len(p.created)})\n", frame)
			fmt.Fprintf(m, "\t\tv = c.result(ta%d, %q)\n", r.id, r.name)
		case r.terminalType != "":
			fmt.Fprintf(m, "\t\tv = trules[%d].term(p, start, p.pos, p.text(start, p.pos))\n", r.id)
		default:
			fmt.Fprintf(m, "\t\tv = p.scopeValue(trules[%d].value, v, start)\n", r.id)
			if len(s.names) > 0 {
				fmt.Fprintf(m, "\t\tv = p.attachCaptures(v, trules[%d].scope, %s.vals, start, p.pos)\n", r.id, frame)
			}
			m.WriteString("\t\tif n := asTval(v); n != nil {\n\t\t\tn.tsetFresh(false)\n\t\t}\n")
		}
		if check := g.resultCheck(r, r.def, "v"); check != "" {
			fmt.Fprintf(m, "\t\t%s\n", check)
		}
	}
	plain := func(name string) {
		if d != nil {
			g.directMethod(r, d, name, "called as by invokePlain", true)
			return
		}
		if g.observability && r.plain && !g.directTypedDocument {
			name = g.tracePlainWrapper(r, name)
		}
		fmt.Fprintf(m, "// %s, called as by invokePlain\nfunc (p *tparser) %s() (any, bool) {\n", r.name, name)
		m.WriteString("\tstart, rec, trail := p.pos, len(p.recovered), len(p.trail)\n\tprevEnv, prevCut := p.env, p.cut\n\tp.cut = false\n\tp.depth++\n\tif p.depth > p.maxDepth {\n\t\tp.tooDeep()\n\t}\n")
		if g.observability && (!r.plain || g.directTypedDocument) {
			m.WriteString("p.observation.evaluated()\n")
		}
		fmt.Fprintf(m, "\tv, ok := p.%s()\n\tp.depth--\n\tp.cut = prevCut\n\tp.dropTrail(trail)\n\tif ok {\n", body)
		finish("emptyTFrame")
		m.WriteString("\t} else {\n\t\tp.pos = start\n\t\tp.recovered = p.recovered[:rec]\n\t}\n\tp.env = prevEnv\n\treturn v, ok\n}\n\n")
	}
	if r.plain {
		if g.directTypedDocument {
			name := fmt.Sprintf("s%d", r.id)
			if g.observability {
				name = g.traceCallWrapper(r, name, false)
			}
			fmt.Fprintf(m, "func (p *tparser) %s() (any, bool) {\n if p.typedDocument != nil { return p.documentCall(trules[%d], 0, (*tparser).ds%d) }; return p.ds%d()\n}\n", name, r.id, r.id, r.id)
			plain(fmt.Sprintf("ds%d", r.id))
			return
		}
		plain(fmt.Sprintf("s%d", r.id))
		return
	}
	// u<id>: the call. An unmemoized call of a rule without captures is plain.
	name := fmt.Sprintf("u%d", r.id)
	if g.observability {
		name = g.traceCallWrapper(r, name, false)
	}
	fmt.Fprintf(m, "// %s, called as by call\nfunc (p *tparser) %s() (any, bool) {\n", r.name, name)
	if g.directTypedDocument {
		fmt.Fprintf(m, " if p.typedDocument != nil { return p.documentCall(trules[%d], 0, (*tparser).i%d) }\n", r.id, r.id)
	}
	memo := r.memo && !r.transient
	if memo {
		fmt.Fprintf(m, "\tif !p.firstCall(rules[%d]) {\n\t\treturn p.callMemo(trules[%d], (*tparser).i%d)\n\t}\n", r.id, r.id, r.id)
	}
	if ncaps == 0 {
		fmt.Fprintf(m, "\treturn p.v%d()\n}\n\n", r.id)
		plain(fmt.Sprintf("v%d", r.id))
	} else {
		fmt.Fprintf(m, "\tstart, rec := p.pos, len(p.recovered)\n\tv, ok := p.i%d()\n\tif !ok {\n\t\tp.pos = start\n\t\tp.recovered = p.recovered[:rec]\n\t}\n\treturn v, ok\n}\n\n", r.id)
	}
	// i<id>: the invocation (also used by the memo).
	if d != nil {
		g.directMethod(r, d, fmt.Sprintf("i%d", r.id), "invoked as by invoke", false)
		return
	}
	fmt.Fprintf(m, "// %s, invoked as by invoke\nfunc (p *tparser) i%d() (any, bool) {\n", r.name, r.id)
	fmt.Fprintf(m, "\tf := p.newFrame(%d)\n\tprevFrame, prevEnv, prevCut, trail := p.frame, p.env, p.cut, len(p.trail)\n\tp.frame, p.cut = f, false\n\tp.depth++\n\tif p.depth > p.maxDepth {\n\t\tp.tooDeep()\n\t}\n\tstart := p.pos\n\t_ = start\n", len(s.names))
	if g.observability {
		m.WriteString("p.observation.evaluated()\n")
	}
	fmt.Fprintf(m, "\tv, ok := p.%s()\n\tp.depth--\n\tp.frame, p.cut = prevFrame, prevCut\n\tp.dropTrail(trail)\n\tif ok {\n", body)
	finish("f")
	m.WriteString("\t}\n\tp.env = prevEnv\n\tp.freeFrame(f)\n\treturn v, ok\n}\n\n")
}

// plainPrefix is the prefix of the methods written by plainCall, and seenVar the variable that
// holds the number of rules with rule.seen set, for the rule table being generated.
func (g *generator) plainPrefix() string {
	switch g.table {
	case "rules":
		return "r"
	case "trules":
		return "s"
	}
	return "q"
}

// pick returns untyped for the Node runtime and typed for the typed runtime (trules).
func (g *generator) pick(untyped, typed string) string {
	if g.table == "trules" {
		return typed
	}
	return untyped
}

func (g *generator) recv() string     { return g.pick("parser", "tparser") }
func (g *generator) valType() string  { return g.pick("*Node", "any") }
func (g *generator) ctxType() string  { return g.pick("actx", "tctx") }
func (g *generator) rt() string       { return g.pick("rt", "trt") }

// comparison reports whether op compares two values.
func comparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// fieldRead reports whether t reads a field of a value, x.F.
func fieldRead(t grammar.Term) bool {
	m, ok := t.(*grammar.Member)
	return ok && !strings.HasPrefix(m.Name, "$")
}

// operand returns a typed runtime operand (toperand) of a comparison: a field read keeps a
// string field unboxed.
func (g *generator) operand(t grammar.Term, s *scope, locals map[string]string) string {
	if fieldRead(t) {
		m := t.(*grammar.Member)
		return fmt.Sprintf("c.fieldOperand(%s, %q)", g.term(m.X, s, locals), m.Name)
	}
	return "toperand{v: " + g.term(t, s, locals) + "}"
}
func (g *generator) frameVar() string { return g.pick("emptyFrame", "emptyTFrame") }

func (g *generator) seenVar() string {
	if g.table == "rules" {
		return "nseen"
	}
	return "recNseen"
}

// method emits a method with the given body and returns its name.
func (g *generator) method(comment, body string) string {
	name, shared := g.methodName(g.recv()+"/"+g.valType(), body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.methods, "// %s\n", comment)
	}
	fmt.Fprintf(&g.methods, "func (p *%s) %s() (%s, bool) {\n\tp.stepWork()\n%s}\n\n", g.recv(), name, g.valType(), body)
	return name
}

// shaperLit returns the runtime shaper of the expression e, its static shape (value_shape.go).
func shaperLit(e grammar.Expr) string {
	return fmt.Sprintf("shaper{elems: %v, k: %d, mode: %d, tuple: %v}", isElems(e), valuedCount(e), singleModeOf(e), scopeTuple(e))
}

// singleMethod returns the method of the value of e where it takes one element (an alternative or
// the operand of ?), from the building method m of e, as singleMatcher does; with capture, that of
// a capture or a recovered expression, as captureMatcher does.
func (g *generator) singleMethod(e grammar.Expr, m string, capture bool) string {
	if singleModeOf(e) == singlePass && !isElems(e) {
		return m
	}
	shape := fmt.Sprintf("\tv, _ = p.shapeSingle(%s, v, start)\n", shaperLit(e))
	if capture {
		shape = fmt.Sprintf("\tv = p.shapeCapture(%s, v, start)\n", shaperLit(e))
	}
	return g.method(describe(e), fmt.Sprintf("\tstart := p.pos\n\tv, ok := p.%s()\n\tif !ok {\n\t\treturn nil, false\n\t}\n%s\treturn v, true\n", m, shape))
}

func describe(e grammar.Expr) string {
	s := grammar.FormatExpr(e)
	if strings.Contains(s, "\n") || len(s) > 100 {
		return ""
	}
	return s
}

// plainCall writes the method r<id>, which does what invokePlain does for the rule r (a rule never
// memoized and without captures) with a direct call of its body.
func (g *generator) plainCall(r *rule, body string) {
	name := fmt.Sprintf("%s%d", g.plainPrefix(), r.id)
	if g.observability {
		name = g.tracePlainWrapper(r, name)
	}
	fmt.Fprintf(&g.methods, "// %s, called as by invokePlain\nfunc (p *%s) %s() (%s, bool) {\n\tp.stepWork()\n", r.name, g.recv(), name, g.valType())
	g.methods.WriteString(`	start, rec, trail := p.pos, len(p.recovered), len(p.trail)
	prevEnv, prevCut := p.env, p.cut
	p.cut = false
	p.depth++
	if p.depth > p.maxDepth {
		p.tooDeep()
	}
`)
	fmt.Fprintf(&g.methods, "\tv, ok := p.%s()\n", body)
	g.methods.WriteString("\tp.depth--\n\tp.cut = prevCut\n" + g.pick("\tp.trail = p.trail[:trail]\n", "\tp.dropTrail(trail)\n") + "\tif ok {\n")
	if r.novalue {
		g.methods.WriteString("\t\tv = nil\n")
	} else {
		fmt.Fprintf(&g.methods, "\t\tv = p.finish(%s[%d], %s, v, start)\n", g.table, r.id, g.frameVar())
	}
	g.methods.WriteString(`	} else {
		p.pos = start
		p.recovered = p.recovered[:rec]
	}
	p.env = prevEnv
	return v, ok
}

`)
}

// projections returns the projected repetition captures of the rule r (see ruleProjections), none
// in the recognizer.
func (g *generator) projections(r *rule) map[string]string {
	if g.table == "recRules" {
		return nil
	}
	return ruleProjections(g.prog, r)
}

// projected reports whether the map call t reads a projected capture (projections), and returns
// the capture.
func (g *generator) projected(t *grammar.Call, locals map[string]string) (grammar.Term, bool) {
	x, ok := projectedMap(t, g.proj, func(name string) bool { _, local := locals[name]; return local })
	return x, ok
}

// projectRepeat writes a repetition that gathers the values of the capture field of its elements
// (see projections). The elements build no values of their own, and share one capture frame.
func (g *generator) projectRepeat(b *strings.Builder, e *grammar.Repeat, field string) {
	elemScope := newScope()
	m := g.expr(e.Expr, elemScope, false)
	slot := elemScope.slots[field]
	fmt.Fprintf(b, "\tstart := p.pos\n\tbase := len(p.kidStack)\n\tf := p.newFrame(%d)\n\tcount := 0\n", len(elemScope.names))
	if e.Max < 0 {
		b.WriteString("\tfor {\n")
	} else {
		fmt.Fprintf(b, "\tfor n := 0; n < %d; n++ {\n", e.Max)
	}
	b.WriteString("\t\tclear(f.vals)\n\t\tm0 := p.mark()\n\t\tprevCut, prevFrame := p.cut, p.frame\n\t\tp.cut = false\n\t\tp.frame = f\n")
	fmt.Fprintf(b, "\t\t_, ok := p.%s()\n", m)
	b.WriteString("\t\tcut := p.cut\n\t\tp.cut, p.frame = prevCut, prevFrame\n\t\tif !ok {\n\t\t\tp.reset(m0)\n\t\t\tif cut {\n\t\t\t\tp.dropKids(base)\n\t\t\t\treturn nil, false\n\t\t\t}\n\t\t\tbreak\n\t\t}\n")
	fmt.Fprintf(b, "\t\tcount++\n\t\tp.kidStack = append(p.kidStack, f.vals[%d])\n", slot)
	b.WriteString("\t\tif p.pos == m0.pos {\n\t\t\tbreak\n\t\t}\n\t}\n")
	fmt.Fprintf(b, "\tif count < %d {\n\t\tp.dropKids(base)\n\t\treturn nil, false\n\t}\n", e.Min)
	b.WriteString(g.pick("\treturn p.newNode(Node{kind: kindList, start: int32(start), end: int32(p.pos), children: p.kids(base), fresh: true}), true\n",
		"\treturn p.newNode(\"List\", start, p.pos, p.kids(base)), true\n"))
}

func (g *generator) expr(e grammar.Expr, s *scope, build bool) string {
	var b strings.Builder
	switch e := e.(type) {
	case *grammar.Literal:
		value := literalValue(e.Value)
		v := g.literal(value)
		fmt.Fprintf(&b, "\treturn p.matchLiteral(%s, %q, %d, %v)\n", v, value, g.desc(strconv.Quote(e.Value)), build)
	case *grammar.CharClass:
		fmt.Fprintf(&b, "\tch, size, ok := p.peek()\n\tif !ok || %s {\n\t\tp.expect(p.pos, %d)\n\t\treturn nil, false\n\t}\n",
			g.controlledClassReject(e), g.desc(charClassString(e)))
		fmt.Fprintf(&b, "\treturn p.single(size, %v)\n", build)
	case *grammar.Any:
		fmt.Fprintf(&b, "\treturn p.matchAny(%v)\n", build)
	case *grammar.Ref:
		fmt.Fprintf(&b, "\treturn %s\n", g.refCall(e, build))
	case *grammar.Seq:
		var items []string
		for _, it := range e.Items {
			items = append(items, g.expr(it, s, build))
		}
		// The value is the tuple node of the elements, flattened through nested sequences.
		if build {
			fmt.Fprintf(&b, "\tstart := p.pos\n\tkids := p.%s(%d)[:0]\n", g.pick("nodes", "newVals"), valuedCount(e))
		}
		for i, it := range e.Items {
			switch {
			case build && isElems(it):
				fmt.Fprintf(&b, "\tif v, ok := p.%s(); !ok {\n\t\treturn nil, false\n\t} else {\n\t\tkids = append(kids, %s...)\n\t}\n", items[i], g.pick("v.children", "asTval(v).tkids()"))
			case build && valuedCount(it) == 1:
				fmt.Fprintf(&b, "\tif v, ok := p.%s(); !ok {\n\t\treturn nil, false\n\t} else {\n\t\tkids = append(kids, v)\n\t}\n", items[i])
			default:
				fmt.Fprintf(&b, "\tif _, ok := p.%s(); !ok {\n\t\treturn nil, false\n\t}\n", items[i])
			}
		}
		if build {
			b.WriteString(g.pick("\treturn p.newNode(Node{kind: kindSeq, start: int32(start), end: int32(p.pos), children: kids, fresh: true}), true\n",
				"\treturn p.newNode(\"Seq\", start, p.pos, kids), true\n"))
		} else {
			b.WriteString("\treturn nil, true\n")
		}
	case *grammar.Choice:
		b.WriteString("\tm0 := p.mark()\n")
		peeked := false
		for _, alt := range e.Alts {
			m := g.expr(alt, s, build)
			if build {
				m = g.singleMethod(alt, m, false)
			}
			if cond, desc, depth, ok := g.first(alt, 0, map[string]bool{}); ok {
				// An alternative whose first terminal does not match the next character is skipped,
				// with the expectation it would record (see first).
				if !peeked {
					b.WriteString("\tch, _, more := p.peek()\n")
					peeked = true
				}
				fmt.Fprintf(&b, "\tif !(more && (%s)) && %s {\n\t\tp.stepWork()\n\t\tp.expect(p.pos, %d)\n%s\t} else ", cond, depthLimit(depth), desc, g.traceSkip(alt))
			}
			fmt.Fprintf(&b, "\t{\n\t\tprevCut := p.cut\n\t\tp.cut = false\n\t\tv, ok := p.%s()\n\t\tcut := p.cut\n\t\tp.cut = prevCut\n"+
				"\t\tif ok {\n\t\t\treturn v, true\n\t\t}\n\t\tp.reset(m0)\n\t\tif cut {\n\t\t\treturn nil, false\n\t\t}\n\t}\n", m)
		}
		b.WriteString("\treturn nil, false\n")
	case *grammar.Repeat:
		g.repeat(&b, e, s, build, false)
	case *grammar.Optional:
		m := g.expr(e.Expr, s, build)
		if build {
			m = g.singleMethod(e.Expr, m, false)
		}
		fmt.Fprintf(&b, "\tm0 := p.mark()\n\tprevCut := p.cut\n\tp.cut = false\n\tv, ok := p.%s()\n\tcut := p.cut\n\tp.cut = prevCut\n"+
			"\tif ok {\n\t\treturn v, true\n\t}\n\tp.reset(m0)\n\tif cut {\n\t\treturn nil, false\n\t}\n\treturn nil, true\n", m)
	case *grammar.And:
		m := g.expr(e.Expr, s, build && hasCaptures(e.Expr))
		fmt.Fprintf(&b, "\tstart, prevCut := p.pos, p.cut\n\tp.silent++\n\t_, ok := p.%s()\n\tp.silent--\n\tp.cut = prevCut\n\tp.pos = start\n\treturn nil, ok\n", m)
	case *grammar.Not:
		m := g.expr(e.Expr, s, false)
		fmt.Fprintf(&b, "\tm0 := p.mark()\n\tprevCut := p.cut\n\tp.silent++\n\t_, ok := p.%s()\n\tp.silent--\n\tp.cut = prevCut\n\tp.reset(m0)\n\treturn nil, !ok\n", m)
	case *grammar.Atomic:
		m := g.expr(e.Expr, s, false)
		fmt.Fprintf(&b, "\tstart := p.pos\n\tif _, ok := p.%s(); !ok {\n\t\treturn nil, false\n\t}\n", m)
		if build {
			b.WriteString(g.pick("\treturn p.newNode(Node{kind: kindMatch, start: int32(start), end: int32(p.pos), text: p.text(start, p.pos), terminal: true, fresh: true}), true\n",
				"\treturn p.newMatch(start, p.pos, p.text(start, p.pos), true), true\n"))
		} else {
			b.WriteString("\t_ = start\n\treturn nil, true\n")
		}
	case *grammar.Discard:
		m := g.expr(e.Expr, s, false)
		fmt.Fprintf(&b, "\t_, ok := p.%s()\n\treturn nil, ok\n", m)
	case *grammar.Capture:
		if !build && g.cur != nil && g.cur.predCaps != nil && !g.cur.predCaps[e.Name] {
			// In the recognizer, captures not referenced by predicates are not recorded (as in
			// the engine).
			return g.expr(e.Expr, s, false)
		}
		slot := s.slot(e.Name)
		var m string
		if field := g.proj[e.Name]; field != "" {
			var rb strings.Builder
			g.projectRepeat(&rb, e.Expr.(*grammar.Repeat), field)
			m = g.method(describe(e.Expr)+" (projected to "+field+")", rb.String())
		} else {
			m = g.singleMethod(e.Expr, g.expr(e.Expr, s, true), true)
		}
		fmt.Fprintf(&b, "\tv, ok := p.%s()\n\tif ok {\n\t\tp.setCapture(%d, v)\n\t}\n", m, slot)
		if build {
			b.WriteString("\treturn v, ok\n")
		} else {
			b.WriteString("\treturn nil, ok\n")
		}
	case *grammar.Cut:
		b.WriteString("\tp.cut = true\n\treturn nil, true\n")
	case *grammar.Top:
		// _ matches the empty input and has no value.
		b.WriteString("\treturn nil, true\n")
	case *grammar.Bottom:
		b.WriteString("\treturn nil, false\n")
	case *grammar.BeginInput:
		b.WriteString("\treturn p.anchor(p.pos == 0, idBeginInput)\n")
	case *grammar.EndInput:
		b.WriteString("\treturn p.anchor(p.atEnd(), idEndInput)\n")
	case *grammar.BeginLine:
		b.WriteString("\treturn p.anchor(p.atLineStart(), idBeginLine)\n")
	case *grammar.EndLine:
		b.WriteString("\treturn p.anchor(p.atLineEnd(), idEndLine)\n")
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Assign); ok {
			fmt.Fprintf(&b, "\treturn nil, p.assign(%q, func(c *%s) any { return %s })\n", a.Name, g.ctxType(), g.term(a.Value, s, nil))
		} else {
			fmt.Fprintf(&b, "\treturn nil, p.predicate(func(c *%s) any { return %s })\n", g.ctxType(), g.predicateTerm(e.Term, s, nil))
		}
	case *grammar.Attributed:
		return g.attributed(e, s, build)
	default:
		panic(fmt.Sprintf("unsupported expression %T", e))
	}
	return g.method(describe(e), b.String())
}

// refCall returns the Go call of the rule the reference e calls (its value-free twin unless
// build).
func (g *generator) refCall(e *grammar.Ref, build bool) string {
	r := g.prog.byName[e.Name]
	min := 0
	if e.Level != "" {
		for i, l := range r.def.Expr.(*grammar.OperatorsRule).Levels {
			if l.Name == e.Level {
				min = i
			}
		}
	}
	if !build && r.twin != nil {
		r = r.twin
	}
	_, operatorsRule := r.def.Expr.(*grammar.OperatorsRule)
	switch {
	case g.document && g.table == "rules":
		// Document parses route every call through call, which memoizes per rule.docMemo,
		// including otherwise plain rules.
		return fmt.Sprintf("p.call(%s[%d], %d)", g.table, r.id, min)
	case g.directTypedDocument && g.table == "trules" && operatorsRule:
		return fmt.Sprintf("p.call(%s[%d], %d)", g.table, r.id, min)
	case r.plain && !operatorsRule:
		// Never memoized and without captures: call does nothing but invokePlain, which a
		// method of its own does with a direct call of the body (see plainCalls).
		return fmt.Sprintf("p.%s%d()", g.plainPrefix(), r.id)
	case g.table == "trules" && !operatorsRule && !r.leader:
		// The typed runtime calls a method of the rule's own (typedCall).
		return fmt.Sprintf("p.u%d()", r.id)
	case r.plain:
		return fmt.Sprintf("p.invokePlain(%s[%d], %d)", g.table, r.id, min)
	}
	return fmt.Sprintf("p.call(%s[%d], %d)", g.table, r.id, min)
}

func (g *generator) repeat(b *strings.Builder, e *grammar.Repeat, s *scope, build, stream bool) {
	if !build && !stream && g.scanRepeat(b, e) {
		return
	}
	// An element with captures has its own scope per iteration (elementScoped).
	elemScope := s
	if elementScoped(e.Expr) {
		elemScope = newScope()
	}
	m := g.expr(e.Expr, elemScope, build)
	own := elemScope != s
	if build {
		// Element values are gathered on p.kidStack and copied out once.
		b.WriteString("\tstart := p.pos\n\tbase := len(p.kidStack)\n")
	}
	b.WriteString("\tcount := 0\n")
	if stream {
		b.WriteString("\tif p.stream != nil && p.stream.emit != nil && p.depth == 1 {\n\t\tp.streamStart()\n\t}\n")
	}
	if e.Max < 0 {
		b.WriteString("\tfor {\n")
	} else {
		fmt.Fprintf(b, "\tfor n := 0; n < %d; n++ {\n", e.Max)
	}
	b.WriteString("\t\tm0 := p.mark()\n\t\tprevCut, prevFrame := p.cut, p.frame\n\t\tp.cut = false\n")
	scopeVar := ""
	if own {
		scopeVar = g.name("scope")
		fmt.Fprintf(&g.vars, "var %s = %s\n", scopeVar, goStrings(elemScope.names))
		fmt.Fprintf(b, "\t\tf := p.newFrame(%d)\n\t\tp.frame = f\n", len(elemScope.names))
	}
	if build {
		fmt.Fprintf(b, "\t\tv, ok := p.%s()\n", m)
	} else {
		fmt.Fprintf(b, "\t\t_, ok := p.%s()\n", m)
	}
	b.WriteString("\t\tcut := p.cut\n\t\tp.cut, p.frame = prevCut, prevFrame\n\t\tif !ok {\n\t\t\tp.reset(m0)\n\t\t\tif cut {\n")
	if build {
		b.WriteString("\t\t\t\tp.dropKids(base)\n")
	}
	b.WriteString("\t\t\t\treturn nil, false\n\t\t\t}\n\t\t\tbreak\n\t\t}\n")
	if build {
		fmt.Fprintf(b, "\t\tv = p.scopeValue(%s, v, m0.pos)\n", shaperLit(e.Expr))
	}
	if own && build {
		fmt.Fprintf(b, "\t\tv = p.attachCaptures(v, %s, %s, m0.pos, p.pos)\n", scopeVar, g.pick("f", "f.vals"))
	}
	if own && g.table == "trules" {
		// The element's frame is dead: its values are in v, and the trail entries since m0 all
		// refer to it (or to frames of its own elements), so they can go too.
		b.WriteString("\t\tp.dropTrail(m0.trail)\n\t\tp.freeFrame(f)\n")
	}
	b.WriteString("\t\tcount++\n")
	if stream {
		// Elements of the start rule's #stream are passed on without being retained, and committed.
		b.WriteString("\t\tif p.stream != nil && p.stream.emit != nil && p.depth == 1 {\n")
		if build {
			b.WriteString("\t\t\tp.emitStream(v, p.pos)\n")
		} else {
			b.WriteString("\t\t\tp.emitStream(nil, p.pos)\n")
		}
		b.WriteString("\t\t\tif p.pos == m0.pos {\n\t\t\t\tbreak\n\t\t\t}\n\t\t\tcontinue\n\t\t}\n")
	}
	if build {
		b.WriteString("\t\tp.kidStack = append(p.kidStack, v)\n")
	}
	b.WriteString("\t\tif p.pos == m0.pos {\n\t\t\tbreak\n\t\t}\n\t}\n")
	fmt.Fprintf(b, "\tif count < %d {\n", e.Min)
	if build {
		b.WriteString("\t\tp.dropKids(base)\n")
	}
	b.WriteString("\t\treturn nil, false\n\t}\n")
	if !build {
		b.WriteString("\treturn nil, true\n")
		return
	}
	b.WriteString(g.pick("\treturn p.newNode(Node{kind: kindList, start: int32(start), end: int32(p.pos), children: p.kids(base), fresh: true}), true\n",
		"\treturn p.newNode(\"List\", start, p.pos, p.kids(base)), true\n"))
}

// scanRepeat generates a value-free repetition of a single-character expression (a character
// class or .) as a scan loop without per-iteration backtracking state, like the engine's
// compiler.scanRepeat. It reports whether e qualifies. Expectations are recorded as in a general
// repetition: once, where the element last failed.
func (g *generator) scanRepeat(b *strings.Builder, e *grammar.Repeat) bool {
	var cond string
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		cond = fmt.Sprintf("!ok || %s {\n\t\t\tp.expect(p.pos, %d)", g.controlledClassReject(x), g.desc(charClassString(x)))
		b.WriteString("\tcount := 0\n")
	case *grammar.Any:
		cond = "!ok {\n\t\t\tp.expect(p.pos, idAny)"
		b.WriteString("\tcount := 0\n")
	default:
		return false
	}
	if e.Max < 0 {
		b.WriteString("\tfor {\n")
	} else {
		fmt.Fprintf(b, "\tfor count < %d {\n", e.Max)
	}
	if _, isAny := e.Expr.(*grammar.Any); isAny {
		b.WriteString("\t\t_, size, ok := p.peek()\n")
	} else {
		b.WriteString("\t\tch, size, ok := p.peek()\n")
	}
	fmt.Fprintf(b, "\t\tif %s\n\t\t\tbreak\n\t\t}\n\t\tp.pos += size\n\t\tcount++\n\t}\n", cond)
	fmt.Fprintf(b, "\treturn nil, count >= %d\n", e.Min)
	return true
}

// first returns the condition on the next character ch under which the expression e can match,
// when e must begin with a given terminal (a literal or a character class, possibly behind
// sequences, captures, @, - and calls of rules that are not left-recursive or OperatorsRule), with the
// expectation e records when the condition does not hold and the number of rule calls on the way.
// Then e fails at once without the terminal, recording only that expectation (a call memoizes the
// same failure, and memoization only affects speed), so a choice can skip it; unless the calls on
// the way would exceed the nesting limit.
func (g *generator) first(e grammar.Expr, depth int, seen map[string]bool) (string, expID, int, bool) {
	switch e := e.(type) {
	case *grammar.Literal:
		if e.Value == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(e.Value)
		return fmt.Sprintf("ch == %d", r), g.desc(strconv.Quote(e.Value)), depth, true
	case *grammar.CharClass:
		return negateCondition(g.controlledClassReject(e)), g.desc(charClassString(e)), depth, true
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return g.first(e.Items[0], depth, seen)
		}
	case *grammar.Capture:
		return g.first(e.Expr, depth, seen)
	case *grammar.Atomic:
		return g.first(e.Expr, depth, seen)
	case *grammar.Discard:
		return g.first(e.Expr, depth, seen)
	case *grammar.Ref:
		r := g.prog.byName[e.Name]
		if r == nil || r.leader || seen[e.Name] {
			break
		}
		if _, operatorsRule := r.def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return g.first(r.def.Expr, depth+1, seen)
	}
	return "", 0, 0, false
}

// depthLimit returns the nesting guard needed before calling depth rules.
func depthLimit(depth int) string {
	if depth == 0 {
		return "p.depth <= p.maxDepth"
	}
	return fmt.Sprintf("p.depth+%d <= p.maxDepth", depth)
}

// negateCondition inverts a generated condition without retaining a leading
// logical negation as a double negation.
func negateCondition(condition string) string {
	if strings.HasPrefix(condition, "!") {
		return strings.TrimPrefix(condition, "!")
	}
	return "!(" + condition + ")"
}

// classReject returns a Go condition on ch that is true when ch is not accepted by the class.
func classReject(e *grammar.CharClass) string {
	var in []string
	for _, r := range e.Ranges {
		if r.Lo == r.Hi {
			in = append(in, fmt.Sprintf("ch == %d", r.Lo))
		} else {
			in = append(in, fmt.Sprintf("ch >= %d && ch <= %d", r.Lo, r.Hi))
		}
	}
	if len(in) == 0 {
		in = []string{"ch < 0"} // never true; mentions ch so that it is used
	}
	cond := strings.Join(in, " || ")
	if e.Negated {
		return "(" + cond + ")"
	}
	return "!(" + cond + ")"
}

func (g *generator) attributed(e *grammar.Attributed, s *scope, build bool) string {
	var m string
	if rep, ok := e.Expr.(*grammar.Repeat); ok && g.streamBody && isStream(e) {
		var b strings.Builder
		g.repeat(&b, rep, s, build, true)
		m = g.method(describe(e), b.String())
	} else {
		m = g.expr(e.Expr, s, build)
	}
	for _, a := range e.Attrs {
		switch a.Name {
		case "error":
			msg := attributeMessage(a)
			m = g.method("@error", fmt.Sprintf("\treturn p.errorAttr((*%s).%s, msgBit|%d)\n", g.recv(), m, g.desc(msg)))
		case "diagnostic":
			name := attributeDiagnosticName(a)
			m = g.method("@diagnostic", fmt.Sprintf("\treturn p.diagnosticAttr((*%s).%s, %d)\n", g.recv(), m, g.desc(name)))
		case "recover":
			skipExpr, _ := a.Arg("skip")
			skip := g.expr(skipExpr, s, false)
			// What recovers is one element: the value of the expression, or the error.
			if build {
				m = g.singleMethod(e.Expr, m, true)
			}
			m = g.method("#recover", fmt.Sprintf("\treturn p.recoverAttr((*%s).%s, (*%s).%s, %v)\n", g.recv(), m, g.recv(), skip, build))
		}
	}
	return m
}

func (g *generator) operatorsRule(r *rule, e *grammar.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "\t%s[%d].operatorsRule = &%s{\n", g.table, r.id, g.pick("operatorsRule", "toperatorsRule"))
	if e.Skip != nil {
		s := newScope()
		m := g.directTypedOperatorsRuleBody(e.Skip, s, false, nil, false)
		if m == "" {
			m = g.expr(e.Skip, s, false)
		}
		fmt.Fprintf(&b, "\t\tskip: (*%s).%s,\n", g.recv(), m)
	}
	fmt.Fprintf(&b, "\t\toperands: []*%s{\n", g.pick("operatorsRuleLine", "toperatorsRuleLine"))
	for _, o := range e.Operands {
		fmt.Fprintf(&b, "\t\t\t%s,\n", g.operatorsRuleLine(o.Expr, o.Action, false, o))
	}
	b.WriteString("\t\t},\n")
	var prefix, led []string
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			s := fmt.Sprintf("{id: %d, kind: %q, assoc: %q, level: %d, line: %s}", id, op.Kind, op.Assoc, i+1, g.operatorsRuleLine(op.Expr, op.Action, true, op))
			id++
			if op.Kind == grammar.Prefix {
				prefix = append(prefix, s)
			} else {
				led = append(led, s)
			}
		}
	}
	op := g.pick("operatorsRuleOp", "toperatorsRuleOp")
	fmt.Fprintf(&b, "\t\tprefix: []*%s{%s},\n", op, strings.Join(prefix, ",\n"))
	fmt.Fprintf(&b, "\t\tled: []*%s{%s},\n", op, strings.Join(led, ",\n"))
	b.WriteString("\t}\n")
	fmt.Fprintf(&b, "\t%s[%d].body = func(p *%s, min int) (%s, bool) { return p.operatorsRuleParse(%s[%d], min) }\n", g.table, r.id, g.recv(), g.valType(), g.table, r.id)
	g.init.WriteString(b.String())
}

func (g *generator) operatorsRuleLine(e grammar.Expr, action grammar.Term, operator bool, key any) string {
	s := newScope()
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	// Lines whose value is unused are matched without values, as in the engine; so are all lines
	// of a rule without a value (in the recognizer).
	build := !g.cur.novalue && !leanLine(action, locals)
	m := g.directTypedOperatorsRuleBody(e, s, build, action, operator)
	if m == "" {
		m = g.expr(e, s, build)
	}
	act := "nil"
	if action != nil && !g.cur.novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.lhs", "rhs": "c.rhs", "op": "c.op"}
		}
		if g.table == "trules" {
			g.final = action // re-ranged by tctx.result, as an action's result
		}
		act = fmt.Sprintf("func(c *%s) any { return %s }", g.ctxType(), g.term(action, s, locals))
		g.final = nil
	}
	check := ""
	if f := g.resultCheckFunc(g.cur, key); f != "" {
		check = ", check: " + f
	}
	return fmt.Sprintf("&%s{scope: %s, m: (*%s).%s, action: %s, value: %s%s}", g.pick("operatorsRuleLine", "toperatorsRuleLine"), goStrings(s.names), g.recv(), m, act, shaperLit(e), check)
}

// term converts a value expression into a Go expression of type any. locals maps names, such as lambda
// parameters, to Go expressions.
func (g *generator) term(t grammar.Term, s *scope, locals map[string]string) string {
	raw := g.termRaw(t, s, locals)
	if !g.goWorkControls {
		return raw
	}
	return "func() any { c.p.stepWork(); return " + raw + " }()"
}

func (g *generator) termRaw(t grammar.Term, s *scope, locals map[string]string) string {
	switch t := t.(type) {
	case *grammar.IntLit:
		return fmt.Sprintf("any(%d)", t.Value)
	case *grammar.StringLit:
		return fmt.Sprintf("any(%q)", t.Value)
	case *grammar.BoolLit:
		return fmt.Sprintf("any(%v)", t.Value)
	case *grammar.NilLit:
		return "any(nil)"
	case *grammar.CaptureRef:
		if l, ok := locals[t.Name]; ok {
			// Parameters and $lhs, $rhs and $op may hold boxes, which stand for their values.
			return g.pick("unboxValue(", "(") + l + ")"
		}
		if g.caps != nil {
			return g.caps[t.Name]
		}
		return fmt.Sprintf("c.cap(%d)", s.slots[t.Name])
	case *grammar.VarRef:
		return fmt.Sprintf("c.lookup(%q)", t.Name)
	case *grammar.Member:
		return fmt.Sprintf("c.member(%s, %q)", g.term(t.X, s, locals), t.Name)
	case *grammar.New:
		name, spec := g.prog.structType(t.Type)
		if g.table == "trules" {
			// The typed runtime calls the constructor of the type (tmk_T, see typedRuntime) with
			// the fields in declaration order.
			// The fields are evaluated in the order written (an error in one must win over the
			// next), then passed in declaration order.
			var b strings.Builder
			b.WriteString("func() any { ")
			vars := map[string]string{}
			for _, given := range t.Fields {
				v := g.name("f_")
				vars[given.Name] = v
				fmt.Fprintf(&b, "%s := %s; ", v, g.checkedField(spec, name, given, g.term(given.Value, s, locals)))
			}
			args := []string{"c", strconv.FormatBool(grammar.Term(t) == g.final)}
			for _, f := range spec.Fields {
				if v, ok := vars[f.Name]; ok {
					args = append(args, v)
				} else {
					args = append(args, "nil")
				}
			}
			b.WriteString("return tmk_" + name + "(" + strings.Join(args, ", ") + ") }()")
			return b.String()
		}
		args := []string{strconv.Quote(name)}
		for _, f := range t.Fields {
			args = append(args, strconv.Quote(f.Name), g.checkedField(spec, name, f, g.term(f.Value, s, locals)))
		}
		return "c.newStruct(" + strings.Join(args, ", ") + ")"
	case *grammar.ListConstructor:
		return g.term(&grammar.Call{Func: "list", Args: t.Items}, s, locals)
	case *grammar.TerminalConstructor:
		name := g.prog.terminalConstructorType(t.Type)
		if g.table == "trules" {
			if g.terminalConstructors == nil {
				g.terminalConstructors = map[string]bool{}
			}
			g.terminalConstructors[name] = true
			return "tmk_" + name + "(c, " + strconv.FormatBool(grammar.Term(t) == g.final) + ", " + g.term(t.Args[0], s, locals) + ")"
		}
		return "c.newTerminal(" + strconv.Quote(name) + ", " + g.term(t.Args[0], s, locals) + ")"
	case *grammar.Call:
		if t.Func == "concat" && fusable(t) {
			// Push the elements of list, map and concat arguments directly (see fusable and
			// evalCtx.pushConcatArg).
			var b strings.Builder
			b.WriteString("func() any { b := len(c.p.kidStack); ")
			g.concatParts(&b, t, s, locals)
			b.WriteString("return c.endList(b) }()")
			return b.String()
		}
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			if _, ok := a.(*grammar.Lambda); !ok {
				args[i] = g.term(a, s, locals)
			}
		}
		switch t.Func {
		case "len":
			return "c.length(" + args[0] + ")"
		case "text":
			return "c.text(" + args[0] + ")"
		case "foldl", "foldr":
			if g.goWorkControls {
				return fmt.Sprintf("c.fold(%q, %v, %s, %s, %s)", t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
			}
			return fmt.Sprintf("%sFold(%q, %v, %s, %s, %s)", g.rt(), t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
		case "map":
			if x, ok := g.projected(t, locals); ok {
				if g.goWorkControls {
					return "c.newList(c.listItems(\"map\", " + g.term(x, s, locals) + "))"
				}
				return "c.newList(" + g.pick("listItems", "tlistItems") + "(\"map\", " + g.term(x, s, locals) + "))"
			}
			return fmt.Sprintf("c.mapList(%s, %s)", args[0], g.lambda(t.Args[1], s, locals))
		case "filter":
			return fmt.Sprintf("c.filterList(%s, %s)", args[0], g.lambda(t.Args[1], s, locals))
		case "list":
			return "c.list(" + strings.Join(args, ", ") + ")"
		case "concat":
			return "c.concat(" + strings.Join(args, ", ") + ")"
		}
	case *grammar.Binary:
		if lx, ok := textArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
			if rx, ok := textArg(t.R); ok {
				// Compare the strings directly (see evalCtx.binary).
				if g.goWorkControls {
					return fmt.Sprintf("any(c.p.textEqual(c.textStr(%s), c.textStr(%s)) == %v)", g.term(lx, s, locals), g.term(rx, s, locals), t.Op == "==")
				}
				return fmt.Sprintf("any(c.textStr(%s) %s c.textStr(%s))", g.term(lx, s, locals), t.Op, g.term(rx, s, locals))
			}
		}
		if g.table == "trules" && comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) {
			// Compare string fields of typed values without boxing them (see tctx.compare).
			return fmt.Sprintf("c.compare(%q, %s, %s)", t.Op, g.operand(t.L, s, locals), g.operand(t.R, s, locals))
		}
		l, r := g.term(t.L, s, locals), g.term(t.R, s, locals)
		switch t.Op {
		case "&&":
			if g.goWorkControls {
				return fmt.Sprintf("c.and(%s, func() any { return %s })", l, r)
			}
			return fmt.Sprintf("%sAnd(%s, func() any { return %s })", g.rt(), l, r)
		case "||":
			if g.goWorkControls {
				return fmt.Sprintf("c.or(%s, func() any { return %s })", l, r)
			}
			return fmt.Sprintf("%sOr(%s, func() any { return %s })", g.rt(), l, r)
		}
		if g.goWorkControls {
			return fmt.Sprintf("c.binary(%q, %s, %s)", t.Op, l, r)
		}
		return fmt.Sprintf("%sBinary(%q, %s, %s)", g.rt(), t.Op, l, r)
	case *grammar.Cast:
		x := g.term(t.X, s, locals)
		ft := g.prog.resolveCheck(t.Type)
		if ft == nil {
			return x
		}
		v := g.fieldTypeVar(t, ft, func(v string, t *fieldType) string { return fmt.Sprintf("var %s = %s\n", v, goFieldType(t)) })
		return fmt.Sprintf("c.cast(%s, %s)", x, v)
	case *grammar.Unary:
		if g.goWorkControls {
			return fmt.Sprintf("c.unary(%q, %s)", t.Op, g.term(t.X, s, locals))
		}
		return fmt.Sprintf("%sUnary(%q, %s)", g.rt(), t.Op, g.term(t.X, s, locals))
	}
	panic(fmt.Sprintf("unsupported term %T", t))
}

// literalTextMembership recognizes a side-effect-free disjunction of text
// equality checks against one captured value. Restricting the capture to a
// direct reference makes evaluating text once observably equivalent to the
// original ordered comparisons.
func literalTextMembership(t grammar.Term) (string, []string, bool) {
	var name string
	var literals []string
	var walk func(grammar.Term) bool
	walk = func(t grammar.Term) bool {
		if b, ok := t.(*grammar.Binary); ok && b.Op == "||" {
			return walk(b.L) && walk(b.R)
		}
		b, ok := t.(*grammar.Binary)
		if !ok || b.Op != "==" {
			return false
		}
		var capture *grammar.CaptureRef
		var literal *grammar.StringLit
		if x, ok := textArg(b.L); ok {
			capture, _ = x.(*grammar.CaptureRef)
			literal, _ = b.R.(*grammar.StringLit)
		}
		if capture == nil || literal == nil {
			if x, ok := textArg(b.R); ok {
				capture, _ = x.(*grammar.CaptureRef)
				literal, _ = b.L.(*grammar.StringLit)
			}
		}
		if capture == nil || literal == nil || (name != "" && name != capture.Name) {
			return false
		}
		name = capture.Name
		literals = append(literals, literal.Value)
		return true
	}
	if !walk(t) || len(literals) < 2 {
		return "", nil, false
	}
	return name, literals, true
}

func (g *generator) predicateTerm(t grammar.Term, s *scope, locals map[string]string) string {
	if !g.disableLiteralTextDispatch {
		if name, literals, ok := literalTextMembership(t); ok {
			var cases []string
			seen := map[string]bool{}
			for _, literal := range literals {
				if !seen[literal] {
					seen[literal] = true
					cases = append(cases, strconv.Quote(literal))
				}
			}
			if g.goWorkControls {
				ordinary := g.term(t, s, locals)
				fast := fmt.Sprintf("func() any { switch c.textStr(%s) { case %s: return true; default: return false } }()", g.term(&grammar.CaptureRef{Name: name}, s, locals), strings.Join(cases, ", "))
				return "func() any { if c.p.control != nil { return " + ordinary + " }; return " + fast + " }()"
			}
			return fmt.Sprintf("func() any { switch c.textStr(%s) { case %s: return true; default: return false } }()", g.term(&grammar.CaptureRef{Name: name}, s, locals), strings.Join(cases, ", "))
		}
	}
	return g.term(t, s, locals)
}

// concatParts writes the statements that push the elements of the arguments of the concat call t.
func (g *generator) concatParts(b *strings.Builder, t *grammar.Call, s *scope, locals map[string]string) {
	for _, a := range t.Args {
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list":
			args := make([]string, len(call.Args))
			for i, x := range call.Args {
				args[i] = g.term(x, s, locals)
			}
			b.WriteString("c.pushList(" + strings.Join(args, ", ") + "); ")
		case ok && call.Func == "map" && len(call.Args) == 2:
			if x, ok := g.projected(call, locals); ok {
				b.WriteString("c.pushItems(" + g.term(x, s, locals) + "); ")
				break
			}
			fmt.Fprintf(b, "c.pushMap(%s, %s); ", g.term(call.Args[0], s, locals), g.lambda(call.Args[1], s, locals))
		case ok && call.Func == "concat":
			g.concatParts(b, call, s, locals)
		default:
			b.WriteString("c.pushItems(" + g.term(a, s, locals) + "); ")
		}
	}
}

func (g *generator) lambda(t grammar.Term, s *scope, locals map[string]string) string {
	l := t.(*grammar.Lambda)
	inner := map[string]string{}
	for k, v := range locals {
		inner[k] = v
	}
	params := make([]string, len(l.Params))
	for i, p := range l.Params {
		params[i] = g.name("l_")
		inner[p] = params[i]
	}
	return fmt.Sprintf("func(%s any) any { return %s }", strings.Join(params, ", "), g.term(l.Body, s, inner))
}

// controlledClassReject preserves the compact inactive condition while bounded
// membership uses immutable generated range tables. TS keeps its own runtime.
func (g *generator) controlledClassReject(e *grammar.CharClass) string {
	ordinary := classReject(e)
	if !g.goWorkControls || len(e.Ranges) <= 4 {
		return ordinary
	}
	key := fmt.Sprintf("%#v", e.Ranges)
	if g.workClassNames == nil {
		g.workClassNames = map[string]string{}
	}
	name := g.workClassNames[key]
	if name == "" {
		name = g.name("workClass")
		g.workClassNames[key] = name
		g.workClassKeys = append(g.workClassKeys, key)
		fmt.Fprintf(&g.vars, "var %s = []workRange{", name)
		for _, r := range e.Ranges {
			fmt.Fprintf(&g.vars, "{%d,%d},", r.Lo, r.Hi)
		}
		g.vars.WriteString("}\n")
	}
	return fmt.Sprintf("((workControlsEnabled && p.control != nil && p.rejectWorkClass(ch, %s, %v)) || ((!workControlsEnabled || p.control == nil) && (%s)))", name, e.Negated, ordinary)
}
