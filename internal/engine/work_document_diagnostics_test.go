//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkDocumentDiagnosticPublication(t *testing.T) {
	prog := compile(t, `rule main = "a;" @error(message="original") #recover(skip="?;")`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		for _, mode := range []string{"budget", "cancel", "syntax-error", "syntax-errors", "panic"} {
			t.Run(mode, func(t *testing.T) {
				d, err := prog.NewDocumentWith("main", "?;", base)
				if err != nil {
					t.Fatal(err)
				}
				warm := base
				warm.Trace = func(e TraceEvent) {
					if e.Kind == TraceExit && e.Rule == "main" {
						// The large private diagnostic makes copying span poll
						// boundaries without making the warm memo hit expensive.
						private := e.Recovered()[0]
						private.Expected = make([]string, 2048)
						for i := range private.Expected {
							private.Expected[i] = "token"
						}
						private.Messages = []string{"original"}
					}
				}
				if _, err := d.ParseWith(warm); err == nil {
					t.Fatal("missing recovered error")
				}
				before := d.memo
				var observed *parser
				var exitSteps uint64
				publicationPolled := false
				token := new(int)
				syntaxError := &SyntaxError{Messages: []string{"context error"}}
				syntaxErrors := SyntaxErrors{syntaxError}
				ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
				ctx.fn = func() error {
					if observed == nil || observed.control.stepsUsed < exitSteps+2048 {
						return nil
					}
					publicationPolled = true
					switch mode {
					case "syntax-error":
						return syntaxError
					case "syntax-errors":
						return syntaxErrors
					case "panic":
						panic(token)
					default:
						return context.Canceled
					}
				}
				o := base
				o.WorkLimits.MaxSteps = 1 << 20
				if mode != "budget" {
					o.Context = ctx
				}
				o.Trace = func(e TraceEvent) {
					if e.Kind == TraceExit && e.Rule == "main" {
						observed, exitSteps = e.p, e.p.control.stepsUsed
						if mode == "budget" {
							e.p.control.limits.MaxSteps = exitSteps + 64
						}
					}
				}
				var result *value.Node
				var caught any
				func() {
					defer func() { caught = recover() }()
					result, err = d.ParseWith(o)
				}()
				if result != nil || observed == nil || d.Stats().Evaluated != 0 {
					t.Fatalf("warm interrupted publication: result=%v observed=%v stats=%+v", result, observed, d.Stats())
				}
				switch mode {
				case "budget":
					if caught != nil || !errors.Is(err, limits.ErrWorkLimit) {
						t.Fatalf("budget: error=%v panic=%v", err, caught)
					}
				case "syntax-error":
					if caught != nil || err != syntaxError {
						t.Fatal("context SyntaxError identity changed")
					}
				case "syntax-errors":
					got, ok := err.(SyntaxErrors)
					if caught != nil || !ok || len(got) != 1 || &got[0] != &syntaxErrors[0] {
						t.Fatal("context SyntaxErrors identity changed")
					}
				case "panic":
					if caught != token {
						t.Fatalf("context panic changed: %v", caught)
					}
				default:
					if caught != nil || !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel: error=%v panic=%v", err, caught)
					}
				}
				if mode != "budget" && !publicationPolled {
					t.Fatal("did not interrupt during diagnostic copying")
				}
				workDocumentAssertAborted(t, d)
				if before == d.memo || observed.control != nil {
					t.Fatal("interruption retained memo or controller")
				}
				workDocumentAssertRetry(t, d)
			})
		}
	})
}

func TestWorkDocumentFatalSyntaxErrorsRemainUnchanged(t *testing.T) {
	prog := compile(t, `rule main = "a"`)
	workDocumentVariants(t, func(t *testing.T, base ParseOptions) {
		d, err := prog.NewDocumentWith("main", "a", base)
		if err != nil {
			t.Fatal(err)
		}
		original := SyntaxErrors{&SyntaxError{Messages: []string{"context failure"}}}
		ctx := &workContextAccessorContext{Context: context.Background(), done: make(chan struct{})}
		o := base
		o.Context = ctx
		o.Trace = func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				ctx.fn = func() error { return original }
				e.p.control.check()
			}
		}
		n, err := d.ParseWith(o)
		got, ok := err.(SyntaxErrors)
		if n != nil || !ok || len(got) != 1 || &got[0] != &original[0] {
			t.Fatal("fatal context error was mistaken for parser diagnostics")
		}
		workDocumentAssertAborted(t, d)
		workDocumentAssertRetry(t, d)
	})
}
