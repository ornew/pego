package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Top-level declarations share TypeScript's type/value namespace with emitted
// grammar declarations. Derive the registry from the complete emitted runtime
// and generated declarations, including private helpers and symbol families.
var tsDeclaration = regexp.MustCompile(`(?m)^(?:export )?(?:const|let|class|interface|type|function) ([\p{L}\p{N}_$]+)`)

// Globals used by the runtime/converter and TS binding keywords also need to
// remain accessible after grammar interfaces and aliases have been declared.
const tsReservedBindings = `Array BigInt Error Infinity Int32Array JSON Map Math NaN Number Object RangeError ReadonlyMap Record RegExp Set String Uint8Array
await break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with yield
abstract any as asserts async bigint boolean constructor declare from get global implements infer interface is keyof let module namespace never number of override private protected public readonly require set static string symbol type undefined unique unknown using`

func (g *tsGen) typedTS(start *rule, emitted string) (string, error) {
	if g.prog.typed == nil {
		return "", fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &tsTypedGen{info: g.prog.typed, names: map[string]string{}, unions: map[string]bool{}, ids: map[string]int{}}
	used := map[string]bool{}
	for _, n := range strings.Fields(tsReservedBindings) {
		used[n] = true
	}
	for _, source := range []string{emitted, g.vars.String(), g.init.String(), g.methods.String(), tsTypedRuntime, tsTypedDeclarations, tsTypedAPI, tsTypedRecipeTable, tsTypedNodeRecipeTable, tsTypedValueDeclaration} {
		for _, m := range tsDeclaration.FindAllStringSubmatch(source, -1) {
			used[m[1]] = true
		}
	}
	if g.observability {
		used["WeakMap"], used["globalThis"] = true, true
	}
	for name := range g.prog.types {
		t.all = append(t.all, name)
	}
	sort.Strings(t.all)
	for _, name := range t.all {
		n := name
		for used[n] {
			n += "_"
		}
		used[n], t.names[name] = true, n
		if a, ok := t.info.aliases[name].(unionTy); ok && tsNodeUnion(a) {
			t.unions[a.String()] = true
		}
	}
	var b strings.Builder
	b.WriteString("\n// --- Typed values (pego gen -types) ---\n\n")
	b.WriteString(tsTypedDeclarations)
	open := []string{"Node", "Match", "RecoveredError", "number", "bigint", "string", "boolean", "null"}
	for _, name := range t.all {
		switch spec := g.prog.types[name].(type) {
		case *grammar.StructSpec:
			open = append(open, t.names[name])
			fmt.Fprintf(&b, "/** Grammar struct %s. */\nexport interface %s extends Span {\n  readonly type: %s;\n", name, t.names[name], tsString(name))
			for _, f := range t.info.fields[name] {
				fmt.Fprintf(&b, "  readonly %s: %s;\n", tsString(f.name), t.tsType(f.t))
				t.recipe(f.t)
			}
			b.WriteString("}\n\n")
			t.recipe(namedTy{name, 's'})
		case *grammar.TerminalSpec:
			open = append(open, t.names[name])
			fmt.Fprintf(&b, "/** Grammar terminal %s. */\nexport interface %s extends Span { readonly type: %s; readonly text: string }\n\n", name, t.names[name], tsString(name))
			t.recipe(namedTy{name, 't'})
		case *grammar.AliasSpec:
			fmt.Fprintf(&b, "/** Grammar alias %s = %s. */\nexport type %s = %s;\n\n", name, grammar.FormatType(spec.Type), t.names[name], t.tsType(t.info.aliases[name]))
			t.recipe(t.info.aliases[name])
		}
	}
	fmt.Fprintf(&b, "%s%s;\n\n", tsTypedValueDeclaration, strings.Join(open, " | "))
	st := t.info.rules[start.name]
	if st == nil {
		st = tyAny
	}
	repr := t.recipe(st)
	stType := t.tsType(st)
	if g.observability {
		api, err := generatedTSObservedInvocations(fmt.Sprintf(tsTypedAPI, stType, start.id, repr), 1)
		if err != nil {
			return "", err
		}
		b.WriteString(api)
	} else {
		fmt.Fprintf(&b, tsTypedAPI, stType, start.id, repr)
	}
	b.WriteString(tsTypedRuntime)
	// recipe() reserves an ID before visiting child types, so recursive fields
	// reference the same representation. Write the table after all registration.
	t.recipe(tyMatch)
	t.recipe(tyError)
	b.WriteString(tsTypedRecipeTable)
	for _, r := range t.recipes {
		b.WriteString("  " + r + ",\n")
	}
	b.WriteString(tsTypedNodeRecipeTable)
	for _, n := range []namedTy{tyMatch, tyError} {
		fmt.Fprintf(&b, "  [%s, %d],\n", tsString(n.name), t.recipe(n))
	}
	for _, name := range t.all {
		switch g.prog.types[name].(type) {
		case *grammar.StructSpec:
			fmt.Fprintf(&b, "  [%s, %d],\n", tsString(name), t.recipe(namedTy{name, 's'}))
		case *grammar.TerminalSpec:
			fmt.Fprintf(&b, "  [%s, %d],\n", tsString(name), t.recipe(namedTy{name, 't'}))
		}
	}
	b.WriteString("]);\n\n")
	return b.String(), nil
}

type tsTypedGen struct {
	info    *typeInfo
	names   map[string]string
	all     []string
	unions  map[string]bool
	ids     map[string]int
	recipes []string
}

func tsNodeUnion(u unionTy) bool {
	for _, a := range u.alts {
		if !isNode(a) {
			return false
		}
	}
	return true
}

func (t *tsTypedGen) tsType(x ty) string {
	switch x := x.(type) {
	case basicTy:
		switch x {
		case tyInt:
			return "number | bigint"
		case tyString:
			return "string"
		case tyBool:
			return "boolean"
		}
	case namedTy:
		switch {
		case x == tyMatch:
			return "Match | null"
		case x == tyError:
			return "RecoveredError | null"
		case x.kind == 'c':
			return "Node | null"
		}
		return t.names[x.name] + " | null"
	case optTy:
		if x.elem == tyInt || x.elem == tyString || x.elem == tyBool {
			return t.tsType(x.elem) + " | null"
		}
		return t.tsType(x.elem)
	case listTy:
		return "readonly (" + t.tsType(x.elem) + ")[] | null"
	case recordTy:
		return "Node | null"
	case unionTy:
		if t.unions[x.String()] {
			parts := []string{"RecoveredError", "null"}
			for _, a := range x.alts {
				switch a := a.(type) {
				case namedTy:
					if a == tyMatch || a.kind == 's' || a.kind == 't' {
						parts = append(parts, strings.TrimSuffix(t.tsType(a), " | null"))
					}
				}
			}
			if cstUnion(x) {
				parts = append(parts, "Node")
			}
			return strings.Join(parts, " | ")
		}
	}
	return "TypedValue"
}

// conversionKey identifies a canonical recipe without expanding named fields.
// Optional node recipes and broad any/node/terminal recipes can share outputs;
// list element recipes retain distinctions such as optional scalars and unions.
func (t *tsTypedGen) conversionKey(x ty) string {
	switch x := x.(type) {
	case basicTy:
		if x == tyInt || x == tyString || x == tyBool {
			return x.String()
		}
	case namedTy:
		if x.kind != 'c' || x == tyMatch || x == tyError {
			return "named:" + x.name
		}
		return "node"
	case recordTy:
		return "node"
	case optTy:
		if x.elem == tyInt || x.elem == tyString || x.elem == tyBool {
			return "opt" + x.elem.String()
		}
		return t.conversionKey(x.elem)
	case listTy:
		return "list:" + t.conversionKey(x.elem)
	case unionTy:
		if t.unions[x.String()] {
			return "union:" + x.String()
		}
	}
	return "any"
}

// recipe follows the existing Go Node-converter recipes, including anyValue
// for mixed/open or nondeclared unions and CST retention in declared unions.
func (t *tsTypedGen) recipe(x ty) int {
	kind, key := "any", "any"
	switch a := x.(type) {
	case basicTy:
		switch a {
		case tyInt, tyString, tyBool:
			kind, key = string(a), string(a)
		}
	case namedTy:
		kind, key = "terminal", "named:"+a.name
		if a == tyError {
			kind = "error"
		} else if a.kind == 's' {
			kind = "struct"
		} else if a.kind == 'c' && a != tyMatch {
			kind, key = "node", "node"
		}
	case optTy:
		if a.elem == tyInt || a.elem == tyString || a.elem == tyBool {
			kind, key = "opt"+a.elem.String(), "opt"+a.elem.String()
		} else {
			return t.recipe(a.elem)
		}
	case listTy:
		kind, key = "list", "list:"+t.conversionKey(a.elem)
	case recordTy:
		kind, key = "node", "node"
	case unionTy:
		if t.unions[a.String()] {
			kind, key = "union", "union:"+a.String()
		}
	}
	if id, ok := t.ids[key]; ok {
		return id
	}
	id := len(t.recipes)
	t.ids[key] = id
	t.recipes = append(t.recipes, "")
	r := "{ kind: " + tsString(kind)
	switch a := x.(type) {
	case namedTy:
		if kind == "struct" || kind == "terminal" || kind == "error" {
			r += ", name: " + tsString(a.name)
		}
		if kind == "struct" {
			var fields []string
			for _, f := range t.info.fields[a.name] {
				fields = append(fields, fmt.Sprintf("{ name: %s, repr: %d }", tsString(f.name), t.recipe(f.t)))
			}
			r += ", fields: [" + strings.Join(fields, ", ") + "]"
		}
	case listTy:
		r += ", elem: " + strconv.Itoa(t.recipe(a.elem))
	case unionTy:
		if kind == "union" {
			members := []string{fmt.Sprintf("[%s, %d]", tsString("Error"), t.recipe(tyError))}
			for _, m := range a.alts {
				if m, ok := m.(namedTy); ok && (m == tyMatch || m.kind == 's' || m.kind == 't') {
					members = append(members, fmt.Sprintf("[%s, %d]", tsString(m.name), t.recipe(m)))
				}
			}
			r += ", members: new Map([" + strings.Join(members, ", ") + "]), cst: " + strconv.FormatBool(cstUnion(a))
		}
	}
	t.recipes[id] = r + " }"
	return id
}

// Reuse declaration templates for emission and reservation so adding a public
// or private typed helper automatically reserves its actual binding name.
const tsTypedDeclarations = `export interface Span { readonly start: number; readonly end: number }
export interface Match extends Span { readonly type: "Match"; readonly text: string }
export interface RecoveredError extends Span {
  readonly type: "Error";
  readonly text: string;
  readonly message: string;
}
export interface TypedParseResult<T> { readonly value: T | null; readonly error: Error | null }

`

const tsTypedAPI = `/** Parses the configured start rule once and converts its completed Node result.
 * Recovery preserves SyntaxErrors; absent values and incompatible recovered fields are null.
 * Positions and integer/text representations are those of parse. Conversion is invocation-local.
 */
export function parseAST(input: string | Uint8Array, unit?: Unit): TypedParseResult<%[1]s> {
  return astResult<%[1]s>(run(R%[2]d, nseen, input, unit), %[3]d);
}

/** Typed configured-start parsing with options read and validated once. */
export function parseASTWithOptions(input: string | Uint8Array, options?: ParseOptions): TypedParseResult<%[1]s> {
  const o = resolveOptions(options);
  if (o instanceof Error) return { value: null, error: o };
  return withWorkControl<TypedParseResult<%[1]s>>(o,
    control => astResult<%[1]s>(run(R%[2]d, nseen, input, o.unit, o.maxDepth, control), %[3]d, control),
    error => ({ value: null, error }));
}

`

const tsTypedValueDeclaration = "export type TypedValue = "

const tsTypedRecipeTable = "\nconst astRecipes: readonly ASTRecipe[] = [\n"
const tsTypedNodeRecipeTable = "];\nconst astNodeRecipes: ReadonlyMap<string, number> = new Map([\n"

const tsTypedRuntime = `interface ASTRecipe {
  readonly kind: string;
  readonly name?: string;
  readonly fields?: readonly { readonly name: string; readonly repr: number }[];
  readonly elem?: number;
  readonly members?: ReadonlyMap<string, number>;
  readonly cst?: boolean;
}
type ASTObject = { [name: string]: unknown };
interface ASTPending { readonly node: Node; readonly repr: number; readonly out: ASTObject | unknown[] }

function astResult<T>(r: ParseResult, repr: number, control: WorkControl | null = null): TypedParseResult<T> {
  return { value: astConvert(r.node, repr, control) as T | null, error: r.error };
}

// Cache canonical Node/representation pairs, including null, in this call only.
// Register shells before filling edges: shared fold/LR DAGs stay shared and deep
// output graphs do not consume the JavaScript call stack. Source Nodes stay intact.
function astConvert(value: Value, repr: number, control: WorkControl | null = null): unknown {
  const cache = new Map<Node, Map<number, unknown>>();
  const pending: ASTPending[] = [];
  const get = (v: Value, id: number): unknown => {
    for (;;) {
      control?.charge(0, 1);
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
          const next = astNodeRecipes.get(v.type);
          if (next === undefined) return v;
          id = next;
          continue;
        }
        case "node": return v instanceof Node ? v : null;
        case "union": {
          if (!(v instanceof Node)) return null;
          const next = recipe.members!.get(v.type);
          if (next === undefined) return recipe.cst ? v : null;
          id = next;
          continue;
        }
      }
      if (!(v instanceof Node)) return null;
      let reps = cache.get(v);
      if (reps === undefined) {
        reps = new Map<number, unknown>();
        cache.set(v, reps);
      }
      if (reps.has(id)) return reps.get(id);
      let out: ASTObject | unknown[] | null = null;
      if (recipe.kind === "list") {
        control?.charge(0, v.children.length);
        out = new Array<unknown>(v.children.length);
      } else if (v.type === recipe.name) {
        const obj: ASTObject = { type: v.type, start: v.start, end: v.end };
        if (recipe.kind !== "struct") obj["text"] = v.text;
        if (recipe.kind === "error") {
          const msg = getFieldWork(v, "message", control);
          obj["message"] = typeof msg === "string" ? msg : "";
        }
        out = obj;
      }
      reps.set(id, out);
      if (out !== null && (recipe.kind === "list" || recipe.kind === "struct")) {
        pending.push({ node: v, repr: id, out });
      }
      return out;
    }
  };
  const out = get(value, repr);
  while (pending.length !== 0) {
    control?.charge(0, 1);
    const work = pending.pop()!;
    const recipe = astRecipes[work.repr]!;
    if (recipe.kind === "list") {
      const array = work.out as unknown[];
      for (let i = 0; i < work.node.children.length; i++) {
        control?.charge(0, 1);
        array[i] = get(work.node.children[i]!, recipe.elem!);
      }
    } else {
      const obj = work.out as ASTObject;
      for (const f of recipe.fields!) {
        control?.charge(0, 1);
        obj[f.name] = get(getFieldWork(work.node, f.name, control), f.repr);
      }
    }
  }
  return out;
}
`
