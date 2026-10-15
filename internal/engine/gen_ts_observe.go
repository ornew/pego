package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Transform only generation-opt-in sources, checking every source anchor so
// runtime maintenance cannot silently drop an instrumentation boundary.
func generatedTSTraceRuntime(rt string) (string, error) {
	changes := []struct {
		old, replacement string
		count            int
	}{
		{"export interface ParseOptions {", `export interface ParseOptions {
  /** Accumulate unfiltered rule metrics; requires host performance.now(). */
  profile?: Profile | null;
  /** Synchronous scalar events; thrown callback values propagate unchanged. */
  trace?: (event: TraceEvent) => void;
  /** Named rules and descendants; empty selects all, unknown names select none. */
  traceRules?: readonly string[];
  /** Start depth is one; nonpositive values leave delivery unlimited. */
  traceMaxDepth?: number;`, 1},
		{"  const unit = options?.unit === Bytes ? Bytes : CodePoints;", `  const profile = options?.profile ?? null;
  if (profile !== null && (!(profile instanceof Profile) || !profileData.has(profile))) return new Error("profile must be a Profile");
  const profileClock = profile === null ? null : resolveProfileClock();
  if (profileClock instanceof Error) return profileClock;
  const trace = options?.trace ?? null;
  const traceMaxDepth = options?.traceMaxDepth ?? 0;
  if (trace !== null && typeof trace !== "function") return new Error("trace must be a function");
  if (!Number.isSafeInteger(traceMaxDepth)) return new Error("trace max depth must be a safe integer");
  const traceRules = Array.from(options?.traceRules ?? []);
  const unit = options?.unit === Bytes ? Bytes : CodePoints;`, 1},
		{"return { unit, maxDepth: depth === 0 ? defaultMaxDepth : depth, signal, maxInputWork, maxSteps };", "return { unit, maxDepth: depth === 0 ? defaultMaxDepth : depth, signal, maxInputWork, maxSteps, trace, traceRules, traceMaxDepth, profile, profileClock };", 1},
		{"control: WorkControl | null = null): ParseResult {", "control: WorkControl | null = null, traceOptions: ResolvedParseOptions | null = null): ParseResult {", 1},
		{"    p = new Parser(input, unit === Bytes ? Bytes : CodePoints, seen, maxDepth, control);", "    p = new Parser(input, unit === Bytes ? Bytes : CodePoints, seen, maxDepth, control);\n    p.observation = traceOptions?.profile != null ? new ProfileObservation(traceOptions) : traceOptions?.trace == null ? null : new Observation(traceOptions);", 1},
		{"    throw x;\n  }\n}\n\n// isStackOverflow", "    throw x;\n  } finally {\n    p?.observation?.release();\n    if (p !== null) p.observation = null;\n  }\n}\n\n// isStackOverflow", 1},
		{"class Parser {", "class Parser {\n  observation: Observation | null = null;", 1},
		{"  call(r: Rule, min: number): R {", "  callObservedBody(r: Rule, min: number): R {", 1},
		{"return this.invokePlain(r, min);", "return this.invokePlainObservedBody(r, min);", 1},
		{"  invokePlain(r: Rule, min: number): R {", "  invokePlainObservedBody(r: Rule, min: number): R {", 1},
		{"      if (!e.growing) {\n        this.mergeKept", "      this.observation?.memoHit();\n      if (!e.growing) {\n        this.mergeKept", 1},
		{"    let v = r.body(this, min);", "    this.observation?.evaluated();\n    let v = r.body(this, min);", 2},
	}
	for _, change := range changes {
		if strings.Count(rt, change.old) != change.count {
			return "", fmt.Errorf("generated TypeScript trace runtime anchor changed: %q", change.old)
		}
		rt = strings.ReplaceAll(rt, change.old, change.replacement)
	}
	// Install wrappers after renaming the original methods.
	const parser = "class Parser {\n  observation: Observation | null = null;"
	rt = strings.Replace(rt, parser, parser+"\n"+tsObservedCallMethods, 1)
	return rt + "\n" + tsTraceSource + "\n" + tsProfileSource, nil
}

func generatedTSTraceWork(rt string) (string, error) {
	const anchor = "interface ResolvedParseOptions {"
	if strings.Count(rt, anchor) != 1 {
		return "", fmt.Errorf("generated TypeScript trace option anchor changed")
	}
	return strings.Replace(rt, anchor, anchor+`
  profile: Profile | null;
  profileClock: (() => number) | null;
  trace: ((event: TraceEvent) => void) | null;
  traceRules: readonly string[];
  readonly traceMaxDepth: number;`, 1), nil
}

// Called before grammar literals/tables are appended, or on the typed API
// template alone, so grammar text cannot be mistaken for runtime source.
func generatedTSObservedInvocations(src string, count int) (string, error) {
	for _, change := range [][2]string{{"\n  return withWorkControl", "\n  return withObservedOptions"}, {"input, o.unit, o.maxDepth, control)", "input, o.unit, o.maxDepth, control, o)"}} {
		if strings.Count(src, change[0]) != count {
			return "", fmt.Errorf("generated TypeScript trace invocation anchor changed: %q", change[0])
		}
		src = strings.ReplaceAll(src, change[0], change[1])
	}
	return src, nil
}

const tsObservedCallMethods = `
  call(r: Rule, min: number): R {
    const o = this.observation;
    if (o === null) return this.callObservedBody(r, min);
    o.enter(r.name, this.pos, min);
    const v = this.callObservedBody(r, min);
    o.exit(v !== undefined, this.pos);
    return v;
  }

  invokePlain(r: Rule, min: number): R {
    const o = this.observation;
    if (o === null) return this.invokePlainObservedBody(r, min);
    o.enter(r.name, this.pos, min);
    const v = this.invokePlainObservedBody(r, min);
    o.exit(v !== undefined, this.pos);
    return v;
  }
`

func (g *tsGen) tracePlainCall(r *rule, name string) string {
	if !g.observability {
		return name
	}
	body := name + "ObservedBody"
	fmt.Fprintf(&g.methods, `// Observes one actual generated rule call.
function %s(p: Parser): R {
  const o = p.observation;
  if (o === null) return %s(p);
  o.enter(%s, p.pos, 0);
  o.evaluated();
  const v = %s(p);
  o.exit(v !== undefined, p.pos);
  return v;
}

`, name, body, tsString(r.name), body)
	return body
}

func (g *tsGen) traceSkipped(expr grammar.Expr) string {
	if !g.observability {
		return ""
	}
	for {
		switch e := expr.(type) {
		case *grammar.Seq:
			if len(e.Items) == 0 {
				return ""
			}
			expr = e.Items[0]
		case *grammar.Capture:
			expr = e.Expr
		case *grammar.Atomic:
			expr = e.Expr
		case *grammar.Discard:
			expr = e.Expr
		case *grammar.Ref:
			min := 0
			if e.Level != "" {
				for i, l := range g.prog.byName[e.Name].def.Expr.(*grammar.OperatorsRule).Levels {
					if l.Name == e.Level {
						min = i
					}
				}
			}
			return fmt.Sprintf("    p.observation?.skipped(%s, p.pos, %d);\n", tsString(e.Name), min)
		default:
			return ""
		}
	}
}
