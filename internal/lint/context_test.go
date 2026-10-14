package lint

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

type lintPollContext struct {
	context.Context
	cancel          context.CancelFunc
	calls, cancelAt int
}

func (c *lintPollContext) Err() error {
	c.calls++
	if c.cancelAt > 0 && c.calls == c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func lintTestContext(t *testing.T, cancelAt int) *lintPollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &lintPollContext{Context: ctx, cancel: cancel, cancelAt: cancelAt}
}

func lintContextGrammar(t *testing.T, source string) *grammar.Grammar {
	t.Helper()
	g, err := syntax.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Compile(g, engine.Options{}); err != nil {
		t.Fatal(err)
	}
	return g
}

func lintManyRules() string {
	var source strings.Builder
	source.WriteString("rule main = \"a\"\n")
	for i := range 512 {
		fmt.Fprintf(&source, "rule unused%d = \"b\"\n", i)
	}
	return source.String()
}

func TestRunContextCancellationDiscardsFindings(t *testing.T) {
	g := lintContextGrammar(t, lintManyRules())
	count := lintTestContext(t, 0)
	complete, err := RunContext(count, g, "main", Options{})
	if err != nil || len(complete) < 256 || count.calls < 4 {
		t.Fatalf("inadequate fixture: findings=%d calls=%d err=%v", len(complete), count.calls, err)
	}
	// The final prior polling point occurs after checks have accumulated findings.
	// Deterministic baseline counting avoids sleeps and fixed wall-clock limits.
	for _, cancelAt := range []int{1, 3, count.calls - 1} {
		ctx := lintTestContext(t, cancelAt)
		findings, err := RunContext(ctx, g, "main", Options{})
		if findings != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt=%d returned partial findings %v / %v", cancelAt, findings, err)
		}
	}
}

func TestLintContextPhaseCancellation(t *testing.T) {
	g := lintContextGrammar(t, lintManyRules())
	an := newAnalysis(g)
	for _, phase := range []string{"analysis", "dependency-graph", "shadowed", "suppression"} {
		t.Run(phase, func(t *testing.T) {
			control := &lintControl{ctx: lintTestContext(t, 1)}
			defer func() {
				cancelled, ok := recover().(lintCancellation)
				if !ok || !errors.Is(cancelled.err, context.Canceled) {
					t.Fatalf("phase did not stop at a lint cancellation: %+v", cancelled)
				}
			}()
			switch phase {
			case "analysis":
				newAnalysisControlled(g, control)
			case "dependency-graph":
				edges := map[string][]string{}
				for i := range 1024 {
					edges[fmt.Sprint(i)] = []string{fmt.Sprint(i + 1)}
				}
				findCycleControlled("0", edges, control)
			case "shadowed":
				alts := make([]grammar.Expr, 1024)
				for i := range alts {
					alts[i] = &grammar.Literal{Value: "a"}
				}
				copyAnalysis := *an
				copyAnalysis.control = control
				l := &linter{control: control, an: &copyAnalysis}
				l.shadowedAlts(alts, "alternative")
			case "suppression":
				findings := make([]Finding, 1024)
				for i := range findings {
					findings[i] = Finding{Check: CheckUnreachableRule, Pos: grammar.Pos{Line: 1, Col: 1}, Message: fmt.Sprint(i)}
				}
				l := &linter{control: control, an: an, findings: findings}
				l.suppress(g)
			}
			t.Fatal("phase completed without cancellation")
		})
	}
}

func TestRunContextOrdinaryParity(t *testing.T) {
	for _, source := range []string{
		lintManyRules(),
		"// lint:ignore shadowed-alternative reason\nrule main = \"a\" / \"ab\"",
		`rule main = operators { operand [0-9]; level sum { infix left "+" } }`,
	} {
		g := lintContextGrammar(t, source)
		for _, opts := range []Options{{}, {Disable: map[string]bool{CheckUnreachableRule: true}}} {
			want := Run(g, "main", opts)
			got, err := RunContext(lintTestContext(t, 0), g, "main", opts)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("controlled lint changed findings: %v / %v, %v", got, want, err)
			}
		}
	}
}
