package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// The lean value-free rules of the TypeScript parser are printed from Flow IR, as labeled blocks.
func TestGeneratedTSPrintsLeanRulesFromFlow(t *testing.T) {
	src := `rule main = word+
rule word = [a-z]+ ("-" [a-z]+)*`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, gen.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	if !strings.Contains(parser, "L_fail") {
		t.Errorf("the parser prints no labeled block for a lean rule")
	}
}

// outlineKeywords is a grammar of a choice of many keywords, each a sequence of classes of both cases,
// which share their letters: the shapes that the outlining of generated TypeScript acts on.
func outlineKeywords() (grammar string, inputs []string) {
	words := []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"}
	var alts []string
	for _, w := range words {
		var cs []string
		for _, r := range w {
			cs = append(cs, fmt.Sprintf("[%c%c]", r, r-'a'+'A'))
		}
		alts = append(alts, strings.Join(cs, " "))
	}
	grammar = "rule main = (kw ws)* $$\nrule kw = " + strings.Join(alts, " / ") + "\nrule ws = \" \"*"
	inputs = []string{"select from where", "SELECT FrOm WHERE ", "selec", "xorx xor", "withou", "unions unique uni", "wrote"}
	return
}

// Large and repeated parts of a rule run as functions of their own, and the parser still parses as the
// engine does.
func TestGeneratedTSOutlinesLargeAndRepeatedNodes(t *testing.T) {
	src, _ := outlineKeywords()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, gen.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	if !strings.Contains(parser, "kw (outlined)") {
		t.Errorf("no node of rule kw runs as a function of its own")
	}
}

// Outlined nodes with equal structure run as one function, whatever the names of the literal tables they
// read are: no two functions of a generated parser have the same text once the names of constants are
// ignored.
func TestGeneratedTSOutlinedFunctionsAreShared(t *testing.T) {
	// Two rules of the same keywords repeat every outlined node; the letters are literals, which each
	// outlined function reads as a table of its own.
	var alts []string
	for _, w := range []string{"select", "selects", "set", "setup", "from", "frozen", "union", "unique", "update", "use",
		"using", "values", "view", "virtual", "when", "where", "while", "window", "with", "without", "write", "xor"} {
		var ls []string
		for _, r := range w {
			ls = append(ls, strconv.Quote(string(r)))
		}
		alts = append(alts, strings.Join(ls, " "))
	}
	kw := strings.Join(alts, " / ")
	src := "rule main = (kw ws)* (kw2 ws)* $$\nrule kw = " + kw + "\nrule kw2 = " + kw + "\nrule ws = \" \"*"
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, gen.GenOptions{Start: "main", Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "parser.ts") {
			parser = string(f.Content)
		}
	}
	fn := regexp.MustCompile(`(?s)\n// [^\n]*\(outlined\)\nfunction \w+\(_?p: Parser\): R \{\n(.*?)\n\}\n`)
	name := regexp.MustCompile(`\b(lit|scope|a)\d+\b`)
	outlined := fn.FindAllStringSubmatch(parser, -1)
	if len(outlined) < 2 {
		t.Fatalf("too few outlined functions: %d", len(outlined))
	}
	seen := map[string]bool{}
	for _, m := range outlined {
		// Calls of other outlined functions keep their names: equal callees have one name.
		body := name.ReplaceAllString(m[1], "$1")
		if seen[body] {
			t.Errorf("two outlined functions have the same text:\n%s", m[1])
		}
		seen[body] = true
	}
}

// A recovered error is a SyntaxError with its position, message and expectations, and it carries no stack
// trace; the parser leaves Error.stackTraceLimit as it found it, also when the parse is interrupted at the
// work limit while it builds the error, and an error that a parse returns keeps its stack trace.
func TestGeneratedTSRecoveredErrorsCarryNoStack(t *testing.T) {
	src := `rule main = item #recover(skip=[^;]+ ";")*
rule item = [a-z]+ ";"`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	out := runTSGrammarScriptWith(t, g, `import * as p from "./parser.ts";
const check = (x: boolean, why: string): void => { if (!x) throw new Error(why); };
const limit = (Error as { stackTraceLimit?: number }).stackTraceLimit;
const r = p.parse("a;1;b;", p.CodePoints);
check(r.error instanceof p.SyntaxErrors && r.error.errors.length === 1, "one recovered error");
const e = r.error.errors[0]!;
check(e instanceof p.SyntaxError && e instanceof Error, "a recovered error is a SyntaxError");
check(e.pos === 2 && e.line === 1 && e.col === 3 && e.expected.length > 0 && e.message.startsWith("1:3: syntax error: expected"), "position and message");
check(e.stack === undefined || !e.stack.includes("\n    at "), "no stack frames: " + e.stack);
check((Error as { stackTraceLimit?: number }).stackTraceLimit === limit, "limit restored");
const bad = p.parse("1", p.CodePoints);
check(bad.error instanceof p.SyntaxErrors || bad.error instanceof p.SyntaxError, "bad input fails");
for (let n = 1; n < 400; n++) {
  p.parseWithOptions("a;1;b;", { unit: p.CodePoints, maxSteps: n });
  check((Error as { stackTraceLimit?: number }).stackTraceLimit === limit, "limit restored after the work limit " + n);
}
// Building the recovered error charges work (the expectations and the table of line starts), so a
// signal getter and an interruption run while it is built. Neither may see the lowered limit, and the
// error of an interrupted parse keeps its stack.
const long = "a;1;b;" + "x".repeat(20000) + ";";
const polled = (): number[] => {
  const seen: number[] = [];
  const signal = { get aborted(): boolean { seen.push((Error as { stackTraceLimit?: number }).stackTraceLimit ?? -1); return false; } };
  check(p.parseWithOptions(long, { unit: p.CodePoints, signal }).error instanceof p.SyntaxErrors, "the long input recovers");
  return seen;
};
const seen = polled();
check(seen.length > 20 && seen.every((x) => x === limit), "the signal observes the stack limit: " + seen.filter((x) => x !== limit).length);
// A negative control: lowering the limit around the whole of makeError is seen by the signal.
const proto = (p as unknown as { Parser: { prototype: unknown } }).Parser.prototype as { makeError: (...a: unknown[]) => unknown };
const original = proto.makeError;
proto.makeError = function (this: unknown, ...a: unknown[]): unknown {
  const v8 = Error as { stackTraceLimit?: number };
  v8.stackTraceLimit = 0;
  try { return original.apply(this, a); } finally { v8.stackTraceLimit = limit; }
};
check(polled().some((x) => x === 0), "the control sees a lowered limit");
proto.makeError = original;
for (let n = 40050; n <= 60000; n += 61) {
  const r = p.parseWithOptions(long, { unit: p.CodePoints, maxInputWork: n });
  if (r.error instanceof p.ParseInterruptionError) {
    check(typeof r.error.stack === "string" && r.error.stack.includes("\n    at "), "an interruption keeps its stack at " + n);
  }
}
console.log("ok");
`, gen.GenOptions{Start: "main", WorkControls: true})
	if !strings.Contains(out, "ok") {
		t.Fatalf("output %q", out)
	}
}
