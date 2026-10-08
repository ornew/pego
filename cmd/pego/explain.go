package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ornew/pego"
)

// explainCmd parses the input and, for each syntax error, prints the rule calls that recorded
// what the error says was expected, each with the calls it was nested in.
func explainCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	f := newDebugFlags("explain")
	limit := f.fs.Int("n", 10, "most calls to show per error (0 shows all)")
	p, src, opts, err := f.load(args, stdin)
	if err != nil {
		return err
	}
	// Parse once to find the errors, then again with a trace that keeps only the calls that
	// recorded expectations at their positions.
	_, perr := p.Parse(src, opts...)
	var errs []*pego.SyntaxError
	var se *pego.SyntaxError
	var list pego.SyntaxErrors
	recovered := false // the parse recovered from every error (Parse returned SyntaxErrors)
	switch {
	case perr == nil:
		fmt.Fprintln(stdout, "ok: the input matches")
		return nil
	case errors.As(perr, &se):
		errs = []*pego.SyntaxError{se}
	case errors.As(perr, &list):
		errs, recovered = list, true
	default:
		return perr
	}
	x := &explainer{at: map[int]*explained{}}
	for _, e := range errs {
		ex := x.at[e.Pos]
		if ex == nil {
			ex = &explained{items: map[string]bool{}}
			x.at[e.Pos] = ex
		}
		for _, s := range e.Expected {
			ex.items[s] = true
		}
		for _, s := range e.Messages {
			ex.items["#"+s] = true
		}
	}
	p.Parse(src, append(opts, pego.WithTrace(x.event))...)
	for i, e := range errs {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		if recovered {
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
	at    map[int]*explained // per error position
	// placed holds the recovered errors already attributed to a call.
	placed map[*pego.SyntaxError]bool
}

type explainFrame struct {
	rule string
	pos  int
	// nested holds the expectations that calls nested in this one recorded ("pos expected" or
	// "pos #message"): the call's own record includes them.
	nested map[string]bool
}

// explained holds the calls that recorded expectations at an error position.
type explained struct {
	calls []explainedCall
	// items holds what the errors at the position say was expected ("expected" or "#message"):
	// #error replaces the expectations of its expression, which calls in it still recorded.
	items map[string]bool
}

type explainedCall struct {
	stack    []string // innermost first: "rule line:col"
	expected []string // recorded by the call itself, not by the calls nested in it
	messages []string
	matched  bool
	text     string // the input matched, if the call matched
	memo     bool
	// recovered reports that a #recover in the call recovered from the error.
	recovered bool
}

func (x *explainer) event(e pego.TraceEvent) {
	if e.Kind == pego.TraceEnter {
		x.stack = append(x.stack, explainFrame{rule: e.Rule, pos: e.Pos})
		return
	}
	top := x.stack[len(x.stack)-1]
	x.stack = x.stack[:len(x.stack)-1]
	if f := e.Failure(); f != nil {
		x.record(e, top, f, false)
	}
	// A recovered error is no longer in the record of the call whose #recover recovered from it:
	// the first call to report it (the innermost, since calls end innermost first) is that call.
	for _, r := range e.Recovered() {
		if !x.placed[r] {
			if x.placed == nil {
				x.placed = map[*pego.SyntaxError]bool{}
			}
			x.placed[r] = true
			x.record(e, top, r, true)
		}
	}
}

// record lists, for the call top that e ends, the expectations of f (its failure or an error it
// recovered from) that it recorded itself, and passes all of them on to its caller.
func (x *explainer) record(e pego.TraceEvent, top explainFrame, f *pego.SyntaxError, recovered bool) {
	if x.at[f.Pos] == nil {
		return
	}
	var parent map[string]bool
	if n := len(x.stack); n > 0 {
		if x.stack[n-1].nested == nil {
			x.stack[n-1].nested = map[string]bool{}
		}
		parent = x.stack[n-1].nested
	}
	ex := x.at[f.Pos]
	c := explainedCall{matched: e.Matched && !recovered, text: e.Text(), memo: e.Memo, recovered: recovered}
	own := func(key, item, s string, dst *[]string) {
		if !top.nested[key] && ex.items[item] {
			*dst = append(*dst, s)
		}
		if parent != nil {
			parent[key] = true
		}
	}
	for _, s := range f.Expected {
		own(fmt.Sprintf("%d %s", f.Pos, s), s, s, &c.expected)
	}
	for _, s := range f.Messages {
		own(fmt.Sprintf("%d #%s", f.Pos, s), "#"+s, s, &c.messages)
	}
	if len(c.expected) == 0 && len(c.messages) == 0 {
		return
	}
	c.stack = append(c.stack, fmt.Sprintf("%s %s", top.rule, position(e, top.pos)))
	for i := len(x.stack) - 1; i >= 0; i-- {
		c.stack = append(c.stack, fmt.Sprintf("%s %s", x.stack[i].rule, position(e, x.stack[i].pos)))
	}
	if !slices.ContainsFunc(ex.calls, func(o explainedCall) bool {
		return slices.Equal(o.stack, c.stack) && slices.Equal(o.expected, c.expected) && slices.Equal(o.messages, c.messages)
	}) {
		ex.calls = append(ex.calls, c)
	}
}

func (x *explainer) print(w io.Writer, e *pego.SyntaxError, limit int) {
	ex := x.at[e.Pos]
	if len(ex.calls) > 0 {
		fmt.Fprintf(w, "\nCalls that recorded what was expected at %d:%d (innermost first):\n", e.Line, e.Col)
	}
	for i, c := range ex.calls {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "\n(%d more; -n 0 shows all)\n", len(ex.calls)-limit)
			break
		}
		var what []string
		if len(c.expected) > 0 {
			what = append(what, "expected "+strings.Join(c.expected, ", "))
		}
		what = append(what, c.messages...)
		how := ""
		if c.matched {
			how = "matched " + snippet(c.text, 30) + ", then "
		}
		note := ""
		if c.memo {
			note = " [memo]"
		}
		if c.recovered {
			note += " (recovered by #recover)"
		}
		fmt.Fprintf(w, "\n  %s: %s%s%s\n", c.stack[0], how, strings.Join(what, "; "), note)
		for _, call := range c.stack[1:] {
			fmt.Fprintf(w, "    in %s\n", call)
		}
	}
	// The end of the input is expected after the start rule returns, outside every call.
	if slices.Contains(e.Expected, "end of input") && !slices.ContainsFunc(ex.calls, func(c explainedCall) bool {
		return slices.Contains(c.expected, "end of input")
	}) {
		fmt.Fprintf(w, "\n  The start rule matched up to %d:%d, but the input does not end there.\n", e.Line, e.Col)
	}
}
