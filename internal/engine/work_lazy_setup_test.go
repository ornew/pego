//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestWorkLazyClosureAdmission(t *testing.T) {
	for _, api := range []string{"batch", "document", "stream"} {
		t.Run(api, func(t *testing.T) {
			prog := compile(t, `rule main = "a"* #stream $$`)
			call := func(o ParseOptions) error {
				switch api {
				case "document":
					_, err := prog.NewDocumentWith("main", "aaa", o)
					return err
				case "stream":
					return prog.ParseStreamWith("main", strings.NewReader("aaa"), nil, o)
				default:
					_, err := prog.ParseWith("main", "aaa", o)
					return err
				}
			}
			for _, o := range []ParseOptions{{}, {Context: context.Background()}, {Backend: Bytecode, WorkLimits: WorkLimits{MaxSteps: 10000}}} {
				if err := call(o); err != nil {
					t.Fatal(err)
				}
				if prog.workProgram != nil {
					t.Fatal("inactive or bytecode call built closure controls")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := call(ParseOptions{Context: ctx}); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if prog.workProgram != nil {
				t.Fatal("pre-cancelled call built closure controls")
			}
			gate := newSetupPhaseContext(func() bool { return prog.workProgram != nil })
			if err := call(ParseOptions{Context: gate}); !errors.Is(err, context.Canceled) || !gate.triggered {
				t.Fatalf("post-setup: %v", err)
			}
			if prog.workProgram == nil {
				t.Fatal("setup not retained after cancellation")
			}
			if err := call(ParseOptions{WorkLimits: WorkLimits{MaxSteps: 10000}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkLazyClosureMetadataAndDocument(t *testing.T) {
	cases := []struct{ source, text string }{
		{`type Word = { Text string }
rule main = item+ $$
rule item = word ";"
rule word Word = t:([a-z]+) #atomic => Word{Text: text($t)}`, strings.Repeat("abc;", 80)},
		{`rule main = first:item rest:(-"," value:item)* $$ => concat(list($first), map($rest, (e) => e.$value))
rule item = [a-z]+`, strings.TrimSuffix(strings.Repeat("abc,", 80), ",")},
		{`rule main = expr $$
rule expr = operators {
 operand "a"
 level additive { infix left "+" }
 level { prefix "-" }
}`, strings.TrimSuffix(strings.Repeat("a+", 80), "+")},
	}
	for _, fixture := range cases {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			prog := compile(t, fixture.source)
			d, err := prog.NewDocumentWith("main", fixture.text, ParseOptions{Unit: unit})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				o := ParseOptions{Unit: unit}
				if i%2 == 1 {
					o.WorkLimits.MaxSteps = 1000000
					o.WorkLimits.MaxInputWork = 1000000
				}
				got, err := d.ParseWith(o)
				if err != nil {
					t.Fatal(err)
				}
				fresh, err := prog.ParseWith("main", d.Text(), o)
				if resultJSON(got, nil) != resultJSON(fresh, err) {
					t.Fatal("Document graph switch changed output")
				}
				if err := d.Edit(0, 1, "a"); err != nil {
					t.Fatal(err)
				}
			}
			// Force failure after a resumed prefix, then retry using the other graph.
			if err := d.Edit(len(fixture.text)/2, len(fixture.text)/2+1, "!"); err != nil {
				t.Fatal(err)
			}
			for _, limits := range []WorkLimits{{MaxSteps: 1000000, MaxInputWork: 1000000}, {}} {
				o := ParseOptions{Unit: unit, WorkLimits: limits}
				got, gotErr := d.ParseWith(o)
				fresh, freshErr := prog.ParseWith("main", d.Text(), o)
				if gotErr == nil || resultJSON(got, gotErr) != resultJSON(fresh, freshErr) {
					t.Fatalf("resumed diagnostics differ: %v / %v", gotErr, freshErr)
				}
			}
			controlled := prog.workProgram
			if controlled == nil || !reflect.DeepEqual(prog.descTable, controlled.descTable) || prog.nseen != controlled.nseen || len(prog.twins) != len(controlled.twins) {
				t.Fatal("lazy graph metadata differs")
			}
			for i, r := range append(append([]*rule{}, prog.rules...), prog.twins...) {
				all := append(append([]*rule{}, controlled.rules...), controlled.twins...)
				other := all[i]
				if r.id != other.id || r.memo != other.memo || r.leader != other.leader || r.seen != other.seen || r.positional != other.positional || r.plain != other.plain || !reflect.DeepEqual(r.scope.names, other.scope.names) {
					t.Fatalf("rule metadata differs: %s", r.name)
				}
			}
		}
	}
}

type lazyReentrantContext struct {
	context.Context
	run     func()
	entered bool
}

func (c *lazyReentrantContext) Err() error {
	if !c.entered {
		c.entered = true
		c.run()
	}
	return nil
}

func TestWorkLazyClosureContextReentry(t *testing.T) {
	for _, recognize := range []bool{false, true} {
		prog := compile(t, `rule main = "a" $$`)
		ctx := &lazyReentrantContext{Context: context.Background()}
		// Done must be non-nil for custom context checks to be active.
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx.Context = parent
		ctx.run = func() {
			n, err := prog.ParseWith("main", "a", ParseOptions{Recognize: recognize, WorkLimits: WorkLimits{MaxSteps: 10000}})
			if err != nil || !recognize && n == nil {
				t.Fatalf("reentrant parse: %v", err)
			}
		}
		_, err := prog.ParseWith("main", "a", ParseOptions{Recognize: recognize, Context: ctx})
		if err != nil || !ctx.entered {
			t.Fatalf("outer parse: %v", err)
		}
	}
}

// Once has completed even if an internal initializer panicked. A later call
// must report failed setup rather than publish or dereference a nil Program.
func TestWorkLazyClosureIncompleteInitialization(t *testing.T) {
	prog := compile(t, `rule main = "a"`)
	token := &workContextPanicToken{phase: "initializer"}
	got := workContextRecover(func() { prog.workOnce.Do(func() { panic(token) }) })
	if got != token {
		t.Fatal("panic identity changed")
	}
	n, err := prog.ParseWith("main", "a", ParseOptions{WorkLimits: WorkLimits{MaxSteps: 100}})
	if n != nil || err == nil || !strings.Contains(err.Error(), "initialization did not complete") {
		t.Fatalf("n=%v error=%v", n, err)
	}
	if n, err := prog.Parse("main", "a"); n == nil || err != nil {
		t.Fatal("plain graph affected", err)
	}
}
