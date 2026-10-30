//go:build !pego_budget_controls_reference

package document_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
)

func TestBudgetDocumentReuseCancellationAndFreshRetry(t *testing.T) {
	prog := mustCompile(t, `rule main = line* $$

rule line = [a-z] #repeat(2) #atomic "\n"`)
	budgetDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"prefix", "shifted-tail"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("ab\n", 600)
				d, err := prog.NewDocumentWith("main", text, base)
				if err != nil {
					t.Fatal(err)
				}
				old, err := d.Parse()
				if err != nil {
					t.Fatal(err)
				}
				clone := old.Clone()
				history := resultJSON(clone, nil)
				start, end, replacement := 1500, 1501, "x"
				if phase == "shifted-tail" {
					start, end, replacement = 0, 0, "xy\n"
				}
				if err := d.Edit(start, end, replacement); err != nil {
					t.Fatal(err)
				}
				expectedText := text[:start] + replacement + text[end:]
				var running *core.Parser
				stoppedAt := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				observed := &budgetDocumentReuseContext{Context: ctx, observe: func() {
					if running != nil && running.Resumed >= 32 {
						stoppedAt = running.Resumed
						cancel()
					}
				}}
				o := base
				o.Context = observed
				o.Trace = func(e TraceEvent) {
					if e.Rule == "main" && e.Kind == TraceEnter {
						running = core.TraceParser(e)
					}
				}
				n, err := d.ParseWith(o)
				if n != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("node=%v error=%v", n, err)
				}
				if stoppedAt < 32 || stoppedAt >= 599 {
					t.Fatalf("canceled outside partial reuse: %d", stoppedAt)
				}
				if phase == "prefix" && stoppedAt >= 500 {
					t.Fatalf("prefix was fully replayed before cancellation: %d", stoppedAt)
				}
				budgetDocumentAssertAborted(t, d)
				if d.Text() != expectedText || d.ParseDefaults().Context != nil {
					t.Fatal("interruption changed text or defaults")
				}
				if running.Control != nil || running.Tr != nil || running.Prog != nil {
					t.Fatal("interruption retained trace or invocation references")
				}
				if resultJSON(clone, nil) != history {
					t.Fatal("reuse interruption changed historical Clone")
				}
				budgetDocumentAssertRetry(t, d)
			})
		}
	})
}
