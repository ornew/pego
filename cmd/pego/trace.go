package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ornew/pego"
)

// traceCmd prints the rule calls of a parse as an indented call tree, or as JSON lines.
func traceCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	f := newDebugFlags("trace")
	format := f.fs.String("f", "text", "output format: text (an indented call tree) or json (one event per line)")
	maxDepth := f.fs.Int("max-depth", 0, "show calls nested at most this deep, counting from the top call shown (1), which is the start rule or a call of a -rule rule; 0 shows all")
	var rules stringsFlag
	f.fs.Var(&rules, "rule", "show only the calls of this rule and the calls nested in them (repeatable, or comma-separated)")
	failures := f.fs.Bool("failures", false, "show what was expected where each failed call failed")
	p, src, opts, err := f.load(args, stdin)
	if err != nil {
		return err
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	w := bufio.NewWriter(stdout)
	t := &tracePrinter{w: w, json: *format == "json", maxDepth: *maxDepth, rules: rules, failures: *failures}
	_, perr := p.Parse(src, append(opts, pego.WithTrace(t.event))...)
	t.flush()
	if err := w.Flush(); err != nil {
		return err
	}
	if t.err != nil {
		return t.err
	}
	return perr
}

// tracePrinter prints trace events. In text, a call that shows no nested calls takes one line;
// otherwise its start and its end take a line each, around the nested calls.
type tracePrinter struct {
	w        *bufio.Writer
	json     bool
	maxDepth int
	rules    []string
	failures bool
	// root is the depth of the call of one of rules being shown (0 if none is in progress, or
	// if rules is empty).
	root int
	// pending is an enter event not printed yet, since the next event shown may be its exit.
	pending      *pego.TraceEvent
	pendingPos   string
	pendingDepth int
	err          error
}

// depth returns the depth at which the event is shown (1 for the top call shown), or 0 if it is
// hidden.
func (t *tracePrinter) depth(e pego.TraceEvent) int {
	d := e.Depth
	if len(t.rules) > 0 {
		if t.root == 0 {
			if e.Kind == pego.TraceExit || !slices.Contains(t.rules, e.Rule) {
				return 0
			}
			t.root = e.Depth
		}
		d = e.Depth - t.root + 1
		if e.Kind == pego.TraceExit && d == 1 {
			t.root = 0
		}
	}
	if t.maxDepth > 0 && d > t.maxDepth {
		return 0
	}
	return d
}

func (t *tracePrinter) event(e pego.TraceEvent) {
	d := t.depth(e)
	if d == 0 || t.err != nil {
		return
	}
	if t.json {
		t.printJSON(e)
		return
	}
	if e.Kind == pego.TraceEnter {
		t.flush()
		t.pending, t.pendingPos, t.pendingDepth = &e, position(e, e.Pos), d
		return
	}
	indent := strings.Repeat("  ", d-1)
	if t.pending != nil && t.pending.Depth == e.Depth {
		// A call that shows no nested calls: one line
		fmt.Fprintf(t.w, "%s%s %s -> %s\n", indent, callName(e), t.pendingPos, t.result(e))
		t.pending = nil
		return
	}
	t.flush()
	fmt.Fprintf(t.w, "%s%s -> %s\n", indent, callName(e), t.result(e))
}

// flush prints the pending enter event.
func (t *tracePrinter) flush() {
	if t.pending == nil {
		return
	}
	fmt.Fprintf(t.w, "%s%s %s\n", strings.Repeat("  ", t.pendingDepth-1), callName(*t.pending), t.pendingPos)
	t.pending = nil
}

func callName(e pego.TraceEvent) string {
	s := e.Rule
	if e.Level != 0 {
		s += fmt.Sprintf("(level %d)", e.Level)
	}
	if e.Lookahead {
		s += " [lookahead]"
	}
	return s
}

func position(e pego.TraceEvent, pos int) string {
	line, col := e.LineCol(pos)
	return pego.Location{Pos: pos, Line: line, Col: col}.String()
}

// result describes the outcome of the call that e ends.
func (t *tracePrinter) result(e pego.TraceEvent) string {
	var b strings.Builder
	if e.Matched {
		fmt.Fprintf(&b, "matched %s-%s %s", position(e, e.Pos), position(e, e.End), snippet(e.Text(), 40))
	} else {
		b.WriteString("failed")
		if f := e.Failure(); t.failures && f != nil {
			fmt.Fprintf(&b, " (at %d:%d: %s)", f.Line, f.Col, strings.TrimPrefix(f.Message(), "syntax error: "))
		}
	}
	if e.Memo {
		b.WriteString(" [memo]")
	}
	if e.Evals > 1 {
		fmt.Fprintf(&b, " [%d evaluations]", e.Evals)
	}
	return b.String()
}

// traceJSON is a trace event in JSON.
type traceJSON struct {
	Event     string       `json:"event"`
	Rule      string       `json:"rule"`
	Level     int          `json:"level,omitempty"`
	Depth     int          `json:"depth"`
	Pos       int          `json:"pos"`
	Line      int          `json:"line"`
	Col       int          `json:"col"`
	Lookahead bool         `json:"lookahead,omitempty"`
	Matched   *bool        `json:"matched,omitempty"`
	End       *int         `json:"end,omitempty"`
	EndLine   int          `json:"endLine,omitempty"`
	EndCol    int          `json:"endCol,omitempty"`
	Memo      bool         `json:"memo,omitempty"`
	Evals     *int         `json:"evals,omitempty"`
	Examined  *int         `json:"examined,omitempty"`
	Failure   *failureJSON `json:"failure,omitempty"`
}

type failureJSON struct {
	Pos      int      `json:"pos"`
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Expected []string `json:"expected,omitempty"`
	Messages []string `json:"messages,omitempty"`
}

func (t *tracePrinter) printJSON(e pego.TraceEvent) {
	j := traceJSON{Event: e.Kind.String(), Rule: e.Rule, Level: e.Level, Depth: e.Depth, Pos: e.Pos, Lookahead: e.Lookahead}
	j.Line, j.Col = e.LineCol(e.Pos)
	if e.Kind == pego.TraceExit {
		j.Matched, j.End, j.Evals, j.Examined = &e.Matched, &e.End, &e.Evals, &e.Examined
		j.EndLine, j.EndCol = e.LineCol(e.End)
		j.Memo = e.Memo
		if f := e.Failure(); t.failures && f != nil && !e.Matched {
			j.Failure = &failureJSON{Pos: f.Pos, Line: f.Line, Col: f.Col, Expected: f.Expected, Messages: f.Messages}
		}
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.err = err
		return
	}
	t.w.Write(data)
	t.w.WriteByte('\n')
}
