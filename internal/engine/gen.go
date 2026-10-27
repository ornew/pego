package engine

import (
	"fmt"
	"go/format"
	"go/token"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
)

// GenOptions configures code generation.
type GenOptions struct {
	// TypedOnly generates only the typed entry points (ParseAST, ParseRuleAST and their
	// siblings): no Parse, ParseRule or Recognize, and no function that builds an untyped Node.
	// It needs Types and excludes Recognize, Document and Stream.
	TypedOnly bool
	// WorkControls emits the work-controlled form of the generated Go functions, which charges
	// cancellation and the input and step limits (WithContext, WithWorkLimits), and re-exports
	// those options. Without it only the ordinary form is emitted, and a parse with a control
	// option fails with an error.
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
	// disableOrdinaryWorkSpecialization retains charged-method nil checks for
	// same-generator tests and measurements without a new parser-time selector.
	disableOrdinaryWorkSpecialization bool
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
	// keepNodes keeps the untyped Node rule functions of a typed-only parser. generate sets it
	// itself, when a typed entry point converts the Node result.
	keepNodes bool
	// keepUnreachable emits the bodies of the rules that no entry point or generated call reaches
	// (reach.go), as before they were left out, for differential tests and size comparisons.
	keepUnreachable bool
	// keepComments keeps the comments of unexported generated code, which tests read as route markers.
	keepComments bool
	// disableKeepNodes leaves the Node rule functions out of a typed-only parser that needs them
	// (a negative control for tests).
	disableKeepNodes bool
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
	// disableTerminalPrograms keeps value-free terminal expressions of lean typed parsers as
	// straight-line code (gen_terminal.go) for differential tests and measurements.
	disableTerminalPrograms bool
	// disableGuardedClasses makes the first class of an alternative read and test the character
	// that the guard of its choice has accepted (core.Alt.GuardedClass), for differential tests.
	disableGuardedClasses bool
	// boxedComparisons evaluates comparisons of fields as other binary operations, for tests.
	boxedComparisons bool
	// Independent generation-only controls for typed object and list slabs.
	// Reference parsers allocate each object or list separately.
	disableTypedObjectSlabs bool
	disableTypedListSlabs   bool
	// disableTypedOutputBootstrap retains the original slab first allocation
	// for same-generator retention and throughput comparisons.
	disableTypedOutputBootstrap bool
	// disableSparseMemo keeps the dense first-call bitset in TypeScript output
	// for same-generator comparisons without a parser-time selector.
	disableSparseMemo bool
}

// DefaultPackageName returns the package name of the generated code for a module path: its last
// element, without a trailing major version element (example.com/m/v2 gives m), as Go tooling
// names a package.
func DefaultPackageName(module string) string {
	base := path.Base(module)
	if dir := path.Dir(module); dir != "." && dir != "/" && len(base) >= 2 && base[0] == 'v' && base[1] != '0' && strings.Trim(base[1:], "0123456789") == "" {
		if base[1] != '1' || len(base) > 2 {
			return path.Base(dir)
		}
	}
	return base
}

// Generate generates the files of a Go parser for a grammar: parser.go with the code generated
// from the grammar, api.go, which re-exports the names the runtime defines for the users of the
// parser, with opts.Types types/types.go, the package of the Go types of the typed values, and, with
// opts.Runtime RuntimeEmbed, a copy of the runtime in internal/runtime. Paths are relative to the
// directory of the generated package.
//
// Eligible rule bodies inline their expressions; other expressions become Go functions. Actions
// and predicates become Go expressions, and the runtime (package runtime of this module) behaves
// exactly like the engine in this package. Without opts.Stream, #stream is a plain repetition.
func Generate(g *grammar.Grammar, opts GenOptions) ([]GeneratedFile, error) {
	if opts.Runtime == RuntimeOnly {
		return runtimeFiles()
	}
	return generate(g, opts)
}

// generatorHook, if set by a test, receives the generator of the pass that writes the output, before it
// prints, so that the test can build the Core IR of the same options (generator.typedCore).
var generatorHook func(*generator)

// generate generates the files of Generate (other than for RuntimeOnly). It generates twice: the
// first pass records which rules of the Node, recognizer and typed tables a generated call or an
// entry point can reach (liveRules), and the second leaves out the others. A typed-only parser
// without Node rules has no Node or recognizer rule to leave out (keepTableRules); one that keeps
// Node rules for its converting entry points prints those that the entry points reach, like any
// other output.
func generate(g *grammar.Grammar, opts GenOptions) ([]GeneratedFile, error) {
	if opts.keepUnreachable {
		return generateOnce(g, opts, nil, nil)
	}
	var lv liveRules
	if _, err := generateOnce(g, opts, nil, &lv); err != nil {
		return nil, err
	}
	if reachHook != nil {
		reachHook(&lv)
	}
	return generateOnce(g, opts, &lv, nil)
}

// generateOnce is one pass of generate. A pass with collect records the reachable rules into it;
// a pass with live omits the rules that are not reachable in live.
func generateOnce(g *grammar.Grammar, opts GenOptions, live, collect *liveRules) (files []GeneratedFile, err error) {
	if collect != nil {
		*collect = liveRules{}
	}
	if opts.Module == "" && (opts.Runtime == RuntimeEmbed || opts.Package == "") {
		return nil, fmt.Errorf("module path is required")
	}
	derived := opts.Package == ""
	if derived {
		opts.Package = DefaultPackageName(opts.Module)
	}
	if !token.IsIdentifier(opts.Package) || opts.Package == "_" {
		if derived {
			return nil, fmt.Errorf("the module path %q gives the package name %q, which is not a valid Go identifier; choose the package name with -pkg (WithPackageName)", opts.Module, opts.Package)
		}
		return nil, fmt.Errorf("invalid package name %q", opts.Package)
	}
	if opts.TypedOnly {
		switch {
		case !opts.Types:
			return nil, fmt.Errorf("typed-only output needs typed output (-types)")
		case opts.Recognize || opts.Document || opts.Stream:
			return nil, fmt.Errorf("typed-only output cannot have Recognize, a Document or a stream, which use the untyped Node")
		}
	}
	if opts.Types && opts.Module == "" {
		return nil, fmt.Errorf("module path is required for typed output: the parser imports the package module/types")
	}
	if err := opts.validateMaxDepth(false); err != nil {
		return nil, err
	}
	if err := opts.validateDefaultValue(); err != nil {
		return nil, err
	}
	info, err := loadRuntime()
	if err != nil {
		return nil, err
	}
	prog, err := Compile(g, Options{SourceInfo: opts.SourceInfo, DefaultValue: opts.DefaultValue})
	if err != nil {
		return nil, err
	}
	if prog.hasMissing {
		return nil, missingUnsupported("generated parsers")
	}
	start, err := prog.startRule(opts.Start)
	if err != nil {
		return nil, err
	}
	if opts.Stream {
		if !start.stream {
			return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", start.name)
		}
	}
	gen := &generator{typedOnly: opts.TypedOnly && !opts.keepNodes, observability: opts.Observability, document: opts.Document, goWorkControls: true, noWork: !opts.WorkControls && !opts.disableOrdinaryWorkSpecialization, ordinary: !opts.WorkControls && !opts.Document && !opts.disableOrdinaryWorkSpecialization, pairs: (opts.WorkControls || opts.Document) && !opts.disableOrdinaryWorkSpecialization, lean: !opts.WorkControls && !opts.Document, prog: prog, table: "rules", disableMethodSharing: opts.disableMethodSharing, disableLiteralSharing: opts.disableLiteralSharing, disableLiteralTextDispatch: opts.disableLiteralTextDispatch, disableTerminalPrograms: opts.disableTerminalPrograms, disableGuardedClasses: opts.disableGuardedClasses, boxedComparisons: opts.boxedComparisons, disableTypedObjectSlabs: opts.disableTypedObjectSlabs, disableTypedListSlabs: opts.disableTypedListSlabs, disableTypedOutputBootstrap: opts.disableTypedOutputBootstrap, live: live, collect: collect, keepTableRules: opts.TypedOnly && !opts.keepNodes}
	if live != nil {
		gen.seedDescs()
		for _, d := range live.descs[len(gen.descs):] {
			gen.descIDs[d] = expID(len(gen.descs))
			gen.descs = append(gen.descs, d)
		}
	}
	if opts.Stream {
		gen.streamStart = start
	}
	gen.desc(fixedDescs[0]) // the fixed expectations come first
	if generatorHook != nil && collect == nil {
		generatorHook(gen)
	}
	if err := gen.startRecFlow(prog); err != nil {
		return nil, err
	}
	gen.rules()
	gen.flow = nil
	if gen.streamErr != nil {
		return nil, gen.streamErr
	}
	if gen.tableErr != nil {
		return nil, gen.tableErr
	}
	if gen.flowErr != nil {
		return nil, gen.flowErr
	}
	var rec *Program
	if opts.Recognize {
		// The recognizer: the program in which no rule builds a value (Program.recognizer), in a
		// rule table of its own.
		if rec, err = prog.recognizer(); err != nil {
			return nil, err
		}
		gen.prog, gen.table = rec, "recRules"
		if err := gen.startRecFlow(rec); err != nil {
			return nil, err
		}
		gen.rules()
		gen.flow = nil
		gen.prog, gen.table = prog, "rules"
		if gen.tableErr != nil {
			return nil, gen.tableErr
		}
		if gen.flowErr != nil {
			return nil, gen.flowErr
		}
	}
	if collect != nil {
		// The expectations of the Node and recognizer tables are those the second pass keeps; the typed
		// code adds its own after them, and only for the rules it reaches.
		collect.descs = append([]string(nil), gen.descs...)
		if !opts.Types {
			return nil, gen.finishReach(prog, rec)
		}
		typed, err := gen.typedGo(start, opts.convertTypes)
		if err != nil {
			return nil, err
		}
		gen.convertsNodes = typed.needNodes
		if opts.TypedOnly && typed.needNodes && !opts.keepNodes && !opts.disableKeepNodes {
			opts.keepNodes = true
			return generateOnce(g, opts, live, collect)
		}
		return nil, gen.finishReach(prog, rec)
	}
	fmt.Fprintf(&gen.init, "\tpegoGrammar.Starts = %s\n", goStrings(prog.starts))
	fmt.Fprintf(&gen.init, "\tpegoGrammar.DefaultStart = %q\n", prog.defaultStart)
	if !opts.TypedOnly {
		fmt.Fprintf(&gen.init, "\tpegoGrammar.Descs = %s\n", goStrings(gen.descs))
	}
	if opts.Observability {
		gen.init.WriteString("\tpegoGrammar.Observable = true\n")
	}
	if !opts.WorkControls {
		gen.init.WriteString("\tpegoGrammar.NoWorkControls = true\n")
	}
	if prog.sourceInfo {
		gen.init.WriteString("\tpegoGrammar.SourceInfo = true\n")
	}
	if t := prog.vars; t != nil {
		var names, vals []string
		for _, d := range t.list {
			names = append(names, d.name)
			vals = append(vals, fmt.Sprintf("%#v", d.init))
		}
		fmt.Fprintf(&gen.init, "\tpegoGrammar.SetVars(%s, []any{%s})\n", goStrings(names), strings.Join(vals, ", "))
	}
	if rec != nil {
		gen.init.WriteString("\tpegoRecGrammar.Share(pegoGrammar)\n")
	}

	var out strings.Builder
	if opts.Observability && opts.Document {
		out.WriteString("// ErrDocumentBusy is returned by a Parse, ParseAST or Edit of a Document that\n// is already parsing, such as one made by a trace callback of that parse.\n// Schedule the work after the parse returns, or use another Document.\nvar ErrDocumentBusy = DocumentBusy\n\n")
	}
	out.WriteString("// --- Generated code ---\n\n")
	if !opts.TypedOnly {
		fmt.Fprintf(&out, "// Parse parses the whole input with the rule %s. Options select position units, depth, cancellation and work limits.\n", start.name)
		out.WriteString("// If the parse recovered from errors with #recover, it returns the node together with SyntaxErrors.\n")
		fmt.Fprintf(&out, "func Parse(input string, opts ...ParseOption) (*Node, error) { return RunParse(rules[%d], input, opts) }\n\n", start.id)
	}
	if opts.Stream {
		fmt.Fprintf(&out, "// ParseStream parses the input read from r with the rule %s, passing each committed element of\n", start.name)
		out.WriteString("// its #stream repetition to emit (nil if the element has no value) without retaining it. Committed\n// elements are final even if the parse fails later. A non-EOF error from r or an error returned by\n// emit ends the parse and is returned unchanged. A nil emit collects the repetition instead.\n")
		out.WriteString("func ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error {\n\treturn RunParseStream(streamRule, r, emit, opts)\n}\n\n")
		out.WriteString(`// Iterate returns an iterator over the elements that ParseStream would pass to emit, parsing only as
// far as each call to Next needs. Options are validated before it returns; r is first read by Next.
// The parser's lookahead and buffering may read ahead of the returned elements.
func Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error) {
	pull, err := IterateStream(pegoGrammar, opts, func(emit func(*Node) error) error { return RunParseStream(streamRule, r, emit, opts) })
	if err != nil {
		return nil, err
	}
	return NewStreamIterator(pull), nil
}

`)
	}
	if opts.Document {
		fmt.Fprintf(&out, "// NewDocument creates an incremental Node document for the start rule %s.\n", start.name)
		if opts.Types {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) {\n\td, err := MakeDocument(rules[%d], text, opts)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn &Document{d}, nil\n}\n\n", start.id)
		} else {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) { return MakeDocument(rules[%d], text, opts) }\n\n", start.id)
		}
	}
	if rec != nil {
		fmt.Fprintf(&out, "// Recognize checks that the whole input matches the rule %s without building a tree, and returns the\n", start.name)
		out.WriteString("// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover). Actions needed by\n// predicates are evaluated and can report runtime errors; other actions are skipped.\n")
		fmt.Fprintf(&out, "func Recognize(input string, opts ...ParseOption) error {\n\t_, err := RunParse(recRules[%d], input, opts)\n\treturn err\n}\n\n", rec.byName[start.name].id)
		fmt.Fprintf(&out, "// recRules is the rule table of Recognize, and pegoRecGrammar its grammar.\nvar recRules []*Rule\nvar pegoRecGrammar = NewGrammar(%d)\n\n", opts.generatedMaxDepth())
	}
	fmt.Fprintf(&out, "// rules is the rule table, and pegoGrammar its grammar.\nvar rules []*Rule\nvar pegoGrammar = NewGrammar(%d)\n\n", opts.generatedMaxDepth())
	if !opts.TypedOnly {
		out.WriteString(`// ParseRule parses the whole input with the start rule name, a rule marked @start; an empty
// name selects the grammar's default start rule.
func ParseRule(name, input string, opts ...ParseOption) (*Node, error) {
	o, err := ResolveOptions(pegoGrammar, opts)
	if err != nil {
		return nil, err
	}
	r, err := pegoGrammar.StartRule(name)
	if err != nil {
		return nil, err
	}
	return RunParseResolved(r, input, o)
}

`)
	}
	var typesBody string
	if opts.Types {
		typed, err := gen.typedGo(start, opts.convertTypes)
		if err != nil {
			return nil, err
		}
		gen.convertsNodes = typed.needNodes
		if opts.TypedOnly && typed.needNodes && !opts.keepNodes && !opts.disableKeepNodes {
			// A typed entry point converts the Node result of its rule, so the Node rule functions
			// are needed; generate again with them.
			opts.keepNodes = true
			return generateOnce(g, opts, live, collect)
		}
		if opts.TypedOnly {
			// The expectations of the typed bodies are known only now.
			fmt.Fprintf(&gen.init, "\tpegoGrammar.Descs = %s\n", goStrings(gen.descs))
		}
		out.WriteString("\n// --- Typed values ---\n\n")
		if opts.Document {
			out.WriteString("// Document is an incremental document: it parses its text again after edits, reusing the\n// results of the parse before that it can.\ntype Document struct{ *" + runtimeAlias + ".Document }\n\n")
		}
		out.WriteString(typed.parser)
		out.WriteString("\n")
		typesBody = typesPreamble + typed.types
	}
	if err := gen.finishReach(prog, rec); err != nil {
		return nil, err
	}
	out.WriteString(gen.vars.String())
	out.WriteString("\nfunc init() {\n")
	out.WriteString(gen.init.String())
	out.WriteString("}\n\n")
	out.WriteString(gen.methods.String())
	paired := out.String()
	methods := selectForm(gen.methods.String(), formControlled)
	typesBody = selectForm(typesBody, formControlled)
	whole, err := format.Source([]byte("package " + opts.Package + "\n" + selectForm(paired, formControlled)))
	if err != nil {
		return nil, fmt.Errorf("generated code does not compile: %w\n%s", err, selectForm(paired, formControlled))
	}
	if !opts.disableOrdinaryWorkSpecialization && !gen.ordinary {
		// A parser without work controls and Documents prints its bodies in the ordinary form only
		// (generator.ordinary); the others get a branch for it.
		guard := "p.Control == nil"
		switch {
		case !opts.WorkControls:
			guard = "p.Document == nil"
		case opts.Document:
			guard += " && p.Document == nil"
		}
		if whole, err = spliceOrdinaryBodies(whole, opts.Package, paired, methods, guard, !opts.WorkControls); err != nil {
			return nil, err
		}
	}
	body := shakeVars(strings.TrimPrefix(string(whole), "package "+opts.Package+"\n"))
	var tp *typesPackage
	var typesFile []byte
	if opts.Types {
		if typesFile, tp, err = opts.typesFile(typesBody); err != nil {
			return nil, err
		}
	}
	body, err = qualifyRuntime(opts.runtimeImport(), body, info, tp)
	if err != nil {
		return nil, err
	}
	var file strings.Builder
	file.WriteString(generatedHeader + "\n\n")
	if !opts.OmitPackageDoc {
		fmt.Fprintf(&file, "// Package %s is a parser generated from a PEGO grammar.\n", opts.Package)
	}
	fmt.Fprintf(&file, "package %s\n\n%s", opts.Package, body)
	src, err := format.Source([]byte(file.String()))
	if err != nil {
		return nil, fmt.Errorf("generated code does not compile: %w", err)
	}
	if !opts.keepComments {
		if src, err = trimComments(src); err != nil {
			return nil, fmt.Errorf("generated code does not compile: %w", err)
		}
	}
	api, err := opts.apiSource(info)
	if err != nil {
		return nil, err
	}
	files = []GeneratedFile{{"parser.go", src}, {"api.go", []byte(api)}}
	if opts.Types {
		files = append(files, GeneratedFile{"types/types.go", typesFile})
	}
	if opts.Runtime == RuntimeEmbed {
		rt, err := runtimeFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, rt...)
	}
	return files, nil
}

// validateDefaultValue rejects typed output with default values: the types come from the actions and
// declarations that default values drop.
func (opts GenOptions) validateDefaultValue() error {
	if opts.DefaultValue && opts.Types {
		return fmt.Errorf("typed output is not available with default Values: the types come from the actions and declared types that default values drop")
	}
	return nil
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
	fmt.Fprintf(&b, "&FieldType{Kind: %q, Text: %q", rune(t.kind), t.text)
	if t.name != "" {
		fmt.Fprintf(&b, ", Name: %q", t.name)
	}
	if t.elem != nil {
		b.WriteString(", Elem: " + goFieldType(t.elem))
	}
	if len(t.alts) > 0 {
		alts := make([]string, len(t.alts))
		for i, a := range t.alts {
			alts[i] = goFieldType(a)
		}
		b.WriteString(", Alts: []*FieldType{" + strings.Join(alts, ", ") + "}")
	}
	return b.String() + "}"
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
	observability bool
	// goWorkControls emits work-control charging; Go output always has it, TypeScript output has
	// its own runtime work controls.
	goWorkControls bool
	// noWork leaves the step charges out of Go output generated without work controls, where
	// no control exists to charge: the statements, and the closures that only held one.
	noWork bool
	// ordinary prints the bodies in their ordinary form only (no work controls and no Documents).
	ordinary bool
	// pairs prints the fragments of gen_form.go: the parser has work controls or Documents and its
	// execution functions get a branch for ordinary parses.
	pairs               bool
	directTypedDocument bool
	document            bool
	workClassNames      map[string]string
	workClassKeys       []string
	shaperNames         map[string]string
	shaperKeys          []string
	recoveryProg        map[*Program]bool // whether a program can recover from errors
	tableErr            error             // the rule table does not fit the form of runtime.RuleSpec
	// coreHook, if set, is called by typedCore with the module after it is built and after each pass.
	coreHook func(pass string, m *core.Module)
	flowErr  error // the first rule, line or stream body that Flow IR does not print, or the first internal error of the unread-variable check
	// lean is set when the output has no work controls and no Documents, so each typed rule body
	// is its ordinary form and may call the runtime's ordinary-parse helpers directly.
	lean                  bool
	disableMethodSharing  bool
	disableLiteralSharing bool
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames                 map[generatedMethod]string
	literalNames                map[string]string
	methodKeys                  []generatedMethod
	literalKeys                 []string
	disableLiteralTextDispatch  bool
	disableTerminalPrograms     bool
	disableGuardedClasses       bool
	boxedComparisons            bool
	disableTypedObjectSlabs     bool
	disableTypedListSlabs       bool
	disableTypedOutputBootstrap bool
	// flow holds the Core IR of the typed rules while they are printed from Flow IR, or nil;
	// flowRules counts the rules printed from Flow IR as direct rules and flowFrames the bodies
	// and lines printed as matchers of runtime-owned frames.
	flow       *flowState
	flowRules  int
	flowFrames int
	// nodeScopes records the frame layouts shared by typed rule metadata.
	nodeScopes map[*rule][]string
	// typedOnly leaves out the untyped Node family: the rule table keeps only the metadata the
	// typed table shares.
	typedOnly bool
	prog      *Program
	table     string // the rule table being generated: rules, or recRules for Recognize
	cur       *rule  // the rule being generated
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
	// descs is the expectation table of the generated parser (descs in runtime); descIDs maps a
	// description to its ID.
	descs   []string
	descIDs map[string]expID
	// streamStart is the start rule of ParseStream, whose separate body
	// (generated while streamBody is set) emits its #stream elements.
	streamStart *rule
	streamBody  bool
	streamErr   error
	// plainCalled holds the plain rules that a generated call refers to by their wrapper
	// (ruleCall). wrappers are the methods of the plain rules, which dropUnusedWrappers keeps only
	// for the rules in plainCalled.
	plainCalled map[*rule]bool
	wrappers    []wrapperSpan
	// live and collect are the passes of generate (reach.go): live leaves out the unreachable
	// rules of the Node and recognizer tables, collect records the calls that reach them.
	live, collect *liveRules
	// keepTableRules marks a typed-only parser without Node rules: its Node and recognizer tables
	// have no rule bodies to check; the typed table leaves its unreached rules out as usual.
	keepTableRules bool
	// convertsNodes reports that a typed entry point converts the Node result of its rule, which
	// needs the Node rule bodies.
	convertsNodes bool
	// typedRoots are the typed rules whose Entry a typed entry point runs, and typedEntry their IDs.
	typedRoots []ruleKey
	typedEntry map[int]bool
	emitting   ruleKey
	emittingOK bool
	bodySet    map[ruleKey]bool
	reachErr   error
	// fieldTypes names the field type variable of each checked constructed field, result and cast (by
	// *grammar.FieldInit or checkInfo.results key), numbered separately so that checks do not
	// rename other generated names (runtime/fieldcheck.go).
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

// seedDescs starts the table of expectation descriptions with the fixed ones.
func (g *generator) seedDescs() {
	if g.descIDs != nil {
		return
	}
	g.descIDs = map[string]expID{}
	for i, d := range fixedDescs {
		g.descs = append(g.descs, d)
		g.descIDs[d] = expID(i)
	}
}

// desc returns the expectation ID of description s, adding it to the table.
func (g *generator) desc(s string) expID {
	g.seedDescs()
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

// ruleTableLimits reports whether a rule table with nstrs lists of strings and nseen deferred
// memoization indexes fits the fields of runtime.RuleSpec (Scope and Vars are 16 bits, Seen 32).
func ruleTableLimits(nstrs, nseen int) error {
	if nstrs > math.MaxUint16+1 {
		return fmt.Errorf("the rule table needs %d distinct capture or variable lists, more than %d", nstrs, math.MaxUint16+1)
	}
	if nseen > math.MaxInt32 {
		return fmt.Errorf("the rule table needs %d deferred memoization indexes, more than %d", nseen, math.MaxInt32)
	}
	return nil
}

// saveRec returns the variables that a backtracking point saves, start and, if the program can
// recover, rec, followed by more; saveRecValues their values. restoreRec returns the statement that
// goes back to them.
func (g *generator) saveRec(more string) string {
	if g.recovers() {
		return "start, rec" + more
	}
	return "start" + more
}

func (g *generator) saveRecValues(more string) string {
	if g.recovers() {
		return "p.Pos, len(p.Recovered)" + more
	}
	return "p.Pos" + more
}

func (g *generator) restoreRec(pos, rec string) string {
	if g.recovers() {
		return fmt.Sprintf("p.Restore(%s, %s)", pos, rec)
	}
	return fmt.Sprintf("p.Pos = %s", pos)
}

// recovers reports whether a parse of the program being generated can append to p.Recovered: only
// recovery (#recover) does. Without it p.Recovered stays empty, so
// the positions that backtracking restores need no saved length of it.
func (g *generator) recovers() bool {
	if v, ok := g.recoveryProg[g.prog]; ok {
		return v
	}
	found := false
	check := func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Recovery:
			found = true
		case *grammar.Attributed:
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					found = true
				}
			}
		}
	}
	for _, rs := range [][]*rule{g.prog.rules, g.prog.twins} {
		for _, r := range rs {
			walkExpr(r.def.Expr, check)
			walkExpr(r.bodyExpr(), check)
		}
	}
	if g.recoveryProg == nil {
		g.recoveryProg = map[*Program]bool{}
	}
	g.recoveryProg[g.prog] = found
	return found
}

// directRouteHook is called with the rule table and the rule for each rule that takes the lean route
// in rules, in a Node or recognizer table. Tests set it to check the route that is chosen, not the text
// of the generated code.
var directRouteHook func(table string, r *rule)

// framedRouteHook is called with the rule table and the rule for each rule whose framed body is printed
// from Flow IR (flowNodeFrameBody), in a Node or recognizer table. Tests set it as directRouteHook.
var framedRouteHook func(table string, r *rule)

// operatorsRouteHook is called with the rule table and the Operators Rule for each line of a Node or
// recognizer table that is printed from Flow IR. Tests set it as directRouteHook.
var operatorsRouteHook func(table string, r *rule)

// streamRouteHook is called with the rule table and the rule for each ParseStream start rule whose
// stream body is printed from Flow IR (flowStreamBody). Tests set it as directRouteHook.
var streamRouteHook func(table string, r *rule)

func (g *generator) rules() {
	init := &g.init
	methodsBefore := g.methods.String()
	bodies := map[*rule]string{}
	scopes := map[*rule]*scope{}
	directs := map[*rule]*direct{}
	all := append(append([]*rule(nil), g.prog.rules...), g.prog.twins...)
	for _, r := range all {
		s := newScope()
		scopes[r] = s
		if g.pruned(r) {
			// Not printed; its table row keeps the capture layout of the first pass.
			s.names = append(s.names, g.live.scopes[ruleKey{g.table, r.id, false}]...)
			continue
		}
		g.beginRule(r)
		if g.table == "trules" {
			// Typed direct rules need no separate body; Flow IR prints every typed rule.
			if d := g.flowDirectRule(r); d != nil {
				directs[r] = d
				continue
			}
		} else if recFlowEligible(r) {
			// flowRecDirect returns nil for a rule whose printed function would read its start or context, and
			// for one whose types it cannot name; such a rule takes its framed body below and is called through
			// the rule table (plainCall).
			if d := g.flowRecDirect(r); d != nil {
				if directRouteHook != nil && g.collect == nil {
					directRouteHook(g.table, r)
				}
				directs[r] = d
				if g.slotReached(r) {
					bodies[r] = g.directLeanBody(r, d)
				}
				continue
			}
		}
		if _, ok := r.def.Expr.(*grammar.OperatorsRule); !ok {
			if g.table != "trules" {
				if body := g.flowNodeFrameBody(r, s); body != "" {
					if framedRouteHook != nil && g.collect == nil {
						framedRouteHook(g.table, r)
					}
					bodies[r] = body
					continue
				}
			} else if body := g.flowFrameBody(r, s); body != "" {
				bodies[r] = body
				continue
			}
			g.failFlow("the body of rule " + r.name)
		}
	}
	g.endRule()
	if g.collect != nil && g.table != "trules" {
		if g.collect.scopes == nil {
			g.collect.scopes = map[ruleKey][]string{}
		}
		for _, r := range all {
			g.collect.scopes[ruleKey{g.table, r.id, false}] = append([]string(nil), scopes[r].names...)
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
		init.WriteString("\ttrules = NewTRules(rules)\n\tdefer tinit()\n")
		g.methods.WriteString("// trules is the typed rule table.\nvar trules []*TRule\n\n")
	} else {
		// The table is static data of a compact form (RuleSpec); the init function builds the rules.
		var tab strings.Builder
		strs := []string{"nil"}
		strIdx := map[string]int{"nil": 0}
		strsOf := func(xs []string) int {
			if len(xs) == 0 {
				return 0
			}
			lit := goStrings(xs)
			if k, ok := strIdx[lit]; ok {
				return k
			}
			strIdx[lit] = len(strs)
			strs = append(strs, strings.TrimPrefix(lit, "[]string"))
			return strIdx[lit]
		}
		var extras []string
		for i, r := range all {
			if r.id != i {
				panic("rule ids are not positions in the rule table")
			}
			var fields []string
			fields = append(fields, fmt.Sprintf("Name: %q", r.name))
			if _, ok := r.def.Expr.(*grammar.OperatorsRule); !ok { // the lines build the value of an Operators Rule
				fields = append(fields, "Value: &"+g.shaper(r.bodyExpr()))
			}
			var flags []string
			if r.memo && !r.transient {
				flags = append(flags, "RuleMemo")
			}
			if r.leader {
				flags = append(flags, "RuleLeader")
			}
			if r.novalue {
				flags = append(flags, "RuleNovalue")
			}
			if g.document && (r.leader || r.memo) {
				flags = append(flags, "RuleDocMemo")
			}
			if r.splice {
				flags = append(flags, "RuleSplice")
			}
			if len(flags) > 0 {
				fields = append(fields, "Flags: "+strings.Join(flags, " | "))
			}
			if r.seen != 0 {
				fields = append(fields, fmt.Sprintf("Seen: %d", r.seen))
			}
			if k := strsOf(scopes[r].names); k != 0 {
				fields = append(fields, fmt.Sprintf("Scope: %d", k))
			}
			if k := strsOf(r.vars); k != 0 {
				fields = append(fields, fmt.Sprintf("Vars: %d", k))
			}
			fmt.Fprintf(&tab, "\t{%s},\n", strings.Join(fields, ", "))
			w := r.whereName()
			if w == r.name {
				w = ""
			}
			if w != "" || r.nodeType != "" || r.opNodeType != "" {
				extras = append(extras, fmt.Sprintf("\t{Id: %d, Where: %q, NodeType: %q, OpNodeType: %q},\n", r.id, w, r.nodeType, r.opNodeType))
			}
		}
		if err := ruleTableLimits(len(strs), g.prog.nseen); err != nil {
			g.tableErr = err
		}
		spec, strTable, extraTable := g.table+"Spec", g.table+"Strs", g.table+"Extras"
		fmt.Fprintf(&g.vars, "// %s is the compact form of the rule table, built by NewRules.\nvar %s = [...]RuleSpec{\n%s}\n\n", spec, spec, tab.String())
		fmt.Fprintf(&g.vars, "var %s = [...][]string{\n", strTable)
		for _, x := range strs {
			if x == "nil" {
				g.vars.WriteString("\tnil,\n")
			} else {
				fmt.Fprintf(&g.vars, "\t%s,\n", x)
			}
		}
		g.vars.WriteString("}\n\n")
		fmt.Fprintf(&g.vars, "var %s = [...]RuleExtra{\n%s}\n\n", extraTable, strings.Join(extras, ""))
		fmt.Fprintf(init, "\t%s = NewRules(%s[:], %s[:], %s[:])\n", g.table, spec, strTable, extraTable)
		fmt.Fprintf(init, "\t%s.SetRules(%s, %d)\n", g.grammarVar(), g.table, g.prog.nseen)
	}
	nodeCode := g.table == "rules" && g.typedOnly
	if nodeCode {
		// The Node bodies were generated for the frame layouts they fix; their methods are not
		// kept, and the typed rules need none of the Node entries.
		g.methods.Reset()
		g.methods.WriteString(methodsBefore)
	}
	for _, r := range all {
		if nodeCode {
			break
		}
		if g.pruned(r) {
			continue
		}
		g.beginRule(r)
		if pr, ok := r.def.Expr.(*grammar.OperatorsRule); ok {
			g.cur = r
			g.operatorsRule(r, pr)
			continue
		}
		if d := directs[r]; d != nil && g.table == "trules" {
			g.typedCall(r, "", scopes[r], d)
			continue
		}
		if g.slotReached(r) {
			fmt.Fprintf(init, "\t%s[%d].Body = func(p *%s, _ int) (%s, bool) { return %s(p) }\n", g.table, r.id, g.recv(), g.valType(), bodies[r])
			g.printedBody(r)
		}
		if r.action != nil && g.table == "trules" {
			// A function of its own, which typedCall calls directly.
			g.proj, g.final = g.projections(r), r.action
			fmt.Fprintf(&g.methods, "// The action of %s.\nfunc ta%d(c *TActCtx) any { return %s }\n\n", r.name, r.id, g.term(r.action, scopes[r], nil))
			g.proj, g.final = nil, nil
			fmt.Fprintf(init, "\ttrules[%d].Action = ta%d\n", r.id, r.id)
		} else if r.action != nil {
			g.proj = g.projections(r)
			fmt.Fprintf(init, "\t%s[%d].Action = func(c *%s) any { return %s }\n", g.table, r.id, g.ctxType(), g.term(r.action, scopes[r], nil))
			g.proj = nil
		}
		if g.table == "trules" {
			g.typedCall(r, bodies[r], scopes[r], nil)
		} else if r.plain {
			start := g.methods.Len()
			if d := directs[r]; d != nil {
				g.directMethod(r, d, fmt.Sprintf("%s%d", g.plainPrefix(), r.id), "called as by invokePlain", true)
			} else {
				g.plainCall(r, bodies[r])
			}
			g.wrappers = append(g.wrappers, wrapperSpan{r: r, start: start, end: g.methods.Len()})
		}
	}
	g.endRule()
	if r := g.streamStart; r != nil && g.table == "rules" {
		// ParseStream calls a copy of the start rule whose body emits the
		// #stream elements, generated last so that the batch code is the same
		// as without streams. Its captures have a layout of their own.
		s := newScope()
		body := g.flowStreamBody(r, s)
		if body != "" {
			if streamRouteHook != nil && g.collect == nil {
				streamRouteHook(g.table, r)
			}
		} else {
			g.failFlow("the stream body of rule " + r.name)
		}
		if strings.Join(s.names, "\x00") != strings.Join(scopes[r].names, "\x00") {
			// The copied action addresses captures by the batch body's slots.
			g.streamErr = fmt.Errorf("internal error: the stream body of %s has a different capture layout", r.name)
		}
		fmt.Fprintf(init, "\tstreamRule = new(Rule)\n\t*streamRule = *rules[%d]\n\tstreamRule.Scope = %s\n\tstreamRule.Body = func(p *Parser, _ int) (*Node, bool) { return %s(p) }\n", r.id, goStrings(s.names), body)
		g.methods.WriteString("// streamRule is the start rule of ParseStream.\nvar streamRule *Rule\n\n")
	}
	g.dropUnusedWrappers()
	if g.table != "rules" {
		return // the recognizer shares the struct fields
	}
	// Struct fields, used to detect references to undeclared fields at run time.
	var names []string
	for name, spec := range g.prog.types {
		if td, ok := spec.(*grammar.TypeDef); ok && td.Struct() != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var fields []string
		for _, f := range defFields(g.prog.types[name].(*grammar.TypeDef)) {
			fields = append(fields, fmt.Sprintf("%q: true", f.Name))
		}
		fmt.Fprintf(init, "\tpegoGrammar.StructFields[%q] = map[string]bool{%s}\n", name, strings.Join(fields, ", "))
		fmt.Fprintf(init, "\tpegoGrammar.TypeKinds[%q] = KindOf(%q, \"\")\n", name, name)
	}
	// The kinds of the named node types, under their names and those of their aliases.
	names = names[:0]
	for name := range g.prog.nodeKinds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(init, "\tpegoGrammar.NodeTypes[%q] = NewNodeTypeKinds(%q)\n", name, g.prog.nodeKinds[name][0].typ)
	}
}

// nodeTypeNames returns the names of the grammar's named node types, sorted, in the order of their
// generated indexes from 1 (nodeTypeIndex).
func (g *generator) nodeTypeNames() []string {
	var names []string
	for name, spec := range g.prog.types {
		if td, ok := spec.(*grammar.TypeDef); ok && td.Struct() == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// nodeTypeIndex returns the generated index of the named node type name, from 1.
func (g *generator) nodeTypeIndex(name string) int {
	n, ok := g.prog.nodeTypeOf(name)
	if !ok {
		panic("internal error: " + name + " is not a named node type")
	}
	return sort.SearchStrings(g.nodeTypeNames(), n.name) + 1
}

// nodeTypeID returns the Go expression of the registered index of the named node type name in the
// typed runtime (nt in tmatch and tnode).
func (g *generator) nodeTypeID(name string) string {
	return fmt.Sprintf("%s[%d]", nodeTypesName, g.nodeTypeIndex(name))
}

// typedResultConversion returns the statement that converts the value v of rule r to the rule's
// named node type in the typed runtime (what finish does), or "" if r has none. body, if not "",
// holds the fresh node of the rule's body (tfreshNode), which converts in place.
func (g *generator) typedResultConversion(r *rule, v, body string) string {
	if r.nodeType == "" {
		return ""
	}
	if body != "" {
		return fmt.Sprintf("%s = p.TConvertResultOwned(trules[%d], %s, %s)", v, r.id, v, body)
	}
	return fmt.Sprintf("%s = p.TConvertResult(trules[%d], %s)", v, r.id, v)
}

// ownsBody reports whether finishing the value of rule r in the typed runtime converts the fresh
// node of its body in place: the rule has a named node type and returns its body's value.
func (g *generator) ownsBody(r *rule) bool {
	return r.nodeType != "" && !r.novalue && r.action == nil
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
	if g.typedEntry[r.id] {
		// Only a typed entry point runs an Entry; every other call names the function.
		fmt.Fprintf(&g.init, "\ttrules[%d].Entry = %s\n", r.id, entry)
	}
	ncaps := len(s.names)
	if d != nil {
		ncaps = len(r.scope.names)
	}
	finish := func(frame string) {
		body := ""
		if g.ownsBody(r) {
			body = "body"
			m.WriteString("\t\tbody := TFreshNode(v)\n")
		}
		switch {
		case r.novalue:
			m.WriteString("\t\tv = nil\n")
		case r.action != nil:
			fmt.Fprintf(m, "\t\tc := p.UseCtx(TActCtx{P: p, F: %s, Start: start, End: p.Pos, Cbase: len(p.Created)})\n", frame)
			fmt.Fprintf(m, "\t\tv = c.Result(ta%d, %q)\n", r.id, r.whereName())
		default:
			fmt.Fprintf(m, "\t\tv = p.ScopeValue(trules[%d].Value, v, start)\n", r.id)
			if len(s.names) > 0 {
				fmt.Fprintf(m, "\t\tv = p.AttachCaptures(v, trules[%d].Scope, %s.Vals, start, p.Pos)\n", r.id, frame)
			}
			m.WriteString("\t\tif n := AsTval(v); n != nil {\n\t\t\tn.TSetFresh(false)\n\t\t}\n")
		}
		if conv := g.typedResultConversion(r, "v", body); conv != "" {
			fmt.Fprintf(m, "\t\t%s\n", conv)
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
		fmt.Fprintf(m, "// %s, called as by invokePlain\nfunc %s(p *TParser) (any, bool) {\n", r.name, name)
		m.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n\tprevEnv, prevCut := p.Env, p.Cut\n\tp.Cut = false\n\tp.Enter()\n")
		if g.observability && (!r.plain || g.directTypedDocument) {
			m.WriteString("p.Observation.Evaluated()\n")
		}
		fmt.Fprintf(m, "\tv, ok := %s(p)\n\tp.Depth--\n\tp.Cut = prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body)
		finish("EmptyTFrame")
		m.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n\tp.Env = prevEnv\n\treturn v, ok\n}\n\n")
	}
	if r.plain {
		if g.directTypedDocument {
			name := fmt.Sprintf("s%d", r.id)
			if g.observability {
				name = g.traceCallWrapper(r, name, false)
			}
			fmt.Fprintf(m, "func %s(p *TParser) (any, bool) {\n if p.TypedDocument != nil { return p.DocumentCall(trules[%d], 0, ds%d) }; return ds%d(p)\n}\n", name, r.id, r.id, r.id)
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
	fmt.Fprintf(m, "// rule %s\nfunc %s(p *TParser) (any, bool) {\n", r.name, name)
	// Without Documents the invocation of a direct rule without captures is its plain method: the
	// two bodies differ only in the rollback to the start on failure, which the callers of the
	// invocation repeat harmlessly, so one body serves both. A Document reads the position the
	// invocation leaves at its failure to find the frontier it examined, so Documents keep both.
	merged := d != nil && ncaps == 0 && !g.document
	iname := fmt.Sprintf("i%d", r.id)
	if merged {
		iname = fmt.Sprintf("v%d", r.id)
	}
	if g.directTypedDocument {
		fmt.Fprintf(m, " if p.TypedDocument != nil { return p.DocumentCall(trules[%d], 0, %s) }\n", r.id, iname)
	}
	memo := r.memo && !r.transient
	if memo {
		fmt.Fprintf(m, "\tif !p.FirstCall(rules[%d]) {\n\t\treturn p.CallMemo(trules[%d], %s)\n\t}\n", r.id, r.id, iname)
	}
	if ncaps == 0 {
		fmt.Fprintf(m, "\treturn v%d(p)\n}\n\n", r.id)
		plain(fmt.Sprintf("v%d", r.id))
	} else {
		fmt.Fprintf(m, "\t%s := %s\n\tv, ok := i%d(p)\n\tif !ok {\n\t\t%s\n\t}\n\treturn v, ok\n}\n\n", g.saveRec(""), g.saveRecValues(""), r.id, g.restoreRec("start", "rec"))
	}
	// i<id>: the invocation (also used by the memo).
	if merged {
		return // v<id>
	}
	if d != nil {
		g.directMethod(r, d, fmt.Sprintf("i%d", r.id), "invoked as by invoke", false)
		return
	}
	fmt.Fprintf(m, "// %s, invoked as by invoke\nfunc i%d(p *TParser) (any, bool) {\n", r.name, r.id)
	fmt.Fprintf(m, "\tf := p.NewFrame(%d)\n\tprevFrame, prevEnv, prevCut, trail := p.Frame, p.Env, p.Cut, len(p.Trail)\n\tp.Frame, p.Cut = f, false\n\tp.Enter()\n\tstart := p.Pos\n\t_ = start\n", len(s.names))
	if g.observability {
		m.WriteString("p.Observation.Evaluated()\n")
	}
	fmt.Fprintf(m, "\tv, ok := %s(p)\n\tp.Depth--\n\tp.Frame, p.Cut = prevFrame, prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body)
	finish("f")
	m.WriteString("\t}\n\tp.Env = prevEnv\n\tp.FreeFrame(f)\n\treturn v, ok\n}\n\n")
}

// wrapperSpan is the span [start, end) of the methods of a plain rule's wrapper in generator.methods.
type wrapperSpan struct {
	r          *rule
	start, end int
}

// dropUnusedWrappers removes the wrappers of the plain rules that no generated call refers to. It
// runs after the bodies and the stream body, which are the last generated calls, so that a wrapper
// is kept exactly when a call of it was generated. Spans are removed from the last one to the first,
// so the earlier spans keep their offsets.
func (g *generator) dropUnusedWrappers() {
	text := g.methods.String()
	for i := len(g.wrappers) - 1; i >= 0; i-- {
		w := g.wrappers[i]
		if !strings.HasPrefix(text[w.start:w.end], "// ") || !strings.HasSuffix(text[w.start:w.end], "}\n\n") {
			panic(fmt.Sprintf("internal error: the wrapper of %s is not at its recorded span", w.r.name))
		}
		if !g.plainCalled[w.r] {
			text = text[:w.start] + text[w.end:]
		}
	}
	g.wrappers = nil
	g.methods.Reset()
	g.methods.WriteString(text)
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

func (g *generator) recv() string    { return g.pick("Parser", "TParser") }
func (g *generator) valType() string { return g.pick("*Node", "any") }
func (g *generator) ctxType() string { return g.pick("ActCtx", "TActCtx") }

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

// operand returns an operand (TOperand) of a comparison: a field read keeps a string field
// unboxed.
func (g *generator) operand(t grammar.Term, s *scope, locals map[string]string) string {
	if fieldRead(t) {
		m := t.(*grammar.Member)
		raw := fmt.Sprintf("c.FieldOperand(%s, %q)", g.term(m.X, s, locals), m.Name)
		if !g.goWorkControls || g.noWork {
			return raw
		}
		// The step that term charges for the member read.
		return g.form("func() TOperand { c.P.StepWork(); return "+raw+" }()", "func() TOperand { return "+ordinaryResult(raw)+" }()")
	}
	return "TOperand{V: " + g.term(t, s, locals) + "}"
}

// step returns the statement that charges one step to the work control of recv, if there is one.
func (g *generator) step(recv string) string {
	if g.noWork {
		return ""
	}
	return g.formRest(recv+".StepWork()\n", "", "")
}

func (g *generator) frameVar() string { return g.pick("EmptyFrame", "EmptyTFrame") }
func (g *generator) rt() string       { return g.pick("rt", "trt") }

// grammarVar returns the generated grammar of the rule table being generated.
func (g *generator) grammarVar() string {
	if g.table == "rules" {
		return "pegoGrammar"
	}
	return "pegoRecGrammar"
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
	fmt.Fprintf(&g.methods, "func %s(p *%s) (%s, bool) {\n%s%s}\n\n", name, g.recv(), g.valType(), g.step("p"), body)
	return name
}

// shaper returns the name of the package variable that holds the runtime shaper of the expression
// e, its static shape (value_shape.go). Few distinct shapes occur, so the variables are shared.
func (g *generator) shaper(e grammar.Expr) string {
	return g.shaperOf(shaperLit(g.prog, e))
}

// shaperOf returns the name of the package variable that holds the shaper of the literal lit,
// declaring it on first use.
func (g *generator) shaperOf(lit string) string {
	if g.shaperNames == nil {
		g.shaperNames = map[string]string{}
	}
	if name := g.shaperNames[lit]; name != "" {
		return name
	}
	name := g.name("sh")
	g.shaperNames[lit] = name
	g.shaperKeys = append(g.shaperKeys, lit)
	fmt.Fprintf(&g.vars, "var %s = %s\n", name, lit)
	return name
}

// shaperLit returns the literal of the runtime shaper of the expression e.
func shaperLit(prog *Program, e grammar.Expr) string {
	return fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", isElems(prog, e), valuedCount(prog, e), singleModeOf(prog, e), scopeTuple(prog, e), isElems(prog, e) && sharesTuple(prog, e))
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
	fmt.Fprintf(&g.methods, "// %s, called as by invokePlain\nfunc %s(p *%s) (%s, bool) {\n%s", r.name, name, g.recv(), g.valType(), g.step("p"))
	g.methods.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n")
	g.methods.WriteString(`	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
	p.Enter()
`)
	fmt.Fprintf(&g.methods, "\tv, ok := %s(p)\n", body)
	g.methods.WriteString("\tp.Depth--\n\tp.Cut = prevCut\n" + g.pick("\tp.Trail = p.Trail[:trail]\n", "\tp.DropTrail(trail)\n") + "\tif ok {\n")
	if r.novalue {
		g.methods.WriteString("\t\tv = nil\n")
	} else {
		fmt.Fprintf(&g.methods, "\t\tv = p.Finish(%s[%d], %s, v, start)\n", g.table, r.id, g.frameVar())
	}
	g.methods.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n")
	g.methods.WriteString(`	p.Env = prevEnv
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
		if e.IgnoreCase {
			// The next character matches the first code point in either case of an ASCII letter.
			f := foldASCIIRune(r)
			cond := fmt.Sprintf("ch == %d", f)
			if 'a' <= f && f <= 'z' {
				cond += fmt.Sprintf(" || ch == %d", f-('a'-'A'))
			}
			return cond, g.desc(literalDesc(e)), depth, true
		}
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
		return "p.Depth <= p.MaxDepth"
	}
	return fmt.Sprintf("p.Depth+%d <= p.MaxDepth", depth)
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

func (g *generator) operatorsRule(r *rule, e *grammar.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "\t%s[%d].OperatorsRule = &%s{\n", g.table, r.id, g.pick("OperatorsRule", "TOperatorsRule"))
	// The lines of the rule's Core IR, in the order of e, if they are printed from Flow IR.
	co := g.flowOperatorsRule(r)
	if e.Skip != nil {
		s := newScope()
		var skip *core.OpLine
		if co != nil {
			skip = co.Skip
		}
		m := g.flowOperatorsRuleBody(skip, e.Skip, s, false, false)
		if m == "" {
			g.failFlow("the skip of an Operators Rule of rule " + g.cur.name)
		}
		fmt.Fprintf(&b, "\t\tSkip: %s,\n", m)
	}
	fmt.Fprintf(&b, "\t\tOperands: []*%s{\n", g.pick("OperatorsRuleLine", "TOperatorsRuleLine"))
	for i, o := range e.Operands {
		var l *core.OpLine
		if co != nil {
			l = co.Operands[i]
		}
		fmt.Fprintf(&b, "\t\t\t%s,\n", g.operatorsRuleLine(o.Expr, o.Action, false, l))
	}
	b.WriteString("\t\t},\n")
	var prefix, led []string
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			var l *core.OpLine
			if co != nil {
				l = co.Ops[id].Line
			}
			s := fmt.Sprintf("{Id: %d, Kind: %q, Assoc: %q, Level: %d, Line: %s}", id, op.Kind, op.Assoc, i+1, g.operatorsRuleLine(op.Expr, op.Action, true, l))
			id++
			if op.Kind == grammar.Prefix {
				prefix = append(prefix, s)
			} else {
				led = append(led, s)
			}
		}
	}
	op := g.pick("OperatorsRuleOp", "TOperatorsRuleOp")
	fmt.Fprintf(&b, "\t\tPrefix: []*%s{%s},\n", op, strings.Join(prefix, ",\n"))
	fmt.Fprintf(&b, "\t\tLed: []*%s{%s},\n", op, strings.Join(led, ",\n"))
	b.WriteString("\t}\n")
	fmt.Fprintf(&b, "\t%s[%d].Body = func(p *%s, min int) (%s, bool) { return p.OperatorsRuleParse(%s[%d], min) }\n", g.table, r.id, g.recv(), g.valType(), g.table, r.id)
	g.printedBody(r)
	g.init.WriteString(b.String())
}

// operatorsRuleLine returns the line of an Operators Rule with the expression e and the action;
// l is its Core IR if it is printed from Flow IR, or nil.
func (g *generator) operatorsRuleLine(e grammar.Expr, action grammar.Term, operator bool, l *core.OpLine) string {
	s := newScope()
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	// Lines whose value is unused are matched without values, as in the engine; so are all lines
	// of a rule without a value (in the recognizer).
	build := !g.cur.novalue && !leanLine(action, locals)
	m := g.flowOperatorsRuleBody(l, e, s, build, true)
	if m == "" {
		g.failFlow("a line of an Operators Rule of rule " + g.cur.name)
	}
	act := "nil"
	if action != nil && !g.cur.novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.Lhs", "rhs": "c.Rhs", "op": "c.Op"}
		}
		if g.table == "trules" {
			g.final = action // re-ranged by tctx.result, as an action's result
		}
		act = fmt.Sprintf("func(c *%s) any { return %s }", g.ctxType(), g.term(action, s, locals))
		g.final = nil
	}
	return fmt.Sprintf("&%s{Scope: %s, M: %s, Action: %s, Value: %s}", g.pick("OperatorsRuleLine", "TOperatorsRuleLine"), goStrings(s.names), m, act, g.shaper(e))
}

// ruleCall returns the call of the rule r from Operators Rule level min.
func (g *generator) ruleCall(r *rule, min int) string {
	_, operatorsRule := r.def.Expr.(*grammar.OperatorsRule)
	switch {
	case g.document && g.table == "rules":
		// Document parses route every call through call, which memoizes per rule.docMemo,
		// including otherwise plain rules.
		g.noteCall(r, false)
		return fmt.Sprintf("p.Call(%s[%d], %d)", g.table, r.id, min)
	case g.directTypedDocument && g.table == "trules" && operatorsRule:
		g.noteCall(r, false)
		return fmt.Sprintf("p.Call(%s[%d], %d)", g.table, r.id, min)
	case r.plain && !operatorsRule:
		g.noteCall(r, true)
		// Never memoized and without captures: call does nothing but invokePlain, which a
		// method of its own does with a direct call of the body (see plainCalls).
		if g.plainCalled == nil {
			g.plainCalled = map[*rule]bool{}
		}
		g.plainCalled[r] = true
		return fmt.Sprintf("%s%d(p)", g.plainPrefix(), r.id)
	case g.table == "trules" && !operatorsRule && !r.leader:
		// The typed runtime calls a method of the rule's own (typedCall).
		g.noteCall(r, true)
		return fmt.Sprintf("u%d(p)", r.id)
	case r.plain:
		g.noteCall(r, false)
		return fmt.Sprintf("p.InvokePlain(%s[%d], %d)", g.table, r.id, min)
	}
	g.noteCall(r, false)
	return fmt.Sprintf("p.Call(%s[%d], %d)", g.table, r.id, min)
}

// term converts a value expression into a Go expression of type any. locals maps names, such as lambda
// parameters, to Go expressions.
func (g *generator) term(t grammar.Term, s *scope, locals map[string]string) string {
	raw := g.termRaw(t, s, locals)
	if !g.goWorkControls || g.noWork {
		return raw
	}
	return g.form("func() any { c.P.StepWork(); return "+raw+" }()", "func() any { return "+ordinaryResult(raw)+" }()")
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
			return g.pick("UnboxValue(", "(") + l + ")"
		}
		if g.caps != nil {
			return g.caps[t.Name]
		}
		return fmt.Sprintf("c.Cap(%d)", s.slots[t.Name])
	case *grammar.VarRef:
		return fmt.Sprintf("c.Lookup(%q)", t.Name)
	case *grammar.Member:
		return fmt.Sprintf("c.Member(%s, %q)", g.term(t.X, s, locals), t.Name)
	case *grammar.New:
		if len(t.Args) != 0 {
			// A node type constructor converts its value (see evalCtx.convert).
			x := g.term(t.Args[0], s, locals)
			n, _ := g.prog.nodeTypeOf(t.Type) // the type's own name, also for an alias of it
			if g.table == "trules" {
				// The typed runtime names the type by its registered index.
				return fmt.Sprintf("c.Convert(%s, %s)", g.nodeTypeID(n.name), x)
			}
			return fmt.Sprintf("c.Convert(%q, %s)", n.name, x)
		}
		name, spec := g.prog.structType(t.Type)
		if g.table == "trules" {
			// The typed runtime calls the constructor of the type (tmk_T, see typedRuntime) with
			// the fields in declaration order.
			// The fields are evaluated in the order written (an error in one must win over the
			// next), then passed in declaration order.
			if g.noWork {
				// Without step charges the fields can be the arguments themselves, when they are
				// written in declaration order: Go evaluates the arguments left to right.
				fields := defFields(spec)
				args := []string{"c", strconv.FormatBool(grammar.Term(t) == g.final)}
				next, inOrder := 0, true
				for _, given := range t.Fields {
					for next < len(fields) && fields[next].Name != given.Name {
						next++
					}
					if next == len(fields) {
						inOrder = false
						break
					}
					next++
				}
				vals := map[string]string{}
				if inOrder {
					for _, given := range t.Fields {
						vals[given.Name] = g.term(given.Value, s, locals)
					}
				}
				if inOrder {
					for _, f := range fields {
						if v, ok := vals[f.Name]; ok {
							args = append(args, v)
						} else {
							args = append(args, "nil")
						}
					}
					return "tmk_" + name + "(" + strings.Join(args, ", ") + ")"
				}
			}
			var b strings.Builder
			b.WriteString("func() any { ")
			vars := map[string]string{}
			for _, given := range t.Fields {
				v := g.name("f_")
				vars[given.Name] = v
				fmt.Fprintf(&b, "%s := %s; ", v, g.term(given.Value, s, locals))
			}
			args := []string{"c", strconv.FormatBool(grammar.Term(t) == g.final)}
			for _, f := range defFields(spec) {
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
			args = append(args, strconv.Quote(f.Name), g.term(f.Value, s, locals))
		}
		return "c.NewStruct(" + strings.Join(args, ", ") + ")"
	case *grammar.ListConstructor:
		return g.term(&grammar.Call{Func: "list", Args: t.Items}, s, locals)
	case *grammar.TupleLit:
		args := make([]string, len(t.Elems))
		for i, x := range t.Elems {
			args[i] = g.term(x, s, locals)
		}
		return "c.Tuple(" + strings.Join(args, ", ") + ")"
	case *grammar.Call:
		if t.Func == "concat" && fusable(t) {
			// Push the elements of list, map and concat arguments directly (see fusable and
			// evalCtx.pushConcatArg).
			var b strings.Builder
			b.WriteString("func() any { b := len(c.P.KidStack); ")
			g.concatParts(&b, t, s, locals)
			b.WriteString("return c.EndList(b) }()")
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
			return "c.Length(" + args[0] + ")"
		case "text":
			return "c.Text(" + args[0] + ")"
		case "foldl", "foldr":
			if g.goWorkControls {
				return fmt.Sprintf("c.Fold(%q, %v, %s, %s, %s)", t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
			}
			return fmt.Sprintf("%sFold(%q, %v, %s, %s, %s)", g.rt(), t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
		case "map":
			if x, ok := g.projected(t, locals); ok {
				// The arguments written above are not part of the code.
				if g.goWorkControls {
					return "c.NewList(c.ListItems(\"map\", " + g.term(x, s, locals) + "))"
				}
				return "c.NewList(" + g.pick("listItems", "tlistItems") + "(\"map\", " + g.term(x, s, locals) + "))"
			}
			return fmt.Sprintf("c.MapList(%s, %s)", args[0], g.lambda(t.Args[1], s, locals))
		case "filter":
			return fmt.Sprintf("c.FilterList(%s, %s)", args[0], g.lambda(t.Args[1], s, locals))
		case "list":
			return "c.List(" + strings.Join(args, ", ") + ")"
		case "concat":
			return "c.Concat(" + strings.Join(args, ", ") + ")"
		}
	case *grammar.Binary:
		if lx, ok := textArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
			if rx, ok := textArg(t.R); ok {
				// Compare the strings directly (see evalCtx.binary).
				if g.goWorkControls {
					return fmt.Sprintf("any(c.P.TextEqual(c.TextStr(%s), c.TextStr(%s)) == %v)", g.term(lx, s, locals), g.term(rx, s, locals), t.Op == "==")
				}
				return fmt.Sprintf("any(c.TextStr(%s) %s c.TextStr(%s))", g.term(lx, s, locals), t.Op, g.term(rx, s, locals))
			}
		}
		if g.table == "trules" && comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) && !g.boxedComparisons {
			// Compare string fields without boxing them (see tctx.compare).
			return fmt.Sprintf("c.Compare(%q, %s, %s)", t.Op, g.operand(t.L, s, locals), g.operand(t.R, s, locals))
		}
		l, r := g.term(t.L, s, locals), g.term(t.R, s, locals)
		switch t.Op {
		case "&&":
			if g.goWorkControls {
				return fmt.Sprintf("c.And(%s, func() any { return %s })", l, r)
			}
			return fmt.Sprintf("%sAnd(%s, func() any { return %s })", g.rt(), l, r)
		case "||":
			if g.goWorkControls {
				return fmt.Sprintf("c.Or(%s, func() any { return %s })", l, r)
			}
			return fmt.Sprintf("%sOr(%s, func() any { return %s })", g.rt(), l, r)
		}
		if g.goWorkControls {
			return fmt.Sprintf("c.Binary(%q, %s, %s)", t.Op, l, r)
		}
		return fmt.Sprintf("%sBinary(%q, %s, %s)", g.rt(), t.Op, l, r)
	case *grammar.Cast:
		x := g.term(t.X, s, locals)
		ft := g.prog.resolveCheck(t.Type)
		if ft == nil {
			return x
		}
		v := g.fieldTypeVar(t, ft, func(v string, t *fieldType) string { return fmt.Sprintf("var %s = %s\n", v, goFieldType(t)) })
		line, col := g.prog.castPos(t)
		return fmt.Sprintf("c.Cast(%s, %s, %d, %d)", x, v, line, col)
	case *grammar.Unary:
		if g.goWorkControls {
			return fmt.Sprintf("c.Unary(%q, %s)", t.Op, g.term(t.X, s, locals))
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
				fast := fmt.Sprintf("func() any { switch c.TextStr(%s) { case %s: return true; default: return false } }()", g.term(&grammar.CaptureRef{Name: name}, s, locals), strings.Join(cases, ", "))
				return "func() any { if c.P.Control != nil { return " + ordinary + " }; return " + fast + " }()"
			}
			return fmt.Sprintf("func() any { switch c.TextStr(%s) { case %s: return true; default: return false } }()", g.term(&grammar.CaptureRef{Name: name}, s, locals), strings.Join(cases, ", "))
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
			b.WriteString("c.PushList(" + strings.Join(args, ", ") + "); ")
		case ok && call.Func == "map" && len(call.Args) == 2:
			if x, ok := g.projected(call, locals); ok {
				b.WriteString("c.PushItems(" + g.term(x, s, locals) + "); ")
				break
			}
			fmt.Fprintf(b, "c.PushMap(%s, %s); ", g.term(call.Args[0], s, locals), g.lambda(call.Args[1], s, locals))
		case ok && call.Func == "concat":
			g.concatParts(b, call, s, locals)
		default:
			b.WriteString("c.PushItems(" + g.term(a, s, locals) + "); ")
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
		if !g.ordinary {
			fmt.Fprintf(&g.vars, "var %s = []WorkRange{", name)
			for _, r := range e.Ranges {
				fmt.Fprintf(&g.vars, "{Lo: %d, Hi: %d},", r.Lo, r.Hi)
			}
			g.vars.WriteString("}\n")
		}
	}
	// The ordinary form tests the class without a control; the name is allocated all the same, so
	// that the other generated names keep their numbers.
	return g.formRest(fmt.Sprintf("((WorkControlsEnabled && p.Control != nil && p.RejectWorkClass(ch, %s, %v)) || ((!WorkControlsEnabled || p.Control == nil) && (%s)))", name, e.Negated, ordinary), "("+ordinary+")", "("+ordinary+")")
}

// atEnd and atLineStart name the runtime methods that test the anchors; a parser without
// ParseStream uses the forms that need not consider a stream, which inline.
func (g *generator) atEnd() string {
	if g.streamStart == nil {
		return g.helper("AtEndBatch")
	}
	return g.helper("AtEnd")
}

func (g *generator) atLineStart() string {
	if g.streamStart == nil {
		return g.helper("AtLineStartBatch")
	}
	return g.helper("AtLineStart")
}

// helper names the runtime method that a generated execution function calls on its parser. A
// parser generated without work controls and without Documents has no controlled form of its
// bodies, so they call the Ordinary form of the method.
func (g *generator) helper(name string) string {
	if ordinaryHelperName(name) {
		return g.form(name, name+"Ordinary")
	}
	return name
}

// label names a label of an execution function; the ordinary branch has its own copies of the labels
// of the function it is inserted into.
func (g *generator) label(name string) string {
	if g.ordinary {
		return name
	}
	return g.form(name, name+"_ordinary")
}

// leanTyped reports whether typed rule bodies are emitted in their ordinary form only.
func (g *generator) leanTyped() bool {
	return g.lean && g.goWorkControls && g.table == "trules"
}

// classTable returns the name of the package variable that holds the membership table of e,
// declaring it on first use.
func (g *generator) classTable(e *grammar.CharClass) string {
	key := fmt.Sprintf("cls%v%#v", e.Negated, e.Ranges)
	if g.workClassNames == nil {
		g.workClassNames = map[string]string{}
	}
	if name := g.workClassNames[key]; name != "" {
		return name
	}
	name := g.name("cls")
	g.workClassNames[key] = name
	g.workClassKeys = append(g.workClassKeys, key)
	ascii, wideRunes := classBits(e)
	var wide []string
	for _, r := range wideRunes {
		wide = append(wide, strconv.Itoa(int(r)))
	}
	fmt.Fprintf(&g.vars, "var %s = Class{ASCII: [2]uint64{%#x, %#x}", name, ascii[0], ascii[1])
	if len(wide) > 0 {
		fmt.Fprintf(&g.vars, ", Wide: []rune{%s}", strings.Join(wide, ", "))
	}
	if e.Negated {
		g.vars.WriteString(", Neg: true")
	}
	g.vars.WriteString("}\n")
	return name
}

// classSteps returns the name of the package variable that lists the class steps of items, which
// are character classes, declaring it on first use.
func (g *generator) classSteps(items []grammar.Expr) string {
	var steps []string
	for _, it := range items {
		c := it.(*grammar.CharClass)
		steps = append(steps, fmt.Sprintf("{C: &%s, ID: %d}", g.classTable(c), g.desc(charClassString(c))))
	}
	body := strings.Join(steps, ", ")
	key := "steps" + body
	if name := g.workClassNames[key]; name != "" {
		return name
	}
	name := g.name("steps")
	g.workClassNames[key] = name
	g.workClassKeys = append(g.workClassKeys, key)
	fmt.Fprintf(&g.vars, "var %s = []ClassStep{%s}\n", name, body)
	return name
}

// classBits returns the membership tables of a character class as the runtime's Class holds
// them: the bitset of the matching characters below 128, with the negation applied, and the
// inclusive pairs of the ranges at or above 128 (a range that starts below is cut at 128).
func classBits(e *grammar.CharClass) (ascii [2]uint64, wide []rune) {
	for c := rune(0); c < 128; c++ {
		in := false
		for _, r := range e.Ranges {
			in = in || (c >= r.Lo && c <= r.Hi)
		}
		if in != e.Negated {
			ascii[c>>6] |= 1 << (uint(c) & 63)
		}
	}
	for _, r := range e.Ranges {
		if r.Hi >= 128 {
			wide = append(wide, max(r.Lo, 128), r.Hi)
		}
	}
	return ascii, wide
}

// ordinaryResult is the result expression of a closure in the ordinary form, which has no
// parentheses around a name that is the whole result.
func ordinaryResult(raw string) string {
	raw = selectForm(raw, formOrdinaryText)
	if n := len(raw); n > 2 && raw[0] == '(' && raw[n-1] == ')' && token.IsIdentifier(raw[1:n-1]) {
		return raw[1 : n-1]
	}
	return raw
}
