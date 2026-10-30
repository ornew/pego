package tsgen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

const tsBudgetFormsGrammar = `type Item = { Name string }
rule main = xs:item* !.
rule item Item = n:name ","? => Item{Name: text($n)}
rule name = [a-z]+
rule fast = "z"`

// The generated TypeScript has two forms: without -budget-controls the rules do not spend budget and the
// entry points reject the control options; with it they charge and accept them.
func TestGeneratedTSBudgetControlForms(t *testing.T) {
	t.Parallel()
	g, err := parseTest(tsBudgetFormsGrammar)
	if err != nil {
		t.Fatal(err)
	}
	generate := func(controls bool) string {
		files, err := gentest.GenerateTS(g, gen.GenOptions{Start: "main", Types: true, Recognize: true, BudgetControls: controls})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.Path == "parser.ts" {
				return string(f.Content)
			}
		}
		t.Fatal("no parser.ts")
		return ""
	}
	plain, controlled := generate(false), generate(true)
	for _, c := range []struct{ name, text string }{{"plain", plain}, {"controlled", controlled}} {
		for _, entry := range []string{"parseWithOptions(", "parseRuleWithOptions(", "recognizeWithOptions(", "parseASTWithOptions("} {
			if !strings.Contains(c.text, "function "+entry) {
				t.Errorf("%s form has no %s", c.name, entry)
			}
		}
	}
	for _, charge := range []string{"p.spend(", "c.spend(", "charge("} {
		if strings.Contains(plain, charge) {
			t.Errorf("a parser generated without budget controls mentions %q:\n%s", charge, plain)
		}
	}
	for _, charge := range []string{"p.spend(", "c.spend("} {
		if !strings.Contains(controlled, charge) {
			t.Errorf("a parser generated with budget controls has no %q", charge)
		}
	}
	if strings.Contains(plain, "CancellationSignal") || strings.Contains(plain, "ParseInterruption") {
		t.Error("a parser generated without budget controls exports the cancellation types")
	}
	if !strings.Contains(plain, "PlainParseOptions as ParseOptions") || !strings.Contains(plain, "options?: PlainParseOptions") {
		t.Error("a parser generated without budget controls has the full options type")
	}
	if strings.Contains(controlled, "PlainParseOptions") || !strings.Contains(controlled, "CancellationSignal") || !strings.Contains(controlled, "options?: ParseOptions") {
		t.Error("a parser generated with budget controls does not have the full options type")
	}
	if !strings.Contains(plain, "mkGrammar(") || !strings.Contains(plain, ", false);") || !strings.Contains(controlled, ", true);") {
		t.Error("the grammar does not record whether the parser is controlled")
	}
	if len(plain) >= len(controlled) {
		t.Errorf("the plain parser (%d bytes) is not smaller than the controlled one (%d bytes)", len(plain), len(controlled))
	}
}

func TestGeneratedTSWithoutBudgetControlsRejectsControlOptions(t *testing.T) {
	t.Parallel()
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
const input = "ab,cd";
check(p.parse(input).error === null, "parse");
for (const unit of [p.CodePoints, p.Bytes]) {
  const ordinary = p.parse(input, unit), typed = p.parseAST(input, unit);
  const o = { unit, maxDepth: 0 };
  check(stable(p.parseWithOptions(input, o).node) === stable(ordinary.node), "options without controls parse as parse");
  check(stable(p.parseASTWithOptions(input, o).value) === stable(typed.value), "typed options without controls");
  check(p.parseRuleWithOptions("fast", "z", o).error === null && p.recognizeWithOptions(input, o) === null, "rule and recognizer options");
  // The options type has no control option; a caller that bypasses it still gets an error.
  // @ts-expect-error The options of a parser without budget controls have no signal.
  const withSignal = p.parseWithOptions(input, { unit, signal: { aborted: false } });
  // @ts-expect-error The options of a parser without budget controls have no budget limit.
  const withBudget = p.parseASTWithOptions(input, { unit, budget: 1 });
  // @ts-expect-error The options of a parser without budget controls have no budget limit.
  const withRecognize = p.recognizeWithOptions(input, { unit, budget: 1 });
  for (const error of [withSignal.error, withBudget.error, withRecognize]) {
    check(error !== null && error.message.includes("generated without budget controls"), "a control option is rejected");
  }
  const rejected = [{ signal: { aborted: false } }, { budget: 1 }] as unknown as p.ParseOptions[];
  for (const options of rejected) {
    for (const error of [p.parseWithOptions(input, options).error, p.parseRuleWithOptions("fast", "z", options).error, p.recognizeWithOptions(input, options), p.parseASTWithOptions(input, options).error]) {
      check(error !== null && error.message.includes("generated without budget controls"), "a control option is rejected by every entry point: " + JSON.stringify(options));
    }
  }
  // Zero limits and an absent signal set nothing.
  check(p.parseWithOptions(input, { unit, signal: null, budget: 0 } as unknown as p.ParseOptions).error === null, "unset controls are accepted");
}
`
	runTypedTSFixture(t, tsBudgetFormsGrammar, consumer, gen.GenOptions{Start: "main", Recognize: true}, "")
}

func TestGeneratedTSWithBudgetControlsAcceptsControlOptions(t *testing.T) {
	t.Parallel()
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
const input = "ab,cd";
for (const unit of [p.CodePoints, p.Bytes]) {
  const ordinary = p.parse(input, unit), typed = p.parseAST(input, unit);
  const o = { unit, signal: { aborted: false }, budget: 1_000_000 };
  check(stable(p.parseWithOptions(input, o).node) === stable(ordinary.node), "controlled parse matches");
  check(stable(p.parseASTWithOptions(input, o).value) === stable(typed.value), "controlled typed parse matches");
  check(p.recognizeWithOptions(input, o) === null && p.parseRuleWithOptions("fast", "z", o).error === null, "controlled rule and recognizer");
  const limited = p.parseWithOptions(input, { unit, budget: 3 });
  check(limited.error instanceof p.ParseInterruptionError && limited.error.kind === "budget-limit", "budget-limit");
  const canceled = p.parseASTWithOptions(input, { unit, signal: { aborted: true } });
  check(canceled.error instanceof p.ParseInterruptionError && canceled.error.kind === "canceled", "cancellation");
  check(p.parse(input, unit).error === null, "a parse after an interruption");
}
`
	runTypedTSFixture(t, tsBudgetFormsGrammar, consumer, gen.GenOptions{Start: "main", Recognize: true, BudgetControls: true}, "")
}
