package gogen

import (
	"fmt"
	"go/format"
	"go/token"
	"math"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/ir/core"
)

// defaultPackageName returns the package name of the generated code for a module path: its last
// element, without a trailing major version element (example.com/m/v2 gives m), as Go tooling
// names a package.
func defaultPackageName(module string) string {
	base := path.Base(module)
	if dir := path.Dir(module); dir != "." && dir != "/" && len(base) >= 2 && base[0] == 'v' && base[1] != '0' && strings.Trim(base[1:], "0123456789") == "" {
		if base[1] != '1' || len(base) > 2 {
			return path.Base(dir)
		}
	}
	return base
}

// Generate generates the files of a Go parser for g, compiling it with c: parser.go with the code
// generated from the grammar, api.go, which re-exports the names the runtime defines for the users
// of the parser, with opts.Types types/types.go, the package of the Go types of the typed values,
// and, with opts.Runtime RuntimeEmbed, a copy of the runtime in internal/runtime. Paths are
// relative to the directory of the generated package. gen.Go describes the options.
//
// Eligible rule bodies inline their expressions; other expressions become Go functions. Actions
// and predicates become Go expressions, and the runtime (package runtime of this module) behaves
// exactly like the engine. Without opts.Stream, #stream is a plain repetition.
func Generate(g *grammar.Grammar, c gen.Compiler, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	if opts.Runtime == gen.RuntimeOnly {
		return runtimeFiles()
	}
	return GenerateParser(g, c, opts, nil)
}

// Hooks let a test observe or change the decisions of one generation. The zero value, like nil,
// changes nothing.
type Hooks struct {
	// Generator receives the generator of the pass that writes the output, before it prints, so
	// that the test can build the Core IR of the same options (Generator.TypedCore).
	Generator func(*Generator)
	// Reach receives the result of the first pass of GenerateParser, before the second pass; a test
	// removes a root from it to show that the output then fails.
	Reach func(*liveRules)
	// LeanRoute is called with the rule table and the rule for each rule that takes the lean route
	// in rules, in a Node or recognizer table, so that a test checks the route that is chosen, not
	// the text of the generated code.
	LeanRoute func(table string, r *compile.Rule)
	// FramedRoute is called like LeanRoute for each rule whose framed body is printed from Flow IR
	// (flowNodeFrameBody), in a Node or recognizer table.
	FramedRoute func(table string, r *compile.Rule)
	// OperatorsRoute is called like LeanRoute for each line of a Node or recognizer table that is
	// printed from Flow IR for an Operators Rule.
	OperatorsRoute func(table string, r *compile.Rule)
	// StreamRoute is called like LeanRoute for each ParseStream start rule whose stream body is
	// printed from Flow IR (flowStreamBody).
	StreamRoute func(table string, r *compile.Rule)
	// RefuseFlowRecRule makes flowRecRule refuse the rules it selects, so that the framed body that
	// takes their place is exercised. No manifest grammar reaches that fallback.
	RefuseFlowRecRule func(*compile.Rule) bool
	// FlowVarsMutant makes every variable of a printed function unread: the generated code must
	// then fail to compile, which shows that the tests of generated code catch a wrong decision.
	FlowVarsMutant bool
	// FlowVarsMutantUses ignores the reads of the values of assignments that stay, which leaves
	// variables that only they read undeclared: the generated code must then fail to compile.
	FlowVarsMutantUses bool
}

// GenerateParser generates the files of Generate (other than for RuntimeOnly). It generates twice: the
// first pass records which rules of the Node, recognizer and typed tables a generated call or an
// entry point can reach (liveRules), and the second leaves out the others. A typed-only parser
// without Node rules has no Node or recognizer rule to leave out (keepTableRules); one that keeps
// Node rules for its converting entry points prints those that the entry points reach, like any
// other output.
func GenerateParser(g *grammar.Grammar, c gen.Compiler, opts gen.GenOptions, h *Hooks) ([]gen.GeneratedFile, error) {
	if h == nil {
		h = &Hooks{}
	}
	if opts.KeepUnreachable {
		return generateOnce(g, c, opts, h, nil, nil)
	}
	var lv liveRules
	if _, err := generateOnce(g, c, opts, h, nil, &lv); err != nil {
		return nil, err
	}
	if h.Reach != nil {
		h.Reach(&lv)
	}
	return generateOnce(g, c, opts, h, &lv, nil)
}

// generateOnce is one pass of GenerateParser. A pass with collect records the reachable rules into it;
// a pass with live omits the rules that are not reachable in live.
func generateOnce(g *grammar.Grammar, c gen.Compiler, opts gen.GenOptions, h *Hooks, live, collect *liveRules) (files []gen.GeneratedFile, err error) {
	if collect != nil {
		*collect = liveRules{}
	}
	if opts.Module == "" && (opts.Runtime == gen.RuntimeEmbed || opts.Package == "") {
		return nil, fmt.Errorf("module path is required")
	}
	derived := opts.Package == ""
	if derived {
		opts.Package = defaultPackageName(opts.Module)
	}
	if !token.IsIdentifier(opts.Package) || opts.Package == "_" {
		if derived {
			return nil, fmt.Errorf("the module path %q gives the package name %q, which is not a valid Go identifier; choose the package name with -pkg (PackageName)", opts.Module, opts.Package)
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
	if err := opts.ValidateMaxDepth(false); err != nil {
		return nil, err
	}
	if err := opts.ValidateDefaultValue(); err != nil {
		return nil, err
	}
	info, err := loadRuntime()
	if err != nil {
		return nil, err
	}
	prog, recognizer, err := c(g, compile.Options{SourceInfo: opts.SourceInfo, DefaultValue: opts.DefaultValue, LeftRecursionBindings: opts.LeftRecursionBindings})
	if err != nil {
		return nil, err
	}
	if prog.HasMissing {
		return nil, compile.MissingUnsupported("generated parsers")
	}
	start, err := prog.StartRule(opts.Start)
	if err != nil {
		return nil, err
	}
	if opts.Stream {
		if !start.Stream {
			return nil, fmt.Errorf("stream generation needs a #stream repetition at the top level of the start rule %s", start.Name)
		}
	}
	goGen := &Generator{typedOnly: opts.TypedOnly && !opts.KeepNodes, Observability: opts.Observability, Document: opts.Document, GoBudgetControls: true, NoBudget: !opts.BudgetControls, Ordinary: !opts.BudgetControls && !opts.Document, Prog: prog, Table: "rules", DisableMethodSharing: opts.DisableMethodSharing, DisableLiteralSharing: opts.DisableLiteralSharing, DisableLiteralTextDispatch: opts.DisableLiteralTextDispatch, disableTerminalPrograms: opts.DisableTerminalPrograms, DisableGuardedClasses: opts.DisableGuardedClasses, BoxedComparisons: opts.BoxedComparisons, disableTypedObjectSlabs: opts.DisableTypedObjectSlabs, disableTypedListSlabs: opts.DisableTypedListSlabs, disableTypedOutputBootstrap: opts.DisableTypedOutputBootstrap, hooks: *h, live: live, collect: collect, keepTableRules: opts.TypedOnly && !opts.KeepNodes}
	if live != nil {
		goGen.SeedDescs()
		for _, d := range live.descs[len(goGen.Descs):] {
			goGen.DescIDs[d] = compile.ExpID(len(goGen.Descs))
			goGen.Descs = append(goGen.Descs, d)
		}
	}
	if opts.Stream {
		goGen.streamStart = start
	}
	goGen.Desc(compile.FixedDescs[0]) // the fixed expectations come first
	if h.Generator != nil && collect == nil {
		h.Generator(goGen)
	}
	if err := goGen.StartRecFlow(prog); err != nil {
		return nil, err
	}
	goGen.rules()
	goGen.Flow = nil
	if goGen.streamErr != nil {
		return nil, goGen.streamErr
	}
	if goGen.tableErr != nil {
		return nil, goGen.tableErr
	}
	if goGen.FlowErr != nil {
		return nil, goGen.FlowErr
	}
	var rec *compile.Program
	if opts.Recognize {
		// The recognizer: the program in which no rule builds a value, in a rule table of its own.
		if rec, err = recognizer(); err != nil {
			return nil, err
		}
		goGen.Prog, goGen.Table = rec, "recRules"
		if err := goGen.StartRecFlow(rec); err != nil {
			return nil, err
		}
		goGen.rules()
		goGen.Flow = nil
		goGen.Prog, goGen.Table = prog, "rules"
		if goGen.tableErr != nil {
			return nil, goGen.tableErr
		}
		if goGen.FlowErr != nil {
			return nil, goGen.FlowErr
		}
	}
	if collect != nil {
		// The expectations of the Node and recognizer tables are those the second pass keeps; the typed
		// code adds its own after them, and only for the rules it reaches.
		collect.descs = append([]string(nil), goGen.Descs...)
		if !opts.Types {
			return nil, goGen.finishReach(prog, rec)
		}
		typed, err := goGen.typedGo(start, opts.ConvertTypes)
		if err != nil {
			return nil, err
		}
		goGen.convertsNodes = typed.needNodes
		if opts.TypedOnly && typed.needNodes && !opts.KeepNodes && !opts.DisableKeepNodes {
			opts.KeepNodes = true
			return generateOnce(g, c, opts, h, live, collect)
		}
		return nil, goGen.finishReach(prog, rec)
	}
	fmt.Fprintf(&goGen.Init, "\tpegoGrammar.Starts = %s\n", goStrings(prog.StartRules))
	fmt.Fprintf(&goGen.Init, "\tpegoGrammar.DefaultStart = %q\n", prog.DefaultStartRule)
	if !opts.TypedOnly {
		fmt.Fprintf(&goGen.Init, "\tpegoGrammar.Descs = %s\n", goStrings(goGen.Descs))
	}
	if opts.Observability {
		goGen.Init.WriteString("\tpegoGrammar.Observable = true\n")
	}
	if !opts.BudgetControls {
		goGen.Init.WriteString("\tpegoGrammar.NoBudgetControls = true\n")
	}
	if prog.RecordsSourceInfo {
		goGen.Init.WriteString("\tpegoGrammar.SourceInfo = true\n")
	}
	if t := prog.Vars; t != nil {
		var names, vals []string
		for _, d := range t.List {
			names = append(names, d.Name)
			vals = append(vals, fmt.Sprintf("%#v", d.Init))
		}
		fmt.Fprintf(&goGen.Init, "\tpegoGrammar.SetVars(%s, []any{%s})\n", goStrings(names), strings.Join(vals, ", "))
	}
	if rec != nil {
		goGen.Init.WriteString("\tpegoRecGrammar.Share(pegoGrammar)\n")
	}

	var out strings.Builder
	if opts.Observability && opts.Document {
		out.WriteString("// ErrDocumentBusy is returned by a Parse, ParseAST or Edit of a Document that\n// is already parsing, such as one made by a trace callback of that parse.\n// Schedule the work after the parse returns, or use another Document.\nvar ErrDocumentBusy = DocumentBusy\n\n")
	}
	out.WriteString("// --- Generated code ---\n\n")
	if !opts.TypedOnly {
		fmt.Fprintf(&out, "// Parse parses the whole input with the rule %s. Options select position units, depth, cancellation and budget limits.\n", start.Name)
		out.WriteString("// If the parse recovered from errors with #recover, it returns the node together with SyntaxErrors.\n")
		fmt.Fprintf(&out, "func Parse(input string, opts ...ParseOption) (*Node, error) { return RunParse(rules[%d], input, opts) }\n\n", start.ID)
	}
	if opts.Stream {
		fmt.Fprintf(&out, "// ParseStream parses the input read from r with the rule %s, passing each committed element of\n", start.Name)
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
		fmt.Fprintf(&out, "// NewDocument creates an incremental Node document for the start rule %s.\n", start.Name)
		if opts.Types {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) {\n\td, err := MakeDocument(rules[%d], text, opts)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn &Document{d}, nil\n}\n\n", start.ID)
		} else {
			fmt.Fprintf(&out, "func NewDocument(text string, opts ...ParseOption) (*Document, error) { return MakeDocument(rules[%d], text, opts) }\n\n", start.ID)
		}
	}
	if rec != nil {
		fmt.Fprintf(&out, "// Recognize checks that the whole input matches the rule %s without building a tree, and returns the\n", start.Name)
		out.WriteString("// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover). Actions needed by\n// predicates are evaluated and can report runtime errors; other actions are skipped.\n")
		fmt.Fprintf(&out, "func Recognize(input string, opts ...ParseOption) error {\n\t_, err := RunParse(recRules[%d], input, opts)\n\treturn err\n}\n\n", rec.ByName[start.Name].ID)
		fmt.Fprintf(&out, "// recRules is the rule table of Recognize, and pegoRecGrammar its grammar.\nvar recRules []*Rule\nvar pegoRecGrammar = NewGrammar(%d)\n\n", gen.GeneratedMaxDepth(opts))
	}
	fmt.Fprintf(&out, "// rules is the rule table, and pegoGrammar its grammar.\nvar rules []*Rule\nvar pegoGrammar = NewGrammar(%d)\n\n", gen.GeneratedMaxDepth(opts))
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
		typed, err := goGen.typedGo(start, opts.ConvertTypes)
		if err != nil {
			return nil, err
		}
		goGen.convertsNodes = typed.needNodes
		if opts.TypedOnly && typed.needNodes && !opts.KeepNodes && !opts.DisableKeepNodes {
			// A typed entry point converts the Node result of its rule, so the Node rule functions
			// are needed; generate again with them.
			opts.KeepNodes = true
			return generateOnce(g, c, opts, h, live, collect)
		}
		if opts.TypedOnly {
			// The expectations of the typed bodies are known only now.
			fmt.Fprintf(&goGen.Init, "\tpegoGrammar.Descs = %s\n", goStrings(goGen.Descs))
		}
		out.WriteString("\n// --- Typed values ---\n\n")
		if opts.Document {
			out.WriteString("// Document is an incremental document: it parses its text again after edits, reusing the\n// results of the parse before that it can.\ntype Document struct{ *" + runtimeAlias + ".Document }\n\n")
		}
		out.WriteString(typed.parser)
		out.WriteString("\n")
		typesBody = typesPreamble + typed.types
	}
	if err := goGen.finishReach(prog, rec); err != nil {
		return nil, err
	}
	out.WriteString(goGen.Vars.String())
	out.WriteString("\nfunc init() {\n")
	out.WriteString(goGen.Init.String())
	out.WriteString("}\n\n")
	out.WriteString(goGen.Methods.String())
	whole, err := format.Source([]byte("package " + opts.Package + "\n" + out.String()))
	if err != nil {
		return nil, fmt.Errorf("generated code does not compile: %w\n%s", err, out.String())
	}
	body := shakeVars(strings.TrimPrefix(string(whole), "package "+opts.Package+"\n"))
	var tp *typesPackage
	var typesFile []byte
	if opts.Types {
		if typesFile, tp, err = typesPackageFile(opts, typesBody); err != nil {
			return nil, err
		}
		// The one reference to the schema of the types package (see schemaName).
		body += "\nconst _ = " + tp.schema + "\n"
	}
	if goGen.chunksInRuntime {
		body = chunkNames.Replace(body)
	}
	body, err = qualifyRuntime(runtimeImport(opts), body, info, tp)
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
	if !opts.KeepComments {
		if src, err = trimComments(src); err != nil {
			return nil, fmt.Errorf("generated code does not compile: %w", err)
		}
	}
	api, err := apiSource(opts, info)
	if err != nil {
		return nil, err
	}
	files = []gen.GeneratedFile{{Path: "parser.go", Content: src}, {Path: "api.go", Content: []byte(api)}}
	if opts.Types {
		files = append(files, gen.GeneratedFile{Path: "types/types.go", Content: typesFile})
	}
	if opts.Runtime == gen.RuntimeEmbed {
		rt, err := runtimeFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, rt...)
	}
	return files, nil
}

// goFieldType writes a field type as a generated fieldType value.
func goFieldType(t *compile.FieldType) string {
	var b strings.Builder
	fmt.Fprintf(&b, "&FieldType{Kind: %q, Text: %q", rune(t.Kind), t.Text)
	if t.Name != "" {
		fmt.Fprintf(&b, ", Name: %q", t.Name)
	}
	if t.Elem != nil {
		b.WriteString(", Elem: " + goFieldType(t.Elem))
	}
	if len(t.Alts) > 0 {
		alts := make([]string, len(t.Alts))
		for i, a := range t.Alts {
			alts[i] = goFieldType(a)
		}
		b.WriteString(", Alts: []*FieldType{" + strings.Join(alts, ", ") + "}")
	}
	return b.String() + "}"
}

// Generator generates Go. It shares the expectation table, names and analyses with the
// TypeScript generator through gen.Generator.
type Generator struct {
	gen.Generator
	// hoist collects the statements the term being printed needs before its expression (see stmts); nil
	// outside a term.
	hoist *[]string
	// GoBudgetControls emits budget-control charging; Go output always has it, TypeScript output has
	// its own runtime budget controls.
	GoBudgetControls    bool
	directTypedDocument bool
	budgetClassNames    map[string]string
	budgetClassKeys     []string
	shaperNames         map[string]string
	shaperKeys          []string
	recoveryProg        map[*compile.Program]bool // whether a program can recover from errors
	tableErr            error                     // the rule table does not fit the form of runtime.RuleSpec
	hooks               Hooks                     // the zero value changes nothing
	// CoreHook, if set, is called by TypedCore with the module after it is built and after each pass.
	CoreHook func(pass string, m *core.Module)
	// chunksInRuntime is set when the typed values take their storage from the runtime (Chunk).
	chunksInRuntime             bool
	ruleTypesDone               bool // the types of the rule table entries are written
	disableTerminalPrograms     bool
	disableTypedObjectSlabs     bool
	disableTypedListSlabs       bool
	disableTypedOutputBootstrap bool
	FlowRules                   int
	FlowFrames                  int
	// NodeScopes records the frame layouts shared by typed rule metadata.
	NodeScopes map[*compile.Rule][]string
	// typedOnly leaves out the untyped Node family: the rule table keeps only the metadata the
	// typed table shares.
	typedOnly bool
	// final is the struct constructor that makes an action's result in the typed runtime: its
	// range is the rule's (see tctx.result), so it computes none and is not recorded as created.
	final grammar.Term
	// caps maps capture names to the Go variables that hold them in a flat rule of the typed
	// runtime (see flowrule.go), instead of frame slots.
	caps map[string]string
	// streamStart is the start rule of ParseStream, whose separate body
	// (generated while streamBody is set) emits its #stream elements.
	streamStart *compile.Rule
	streamBody  bool
	streamErr   error
	// plainCalled holds the plain rules that a generated call refers to by their wrapper
	// (ruleCall). wrappers are the methods of the plain rules, which dropUnusedWrappers keeps only
	// for the rules in plainCalled.
	plainCalled map[*compile.Rule]bool
	wrappers    []wrapperSpan
	// live and collect are the passes of GenerateParser (reach.go): live leaves out the unreachable
	// rules of the Node and recognizer tables, collect records the rules that the first pass reaches.
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
	bodySet    map[ruleKey]bool
	reachErr   error
}

func (g *Generator) literal(value string) string {
	name, shared := g.LiteralName(value)
	if !shared {
		fmt.Fprintf(&g.Vars, "var %s = []rune(%q)\n", name, value)
	}
	return name
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
func (g *Generator) saveRec(more string) string {
	if g.recovers() {
		return "start, rec" + more
	}
	return "start" + more
}

func (g *Generator) saveRecValues(more string) string {
	if g.recovers() {
		return "p.Pos, len(p.Recovered)" + more
	}
	return "p.Pos" + more
}

func (g *Generator) restoreRec(pos, rec string) string {
	if g.recovers() {
		return fmt.Sprintf("p.Restore(%s, %s)", pos, rec)
	}
	return fmt.Sprintf("p.Pos = %s", pos)
}

// recovers reports whether a parse of the program being generated can append to p.Recovered: only
// recovery (#recover) does. Without it p.Recovered stays empty, so
// the positions that backtracking restores need no saved length of it.
func (g *Generator) recovers() bool {
	if v, ok := g.recoveryProg[g.Prog]; ok {
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
	for _, rs := range [][]*compile.Rule{g.Prog.RuleList, g.Prog.Twins} {
		for _, r := range rs {
			compile.WalkExpr(r.Def.Expr, check)
			compile.WalkExpr(r.BodyExpr(), check)
		}
	}
	if g.recoveryProg == nil {
		g.recoveryProg = map[*compile.Program]bool{}
	}
	g.recoveryProg[g.Prog] = found
	return found
}

func (g *Generator) rules() {
	init := &g.Init
	methodsBefore := g.Methods.String()
	bodies := map[*compile.Rule]string{}
	scopes := map[*compile.Rule]*compile.Scope{}
	flats := map[*compile.Rule]*flatRule{}
	all := append(append([]*compile.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...)
	for _, r := range all {
		s := compile.NewScope()
		scopes[r] = s
		if g.pruned(r) {
			// Not printed; its table row keeps the capture layout of the first pass.
			s.Names = append(s.Names, g.live.scopes[ruleKey{g.Table, r.ID, false}]...)
			continue
		}
		if g.Table == "trules" {
			// Typed flat rules need no separate body; Flow IR prints every typed rule.
			if d := g.flowTypedRule(r); d != nil {
				flats[r] = d
				continue
			}
		} else if gen.RecFlowEligible(r) {
			// flowRecRule returns nil for a rule whose printed function would read its start or context, and
			// for one whose types it cannot name; such a rule takes its framed body below and is called through
			// the rule table (plainCall).
			if d := g.flowRecRule(r); d != nil {
				if g.hooks.LeanRoute != nil && g.collect == nil {
					g.hooks.LeanRoute(g.Table, r)
				}
				flats[r] = d
				if g.slotReached(r) {
					bodies[r] = g.leanRuleBody(r, d)
				}
				continue
			}
		}
		if _, ok := r.Def.Expr.(*grammar.OperatorsRule); !ok {
			if g.Table != "trules" {
				if body := g.flowNodeFrameBody(r, s); body != "" {
					if g.hooks.FramedRoute != nil && g.collect == nil {
						g.hooks.FramedRoute(g.Table, r)
					}
					bodies[r] = body
					continue
				}
			} else if body := g.flowFrameBody(r, s); body != "" {
				bodies[r] = body
				continue
			}
			g.FailFlow("the body of rule " + r.Name)
		}
	}
	if g.collect != nil && g.Table != "trules" {
		if g.collect.scopes == nil {
			g.collect.scopes = map[ruleKey][]string{}
		}
		for _, r := range all {
			g.collect.scopes[ruleKey{g.Table, r.ID, false}] = append([]string(nil), scopes[r].Names...)
		}
	}
	if g.Table == "rules" {
		g.NodeScopes = make(map[*compile.Rule][]string, len(all))
		for _, r := range all {
			g.NodeScopes[r] = append([]string(nil), scopes[r].Names...)
		}
	}
	if g.Table == "trules" {
		// The typed rules share the analysis results of the rules.
		init.WriteString("\ttrules = NewTRules(rules)\n\tdefer tinit()\n")
		g.Methods.WriteString("// trules is the typed rule table.\nvar trules []*TRule\n\n")
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
			if r.ID != i {
				panic("rule ids are not positions in the rule table")
			}
			value := "nil"
			if _, ok := r.Def.Expr.(*grammar.OperatorsRule); !ok { // the lines build the value of an Operators Rule
				value = "&" + g.shaper(r.BodyExpr())
			}
			var flags []string
			if r.Memo && !r.Transient {
				flags = append(flags, "RuleMemo")
			}
			if r.Leader {
				flags = append(flags, "RuleLeader")
			}
			if r.Novalue {
				flags = append(flags, "RuleNovalue")
			}
			if g.Document && (r.Leader || r.Memo) {
				flags = append(flags, "RuleDocMemo")
			}
			if r.Splice {
				flags = append(flags, "RuleSplice")
			}
			flagsLit := "0"
			if len(flags) > 0 {
				flagsLit = strings.Join(flags, " | ")
			}
			seen := 0
			if r.Seen >= 0 {
				seen = r.Seen + 1
			}
			fmt.Fprintf(&tab, "\t{%q, %s, %s, %d, %d, %d},\n", r.Name, value, flagsLit, seen, strsOf(scopes[r].Names), strsOf(r.Vars))
			w := r.WhereName()
			if w == r.Name {
				w = ""
			}
			if w != "" || r.NodeType != "" || r.OpNodeType != "" {
				extras = append(extras, fmt.Sprintf("\t{%d, %q, %q, %q},\n", r.ID, w, r.NodeType, r.OpNodeType))
			}
		}
		if err := ruleTableLimits(len(strs), g.Prog.NSeen); err != nil {
			g.tableErr = err
		}
		spec, strTable, extraTable := g.Table+"Spec", g.Table+"Strs", g.Table+"Extras"
		// The entries are positional, which vet accepts only for a type of the package; NewRules takes
		// any type of the layout of RuleSpec and RuleExtra.
		if !g.ruleTypesDone {
			g.ruleTypesDone = true
			g.Vars.WriteString("// ruleSpec and ruleExtra have the layout of RuleSpec and RuleExtra, with the fields in this order:\n// name, value, flags, seen, scope, vars; and id, where, node type, operators rule node type.\ntype ruleSpec struct {\n\tName  string\n\tValue *Shaper\n\tFlags uint8\n\tSeen  int32\n\tScope uint16\n\tVars  uint16\n}\n\ntype ruleExtra struct {\n\tId         int\n\tWhere      string\n\tNodeType   string\n\tOpNodeType string\n}\n\n")
		}
		fmt.Fprintf(&g.Vars, "// %s is the compact form of the rule table, built by NewRules.\nvar %s = [...]ruleSpec{\n%s}\n\n", spec, spec, tab.String())
		fmt.Fprintf(&g.Vars, "var %s = [...][]string{\n", strTable)
		for _, x := range strs {
			if x == "nil" {
				g.Vars.WriteString("\tnil,\n")
			} else {
				fmt.Fprintf(&g.Vars, "\t%s,\n", x)
			}
		}
		g.Vars.WriteString("}\n\n")
		fmt.Fprintf(&g.Vars, "var %s = [...]ruleExtra{\n%s}\n\n", extraTable, strings.Join(extras, ""))
		fmt.Fprintf(init, "\t%s = NewRules(%s[:], %s[:], %s[:])\n", g.Table, spec, strTable, extraTable)
		fmt.Fprintf(init, "\t%s.SetRules(%s, %d)\n", g.grammarVar(), g.Table, g.Prog.NSeen)
	}
	nodeCode := g.Table == "rules" && g.typedOnly
	if nodeCode {
		// The Node bodies were generated for the frame layouts they fix; their methods are not
		// kept, and the typed rules need none of the Node entries.
		g.Methods.Reset()
		g.Methods.WriteString(methodsBefore)
	}
	for _, r := range all {
		if nodeCode {
			break
		}
		if g.pruned(r) {
			continue
		}
		if pr, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
			g.Cur = r
			g.operatorsRule(r, pr)
			continue
		}
		if d := flats[r]; d != nil && g.Table == "trules" {
			g.typedCall(r, "", scopes[r], d)
			continue
		}
		if g.slotReached(r) {
			fmt.Fprintf(init, "\t%s[%d].Body = %s\n", g.Table, r.ID, bodies[r])
			g.printedBody(r)
		}
		if r.Action != nil && g.Table == "trules" {
			// A function of its own, which typedCall calls directly.
			g.Proj, g.final = g.Projections(r), r.Action
			fmt.Fprintf(&g.Methods, "// The action of %s.\nfunc ta%d(c *TActCtx) any { %s }\n\n", r.Name, r.ID, g.termBody(r.Action, scopes[r], nil))
			g.Proj, g.final = nil, nil
			fmt.Fprintf(init, "\ttrules[%d].Action = ta%d\n", r.ID, r.ID)
		} else if r.Action != nil {
			g.Proj = g.Projections(r)
			fmt.Fprintf(init, "\t%s[%d].Action = func(c *%s) any { %s }\n", g.Table, r.ID, g.ctxType(), g.termBody(r.Action, scopes[r], nil))
			g.Proj = nil
		}
		if g.Table == "trules" {
			g.typedCall(r, bodies[r], scopes[r], nil)
		} else if r.Plain {
			start := g.Methods.Len()
			if d := flats[r]; d != nil {
				g.flatMethod(r, d, fmt.Sprintf("%s%d", g.plainPrefix(), r.ID), "called as by invokePlain", true)
			} else {
				g.plainCall(r, bodies[r])
			}
			g.wrappers = append(g.wrappers, wrapperSpan{r: r, start: start, end: g.Methods.Len()})
		}
	}
	if r := g.streamStart; r != nil && g.Table == "rules" {
		// ParseStream calls a copy of the start rule whose body emits the
		// #stream elements, generated last so that the batch code is the same
		// as without streams. Its captures have a layout of their own.
		s := compile.NewScope()
		body := g.flowStreamBody(r, s)
		if body != "" {
			if g.hooks.StreamRoute != nil && g.collect == nil {
				g.hooks.StreamRoute(g.Table, r)
			}
		} else {
			g.FailFlow("the stream body of rule " + r.Name)
		}
		if strings.Join(s.Names, "\x00") != strings.Join(scopes[r].Names, "\x00") {
			// The copied action addresses captures by the batch body's slots.
			g.streamErr = fmt.Errorf("internal error: the stream body of %s has a different capture layout", r.Name)
		}
		fmt.Fprintf(init, "\tstreamRule = new(Rule)\n\t*streamRule = *rules[%d]\n\tstreamRule.Scope = %s\n\tstreamRule.Body = %s\n", r.ID, goStrings(s.Names), body)
		g.Methods.WriteString("// streamRule is the start rule of ParseStream.\nvar streamRule *Rule\n\n")
	}
	g.dropUnusedWrappers()
	if g.Table != "rules" {
		return // the recognizer shares the struct fields
	}
	// Struct fields, used to detect references to undeclared fields at run time.
	var names []string
	for name, spec := range g.Prog.Types {
		if td, ok := spec.(*grammar.TypeDef); ok && td.Struct() != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var fields []string
		for _, f := range compile.DefFields(g.Prog.Types[name].(*grammar.TypeDef)) {
			fields = append(fields, fmt.Sprintf("%q: true", f.Name))
		}
		fmt.Fprintf(init, "\tpegoGrammar.StructFields[%q] = map[string]bool{%s}\n", name, strings.Join(fields, ", "))
		fmt.Fprintf(init, "\tpegoGrammar.TypeKinds[%q] = KindOf(%q, \"\")\n", name, name)
	}
	// The kinds of the named node types, under their names and those of their aliases.
	names = names[:0]
	for name := range g.Prog.NodeKinds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(init, "\tpegoGrammar.NodeTypes[%q] = NewNodeTypeKinds(%q)\n", name, g.Prog.NodeKinds[name][0].Type)
	}
}

// nodeTypeNames returns the names of the grammar's named node types, sorted, in the order of their
// generated indexes from 1 (nodeTypeIndex).
func (g *Generator) nodeTypeNames() []string {
	var names []string
	for name, spec := range g.Prog.Types {
		if td, ok := spec.(*grammar.TypeDef); ok && td.Struct() == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// nodeTypeIndex returns the generated index of the named node type name, from 1.
func (g *Generator) nodeTypeIndex(name string) int {
	n, ok := g.Prog.NodeTypeOf(name)
	if !ok {
		panic("internal error: " + name + " is not a named node type")
	}
	return sort.SearchStrings(g.nodeTypeNames(), n.Name) + 1
}

// nodeTypeID returns the Go expression of the registered index of the named node type name in the
// typed runtime (nt in tmatch and tnode).
func (g *Generator) nodeTypeID(name string) string {
	return fmt.Sprintf("%s[%d]", nodeTypesName, g.nodeTypeIndex(name))
}

// typedResultConversion returns the statement that converts the value v of rule r to the rule's
// named node type in the typed runtime (what finish does), or "" if r has none. body, if not "",
// holds the fresh node of the rule's body (tfreshNode), which converts in place.
func (g *Generator) typedResultConversion(r *compile.Rule, v, body string) string {
	if r.NodeType == "" {
		return ""
	}
	if body != "" {
		return fmt.Sprintf("%s = p.TConvertResultOwned(trules[%d], %s, %s)", v, r.ID, v, body)
	}
	return fmt.Sprintf("%s = p.TConvertResult(trules[%d], %s)", v, r.ID, v)
}

// ownsBody reports whether finishing the value of rule r in the typed runtime converts the fresh
// node of its body in place: the rule has a named node type and returns its body's value.
func (g *Generator) ownsBody(r *compile.Rule) bool {
	return r.NodeType != "" && !r.Novalue && r.Action == nil
}

// typedCall writes the methods that call the rule r in the typed runtime: s<id> for a rule never
// memoized and without captures (what invokePlain does), and otherwise u<id> (what call does, the
// memo kept by callMemo) and i<id> (what invoke does). Each calls the body's method and the action
// directly and finishes the value as the rule's kind requires (what finish does), so a call goes
// through no rule table entry or function value.
//
// For a flat rule (d, see flowrule.go), the method that runs the body (s<id>, v<id> or i<id>)
// has it inlined, with the action.
func (g *Generator) typedCall(r *compile.Rule, body string, s *compile.Scope, d *flatRule) {
	if _, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule); operatorsRule || r.Leader {
		return // the general call
	}
	m := &g.Methods
	entry := fmt.Sprintf("u%d", r.ID)
	if r.Plain {
		entry = fmt.Sprintf("s%d", r.ID)
	}
	if g.typedEntry[r.ID] {
		// Only a typed entry point runs an Entry; every other call names the function.
		fmt.Fprintf(&g.Init, "\ttrules[%d].Entry = %s\n", r.ID, entry)
	}
	ncaps := len(s.Names)
	if d != nil {
		ncaps = len(r.Scope.Names)
	}
	finish := func(frame string) {
		body := ""
		if g.ownsBody(r) {
			body = "body"
			m.WriteString("\t\tbody := TFreshNode(v)\n")
		}
		switch {
		case r.Novalue:
			m.WriteString("\t\tv = nil\n")
		case r.Action != nil:
			fmt.Fprintf(m, "\t\tc := p.UseCtx(TActCtx{P: p, F: %s, Start: start, End: p.Pos, Cbase: len(p.Created)})\n", frame)
			fmt.Fprintf(m, "\t\tv = c.Result(ta%d, %q)\n", r.ID, r.WhereName())
		default:
			fmt.Fprintf(m, "\t\tv = p.ScopeValue(trules[%d].Value, v, start)\n", r.ID)
			if len(s.Names) > 0 {
				fmt.Fprintf(m, "\t\tv = p.AttachCaptures(v, trules[%d].Scope, %s.Vals, start, p.Pos)\n", r.ID, frame)
			}
			m.WriteString("\t\tif n := AsTval(v); n != nil {\n\t\t\tn.TSetFresh(false)\n\t\t}\n")
		}
		if conv := g.typedResultConversion(r, "v", body); conv != "" {
			fmt.Fprintf(m, "\t\t%s\n", conv)
		}
	}
	plain := func(name string) {
		if d != nil {
			g.flatMethod(r, d, name, "called as by invokePlain", true)
			return
		}
		if g.Observability && r.Plain && !g.directTypedDocument {
			name = g.tracePlainWrapper(r, name)
		}
		fmt.Fprintf(m, "// %s, called as by invokePlain\nfunc %s(p *TParser) (any, bool) {\n", r.Name, name)
		m.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n\tprevEnv, prevCut := p.Env, p.Cut\n\tp.Cut = false\n\tp.Enter()\n")
		if g.Observability && (!r.Plain || g.directTypedDocument) {
			m.WriteString("p.Observation.Evaluated()\n")
		}
		fmt.Fprintf(m, "\tv, ok := %s(p, 0)\n\tp.Depth--\n\tp.Cut = prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body)
		finish("EmptyTFrame")
		m.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n\tp.Env = prevEnv\n\treturn v, ok\n}\n\n")
	}
	if r.Plain {
		if g.directTypedDocument {
			name := fmt.Sprintf("s%d", r.ID)
			if g.Observability {
				name = g.traceCallWrapper(r, name, false)
			}
			fmt.Fprintf(m, "func %s(p *TParser) (any, bool) {\n if p.TypedDocument != nil { return p.DocumentCall(trules[%d], 0, ds%d) }; return ds%d(p)\n}\n", name, r.ID, r.ID, r.ID)
			plain(fmt.Sprintf("ds%d", r.ID))
			return
		}
		plain(fmt.Sprintf("s%d", r.ID))
		return
	}
	// u<id>: the call. An unmemoized call of a rule without captures is plain.
	name := fmt.Sprintf("u%d", r.ID)
	if g.Observability {
		name = g.traceCallWrapper(r, name, false)
	}
	fmt.Fprintf(m, "// rule %s\nfunc %s(p *TParser) (any, bool) {\n", r.Name, name)
	// Without Documents the invocation of a flat rule without captures is its plain method: the
	// two bodies differ only in the rollback to the start on failure, which the callers of the
	// invocation repeat harmlessly, so one body serves both. A Document reads the position the
	// invocation leaves at its failure to find the frontier it examined, so Documents keep both.
	merged := d != nil && ncaps == 0 && !g.Document
	iname := fmt.Sprintf("i%d", r.ID)
	if merged {
		iname = fmt.Sprintf("v%d", r.ID)
	}
	if g.directTypedDocument {
		fmt.Fprintf(m, " if p.TypedDocument != nil { return p.DocumentCall(trules[%d], 0, %s) }\n", r.ID, iname)
	}
	memo := r.Memo && !r.Transient
	if memo {
		fmt.Fprintf(m, "\tif !p.FirstCall(rules[%d]) {\n\t\treturn p.CallMemo(trules[%d], %s)\n\t}\n", r.ID, r.ID, iname)
	}
	if ncaps == 0 {
		fmt.Fprintf(m, "\treturn v%d(p)\n}\n\n", r.ID)
		plain(fmt.Sprintf("v%d", r.ID))
	} else {
		fmt.Fprintf(m, "\t%s := %s\n\tv, ok := i%d(p)\n\tif !ok {\n\t\t%s\n\t}\n\treturn v, ok\n}\n\n", g.saveRec(""), g.saveRecValues(""), r.ID, g.restoreRec("start", "rec"))
	}
	// i<id>: the invocation (also used by the memo).
	if merged {
		return // v<id>
	}
	if d != nil {
		g.flatMethod(r, d, fmt.Sprintf("i%d", r.ID), "invoked as by invoke", false)
		return
	}
	fmt.Fprintf(m, "// %s, invoked as by invoke\nfunc i%d(p *TParser) (any, bool) {\n", r.Name, r.ID)
	fmt.Fprintf(m, "\tf := p.NewFrame(%d)\n\tprevFrame, prevEnv, prevCut, trail := p.Frame, p.Env, p.Cut, len(p.Trail)\n\tp.Frame, p.Cut = f, false\n\tp.Enter()\n\tstart := p.Pos\n\t_ = start\n", len(s.Names))
	if g.Observability {
		m.WriteString("p.Observation.Evaluated()\n")
	}
	fmt.Fprintf(m, "\tv, ok := %s(p, 0)\n\tp.Depth--\n\tp.Frame, p.Cut = prevFrame, prevCut\n\tp.DropTrail(trail)\n\tif ok {\n", body)
	finish("f")
	m.WriteString("\t}\n\tp.Env = prevEnv\n\tp.FreeFrame(f)\n\treturn v, ok\n}\n\n")
}

// wrapperSpan is the span [start, end) of the methods of a plain rule's wrapper in generator.methods.
type wrapperSpan struct {
	r          *compile.Rule
	start, end int
}

// dropUnusedWrappers removes the wrappers of the plain rules that no generated call refers to. It
// runs after the bodies and the stream body, which are the last generated calls, so that a wrapper
// is kept exactly when a call of it was generated. Spans are removed from the last one to the first,
// so the earlier spans keep their offsets.
func (g *Generator) dropUnusedWrappers() {
	text := g.Methods.String()
	for i := len(g.wrappers) - 1; i >= 0; i-- {
		w := g.wrappers[i]
		if !strings.HasPrefix(text[w.start:w.end], "// ") || !strings.HasSuffix(text[w.start:w.end], "}\n\n") {
			panic(fmt.Sprintf("internal error: the wrapper of %s is not at its recorded span", w.r.Name))
		}
		if !g.plainCalled[w.r] {
			text = text[:w.start] + text[w.end:]
		}
	}
	g.wrappers = nil
	g.Methods.Reset()
	g.Methods.WriteString(text)
}

// plainPrefix is the prefix of the methods written by plainCall, and seenVar the variable that
// holds the number of rules with rule.seen set, for the rule table being generated.
func (g *Generator) plainPrefix() string {
	switch g.Table {
	case "rules":
		return "r"
	case "trules":
		return "s"
	}
	return "q"
}

// pick returns untyped for the Node runtime and typed for the typed runtime (trules).
func (g *Generator) pick(untyped, typed string) string {
	if g.Table == "trules" {
		return typed
	}
	return untyped
}

func (g *Generator) recv() string    { return g.pick("Parser", "TParser") }
func (g *Generator) valType() string { return g.pick("*Node", "any") }
func (g *Generator) ctxType() string { return g.pick("ActCtx", "TActCtx") }

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
func (g *Generator) operand(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	if fieldRead(t) {
		m := t.(*grammar.Member)
		return fmt.Sprintf("c.FieldOperand(%s, %q)", g.term(m.X, s, locals), m.Name)
	}
	return "TOperand{V: " + g.term(t, s, locals) + "}"
}

// step returns the statement that charges one step to the budget control of recv, if there is one.
func (g *Generator) step(recv string) string {
	if g.NoBudget {
		return ""
	}
	return recv + ".Spend()\n"
}

func (g *Generator) frameVar() string { return g.pick("EmptyFrame", "EmptyTFrame") }
func (g *Generator) rt() string       { return g.pick("rt", "trt") }

// grammarVar returns the generated grammar of the rule table being generated.
func (g *Generator) grammarVar() string {
	if g.Table == "rules" {
		return "pegoGrammar"
	}
	return "pegoRecGrammar"
}

// Method emits a method with the given body and returns its name.
func (g *Generator) Method(comment, body string) string {
	name, shared := g.MethodName(g.recv()+"/"+g.valType(), body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	fmt.Fprintf(&g.Methods, "func %s(p *%s) (%s, bool) {\n%s%s}\n\n", name, g.recv(), g.valType(), g.step("p"), body)
	return name
}

// bodyMethod emits the function of a rule body and returns its name. It has the signature of the
// Body field of the rule table, so the table names it without a closure; the Operators Rule level
// it ignores is 0 where the generated code calls it.
func (g *Generator) bodyMethod(comment, body string) string {
	name, shared := g.MethodName(g.recv()+"/"+g.valType()+"/body", body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	fmt.Fprintf(&g.Methods, "func %s(p *%s, _ int) (%s, bool) {\n%s%s}\n\n", name, g.recv(), g.valType(), g.step("p"), body)
	return name
}

// shaper returns the name of the package variable that holds the runtime shaper of the expression
// e, its static shape (internal/compile/value_shape.go). Few distinct shapes occur, so the variables are shared.
func (g *Generator) shaper(e grammar.Expr) string {
	return g.shaperOf(shaperLit(g.Prog, e))
}

// shaperOf returns the name of the package variable that holds the shaper of the literal lit,
// declaring it on first use.
func (g *Generator) shaperOf(lit string) string {
	if g.shaperNames == nil {
		g.shaperNames = map[string]string{}
	}
	if name := g.shaperNames[lit]; name != "" {
		return name
	}
	name := g.Name("sh")
	g.shaperNames[lit] = name
	g.shaperKeys = append(g.shaperKeys, lit)
	fmt.Fprintf(&g.Vars, "var %s = %s\n", name, lit)
	return name
}

// shaperLit returns the literal of the runtime shaper of the expression e.
func shaperLit(prog *compile.Program, e grammar.Expr) string {
	return fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", compile.IsElems(prog, e), compile.ValuedCount(prog, e), compile.SingleModeOf(prog, e), compile.ScopeTuple(prog, e), compile.IsElems(prog, e) && compile.SharesTuple(prog, e))
}

// plainCall writes the method r<id>, which does what invokePlain does for the rule r (a rule never
// memoized and without captures) with a direct call of its body.
func (g *Generator) plainCall(r *compile.Rule, body string) {
	name := fmt.Sprintf("%s%d", g.plainPrefix(), r.ID)
	if g.Observability {
		name = g.tracePlainWrapper(r, name)
	}
	fmt.Fprintf(&g.Methods, "// %s, called as by invokePlain\nfunc %s(p *%s) (%s, bool) {\n%s", r.Name, name, g.recv(), g.valType(), g.step("p"))
	g.Methods.WriteString("\t" + g.saveRec(", trail") + " := " + g.saveRecValues(", len(p.Trail)") + "\n")
	g.Methods.WriteString(`	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
	p.Enter()
`)
	fmt.Fprintf(&g.Methods, "\tv, ok := %s(p, 0)\n", body)
	g.Methods.WriteString("\tp.Depth--\n\tp.Cut = prevCut\n" + g.pick("\tp.Trail = p.Trail[:trail]\n", "\tp.DropTrail(trail)\n") + "\tif ok {\n")
	if r.Novalue {
		g.Methods.WriteString("\t\tv = nil\n")
	} else {
		fmt.Fprintf(&g.Methods, "\t\tv = p.Finish(%s[%d], %s, v, start)\n", g.Table, r.ID, g.frameVar())
	}
	g.Methods.WriteString("\t} else {\n\t\t" + g.restoreRec("start", "rec") + "\n\t}\n")
	g.Methods.WriteString(`	p.Env = prevEnv
	return v, ok
}

`)
}

// first returns the condition on the next character ch under which the expression e can match,
// when e must begin with a given terminal (a literal or a character class, possibly behind
// sequences, captures, @, - and calls of rules that are not left-recursive or OperatorsRule), with the
// expectation e records when the condition does not hold and the number of rule calls on the way.
// Then e fails at once without the terminal, recording only that expectation (a call memoizes the
// same failure, and memoization only affects speed), so a choice can skip it; unless the calls on
// the way would exceed the nesting limit.
func (g *Generator) first(e grammar.Expr, depth int, seen map[string]bool) (string, compile.ExpID, int, bool) {
	switch e := e.(type) {
	case *grammar.Literal:
		if e.Value == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(e.Value)
		if e.IgnoreCase {
			// The next character matches the first code point in either case of an ASCII letter.
			f := compile.FoldASCIIRune(r)
			cond := fmt.Sprintf("ch == %d", f)
			if 'a' <= f && f <= 'z' {
				cond += fmt.Sprintf(" || ch == %d", f-('a'-'A'))
			}
			return cond, g.Desc(compile.LiteralDesc(e)), depth, true
		}
		return fmt.Sprintf("ch == %d", r), g.Desc(strconv.Quote(e.Value)), depth, true
	case *grammar.CharClass:
		return NegateCondition(g.ControlledClassReject(e)), g.Desc(compile.CharClassString(e)), depth, true
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
		r := g.Prog.ByName[e.Name]
		if r == nil || r.Leader || seen[e.Name] {
			break
		}
		if _, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return g.first(r.Def.Expr, depth+1, seen)
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

// NegateCondition inverts a generated condition without retaining a leading
// logical negation as a double negation.
func NegateCondition(condition string) string {
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

func (g *Generator) operatorsRule(r *compile.Rule, e *grammar.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "\t%s[%d].OperatorsRule = &%s{\n", g.Table, r.ID, g.pick("OperatorsRule", "TOperatorsRule"))
	// The lines of the rule's Core IR, in the order of e, if they are printed from Flow IR.
	co := g.flowOperatorsRule(r)
	if e.Skip != nil {
		s := compile.NewScope()
		var skip *core.OpLine
		if co != nil {
			skip = co.Skip
		}
		m := g.flowOperatorsRuleBody(skip, e.Skip, s, false)
		if m == "" {
			g.FailFlow("the skip of an Operators Rule of rule " + g.Cur.Name)
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
	fmt.Fprintf(&b, "\t%s[%d].Body = func(p *%s, min int) (%s, bool) { return p.OperatorsRuleParse(%s[%d], min) }\n", g.Table, r.ID, g.recv(), g.valType(), g.Table, r.ID)
	g.printedBody(r)
	g.Init.WriteString(b.String())
}

// operatorsRuleLine returns the line of an Operators Rule with the expression e and the action;
// l is its Core IR if it is printed from Flow IR, or nil.
func (g *Generator) operatorsRuleLine(e grammar.Expr, action grammar.Term, operator bool, l *core.OpLine) string {
	s := compile.NewScope()
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	// Lines whose value is unused are matched without values, as in the engine; so are all lines
	// of a rule without a value (in the recognizer).
	build := !g.Cur.Novalue && !compile.LeanLine(action, locals)
	m := g.flowOperatorsRuleBody(l, e, s, build)
	if m == "" {
		g.FailFlow("a line of an Operators Rule of rule " + g.Cur.Name)
	}
	act := "nil"
	if action != nil && !g.Cur.Novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.Lhs", "rhs": "c.Rhs", "op": "c.Op"}
		}
		if g.Table == "trules" {
			g.final = action // re-ranged by tctx.result, as an action's result
		}
		act = fmt.Sprintf("func(c *%s) any { %s }", g.ctxType(), g.termBody(action, s, locals))
		g.final = nil
	}
	return fmt.Sprintf("&%s{Scope: %s, M: %s, Action: %s, Value: %s}", g.pick("OperatorsRuleLine", "TOperatorsRuleLine"), goStrings(s.Names), m, act, g.shaper(e))
}

// ruleCall returns the call of the rule r from Operators Rule level min.
func (g *Generator) ruleCall(r *compile.Rule, min int) string {
	_, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule)
	wrapper := g.callWrapper(g.Table, r)
	g.checkCall(r, wrapper)
	switch {
	case g.Document && g.Table == "rules":
		// Document parses route every call through call, which memoizes per rule.docMemo,
		// including otherwise plain rules.
		return fmt.Sprintf("p.Call(%s[%d], %d)", g.Table, r.ID, min)
	case g.directTypedDocument && g.Table == "trules" && operatorsRule:
		return fmt.Sprintf("p.Call(%s[%d], %d)", g.Table, r.ID, min)
	case r.Plain && !operatorsRule:
		// Never memoized and without captures: call does nothing but invokePlain, which a
		// method of its own does with a direct call of the body (see plainCalls).
		if g.plainCalled == nil {
			g.plainCalled = map[*compile.Rule]bool{}
		}
		g.plainCalled[r] = true
		return fmt.Sprintf("%s%d(p)", g.plainPrefix(), r.ID)
	case g.Table == "trules" && !operatorsRule && !r.Leader:
		// The typed runtime calls a method of the rule's own (typedCall).
		return fmt.Sprintf("u%d(p)", r.ID)
	case r.Plain:
		return fmt.Sprintf("p.InvokePlain(%s[%d], %d)", g.Table, r.ID, min)
	}
	return fmt.Sprintf("p.Call(%s[%d], %d)", g.Table, r.ID, min)
}

// term converts a value expression into a Go expression of type any. locals maps names, such as lambda
// parameters, to Go expressions.
func (g *Generator) term(t grammar.Term, s *compile.Scope, locals map[string]string) string {
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
		return fmt.Sprintf("c.Cap(%d)", s.Slots[t.Name])
	case *grammar.VarRef:
		return fmt.Sprintf("c.Lookup(%q)", t.Name)
	case *grammar.Member:
		return fmt.Sprintf("c.Member(%s, %q)", g.term(t.X, s, locals), t.Name)
	case *grammar.New:
		if len(t.Args) != 0 {
			// A node type constructor converts its value (see evalCtx.convert).
			x := g.term(t.Args[0], s, locals)
			n, _ := g.Prog.NodeTypeOf(t.Type) // the type's own name, also for an alias of it
			if g.Table == "trules" {
				// The typed runtime names the type by its registered index.
				return fmt.Sprintf("c.Convert(%s, %s)", g.nodeTypeID(n.Name), x)
			}
			return fmt.Sprintf("c.Convert(%q, %s)", n.Name, x)
		}
		name, spec := g.Prog.StructType(t.Type)
		if g.Table == "trules" {
			// The typed runtime calls the constructor of the type (tmk_T, see typedRuntime) with
			// the fields in declaration order.
			// The fields are evaluated in the order written (an error in one must win over the
			// next), then passed in declaration order.
			// The fields can be the arguments themselves when they are written in declaration order: Go
			// evaluates the arguments from left to right.
			fields := compile.DefFields(spec)
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
				parts := make([]func() string, len(t.Fields))
				for i, given := range t.Fields {
					parts[i] = func() string { return g.term(given.Value, s, locals) }
				}
				for i, v := range g.seq(parts...) {
					vals[t.Fields[i].Name] = v
				}
			} else {
				// Otherwise the temporaries are assigned in the order written, because an error in one
				// field has to win over the next.
				for _, given := range t.Fields {
					v := g.Name("f_")
					g.emit(fmt.Sprintf("%s := %s", v, g.term(given.Value, s, locals)))
					vals[given.Name] = v
				}
			}
			for _, f := range fields {
				if v, ok := vals[f.Name]; ok {
					args = append(args, v)
				} else {
					args = append(args, "nil")
				}
			}
			return "tmk_" + name + "(" + strings.Join(args, ", ") + ")"
		}
		values := make([]grammar.Term, len(t.Fields))
		for i, f := range t.Fields {
			values[i] = f.Value
		}
		args := []string{strconv.Quote(name)}
		for i, v := range g.terms(values, s, locals) {
			args = append(args, strconv.Quote(t.Fields[i].Name), v)
		}
		return "c.NewStruct(" + strings.Join(args, ", ") + ")"
	case *grammar.ListConstructor:
		return g.term(&grammar.Call{Func: "list", Args: t.Items}, s, locals)
	case *grammar.TupleLit:
		return "c.Tuple(" + strings.Join(g.terms(t.Elems, s, locals), ", ") + ")"
	case *grammar.Call:
		if t.Func == "concat" && compile.Fusable(t) {
			// Push the elements of list, map and concat arguments directly (see fusable and
			// evalCtx.pushConcatArg).
			base := g.Name("b_")
			g.emit(base + " := len(c.P.KidStack)")
			g.concatParts(t, s, locals)
			return "c.EndList(" + base + ")"
		}
		// The arguments other than lambdas, in the order written.
		var args []string
		if t.Func != "map" || len(t.Args) != 2 {
			args = g.terms(t.Args, s, locals)
		} else if _, ok := g.Projected(t, locals); !ok {
			args = g.terms(t.Args[:1], s, locals)
		}
		switch t.Func {
		case "len":
			return "c.Length(" + args[0] + ")"
		case "text":
			return "c.Text(" + args[0] + ")"
		case "foldl", "foldr":
			if g.GoBudgetControls {
				return fmt.Sprintf("c.Fold(%q, %v, %s, %s, %s)", t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
			}
			return fmt.Sprintf("%sFold(%q, %v, %s, %s, %s)", g.rt(), t.Func, t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
		case "map":
			if x, ok := g.Projected(t, locals); ok {
				if g.GoBudgetControls {
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
		if lx, ok := compile.TextArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
			if rx, ok := compile.TextArg(t.R); ok {
				// Compare the strings directly (see evalCtx.binary).
				xs := g.terms([]grammar.Term{lx, rx}, s, locals)
				if g.GoBudgetControls {
					return fmt.Sprintf("any(c.P.TextEqual(c.TextStr(%s), c.TextStr(%s)) == %v)", xs[0], xs[1], t.Op == "==")
				}
				return fmt.Sprintf("any(c.TextStr(%s) %s c.TextStr(%s))", xs[0], t.Op, xs[1])
			}
		}
		if g.Table == "trules" && comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) && !g.BoxedComparisons {
			// Compare string fields without boxing them (see tctx.compare).
			xs := g.seq(func() string { return g.operand(t.L, s, locals) }, func() string { return g.operand(t.R, s, locals) })
			return fmt.Sprintf("c.Compare(%q, %s, %s)", t.Op, xs[0], xs[1])
		}
		if t.Op == "&&" || t.Op == "||" {
			// The right operand is evaluated only if the left one does not decide the result.
			l, r := g.term(t.L, s, locals), g.closure(t.R, s, locals)
			fn := map[string]string{"&&": "And", "||": "Or"}[t.Op]
			if g.GoBudgetControls {
				return fmt.Sprintf("c.%s(%s, %s)", fn, l, r)
			}
			return fmt.Sprintf("%s%s(%s, %s)", g.rt(), fn, l, r)
		}
		xs := g.terms([]grammar.Term{t.L, t.R}, s, locals)
		l, r := xs[0], xs[1]
		if g.GoBudgetControls {
			return fmt.Sprintf("c.Binary(%q, %s, %s)", t.Op, l, r)
		}
		return fmt.Sprintf("%sBinary(%q, %s, %s)", g.rt(), t.Op, l, r)
	case *grammar.Cast:
		x := g.term(t.X, s, locals)
		ft := g.Prog.ResolveCheck(t.Type)
		if ft == nil {
			return x
		}
		v := g.FieldTypeVar(t, ft, func(v string, t *compile.FieldType) string { return fmt.Sprintf("var %s = %s\n", v, goFieldType(t)) })
		line, col := g.Prog.CastPos(t)
		return fmt.Sprintf("c.Cast(%s, %s, %d, %d)", x, v, line, col)
	case *grammar.Unary:
		if g.GoBudgetControls {
			return fmt.Sprintf("c.Unary(%q, %s)", t.Op, g.term(t.X, s, locals))
		}
		return fmt.Sprintf("%sUnary(%q, %s)", g.rt(), t.Op, g.term(t.X, s, locals))
	}
	panic(fmt.Sprintf("unsupported term %T", t))
}

// predicateBody returns the body of a function that returns the value of the predicate term t. A
// membership of a captured text in literals is a switch.
func (g *Generator) predicateBody(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	if !g.DisableLiteralTextDispatch {
		if name, literals, ok := gen.LiteralTextMembership(t); ok {
			var cases []string
			seen := map[string]bool{}
			for _, literal := range literals {
				if !seen[literal] {
					seen[literal] = true
					cases = append(cases, strconv.Quote(literal))
				}
			}
			_, text := g.stmts(func() string { return g.term(&grammar.CaptureRef{Name: name}, s, locals) })
			fast := fmt.Sprintf("switch c.TextStr(%s) { case %s: return true }; return false", text, strings.Join(cases, ", "))
			if g.GoBudgetControls {
				return "if c.P.Control != nil { " + g.termBody(t, s, locals) + " }; " + fast
			}
			return fast
		}
	}
	return g.termBody(t, s, locals)
}

// concatParts emits the statements that push the elements of the arguments of the concat call t.
func (g *Generator) concatParts(t *grammar.Call, s *compile.Scope, locals map[string]string) {
	for _, a := range t.Args {
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list":
			g.emit("c.PushList(" + strings.Join(g.terms(call.Args, s, locals), ", ") + ")")
		case ok && call.Func == "map" && len(call.Args) == 2:
			if x, ok := g.Projected(call, locals); ok {
				g.emit("c.PushItems(" + g.term(x, s, locals) + ")")
				break
			}
			list := g.term(call.Args[0], s, locals)
			g.emit(fmt.Sprintf("c.PushMap(%s, %s)", list, g.lambda(call.Args[1], s, locals)))
		case ok && call.Func == "concat":
			g.concatParts(call, s, locals)
		default:
			g.emit("c.PushItems(" + g.term(a, s, locals) + ")")
		}
	}
}

// emit adds a statement to run before the expression of the term being printed.
func (g *Generator) emit(stmt string) {
	if g.hoist == nil {
		panic("generator: a term is printed outside a statement context")
	}
	*g.hoist = append(*g.hoist, stmt)
}

// stmts prints with f, which prints terms, and returns the statements the terms need before the
// expression f returns, joined as the statements of a block.
func (g *Generator) stmts(f func() string) (stmts, expr string) {
	prev := g.hoist
	var own []string
	g.hoist = &own
	defer func() { g.hoist = prev }()
	expr = f()
	var b strings.Builder
	for _, st := range own {
		b.WriteString(st + "; ")
	}
	return b.String(), expr
}

// termBody returns the body of a function that returns the value of t: the statements t needs, then
// the return of its expression.
func (g *Generator) termBody(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	stmts, expr := g.stmts(func() string { return g.term(t, s, locals) })
	return stmts + "return " + expr
}

// closure returns a function literal that evaluates t when it is called, for a term that is
// evaluated only if needed.
func (g *Generator) closure(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	return "func() any { " + g.termBody(t, s, locals) + " }"
}

// terms prints the terms of ts, other than lambdas, in the order they are evaluated (see seq). The
// lambdas stay empty.
func (g *Generator) terms(ts []grammar.Term, s *compile.Scope, locals map[string]string) []string {
	parts := make([]func() string, len(ts))
	for i, t := range ts {
		parts[i] = func() string {
			if _, ok := t.(*grammar.Lambda); ok {
				return ""
			}
			return g.term(t, s, locals)
		}
	}
	return g.seq(parts...)
}

// seq prints the operands of an expression in the order Go evaluates them. If an operand needs
// statements before its expression, the expressions of the operands before it are assigned to
// temporaries first, ahead of those statements, so they are still evaluated before it.
func (g *Generator) seq(parts ...func() string) []string {
	out := make([]string, len(parts))
	for i, part := range parts {
		mark := len(*g.hoist)
		out[i] = part()
		if len(*g.hoist) == mark {
			continue
		}
		var spill []string
		for j := range i {
			if out[j] == "" || fixedValue.MatchString(out[j]) {
				continue
			}
			v := g.Name("t_")
			spill = append(spill, v+" := "+out[j])
			out[j] = v
		}
		h := *g.hoist
		*g.hoist = slices.Concat(h[:mark:mark], spill, h[mark:])
	}
	return out
}

// fixedValue matches the expressions of operands that no statement changes: literals, a read of a
// capture slot, the variables of captures (k1, v2), and the bare names of lambda parameters and
// temporaries, alone or in the parentheses or UnboxValue call that print a parameter (see term).
var fixedValue = regexp.MustCompile(`^(any\((-?\d+|"([^"\\]|\\.)*"|true|false|nil)\)|c\.Cap\(\d+\)|(?:(?:\w+\.)?UnboxValue)?\(l_\d+\)|[lt]_\d+|[kv]\d+)$`)

func (g *Generator) lambda(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	l := t.(*grammar.Lambda)
	inner := map[string]string{}
	for k, v := range locals {
		inner[k] = v
	}
	params := make([]string, len(l.Params))
	for i, p := range l.Params {
		params[i] = g.Name("l_")
		inner[p] = params[i]
	}
	// The body charges nothing: the runtime built-in that calls the lambda charges one step per element.
	return fmt.Sprintf("func(%s any) any { %s }", strings.Join(params, ", "), g.termBody(l.Body, s, inner))
}

// ControlledClassReject preserves the compact inactive condition while bounded
// membership uses immutable generated range tables. TS keeps its own runtime.
func (g *Generator) ControlledClassReject(e *grammar.CharClass) string {
	ordinary := classReject(e)
	if !g.GoBudgetControls || g.NoBudget || len(e.Ranges) <= 4 {
		// Without budget controls the class is tested without a control, so it has no range table.
		return ordinary
	}
	key := fmt.Sprintf("%v%#v", e.Negated, e.Ranges)
	if g.budgetClassNames == nil {
		g.budgetClassNames = map[string]string{}
	}
	name := g.budgetClassNames[key]
	if name == "" {
		name = g.Name("budgetClass")
		g.budgetClassNames[key] = name
		g.budgetClassKeys = append(g.budgetClassKeys, key)
		fmt.Fprintf(&g.Vars, "var %s = NewBudgetClass([]BudgetRange{", name)
		for _, r := range e.Ranges {
			fmt.Fprintf(&g.Vars, "{Lo: %d, Hi: %d},", r.Lo, r.Hi)
		}
		fmt.Fprintf(&g.Vars, "}, %v)\n", e.Negated)
	}
	return fmt.Sprintf("((p.Control != nil && p.RejectBudgetClass(ch, %s)) || (p.Control == nil && (%s)))", name, ordinary)
}

// atEnd and atLineStart name the runtime methods that test the anchors; a parser without
// ParseStream uses the forms that need not consider a stream, which inline.
func (g *Generator) atEnd() string {
	if g.streamStart == nil {
		return g.helper("AtEndBatch")
	}
	return g.helper("AtEnd")
}

func (g *Generator) atLineStart() string {
	if g.streamStart == nil {
		return g.helper("AtLineStartBatch")
	}
	return g.helper("AtLineStart")
}

// helper names the runtime method that a generated execution function calls on its parser. A
// parser generated without budget controls and without Documents has no controlled form of its
// bodies, so they call the Ordinary form of the method.
func (g *Generator) helper(name string) string {
	if g.Ordinary && ordinaryHelperName(name) {
		return name + "Ordinary"
	}
	return name
}

// leanTyped reports whether typed rule bodies are emitted in their ordinary form only.
func (g *Generator) leanTyped() bool {
	return g.Ordinary && g.GoBudgetControls && g.Table == "trules"
}

// classTable returns the name of the package variable that holds the membership table of e,
// declaring it on first use.
func (g *Generator) classTable(e *grammar.CharClass) string {
	key := fmt.Sprintf("cls%v%#v", e.Negated, e.Ranges)
	if g.budgetClassNames == nil {
		g.budgetClassNames = map[string]string{}
	}
	if name := g.budgetClassNames[key]; name != "" {
		return name
	}
	name := g.Name("cls")
	g.budgetClassNames[key] = name
	g.budgetClassKeys = append(g.budgetClassKeys, key)
	ascii, wideRunes := classBits(e)
	var wide []string
	for _, r := range wideRunes {
		wide = append(wide, strconv.Itoa(int(r)))
	}
	fmt.Fprintf(&g.Vars, "var %s = Class{ASCII: [2]uint64{%#x, %#x}", name, ascii[0], ascii[1])
	if len(wide) > 0 {
		fmt.Fprintf(&g.Vars, ", Wide: []rune{%s}", strings.Join(wide, ", "))
	}
	if e.Negated {
		g.Vars.WriteString(", Neg: true")
	}
	g.Vars.WriteString("}\n")
	return name
}

// classSteps returns the name of the package variable that lists the class steps of items, which
// are character classes, declaring it on first use.
func (g *Generator) classSteps(items []grammar.Expr) string {
	var steps []string
	for _, it := range items {
		c := it.(*grammar.CharClass)
		steps = append(steps, fmt.Sprintf("{C: &%s, ID: %d}", g.classTable(c), g.Desc(compile.CharClassString(c))))
	}
	body := strings.Join(steps, ", ")
	key := "steps" + body
	if name := g.budgetClassNames[key]; name != "" {
		return name
	}
	name := g.Name("steps")
	g.budgetClassNames[key] = name
	g.budgetClassKeys = append(g.budgetClassKeys, key)
	fmt.Fprintf(&g.Vars, "var %s = []ClassStep{%s}\n", name, body)
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
