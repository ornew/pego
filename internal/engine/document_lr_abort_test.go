//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// TestDocumentLeftRecursionAbortedGrowth interrupts a Document parse while a
// left-recursive rule is growing, by cancellation, a work limit or a trace
// panic, after an earlier parse left reusable results. The interrupted parse
// must leave nothing reusable, and parsing again, before and after further
// edits, must match a fresh parse.
func TestDocumentLeftRecursionAbortedGrowth(t *testing.T) {
	grammars := []struct{ name, src, text string }{
		{"direct", `rule main = x $$
rule x = x "+" "a" / "a"`, "a+a+a+a"},
		{"indirect", `rule main = x $$
rule x = y "+" "a" / "a"
rule y = x`, "a+a+a+a"},
		{"cut", `rule main = x $$
rule x = x "+" #cut "a" / "a"`, "a+a+a+a"},
		{"nested", `rule main = x $$
rule x = x "+" t / t
rule t = t "*" "a" / "a"`, "a*a+a+a*a*a"},
	}
	for _, g := range grammars {
		prog := mustCompile(t, g.src)
		for _, cause := range []string{"cancel", "steps", "panic"} {
			t.Run(g.name+"/"+cause, func(t *testing.T) {
				workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
					d, err := prog.NewDocumentWith("main", g.text, base)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := d.Parse(); err != nil {
						t.Fatal(err)
					}
					if err := d.Edit(len(g.text), len(g.text), "+a"); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					o := base
					o.Context = ctx
					grown := 0
					o.Trace = func(e TraceEvent) {
						// A matched memo hit of a growing rule returns its
						// provisional result to a deeper call.
						if e.Kind != TraceExit || !e.Memo || !e.Matched || e.Rule == "main" {
							return
						}
						if grown++; grown != 2 {
							return
						}
						switch cause {
						case "cancel":
							cancel()
						case "steps":
							traceParser(e).control.limits.MaxSteps = traceParser(e).control.stepsUsed
						case "panic":
							panic("interrupt growth")
						}
					}
					var n *value.Node
					func() {
						defer func() {
							if x := recover(); x != nil && (cause != "panic" || x != "interrupt growth") {
								panic(x)
							}
						}()
						n, err = d.ParseWith(o)
					}()
					if grown < 2 {
						t.Fatalf("growth reached %d provisional results", grown)
					}
					switch cause {
					case "cancel":
						if n != nil || !errors.Is(err, context.Canceled) {
							t.Fatalf("node=%v err=%v", n, err)
						}
					case "steps":
						if n != nil || !errors.Is(err, limits.ErrWorkLimit) {
							t.Fatalf("node=%v err=%v", n, err)
						}
					}
					if d.memo.len() != 0 || len(d.runs) != 0 || len(d.kids) != 0 || d.machine != nil {
						t.Fatal("interrupted growth retained reusable state")
					}
					workDocumentAssertRetry(t, d)
					workDocumentAssertRetry(t, d)
					text := d.Text()
					mid := strings.Index(text, "+")
					for _, edit := range []struct {
						start, end int
						text       string
					}{{mid, mid + 1, "+a+"}, {0, 1, "a+a"}, {len(text), len(text), "+"}} {
						if err := d.Edit(edit.start, edit.end, edit.text); err != nil {
							t.Fatal(err)
						}
						workDocumentAssertRetry(t, d)
					}
				})
			})
		}
	}
}
