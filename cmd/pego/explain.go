package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ornew/pego"
)

// explainCmd parses the input and, for each syntax error, prints the rule calls that failed at the
// error's position: the innermost calls whose failures make up the error, with the calls they
// were nested in.
func explainCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	f := newDebugFlags("explain")
	limit := f.fs.Int("n", 10, "most call stacks to show per error (0 shows all)")
	p, src, opts, err := f.load(args, stdin)
	if err != nil {
		return err
	}
	// Parse once to find the errors, then again with a trace that keeps only the calls that
	// failed at their positions.
	_, perr := p.Parse(src, opts...)
	var errs []*pego.SyntaxError
	var se *pego.SyntaxError
	var list pego.SyntaxErrors
	switch {
	case perr == nil:
		fmt.Fprintln(stdout, "ok: the input matches")
		return nil
	case errors.As(perr, &se):
		errs = []*pego.SyntaxError{se}
	case errors.As(perr, &list):
		errs = list
	default:
		return perr
	}
	x := &explainer{at: map[int]*explained{}}
	for _, e := range errs {
		x.at[e.Pos] = &explained{covered: map[int]bool{}}
	}
	p.Parse(src, append(opts, pego.WithTrace(x.event))...)
	for i, e := range errs {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		if len(errs) > 1 {
			fmt.Fprintf(stdout, "%s (recovered)\n", e)
		} else {
			fmt.Fprintln(stdout, e)
		}
		x.print(stdout, e, *limit)
	}
	return nil
}

type explainer struct {
	stack []explainFrame
	seq   int
	at    map[int]*explained // per error position
}

type explainFrame struct {
	id   int
	rule string
	pos  int
}

// explained holds the failed calls found at an error position.
type explained struct {
	stacks []failedCall
	// covered holds the ids of the calls on the stacks found: a call that fails at the position
	// after a call nested in it did is not innermost.
	covered map[int]bool
}

type failedCall struct {
	calls  []string // innermost first: "rule line:col"
	what   string
	memo   bool
	failed bool
}

func (x *explainer) event(e pego.TraceEvent) {
	if e.Kind == pego.TraceEnter {
		x.seq++
		x.stack = append(x.stack, explainFrame{id: x.seq, rule: e.Rule, pos: e.Pos})
		return
	}
	top := x.stack[len(x.stack)-1]
	defer func() { x.stack = x.stack[:len(x.stack)-1] }()
	f := e.Failure()
	if f == nil {
		return
	}
	ex := x.at[f.Pos]
	if ex == nil || ex.covered[top.id] {
		return
	}
	c := failedCall{what: strings.TrimPrefix(f.Message(), "syntax error: "), memo: e.Memo, failed: !e.Matched}
	for i := len(x.stack) - 1; i >= 0; i-- {
		fr := x.stack[i]
		ex.covered[fr.id] = true
		c.calls = append(c.calls, fmt.Sprintf("%s %s", fr.rule, position(e, fr.pos)))
	}
	if !slices.ContainsFunc(ex.stacks, func(o failedCall) bool {
		return o.what == c.what && slices.Equal(o.calls, c.calls)
	}) {
		ex.stacks = append(ex.stacks, c)
	}
}

func (x *explainer) print(w io.Writer, e *pego.SyntaxError, limit int) {
	ex := x.at[e.Pos]
	if len(ex.stacks) == 0 {
		fmt.Fprintf(w, "\nNo rule call failed at %d:%d: the start rule matched up to there, and the input does not end.\n", e.Line, e.Col)
		return
	}
	fmt.Fprintf(w, "\nCalls that failed at %d:%d (innermost first):\n", e.Line, e.Col)
	for i, c := range ex.stacks {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "\n(%d more; -n 0 shows all)\n", len(ex.stacks)-limit)
			break
		}
		how := "expected"
		if !c.failed {
			how = "matched, but at the error position expected"
		}
		note := ""
		if c.memo {
			note = " [memo]"
		}
		fmt.Fprintf(w, "\n  %s: %s %s%s\n", c.calls[0], how, strings.TrimPrefix(c.what, "expected "), note)
		for _, call := range c.calls[1:] {
			fmt.Fprintf(w, "    in %s\n", call)
		}
	}
	// Expectations recorded outside every rule call: the start rule matched up to the error
	// position and the input continues there.
	for _, s := range e.Expected {
		if s == "end of input" && !slices.ContainsFunc(ex.stacks, func(c failedCall) bool { return strings.Contains(c.what, "end of input") }) {
			fmt.Fprintf(w, "\n  The start rule matched up to %d:%d, where the input does not end.\n", e.Line, e.Col)
		}
	}
}
