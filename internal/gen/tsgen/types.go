package tsgen

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/value"
)

// TsReservedTypes are the names types.ts declares itself, or imports. A grammar type with one of
// these names would shadow or redeclare it, so the generator rejects it.
var TsReservedTypes = []string{"Span", "TNode", "StringNode", "RecoveredError", "TypedParseResult", "TypedValue", "RuntimeNode"}

// tsTypedNameRef matches the identifiers of a TypeScript type expression of typedTS.
var tsTypedNameRef = regexp.MustCompile(`[\p{L}_$][\p{L}\p{N}_$]*`)

// qualifyTypes prefixes the names of the declarations of types.ts in a type expression with Types.,
// the namespace parser.ts imports them as.
func qualifyTypes(typ string) string {
	return tsTypedNameRef.ReplaceAllStringFunc(typ, func(id string) string {
		switch id {
		case "null", "number", "bigint", "string", "boolean", "unknown", "readonly":
			return id
		}
		return "Types." + id
	})
}

// typedTS returns the declarations of types.ts and the code that parser.ts adds for typed values.
func (g *Generator) typedTS(start *compile.Rule) (types, parser string, err error) {
	if g.Prog.Typed == nil {
		return "", "", fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &tsTypedGen{info: g.Prog.Typed, names: map[string]string{}, joins: map[string]bool{}, ids: map[string]int{}}
	t.ValueResolver = gen.NewValueResolver(g.Prog, func(x compile.Ty) bool { return t.joins[x.String()] })
	reserved := map[string]bool{}
	for _, n := range TsReservedTypes {
		reserved[n] = true
	}
	for name := range g.Prog.Types {
		t.all = append(t.all, name)
	}
	sort.Strings(t.all)
	for _, name := range t.all {
		if reserved[name] {
			return "", "", fmt.Errorf("the grammar type %s cannot be generated for TypeScript: types.ts declares a type with that name (reserved: %s)", name, strings.Join(TsReservedTypes, ", "))
		}
		t.names[name] = name
		if a, ok := t.info.Aliases[name].(compile.JoinTy); ok && tsNodeJoin(a) {
			t.joins[a.String()] = true
		}
	}
	// A join that no alias declares is a union of its members, too.
	for _, j := range t.AnonJoins() {
		t.joins[j.Members.String()] = true
		for _, s := range j.Spellings {
			t.joins[s] = true
		}
	}
	var b strings.Builder
	b.WriteString(tsTypedDeclarations)
	open := []string{"RuntimeNode", "StringNode", "TNode<unknown>", "TypedValue[]", "RecoveredError", "number", "bigint", "string", "boolean", "null"}
	for _, name := range t.all {
		switch spec := g.Prog.Types[name].(type) {
		case *grammar.TypeDef:
			open = append(open, t.names[name])
			if n, ok := g.Prog.NodeTypeOf(name); ok && spec.Struct() == nil {
				under := strings.TrimSuffix(t.tsType(n.Underlying()), " | null")
				fmt.Fprintf(&b, "/** Grammar node type %s = %s. */\nexport interface %s extends %s {\n  readonly type: %s;\n}\n\n", name, n.Underlying(), t.names[name], under, TsString(name))
				t.recipe(n)
				continue
			}
			fmt.Fprintf(&b, "/** Grammar struct %s. */\nexport interface %s extends Span {\n  readonly type: %s;\n", name, t.names[name], TsString(name))
			for _, f := range t.info.Fields[name] {
				fmt.Fprintf(&b, "  readonly %s: %s;\n", TsString(f.Name), t.tsType(f.T))
				t.recipe(f.T)
			}
			b.WriteString("}\n\n")
			t.recipe(compile.NamedTy{Name: name, Kind: 's'})
		case *grammar.TypeAlias:
			fmt.Fprintf(&b, "/** Grammar alias %s = %s. */\nexport type %s = %s;\n\n", name, grammar.FormatType(spec.Type), t.names[name], t.tsType(t.info.Aliases[name]))
			t.recipe(t.info.Aliases[name])
		}
	}
	fmt.Fprintf(&b, "%s%s;\n\n", tsTypedValueDeclaration, strings.Join(open, " | "))
	types = b.String()
	b.Reset()
	st := t.RuleRef(start.Name)
	repr := t.recipe(st)
	stType := qualifyTypes(t.tsType(st))
	b.WriteString("\n// --- Typed values (pego gen -types) ---\n\n")
	fmt.Fprintf(&b, tsTypedAPI, stType, start.ID, repr, g.optionsType)
	runtime := tsTypedRuntime
	if g.NoWork {
		runtime = tsTypedCharge.ReplaceAllString(runtime, "")
	}
	b.WriteString(runtime)
	// recipe() reserves an ID before visiting child types, so recursive fields
	// reference the same representation. Write the table after all registration.
	t.recipe(compile.TyMatch)
	t.recipe(compile.TyError)
	openID, elemsID := t.recipe(openNode), t.recipe(compile.ListTy{Elem: compile.TyAny})
	b.WriteString(tsTypedRecipeTable)
	for _, r := range t.recipes {
		b.WriteString("  " + r + ",\n")
	}
	b.WriteString(tsTypedNodeRecipeTable)
	for _, n := range []compile.NamedTy{compile.TyMatch, compile.TyError} {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(n.Name), t.recipe(n))
	}
	// Open values: tuple and list nodes become TNode<unknown>, and tuple and list values arrays.
	for _, name := range []string{value.TypeTuple, value.TypeList} {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), openID)
	}
	for _, name := range []string{value.TypeTupleValue, value.TypeListValue} {
		fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), elemsID)
	}
	for _, name := range t.all {
		if td, ok := g.Prog.Types[name].(*grammar.TypeDef); ok {
			if n, ok := g.Prog.NodeTypeOf(name); ok && td.Struct() == nil {
				fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), t.recipe(n))
				continue
			}
			fmt.Fprintf(&b, "  [%s, %d],\n", TsString(name), t.recipe(compile.NamedTy{Name: name, Kind: 's'}))
		}
	}
	b.WriteString("]);\n\n")
	return types, b.String(), nil
}

type tsTypedGen struct {
	info *compile.TypeInfo
	// ValueResolver selects the rules whose typed values come from their value types, as in the Go
	// generator: node[T] becomes TNode<T>, a tuple a readonly array type and () the empty
	// tuple type.
	gen.ValueResolver
	names   map[string]string
	all     []string
	joins   map[string]bool
	ids     map[string]int
	recipes []string
}

func tsNodeJoin(u compile.JoinTy) bool {
	for _, a := range u.Alts {
		if !compile.IsNode(a) {
			return false
		}
	}
	return true
}

func (t *tsTypedGen) tsType(x compile.Ty) string {
	switch x := x.(type) {
	case compile.BasicTy:
		switch x {
		case compile.TyInt:
			return "number | bigint"
		case compile.TyString:
			return "string"
		case compile.TyBool:
			return "boolean"
		case compile.TyErr:
			return "RecoveredError | null"
		}
	case gen.Vrule:
		return t.tsType(t.Vres[x.Name])
	case compile.TupleTy:
		elems := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			elems[i] = t.tsType(e)
		}
		return "readonly [" + strings.Join(elems, ", ") + "]"
	case compile.NodeTy:
		if x.Name != "" {
			return t.names[x.Name] + " | null"
		}
		if x.Value == compile.TyTop || x.Value == compile.TyAny {
			return "TNode<unknown> | null" // the value of an open node is its text or its elements
		}
		return "TNode<" + t.tsType(x.Value) + "> | null"
	case compile.NamedTy:
		switch {
		case x == compile.TyMatch:
			return "StringNode | null"
		case x == compile.TyError:
			return "RecoveredError | null"
		case x.Kind == 'c':
			return "TNode<unknown> | null"
		}
		return t.names[x.Name] + " | null"
	case compile.OptTy:
		if _, ok := x.Elem.(compile.TupleTy); ok || x.Elem == compile.TyInt || x.Elem == compile.TyString || x.Elem == compile.TyBool {
			return t.tsType(x.Elem) + " | null"
		}
		return t.tsType(x.Elem)
	case compile.ListTy:
		return "readonly (" + t.tsType(x.Elem) + ")[] | null"
	case compile.RecordTy:
		return "TNode<unknown> | null"
	case compile.JoinTy:
		if t.joins[x.String()] {
			parts := []string{"RecoveredError", "null"}
			for _, a := range x.Alts {
				switch a := a.(type) {
				case compile.NamedTy:
					if a == compile.TyMatch || a.Kind == 's' {
						parts = append(parts, strings.TrimSuffix(t.tsType(a), " | null"))
					}
				case compile.NodeTy:
					if a.Name != "" {
						parts = append(parts, strings.TrimSuffix(t.tsType(a), " | null"))
					}
				}
			}
			if gen.CstJoin(x) {
				parts = append(parts, "TNode<unknown>")
			}
			return strings.Join(parts, " | ")
		}
		if _, ok := t.commonRecipe(x); ok {
			return t.tsType(x.Alts[0])
		}
	}
	return "TypedValue"
}

// commonRecipe reports whether the arguments of a union share a recipe (and with it a TypeScript
// type), and returns its key.
func (t *tsTypedGen) commonRecipe(x compile.JoinTy) (string, bool) {
	common := ""
	for _, a := range x.Alts {
		key := t.conversionKey(a)
		if common != "" && key != common {
			return "", false
		}
		common = key
	}
	return common, common != "" && common != "any"
}

// conversionKey identifies a canonical recipe without expanding named fields.
// Optional node recipes and broad any/node/terminal recipes can share outputs;
// list element recipes retain distinctions such as optional scalars and joins.
func (t *tsTypedGen) conversionKey(x compile.Ty) string {
	switch x := x.(type) {
	case compile.BasicTy:
		if x == compile.TyInt || x == compile.TyString || x == compile.TyBool {
			return x.String()
		}
		if x == compile.TyErr {
			return "named:Error"
		}
	case gen.Vrule:
		return t.conversionKey(t.Vres[x.Name])
	case compile.TupleTy:
		keys := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			keys[i] = t.conversionKey(e)
		}
		return "tuple(" + strings.Join(keys, ",") + ")"
	case compile.NodeTy:
		if x.Name != "" {
			return "tnode:named:" + x.Name
		}
		return "tnode:" + tnodeMode(x) + ":" + t.conversionKey(x.Value)
	case compile.NamedTy:
		if x.Kind != 'c' || x == compile.TyMatch || x == compile.TyError {
			return "named:" + x.Name
		}
		return t.conversionKey(openNode)
	case compile.RecordTy:
		return t.conversionKey(openNode)
	case compile.OptTy:
		if x.Elem == compile.TyInt || x.Elem == compile.TyString || x.Elem == compile.TyBool {
			return "opt" + x.Elem.String()
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return "opt:" + t.conversionKey(x.Elem)
		}
		return t.conversionKey(x.Elem)
	case compile.ListTy:
		return "list:" + t.conversionKey(x.Elem)
	case compile.JoinTy:
		if t.joins[x.String()] {
			return "union:" + x.String()
		}
		if key, ok := t.commonRecipe(x); ok {
			return key
		}
	}
	return "any"
}

// openNode is the type of the nodes of CST types: their values are their text or their elements.
var openNode = compile.NodeTy{Value: compile.TyTop}

// tnodeMode returns where the value of a node of type x comes from: its text ("text"), the node
// itself for a tuple or list value ("self"), or the text or elements of the node ("value").
func tnodeMode(x compile.NodeTy) string {
	switch x.Value.(type) {
	case compile.TupleTy, compile.ListTy:
		return "self"
	}
	if x.Value == compile.TyString {
		return "text"
	}
	return "value"
}

// recipe follows the existing Go Node-converter recipes, including anyValue
// for mixed/open or nondeclared joins and CST retention in declared joins.
func (t *tsTypedGen) recipe(x compile.Ty) int {
	kind, key := "any", "any"
	switch a := x.(type) {
	case compile.BasicTy:
		switch a {
		case compile.TyInt, compile.TyString, compile.TyBool:
			kind, key = string(a), string(a)
		case compile.TyErr:
			return t.recipe(compile.TyError)
		}
	case gen.Vrule:
		return t.recipe(t.Vres[a.Name])
	case compile.TupleTy:
		kind, key = "tuple", t.conversionKey(a)
	case compile.NodeTy:
		kind, key = "tnode", t.conversionKey(a)
		if a.Name != "" {
			// The type's definition gives its value, also inside the type itself.
			x, _ = t.Prog.NodeTypeOf(a.Name)
		}
	case compile.NamedTy:
		kind, key = "terminal", "named:"+a.Name
		if a == compile.TyError {
			kind = "error"
		} else if a.Kind == 's' {
			kind = "struct"
		} else if a.Kind == 'c' && a != compile.TyMatch {
			return t.recipe(openNode)
		}
	case compile.OptTy:
		if a.Elem == compile.TyInt || a.Elem == compile.TyString || a.Elem == compile.TyBool {
			kind, key = "opt"+a.Elem.String(), "opt"+a.Elem.String()
		} else if _, ok := a.Elem.(compile.TupleTy); ok {
			kind, key = "opt", t.conversionKey(a)
		} else {
			return t.recipe(a.Elem)
		}
	case compile.ListTy:
		kind, key = "list", "list:"+t.conversionKey(a.Elem)
	case compile.RecordTy:
		return t.recipe(openNode)
	case compile.JoinTy:
		if t.joins[a.String()] {
			kind, key = "union", "union:"+a.String()
		} else if _, ok := t.commonRecipe(a); ok {
			return t.recipe(a.Alts[0])
		}
	}
	if id, ok := t.ids[key]; ok {
		return id
	}
	id := len(t.recipes)
	t.ids[key] = id
	t.recipes = append(t.recipes, "")
	r := "{ kind: " + TsString(kind)
	switch a := x.(type) {
	case compile.NamedTy:
		if kind == "struct" || kind == "terminal" || kind == "error" {
			r += ", name: " + TsString(a.Name)
		}
		if kind == "struct" {
			var fields []string
			for _, f := range t.info.Fields[a.Name] {
				fields = append(fields, fmt.Sprintf("{ name: %s, repr: %d }", TsString(f.Name), t.recipe(f.T)))
			}
			r += ", fields: [" + strings.Join(fields, ", ") + "]"
		}
	case compile.ListTy:
		r += ", elem: " + strconv.Itoa(t.recipe(a.Elem))
	case compile.OptTy:
		r += ", elem: " + strconv.Itoa(t.recipe(a.Elem))
	case compile.NodeTy:
		if a.Name != "" {
			r += ", name: " + TsString(a.Name)
		}
		r += ", mode: " + TsString(tnodeMode(a)) + ", elem: " + strconv.Itoa(t.recipe(a.Value))
	case compile.TupleTy:
		elems := make([]string, len(a.Elems))
		for i, e := range a.Elems {
			elems[i] = strconv.Itoa(t.recipe(e))
		}
		r += ", elems: [" + strings.Join(elems, ", ") + "]"
	case compile.JoinTy:
		if kind == "union" {
			members := []string{fmt.Sprintf("[%s, %d]", TsString(value.TypeError), t.recipe(compile.TyError))}
			for _, m := range a.Alts {
				if m, ok := m.(compile.NamedTy); ok && (m == compile.TyMatch || m.Kind == 's') {
					members = append(members, fmt.Sprintf("[%s, %d]", TsString(m.Name), t.recipe(m)))
				}
				if m, ok := m.(compile.NodeTy); ok && m.Name != "" {
					members = append(members, fmt.Sprintf("[%s, %d]", TsString(m.Name), t.recipe(m)))
				}
			}
			r += ", members: new Map([" + strings.Join(members, ", ") + "])"
			if gen.CstJoin(a) {
				r += ", fallback: " + strconv.Itoa(t.recipe(openNode))
			}
		}
	}
	t.recipes[id] = r + " }"
	return id
}

// Reuse declaration templates for emission and reservation so adding a public
// or private typed helper automatically reserves its actual binding name.
const tsTypedDeclarations = `export interface Span { readonly start: number; readonly end: number }
/** A node with the value T: a string node has its text, other nodes their elements. */
export interface TNode<T> extends Span { readonly type: string; readonly value: T }
export interface StringNode extends TNode<string> { readonly type: "string" }
export interface RecoveredError extends Span {
  readonly type: "error";
  readonly text: string;
  readonly message: string;
}
export interface TypedParseResult<T> { readonly value: T | null; readonly error: Error | null }

`

const tsTypedAPI = `/** Parses the configured start rule once and converts its completed Node result.
 * Recovery preserves SyntaxErrors; absent values and incompatible recovered fields are null.
 * Positions and integer/text representations are those of parse. Conversion is invocation-local.
 */
export function parseAST(input: string | Uint8Array, unit?: Unit): Types.TypedParseResult<%[1]s> {
  return astResult<%[1]s>(run(G, R%[2]d, nseen, input, unit), %[3]d);
}

/** Typed configured-start parsing with options read and validated once. */
export function parseASTWithOptions(input: string | Uint8Array, options?: %[4]s): Types.TypedParseResult<%[1]s> {
  const o = resolveOptions(G, options);
  if (o instanceof Error) return { value: null, error: o };
  return withObservedOptions<Types.TypedParseResult<%[1]s>>(o,
    control => astResult<%[1]s>(run(G, R%[2]d, nseen, input, o.unit, o.maxDepth, control, o), %[3]d, control),
    error => ({ value: null, error }));
}

`

const tsTypedValueDeclaration = "export type TypedValue = "

const tsTypedRecipeTable = "\nconst astRecipes: readonly ASTRecipe[] = [\n"
const tsTypedNodeRecipeTable = "];\nconst astNodeRecipes: ReadonlyMap<string, number> = new Map([\n"

// tsTypedCharge matches the statements of tsTypedRuntime that charge the work control.
var tsTypedCharge = regexp.MustCompile(`(?m)^[ \t]*control\?\.charge\([^\n]*\);\n`)

const tsTypedRuntime = `interface ASTRecipe {
  readonly kind: string;
  readonly name?: string;
  readonly fields?: readonly { readonly name: string; readonly repr: number }[];
  readonly elem?: number;
  readonly elems?: readonly number[];
  readonly mode?: string;
  readonly members?: ReadonlyMap<string, number>;
  readonly fallback?: number;
}
type ASTObject = { [name: string]: unknown };
interface ASTPending { readonly node: Node; readonly repr: number; readonly out: ASTObject | unknown[] }

// astKid returns the value of the i-th element of a tuple or list node.
function astKid(n: Node, i: number): Value {
  const kid = n.children[i];
  return kid === undefined ? null : unboxNode(kid);
}

function astResult<T>(r: ParseResult, repr: number, control: WorkControl | null = null): Types.TypedParseResult<T> {
  return { value: astConvert(unboxNode(r.node), repr, control) as T | null, error: r.error };
}

// Cache canonical Node/representation pairs, including null, in this call only.
// Register shells before filling edges: shared fold/LR DAGs stay shared and deep
// output graphs do not consume the JavaScript call stack. Source Nodes stay intact.
function astConvert(value: Value, repr: number, control: WorkControl | null = null): unknown {
  const cache = new Map<Node, Map<number, unknown>>();
  const pending: ASTPending[] = [];
  const get = (v: Value, id: number): unknown => {
    for (;;) {
      control?.charge(1);
      const recipe = astRecipes[id]!;
      switch (recipe.kind) {
        case "int": return isInt(v) ? v : 0;
        case "string": return typeof v === "string" ? v : "";
        case "bool": return typeof v === "boolean" ? v : false;
        case "optint": return isInt(v) ? v : null;
        case "optstring": return typeof v === "string" ? v : null;
        case "optbool": return typeof v === "boolean" ? v : null;
        case "any": {
          if (!(v instanceof Node)) return v;
          if (v.type === typeBox) {
            v = v.box;
            continue;
          }
          const next = astNodeRecipes.get(v.type);
          if (next === undefined) return v;
          id = next;
          continue;
        }
        case "opt":
          if (v === null) return null;
          id = recipe.elem!;
          continue;
        case "union": {
          if (!(v instanceof Node)) return null;
          const next = recipe.members!.get(v.type);
          if (next === undefined) {
            if (recipe.fallback === undefined) return null;
            id = recipe.fallback;
            continue;
          }
          id = next;
          continue;
        }
      }
      if (!(v instanceof Node)) {
        if (recipe.kind !== "tuple") return null;
        control?.charge(recipe.elems!.length);
        return recipe.elems!.map(e => get(null, e));
      }
      if (recipe.kind === "tnode" && (v.type === "error" || v.type === typeTupleValue || v.type === typeListValue)) return null;
      let reps = cache.get(v);
      if (reps === undefined) {
        reps = new Map<number, unknown>();
        cache.set(v, reps);
      }
      if (reps.has(id)) return reps.get(id);
      let out: ASTObject | unknown[] | null = null;
      if (recipe.kind === "list") {
        control?.charge(v.children.length);
        out = new Array<unknown>(v.children.length);
      } else if (recipe.kind === "tuple") {
        control?.charge(recipe.elems!.length);
        out = new Array<unknown>(recipe.elems!.length);
      } else if (recipe.kind === "tnode") {
        // A named node type takes only its own nodes.
        out = recipe.name === undefined || v.type === recipe.name ? { type: v.type, start: v.start, end: v.end, value: null } : null;
      } else if (v.type === recipe.name) {
        const obj: ASTObject = { type: v.type, start: v.start, end: v.end };
        if (recipe.name === "string") obj["value"] = v.text;
        else if (recipe.kind !== "struct") obj["text"] = v.text;
        if (recipe.kind === "error") {
          const msg = getFieldWork(v, "message", control);
          obj["message"] = typeof msg === "string" ? msg : "";
        }
        out = obj;
      }
      reps.set(id, out);
      if (out !== null && (recipe.kind === "list" || recipe.kind === "struct" || recipe.kind === "tuple" || recipe.kind === "tnode")) {
        pending.push({ node: v, repr: id, out });
      }
      return out;
    }
  };
  const out = get(value, repr);
  while (pending.length !== 0) {
    control?.charge(1);
    const work = pending.pop()!;
    const recipe = astRecipes[work.repr]!;
    if (recipe.kind === "list") {
      const array = work.out as unknown[];
      for (let i = 0; i < work.node.children.length; i++) {
        control?.charge(1);
        array[i] = get(astKid(work.node, i), recipe.elem!);
      }
    } else if (recipe.kind === "tuple") {
      const array = work.out as unknown[];
      for (let i = 0; i < recipe.elems!.length; i++) {
        control?.charge(1);
        array[i] = get(astKid(work.node, i), recipe.elems![i]!);
      }
    } else if (recipe.kind === "tnode") {
      const n = work.node, obj = work.out as ASTObject;
      if (recipe.mode === "text") obj["value"] = n.text;
      else if (recipe.mode === "self") obj["value"] = get(n, recipe.elem!);
      else if (n.terminal) obj["value"] = get(n.text, recipe.elem!);
      else if (astRecipes[recipe.elem!]!.kind !== "any") obj["value"] = get(null, recipe.elem!);
      else {
        // The elements of an open value, converted like any other open value.
        control?.charge(n.children.length);
        obj["value"] = n.children.map((_, i) => get(astKid(n, i), recipe.elem!));
      }
    } else {
      const obj = work.out as ASTObject;
      for (const f of recipe.fields!) {
        control?.charge(1);
        obj[f.name] = get(getFieldWork(work.node, f.name, control), f.repr);
      }
    }
  }
  return out;
}
`
