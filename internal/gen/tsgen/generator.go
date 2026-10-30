package tsgen

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/ir/core"
)

//go:embed tsrt/runtime.ts
var tsRuntimeSource string

//go:embed tsrt/budget.ts
var tsBudgetSource string

//go:embed tsrt/trace.ts
var tsTraceSource string

//go:embed tsrt/profile.ts
var tsProfileSource string

// tsRuntimeMarker starts the part of tsrt/runtime.ts that is copied into generated code.
const tsRuntimeMarker = "// --- Runtime ---\n"

// Generate generates the files of a TypeScript parser for g, compiling it with c, that behaves
// exactly like the engine (and the generated Go parsers): runtime.ts, with the runtime
// (RuntimeEmbed), parser.ts, with the code generated from the grammar, and with opts.Types
// types.ts, with the declarations of the grammar's types. RuntimeImport imports the runtime from
// opts.RuntimeModule instead of writing it, and RuntimeOnly writes only runtime.ts, for which g may
// be nil. gen.TypeScript describes the options.
//
// Each parsing expression becomes a function and actions and predicates become TypeScript
// expressions, as in the Go generator; the embedded runtime (tsrt) mirrors runtime. opts.Package
// is not used. opts.Types adds declarations and Node-to-value conversion entry points.
func Generate(g *grammar.Grammar, c gen.Compiler, opts gen.GenOptions) ([]gen.GeneratedFile, error) {
	return GenerateParser(g, c, opts, nil)
}

// Hooks change how GenerateParser generates, for tests.
type Hooks struct {
	// ExecRoute prints the functions that the Exec IR switch routes through Exec IR that way even
	// with the switch off (core.Enabled), so that a test compares the code of both routes.
	ExecRoute bool
	// Generator, if not nil, is called with the generator once it has generated.
	Generator func(*Generator)
}

// GenerateParser generates the files of Generate with the hooks h, which may be nil.
func GenerateParser(g *grammar.Grammar, c gen.Compiler, opts gen.GenOptions, h *Hooks) ([]gen.GeneratedFile, error) {
	if h == nil {
		h = &Hooks{}
	}
	if opts.Runtime == gen.RuntimeOnly {
		return TsRuntimeFiles(), nil
	}
	if opts.Runtime == gen.RuntimeImport && opts.RuntimeModule == "" {
		return nil, fmt.Errorf("the TypeScript runtime module (the specifier the parser imports the runtime from) is required with the import runtime mode")
	}
	if opts.Runtime != gen.RuntimeImport && opts.RuntimeModule != "" {
		return nil, fmt.Errorf("the TypeScript runtime module can only be set with the import runtime mode")
	}
	if opts.Document {
		return nil, fmt.Errorf("Document generation is only supported for Go")
	}
	if opts.Stream {
		return nil, fmt.Errorf("stream generation is only supported for Go")
	}
	if opts.TypedOnly {
		return nil, fmt.Errorf("typed-only generation is only supported for Go")
	}
	if err := opts.ValidateMaxDepth(true); err != nil {
		return nil, err
	}
	if err := opts.ValidateDefaultValue(); err != nil {
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
	var rec *compile.Program
	if opts.Recognize {
		if rec, err = recognizer(); err != nil {
			return nil, err
		}
	}
	live, err := reachableRules(prog, rec, start)
	if err != nil {
		return nil, err
	}
	tsg := &Generator{Generator: gen.Generator{Prog: prog, Table: "rules", Observability: opts.Observability, DisableMethodSharing: opts.DisableMethodSharing, DisableLiteralSharing: opts.DisableLiteralSharing || !opts.EnableTSLiteralSharing, DisableLiteralTextDispatch: opts.DisableLiteralTextDispatch}, plainUsed: map[*compile.Rule]bool{}, live: live}
	tsg.NoBudget = !opts.BudgetControls
	tsg.execHook = h.ExecRoute
	if h.Generator != nil {
		defer h.Generator(tsg)
	}
	tsg.optionsType = "ParseOptions"
	if !opts.BudgetControls {
		tsg.optionsType = "PlainParseOptions"
	}
	tsg.Desc(compile.FixedDescs[0]) // the fixed expectations come first
	tsg.rules()
	if tsg.FlowErr != nil {
		return nil, tsg.FlowErr
	}
	if tsg.reachErr != nil {
		return nil, tsg.reachErr
	}
	if rec != nil {
		tsg.Prog, tsg.Table = rec, "recRules"
		tsg.rules()
		tsg.Prog, tsg.Table = prog, "rules"
		if tsg.FlowErr != nil {
			return nil, tsg.FlowErr
		}
		if tsg.reachErr != nil {
			return nil, tsg.reachErr
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "const G = mkGrammar(%d, %v, %v, %v);\n\n", gen.GeneratedMaxDepth(opts), gen.UseSparseMemo(opts, prog, rec), opts.Observability, opts.BudgetControls)
	fmt.Fprintf(&out, "/**\n * Parses the whole input with the rule %s. unit selects the position unit (CodePoints by default).\n", start.Name)
	out.WriteString(" * A string is parsed as its UTF-8 encoding would be (lone surrogates as U+FFFD), and bytes are\n")
	out.WriteString(" * decoded as UTF-8 (invalid sequences as U+FFFD, one per byte). If the parse recovered from errors\n")
	out.WriteString(" * with #recover, the result has both the node and SyntaxErrors.\n */\n")
	// With budget controls every parse is controlled: the entry points without options use the default
	// budget of the input, as parseWithOptions does with no budget.
	parseBody, ruleBody := fmt.Sprintf("run(G, R%d, nseen, input, unit)", start.ID), "run(G, r, nseen, input, unit)"
	if opts.BudgetControls {
		parseBody, ruleBody = "parseWithOptions(input, { unit: unit ?? CodePoints })", "parseRuleWithOptions(name, input, { unit: unit ?? CodePoints })"
	}
	fmt.Fprintf(&out, "export function parse(input: string | Uint8Array, unit?: Unit): ParseResult {\n  return %s;\n}\n\n", parseBody)
	if opts.BudgetControls {
		fmt.Fprintf(&out, `/** Parses the whole input with the start rule name, a rule marked @start; "" selects the default start rule. */
export function parseRule(name: string, input: string | Uint8Array, unit?: Unit): ParseResult {
  return %s;
}

`, ruleBody)
	} else {
		fmt.Fprintf(&out, `/** Parses the whole input with the start rule name, a rule marked @start; "" selects the default start rule. */
export function parseRule(name: string, input: string | Uint8Array, unit?: Unit): ParseResult {
  const r = startIn(rules, ruleNames, starts, defaultStart, name);
  if (r instanceof Error) return { node: null, error: r };
  return %s;
}

`, ruleBody)
	}
	if rec != nil {
		fmt.Fprintf(&out, "/**\n * Checks that the whole input matches the rule %s without building a tree, and returns the syntax\n", start.Name)
		out.WriteString(" * error parse would return (SyntaxErrors for errors recovered with #recover), or null. Actions needed by\n")
		out.WriteString(" * predicates are evaluated and can report runtime errors; other actions are skipped.\n */\n")
		recBody := fmt.Sprintf("run(G, Q%d, recNseen, input, unit).error", rec.ByName[start.Name].ID)
		if opts.BudgetControls {
			recBody = "recognizeWithOptions(input, { unit: unit ?? CodePoints })"
		}
		fmt.Fprintf(&out, "export function recognize(input: string | Uint8Array, unit?: Unit): Error | null {\n  return %s;\n}\n\n", recBody)
	}
	fmt.Fprintf(&out, `/** Parses with invocation-local position and depth options. */
export function parseWithOptions(input: string | Uint8Array, options?: %[1]s): ParseResult {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { node: null, error: o };
  return withObservedOptions(o, control => run(G, R%[2]d, nseen, input, o.unit, o.maxDepth, control, o), error => ({ node: null, error }));
}

/** Parses a named start rule ("" for the default) with invocation-local options, validated before rule lookup. */
export function parseRuleWithOptions(name: string, input: string | Uint8Array, options?: %[1]s): ParseResult {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return { node: null, error: o };
  const r = startIn(rules, ruleNames, starts, defaultStart, name);
  if (r instanceof Error) return { node: null, error: r };
  return withObservedOptions(o, control => run(G, r, nseen, input, o.unit, o.maxDepth, control, o), error => ({ node: null, error }));
}

`, tsg.optionsType, start.ID)
	if rec != nil {
		fmt.Fprintf(&out, `/** Recognizes with invocation-local options; predicate-dependent actions may run. */
export function recognizeWithOptions(input: string | Uint8Array, options?: %[1]s): Error | null {
  const o = resolveOptions(G, options, input);
  if (o instanceof Error) return o;
  return withObservedOptions<Error | null>(o, control => run(G, Q%[2]d, recNseen, input, o.unit, o.maxDepth, control, o).error, error => error);
}

`, tsg.optionsType, rec.ByName[start.Name].ID)
	}
	var types string
	if opts.Types {
		var typed string
		if types, typed, err = tsg.typedTS(start); err != nil {
			return nil, err
		}
		out.WriteString(typed)
	}
	if len(tsg.FieldTypes) > 0 {
		out.WriteString(tsg.fieldCheckSource())
	}
	out.WriteString(tsg.Vars.String())
	if opts.Expectations {
		fmt.Fprintf(&out, "\nG.descs = %s;\nG.expectations = true;\n", TsStrings(tsg.Descs))
	}
	if prog.RecordsSourceInfo {
		out.WriteString("G.sourceInfo = true;\n")
	}
	for _, v := range prog.Vars.All() {
		switch x := v.Init.(type) {
		case int:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %s]);\n", TsString(v.Name), tsInt(x))
		case string:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %s]);\n", TsString(v.Name), TsString(x))
		default:
			fmt.Fprintf(&out, "G.initialVars.push([%s, %v]);\n", TsString(v.Name), x)
		}
	}
	out.WriteString("\n")
	out.WriteString(tsg.Init.String())
	out.WriteString("\n")
	out.WriteString(tsg.Methods.String())
	return tsFiles(opts, out.String(), types), nil
}

// TsPrintRuns returns the code points that strconv.IsPrint accepts, for the runtime's quoteGo: the
// lengths of the alternating runs of code points that it rejects and accepts, from U+0000, in base
// 36 separated by commas (about 3 KB). Writing the table of the Go that generates the parser, rather
// than relying on the JavaScript runtime's Unicode properties, makes toString quote as the engine's
// Node.String does even where their Unicode versions differ.
var TsPrintRuns = sync.OnceValue(func() string {
	var b strings.Builder
	prev, start := false, 0
	for r := 0; r <= unicode.MaxRune; r++ {
		if p := strconv.IsPrint(rune(r)); p != prev {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatInt(int64(r-start), 36))
			prev, start = p, r
		}
	}
	return b.String()
})

// fieldCheckSource returns the run-time type checks the parser uses, as runtime/fieldcheck.go
// declares them: tsFieldCheckSource with castValue if called.
func (g *Generator) fieldCheckSource() string {
	src := tsFieldCheckSource
	if g.NoBudget {
		src = strings.ReplaceAll(src, "    p.spend();\n", "")
	}
	for _, f := range []string{"castValue"} {
		if strings.Contains(g.Init.String(), f+"(") || strings.Contains(g.Vars.String(), f+"(") || strings.Contains(g.Methods.String(), f+"(") {
			src += tsCheckSources[f]
		}
	}
	return src
}

// tsCheckSources are the check functions of fieldCheckSource, by name.
var tsCheckSources = map[string]string{
	"castValue": `
// castValue returns v if it fits t, the type of a cast, and raises an evaluation error otherwise.
// The error names the input position where the action started and, when gline is positive, the
// grammar position of the cast.
function castValue(c: Actx, v: Value, t: FieldType, gline: number, gcol: number): Value {
  if (!fieldConforms(c.p, t, v)) {
    const e = c.p.makeError(c.start, []);
    return evalError(` + "`cannot cast ${typeName(v)} to ${t.text} at input position ${c.start} (line ${e.line}, column ${e.col})${gline > 0 ? `; cast at grammar position ${gline}:${gcol}` : \"\"}`" + `);
  }
  return v;
}
`,
}

// tsFieldCheckSource declares the check types and fieldConforms, which checks constructed field
// values and rule results, as runtime/fieldcheck.go does.
const tsFieldCheckSource = `
// --- Run-time type checks ---

type FieldType = { kind: string; name: string; elem: FieldType | null; alts: readonly FieldType[]; text: string };

// fieldConforms reports whether v is a value of type t in the value model, charging a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value; a list or tuple type takes a list or tuple value,
// not the node of a repetition or sequence.
function fieldConforms(p: Parser, t: FieldType, v: Value | undefined): boolean {
  if (v instanceof Node && nodeShape(v) === shapeBox) {
    return fieldConforms(p, t, v.box);
  }
  switch (t.kind) {
    case "_":
      return true;
    case "i":
      return typeof v === "number" || typeof v === "bigint";
    case "s":
      return typeof v === "string";
    case "b":
      return typeof v === "boolean";
    case "z":
      return v === null || v === undefined;
    case "o":
      return v === null || v === undefined || fieldConforms(p, t.elem!, v);
    case "u":
      for (const a of t.alts) {
        if (fieldConforms(p, a, v)) return true;
      }
      return false;
  }
  if (!(v instanceof Node)) return false;
  const shape = nodeShape(v);
  const valueNode = shape === shapeString || shape === shapeTuple || shape === shapeList;
  switch (t.kind) {
    case "n":
      return valueNode;
    case "S":
      return shape === shapeString;
    case "E":
      return shape === shapeError;
    case "N":
      return v.type === t.name;
    case "l":
      return shape === shapeListValue && elemsConform(p, t.elem!, v.children);
    case "t":
      return shape === shapeTupleValue && tupleConforms(p, t.alts, v.children);
    case "v":
      return nodeValueConforms(p, t.elem!, v, shape);
  }
  return false;
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
function nodeValueConforms(p: Parser, t: FieldType, n: Node, shape: number): boolean {
  switch (t.kind) {
    case "_":
      return shape === shapeString || shape === shapeTuple || shape === shapeList;
    case "s":
      return shape === shapeString;
    case "l":
      return shape === shapeList && elemsConform(p, t.elem!, n.children);
    case "t":
      return shape === shapeTuple && tupleConforms(p, t.alts, n.children);
    case "o":
      return nodeValueConforms(p, t.elem!, n, shape); // a node always has a value
    case "u":
      for (const a of t.alts) {
        if (nodeValueConforms(p, a, n, shape)) return true;
      }
      return false;
  }
  // No node has a value of another type yet.
  return false;
}

// elemsConform reports whether every element of a list is of type t, charging a step for each.
function elemsConform(p: Parser, t: FieldType, elems: readonly (Node | null)[]): boolean {
  if (t.kind === "_") return true;
  for (const k of elems) {
    p.spend();
    if (!fieldConforms(p, t, k)) return false;
  }
  return true;
}

// tupleConforms reports whether the elements of a tuple are of the types ts, charging a step for
// each.
function tupleConforms(p: Parser, ts: readonly FieldType[], elems: readonly (Node | null)[]): boolean {
  if (elems.length !== ts.length) return false;
  for (let i = 0; i < elems.length; i++) {
    p.spend();
    if (!fieldConforms(p, ts[i]!, elems[i])) return false;
  }
  return true;
}

`

// tsFieldTypeDecl declares the check type variable v.
func tsFieldTypeDecl(v string, t *compile.FieldType) string {
	return fmt.Sprintf("const %s: FieldType = %s;\n", v, tsFieldType(t))
}

// tsFieldType writes a field type as a generated FieldType value.
func tsFieldType(t *compile.FieldType) string {
	elem := "null"
	if t.Elem != nil {
		elem = tsFieldType(t.Elem)
	}
	alts := make([]string, len(t.Alts))
	for i, a := range t.Alts {
		alts[i] = tsFieldType(a)
	}
	return fmt.Sprintf("{ kind: %s, name: %s, elem: %s, alts: [%s], text: %s }", TsString(string(rune(t.Kind))), TsString(t.Name), elem, strings.Join(alts, ", "), TsString(t.Text))
}

// Generator generates TypeScript. It shares the expectation table, names and analyses of the Go
// generator through gen.Generator.
type Generator struct {
	gen.Generator
	// plainUsed holds the plain rules whose function (plainCall) is called.
	plainUsed map[*compile.Rule]bool
	// optionsType is the name of the runtime's options type that the entry points take: ParseOptions
	// with budget controls, PlainParseOptions without.
	optionsType string
	// live holds the rules that a parse can enter (reach.go).
	live     *tsLive
	reachErr error
	// execHook routes the functions of execRoute through Exec IR with the switch off (Hooks).
	execHook bool
	// ExecFuncs counts the functions printed from Exec IR (execRoute).
	ExecFuncs int
}

// TsString returns s as a TypeScript string literal (JSON is valid TypeScript).
func TsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TsStrings returns xs as a TypeScript array literal of strings.
func TsStrings(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = TsString(x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// TsInts returns xs as a TypeScript array literal of numbers.
func TsInts[T ~int32 | ~byte](xs []T) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Itoa(int(x))
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func tsJoinCondition(parts []string, op string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	mid := len(parts) / 2
	return "(" + tsJoinCondition(parts[:mid], op) + op + tsJoinCondition(parts[mid:], op) + ")"
}

func (g *Generator) seenVar() string {
	if g.Table == "rules" {
		return "nseen"
	}
	return "recNseen"
}

// ruleVar returns the constant that holds the rule r of the table being generated.
func (g *Generator) ruleVar(r *compile.Rule) string {
	if g.Table == "rules" {
		return fmt.Sprintf("R%d", r.ID)
	}
	return fmt.Sprintf("Q%d", r.ID)
}

func (g *Generator) plainPrefix() string {
	if g.Table == "rules" {
		return "r"
	}
	return "q"
}

// Method emits a function of an expression with the given body and returns its name.
//
// fn turns a call expression into a function.
func (g *Generator) Method(comment, body string) string {
	if !g.NoBudget {
		body = "  p.spend();\n" + body
	}
	name, shared := g.MethodName("Parser/R", body)
	if shared {
		return name
	}
	if comment != "" {
		fmt.Fprintf(&g.Methods, "// %s\n", comment)
	}
	param := "p"
	if !strings.Contains(body, "p.") && !strings.Contains(body, "(p)") {
		param = "_p" // not reported as unused (the body is in generated code only, without user text)
	}
	fmt.Fprintf(&g.Methods, "function %s(%s: Parser): R {\n%s}\n\n", name, param, body)
	return name
}

// fn returns a function (a Matcher) that does what the call expression x does.
func (g *Generator) fn(x string) string {
	if name, ok := strings.CutSuffix(x, "(p)"); ok && !strings.ContainsAny(name, "(). ") {
		return name
	}
	return g.Method("", "  return "+x+";\n")
}

// tsActxUse matches a use of the evaluation context c in the text of a term.
var tsActxUse = regexp.MustCompile(`\bc\b`)

// tsActxParam returns the name of the evaluation context parameter of a function that returns the term
// body: c, or _c if the term reads no context (a leading _ keeps it from being reported as unused).
func tsActxParam(body string) string {
	if tsActxUse.MatchString(body) {
		return "c"
	}
	return "_c"
}

// action emits a function that evaluates the term t (an action or a predicate) and returns its name.
func (g *Generator) action(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	name := g.Name("a")
	body := g.term(t, s, locals)
	param := tsActxParam(body)
	fmt.Fprintf(&g.Methods, "function %s(%s: Actx): Value {\n  return %s;\n}\n\n", name, param, body)
	return name
}

func (g *Generator) rules() {
	g.startTSFlow()
	init := &g.Init
	bodies := map[*compile.Rule]string{}
	scopes := map[*compile.Rule]*compile.Scope{}
	var all []*compile.Rule
	for _, r := range append(append([]*compile.Rule(nil), g.Prog.RuleList...), g.Prog.Twins...) {
		if !g.pruned(r) {
			all = append(all, r)
		}
	}
	for _, r := range all {
		s := compile.NewScope()
		scopes[r] = s
		if _, ok := r.Def.Expr.(*grammar.OperatorsRule); !ok {
			g.Cur, g.Proj = r, g.Projections(r)
			if call, ok := g.flowRule(r, s); ok {
				bodies[r] = call
			} else {
				g.FailFlow("rule " + r.Name + " of table " + g.Table)
				bodies[r] = "null" // the generation fails with the error g.failFlow records
			}
			g.Proj = nil
		}
	}
	// Each rule is a constant, which the generated code refers to.
	var vars []string
	for _, r := range all {
		if !g.tableRow(r) {
			continue
		}
		value := tsShaper(g.Prog, r.BodyExpr())
		if _, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
			value = 0 // the lines build the rule's value
		}
		fmt.Fprintf(init, "const %s = mkRule(%d, %s, %s, %d, %v, %v, %d, %v, %s);\n", g.ruleVar(r),
			r.ID, TsString(r.Name), TsStrings(scopes[r].Names), value, r.Memo && !r.Transient, r.Leader, r.Seen, r.Novalue, tsIntList(g.Prog.Vars.Slots(r.Vars)))
		vars = append(vars, g.ruleVar(r))
		if r.Splice {
			fmt.Fprintf(init, "%s.splice = true;\n", g.ruleVar(r))
		}
		if r.Rec {
			fmt.Fprintf(init, "%s.rec = true;\n", g.ruleVar(r))
		}
		if w := r.WhereName(); w != r.Name {
			fmt.Fprintf(init, "%s.where = %s;\n", g.ruleVar(r), TsString(w))
		}
		if r.NodeType != "" {
			fmt.Fprintf(init, "%s.nodeType = %s;\n", g.ruleVar(r), TsString(r.NodeType))
		}
		if r.OpNodeType != "" {
			fmt.Fprintf(init, "%s.opNodeType = %s;\n", g.ruleVar(r), TsString(r.OpNodeType))
		}
	}
	if g.Table == "rules" {
		// The table and the start rules, for parseRule (the recognizer has none).
		fmt.Fprintf(init, "const rules: Rule[] = [%s];\n", strings.Join(vars, ", "))
		var names []string
		for _, r := range g.Prog.RuleList {
			names = append(names, r.Name)
		}
		fmt.Fprintf(init, "const ruleNames = %s;\n", TsStrings(names))
		fmt.Fprintf(init, "const starts = %s;\n", TsStrings(g.Prog.StartRules))
		fmt.Fprintf(init, "const defaultStart = %s;\n", TsString(g.Prog.DefaultStartRule))
	}
	fmt.Fprintf(init, "const %s = %d;\n", g.seenVar(), g.Prog.NSeen)
	for _, r := range all {
		if pr, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
			g.Cur = r
			g.operatorsRule(r, pr)
			if r.Action != nil && !r.Novalue {
				// The whole-rule action, which the runtime runs on the folded value after each call.
				locals := map[string]string{compile.OperatorsRuleValue: "c.value"}
				fmt.Fprintf(init, "%s.action = %s;\n", g.ruleVar(r), g.action(r.Action, compile.NewScope(), locals))
			}
			continue
		}
		if g.slotReached(r) {
			fmt.Fprintf(init, "%s.body = %s;\n", g.ruleVar(r), g.fn(bodies[r]))
		}
		if r.Action != nil && g.tableRow(r) {
			g.Proj = g.Projections(r)
			fmt.Fprintf(init, "%s.action = %s;\n", g.ruleVar(r), g.action(r.Action, scopes[r], nil))
			g.Proj = nil
		}
	}
	// The functions of plain rules that are called (all calls are generated by now).
	for _, r := range all {
		if g.plainUsed[r] {
			g.plainCall(r, bodies[r])
		}
	}
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
			fields = append(fields, f.Name)
		}
		fmt.Fprintf(init, "G.structFields.set(%s, new Set(%s));\n", TsString(name), TsStrings(fields))
	}
}

// plainCall writes the function r<id>, which does what invokePlain does for the rule r (a rule never
// memoized and without captures) with a direct call of its body.
func (g *Generator) plainCall(r *compile.Rule, body string) {
	m := &g.Methods
	name := g.tracePlainCall(r, fmt.Sprintf("%s%d", g.plainPrefix(), r.ID))
	fmt.Fprintf(m, "// %s, called as by invokePlain\nfunction %s(p: Parser): R {\n", r.Name, name)
	if !g.NoBudget {
		m.WriteString("  p.spend();\n")
	}
	m.WriteString(`  const start = p.pos;
  const rec = p.recovered.length;
  const trail = p.trail.length;
  const prevEnv = p.env;
  const prevCut = p.cut;
  p.cut = false;
`)
	if r.Rec {
		m.WriteString("  if (++p.depth > p.maxDepth) {\n    p.tooDeep();\n  }\n")
	}
	fmt.Fprintf(m, "  let v = %s;\n", body)
	if r.Rec {
		m.WriteString("  p.depth--;\n")
	}
	m.WriteString(`  p.cut = prevCut;
  p.trail.length = trail;
  if (v !== undefined) {
`)
	if r.Novalue {
		m.WriteString("    v = null;\n")
	} else {
		fmt.Fprintf(m, "    v = p.finish(%s, emptyFrame, v, start);\n", g.ruleVar(r))
	}
	m.WriteString(`  } else {
    p.pos = start;
    p.recovered.length = rec;
  }
  p.env = prevEnv;
  return v;
}

`)
}

// tsShaper returns the runtime shaper of the expression e, its static shape (internal/compile/value_shape.go), packed
// as the runtime's Shaper.
func tsShaper(prog *compile.Program, e grammar.Expr) int {
	s := int(compile.SingleModeOf(prog, e))<<2 | compile.ValuedCount(prog, e)<<5
	if compile.IsElems(prog, e) {
		s |= 1
	}
	if compile.ScopeTuple(prog, e) {
		s |= 2
	}
	if compile.IsElems(prog, e) && compile.SharesTuple(prog, e) {
		s |= 16
	}
	return s
}

func (g *Generator) operatorsRule(r *compile.Rule, e *grammar.OperatorsRule) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.operatorsRule = {\n", g.ruleVar(r))
	// The lines of the rule's Core IR, in the order of e, if the printer prints them from Flow IR.
	var co *core.OperatorsRule
	if g.Flow != nil {
		co, _ = g.Flow.M.Rules[r.ID].Body.(*core.OperatorsRule)
	}
	if e.Skip != nil {
		var skip *core.OpLine
		if co != nil {
			skip = co.Skip
		}
		m, ok := g.flowLine(skip, e.Skip, compile.NewScope(), false)
		if !ok {
			g.FailFlow("the skip of an Operators Rule of rule " + r.Name)
			m = "null"
		}
		fmt.Fprintf(&b, "  skip: %s,\n", g.fn(m))
	} else {
		b.WriteString("  skip: null,\n")
	}
	b.WriteString("  operands: [\n")
	for i, o := range e.Operands {
		var l *core.OpLine
		if co != nil {
			l = co.Operands[i]
		}
		fmt.Fprintf(&b, "    %s,\n", g.operatorsRuleLine(o.Expr, o.Action, false, l))
	}
	b.WriteString("  ],\n")
	var prefix, led []string
	id := 0
	for i, l := range e.Levels {
		for _, op := range l.Operators {
			var line *core.OpLine
			if co != nil {
				line = co.Ops[id].Line
			}
			s := fmt.Sprintf("    { id: %d, kind: %s, assoc: %s, level: %d, line: %s },\n", id, TsString(op.Kind), TsString(op.Assoc), i+1, g.operatorsRuleLine(op.Expr, op.Action, true, line))
			id++
			if op.Kind == grammar.Prefix {
				prefix = append(prefix, s)
			} else {
				led = append(led, s)
			}
		}
	}
	fmt.Fprintf(&b, "  prefix: [\n%s  ],\n", strings.Join(prefix, ""))
	fmt.Fprintf(&b, "  led: [\n%s  ],\n", strings.Join(led, ""))
	b.WriteString("};\n")
	fmt.Fprintf(&b, "%s.body = (p: Parser, min: number): R => p.operatorsRuleParse(%s, min);\n", g.ruleVar(r), g.ruleVar(r))
	g.Init.WriteString(b.String())
}

// operatorsRuleLine returns the line of an Operators Rule with the expression e and the action; l is
// its Core IR if the printer prints it from Flow IR, or nil.
func (g *Generator) operatorsRuleLine(e grammar.Expr, action grammar.Term, operator bool, l *core.OpLine) string {
	s := compile.NewScope()
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	// Lines whose value is unused are matched without values, as in the engine; so are all lines
	// of a rule without a value (in the recognizer).
	build := !g.Cur.Novalue && !compile.LeanLine(action, locals)
	m, ok := g.flowLine(l, e, s, build)
	if !ok {
		g.FailFlow("a line of an Operators Rule of rule " + g.Cur.Name)
		m = "null"
	}
	m = g.fn(m)
	act := "null"
	if action != nil && !g.Cur.Novalue {
		var locals map[string]string
		if operator {
			locals = map[string]string{"lhs": "c.lhs", "rhs": "c.rhs", "op": "c.op"}
		}
		act = g.action(action, s, locals)
	}
	return fmt.Sprintf("{ scope: %s, m: %s, action: %s, value: %d }", TsStrings(s.Names), m, act, tsShaper(g.Prog, e))
}

// tsInt returns the TypeScript value of the int v: a number when it is a safe integer, otherwise a
// bigint (see the runtime's ints).
func tsInt(v int) string {
	const maxSafe = 1<<53 - 1
	if int64(v) >= -maxSafe && int64(v) <= maxSafe {
		return strconv.Itoa(v)
	}
	return "BigInt(\"" + strconv.Itoa(v) + "\")"
}

// term converts a value expression into a TypeScript expression of type Value, charging a step for
// it with budget controls. locals maps names, such as lambda parameters, to TypeScript expressions.
func (g *Generator) term(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	if g.NoBudget {
		return g.termBody(t, s, locals)
	}
	return "(c.spend(), " + g.termBody(t, s, locals) + ")"
}

func (g *Generator) termBody(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	switch t := t.(type) {
	case *grammar.IntLit:
		return tsInt(t.Value)
	case *grammar.StringLit:
		return TsString(t.Value)
	case *grammar.BoolLit:
		return strconv.FormatBool(t.Value)
	case *grammar.NilLit:
		return "null"
	case *grammar.CaptureRef:
		if l, ok := locals[t.Name]; ok {
			return "unboxValue(" + l + ")"
		}
		return fmt.Sprintf("c.cap(%d)", s.Slots[t.Name])
	case *grammar.VarRef:
		return fmt.Sprintf("c.variable(%d)", g.Prog.Vars.Index(t.Name))
	case *grammar.Member:
		return fmt.Sprintf("c.member(%s, %s)", g.term(t.X, s, locals), TsString(t.Name))
	case *grammar.New:
		if len(t.Args) != 0 {
			// A node type constructor converts its value (see evalCtx.convert).
			x := g.term(t.Args[0], s, locals)
			n, _ := g.Prog.NodeTypeOf(t.Type) // the type's own name, also for an alias of it
			return fmt.Sprintf("c.convert(%s, %s)", TsString(n.Name), x)
		}
		name, _ := g.Prog.StructType(t.Type)
		args := []string{TsString(name)}
		for _, f := range t.Fields {
			args = append(args, TsString(f.Name), g.term(f.Value, s, locals))
		}
		return "c.newStruct(" + strings.Join(args, ", ") + ")"
	case *grammar.ListConstructor:
		return g.term(&grammar.Call{Func: "list", Args: t.Items}, s, locals)
	case *grammar.TupleLit:
		args := make([]string, len(t.Elems))
		for i, x := range t.Elems {
			args[i] = g.term(x, s, locals)
		}
		return "c.tuple(" + strings.Join(args, ", ") + ")"
	case *grammar.Call:
		if t.Func == "concat" && compile.Fusable(t) {
			// Push the elements of list, map and concat arguments directly (see fusable and
			// evalCtx.pushConcatArg).
			var b strings.Builder
			b.WriteString("((): Value => { const b = c.p.kidStack.length; ")
			g.concatParts(&b, t, s, locals)
			b.WriteString("return c.endList(b); })()")
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
			return fmt.Sprintf("rtFold(%s, %v, %s, %s, %s, c.p.control)", TsString(t.Func), t.Func == "foldr", args[0], args[1], g.lambda(t.Args[2], s, locals))
		case "map":
			if x, ok := g.Projected(t, locals); ok {
				return "c.newList(listItems(\"map\", " + g.term(x, s, locals) + "))"
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
		if lx, ok := compile.TextArg(t.L); ok && (t.Op == "==" || t.Op == "!=") {
			if rx, ok := compile.TextArg(t.R); ok {
				// Compare the strings directly (see evalCtx.binary).
				return fmt.Sprintf("c.binary(%s, c.textStr(%s), c.textStr(%s))", TsString(t.Op), g.term(lx, s, locals), g.term(rx, s, locals))
			}
		}
		l, r := g.term(t.L, s, locals), g.term(t.R, s, locals)
		switch t.Op {
		case "&&":
			return fmt.Sprintf("rtAnd(%s, (): Value => %s)", l, r)
		case "||":
			return fmt.Sprintf("rtOr(%s, (): Value => %s)", l, r)
		}
		return fmt.Sprintf("c.binary(%s, %s, %s)", TsString(t.Op), l, r)
	case *grammar.Unary:
		return fmt.Sprintf("rtUnary(%s, %s)", TsString(t.Op), g.term(t.X, s, locals))
	case *grammar.Cast:
		x := g.term(t.X, s, locals)
		ft := g.Prog.ResolveCheck(t.Type)
		if ft == nil {
			return x
		}
		line, col := g.Prog.CastPos(t)
		return fmt.Sprintf("castValue(c, %s, %s, %d, %d)", x, g.FieldTypeVar(t, ft, tsFieldTypeDecl), line, col)
	}
	panic(fmt.Sprintf("unsupported term %T", t))
}

func (g *Generator) predicateTerm(t grammar.Term, s *compile.Scope) string {
	if !g.DisableLiteralTextDispatch {
		if name, literals, ok := gen.LiteralTextMembership(t); ok {
			var cases []string
			seen := map[string]bool{}
			for _, literal := range literals {
				if !seen[literal] {
					seen[literal] = true
					cases = append(cases, "case "+TsString(literal)+":")
				}
			}
			fast := fmt.Sprintf("((): Value => { switch (c.textStr(%s)) { %s return true; default: return false; } })()", g.term(&grammar.CaptureRef{Name: name}, s, nil), strings.Join(cases, " "))
			return "(c.p.control === null ? " + fast + " : " + g.term(t, s, nil) + ")"
		}
	}
	return g.term(t, s, nil)
}

// concatParts writes the statements that push the elements of the arguments of the concat call t.
func (g *Generator) concatParts(b *strings.Builder, t *grammar.Call, s *compile.Scope, locals map[string]string) {
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
			if x, ok := g.Projected(call, locals); ok {
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

func (g *Generator) lambda(t grammar.Term, s *compile.Scope, locals map[string]string) string {
	l := t.(*grammar.Lambda)
	inner := map[string]string{}
	for k, v := range locals {
		inner[k] = v
	}
	params := make([]string, len(l.Params))
	for i, p := range l.Params {
		params[i] = g.Name("_l") // a leading _: an unused parameter is not reported
		inner[p] = params[i]
	}
	for i := range params {
		params[i] += ": Value"
	}
	return fmt.Sprintf("(%s): Value => %s", strings.Join(params, ", "), g.term(l.Body, s, inner))
}

// tsIntList returns xs as a TypeScript array literal of numbers.
func tsIntList(xs []int) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = strconv.Itoa(x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
