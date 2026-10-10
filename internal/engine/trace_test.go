package engine

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"
	"testing"
)

// traceLines parses input with a trace and returns one line per event:
//
//	> rule@pos[:level][ lookahead]                      (enter)
//	< rule@pos ok end|fail [memo] [evals=n] [! far: expected]  (exit)
//
// indented by depth, and the parse result.
func traceLines(prog *Program, start, input string, o ParseOptions) ([]string, string) {
	var out []string
	o.Trace = func(e TraceEvent) {
		var b strings.Builder
		b.WriteString(strings.Repeat(" ", e.Depth-1))
		if e.Kind == TraceEnter {
			fmt.Fprintf(&b, "> %s@%d", e.Rule, e.Pos)
		} else {
			fmt.Fprintf(&b, "< %s@%d", e.Rule, e.Pos)
		}
		if e.Level != 0 {
			fmt.Fprintf(&b, ":%d", e.Level)
		}
		if e.Lookahead {
			b.WriteString(" lookahead")
		}
		if e.Kind == TraceExit {
			if e.Matched {
				fmt.Fprintf(&b, " ok %d %q", e.End, e.Text())
			} else {
				b.WriteString(" fail")
			}
			if e.Memo {
				b.WriteString(" memo")
			}
			if e.Evals > 1 {
				fmt.Fprintf(&b, " evals=%d", e.Evals)
			}
			fmt.Fprintf(&b, " examined=%d", e.Examined)
			if f := e.Failure(); f != nil {
				fmt.Fprintf(&b, " ! %d %s", f.Pos, f.Message())
			}
		}
		out = append(out, b.String())
	}
	n, err := prog.ParseWith(start, input, o)
	return out, resultJSON(n, err)
}

// checkTrace checks that tracing changes no result on any backend, nor the work of the parse
// (evaluations, memo reuses, memo entries and memoization decisions), and that the events are
// well formed. (Backends need not report the same calls: they skip different calls by
// first-character dispatch.)
func checkTrace(t *testing.T, prog *Program, start, input string, o ParseOptions) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		o.Backend = b
		o.Trace = nil
		n, err := prog.ParseWith(start, input, o)
		want := resultJSON(n, err)
		if _, got := traceLines(prog, start, input, o); got != want {
			t.Errorf("input %q (%v, unit %v, recognize %v): tracing changes the result\n without %s\n with    %s", input, b, o.Unit, o.Recognize, want, got)
		}
		w1, r1 := parseWork(prog, start, input, o)
		o.Trace = func(TraceEvent) {}
		w2, r2 := parseWork(prog, start, input, o)
		o.Trace = nil
		if r1 != want || r2 != want || w1 != w2 {
			t.Errorf("input %q (%v, unit %v, recognize %v): tracing changes the work of the parse\n without %+v %s\n with    %+v %s", input, b, o.Unit, o.Recognize, w1, r1, w2, r2)
		}
		checkEvents(t, prog, start, input, o)
	}
}

// parseWorkStats is what a parse did: its counts, the memo entries it left, and the memoization
// decisions it made (the first calls recorded, and the rules memoized eagerly).
type parseWorkStats struct {
	Stats
	entries, firstCalls int
	eager               string
}

// parseWork parses as ParseWith does and returns what the parse did, with the result (as
// resultJSON).
func parseWork(prog *Program, start, input string, o ParseOptions) (parseWorkStats, string) {
	target := prog
	if o.Recognize {
		rp, err := prog.recognizer()
		if err != nil {
			return parseWorkStats{}, resultJSON(nil, err)
		}
		target = rp
	}
	p := newParser(target, input, o.Unit, !o.Recognize)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	p.setTrace(o.Trace)
	n, err := target.run(p, o.Backend, start)
	if o.Recognize {
		n = nil
	}
	w := parseWorkStats{Stats: p.stats}
	for _, e := range p.memo.slots {
		for ; e != nil; e = e.next {
			w.entries++
		}
	}
	for _, x := range p.memo.seen {
		w.firstCalls += bits.OnesCount64(x)
	}
	w.eager = fmt.Sprint(slices.Collect(func(yield func(bool) bool) {
		for _, c := range p.memo.calls {
			if !yield(c.eager) {
				return
			}
		}
	}))
	return w, resultJSON(n, err)
}

// checkEvents checks that the events of a parse nest and that exits agree with their enters.
func checkEvents(t *testing.T, prog *Program, start, input string, o ParseOptions) {
	t.Helper()
	var stack []TraceEvent
	bad := ""
	o.Trace = func(e TraceEvent) {
		if bad != "" {
			return
		}
		if e.Kind == TraceEnter {
			stack = append(stack, e)
			if e.Depth != len(stack) {
				bad = fmt.Sprintf("enter %s at depth %d, want %d", e.Rule, e.Depth, len(stack))
			}
			return
		}
		if len(stack) == 0 {
			bad = "exit without enter"
			return
		}
		in := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch {
		case in.Rule != e.Rule || in.Pos != e.Pos || in.Level != e.Level || in.Depth != e.Depth || in.Lookahead != e.Lookahead:
			bad = fmt.Sprintf("exit %+v does not match enter %+v", e, in)
		case !e.Matched && e.End != e.Pos, e.End < e.Pos:
			bad = fmt.Sprintf("exit %s: end %d, pos %d", e.Rule, e.End, e.Pos)
		case e.Memo != (e.Evals == 0):
			bad = fmt.Sprintf("exit %s: memo %v with %d evaluations", e.Rule, e.Memo, e.Evals)
		case e.Examined < e.Pos || !e.Memo && e.Matched && e.Examined < e.End:
			bad = fmt.Sprintf("exit %s: examined %d, range [%d,%d)", e.Rule, e.Examined, e.Pos, e.End)
		}
	}
	_, err := prog.ParseWith(start, input, o)
	if _, ok := err.(*SyntaxError); err == nil || ok {
		if len(stack) != 0 && bad == "" {
			bad = fmt.Sprintf("%d calls not exited", len(stack))
		}
	}
	if bad != "" {
		t.Errorf("input %q (%v, unit %v): %s", input, o.Backend, o.Unit, bad)
	}
}

// TestTraceKeepsResults parses the corpus with and without tracing on every backend, in both
// units and in recognition mode.
func TestTraceKeepsResults(t *testing.T) {
	for _, c := range genCorpus(t) {
		prog := compile(t, c.src)
		for _, in := range c.inputs {
			for _, u := range []Unit{CodePoints, Bytes} {
				checkTrace(t, prog, "main", in, ParseOptions{Unit: u})
				checkTrace(t, prog, "main", in, ParseOptions{Unit: u, Recognize: true})
			}
			checkTraceDocument(t, prog, in)
		}
	}
}

// checkTraceDocument checks that tracing a Document changes neither its results nor its counts
// (Document.Stats), before and after edits.
func checkTraceDocument(t *testing.T, prog *Program, input string) {
	t.Helper()
	runes := []rune(input)
	mid := len(runes) / 2
	edits := []struct {
		start, end int
		text       string
	}{{mid, mid, "x"}, {mid, mid + 1, ""}, {0, min(1, len(runes)), "a"}}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		plain, err1 := prog.NewDocumentWith("main", input, ParseOptions{Backend: b})
		traced, err2 := prog.NewDocumentWith("main", input, ParseOptions{Backend: b, Trace: func(TraceEvent) {}})
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		for i := 0; ; i++ {
			n1, e1 := plain.Parse()
			n2, e2 := traced.Parse()
			if a, c := resultJSON(n1, e1), resultJSON(n2, e2); a != c || plain.Stats() != traced.Stats() {
				t.Errorf("input %q (%v), after %d edits: tracing changes the document\n without %+v %s\n with    %+v %s", input, b, i, plain.Stats(), a, traced.Stats(), c)
			}
			if i == len(edits) {
				break
			}
			ed := edits[i]
			if err1, err2 := plain.Edit(ed.start, ed.end, ed.text), traced.Edit(ed.start, ed.end, ed.text); err1 != nil || err2 != nil {
				t.Fatal(err1, err2)
			}
		}
	}
}

func TestTraceEvents(t *testing.T) {
	tests := []struct {
		name, src, input string
		want             []string
		err              string
	}{
		{
			name: "calls and failures",
			src: `
def main = item ("," item)* $$
def item = num / word
def num = @(?0-9)+
def word = @(?a-z)+`,
			input: "1,ab",
			want: []string{
				`> main@0`,
				` > item@0`,
				`  > num@0`,
				`  < num@0 ok 1 "1" examined=2 ! 1 syntax error: expected (?0-9)`,
				` < item@0 ok 1 "1" examined=2 ! 1 syntax error: expected (?0-9)`,
				` > item@2`,
				`  > num@2`,
				`  < num@2 fail examined=3 ! 2 syntax error: expected (?0-9)`,
				`  > word@2`,
				`  < word@2 ok 4 "ab" examined=5 ! 4 syntax error: expected (?a-z)`,
				` < item@2 ok 4 "ab" examined=5 ! 4 syntax error: expected (?a-z)`,
				`< main@0 ok 4 "1,ab" examined=5 ! 4 syntax error: expected ",", (?a-z)`,
			},
		},
		{
			// The second call of a at 0 memoizes it, and the third reuses the result.
			name: "memo hits",
			src: `
def main = a "x" / a "y" / a "z"
def a = b "!"
def b = "b"`,
			input: "b!z",
			want: []string{
				`> main@0`,
				` > a@0`,
				`  > b@0`,
				`  < b@0 ok 1 "b" examined=1`,
				` < a@0 ok 2 "b!" examined=2`,
				` > a@0`,
				`  > b@0`,
				`  < b@0 ok 1 "b" examined=1`,
				` < a@0 ok 2 "b!" examined=2`,
				` > a@0`,
				` < a@0 ok 2 "b!" memo examined=2`,
				`< main@0 ok 3 "b!z" examined=3 ! 2 syntax error: expected "x", "y"`,
			},
		},
		{
			// The leader grows its match by evaluating its body again; the recursive calls return
			// the result so far from the memo.
			name: "left recursion",
			src: `
def main = a $$
def a = a "x" / "y"`,
			input: "yx",
			want: []string{
				`> main@0`,
				` > a@0`,
				`  > a@0`,
				`  < a@0 fail memo examined=0`,
				`  > a@0`,
				`  < a@0 ok 1 "y" memo examined=1`,
				`  > a@0`,
				`  < a@0 ok 2 "yx" memo examined=2`,
				` < a@0 ok 2 "yx" evals=3 examined=3 ! 2 syntax error: expected "x"`,
				// The end of input is expected after the start rule returns.
				`< main@0 ok 2 "yx" examined=3 ! 2 syntax error: expected "x"`,
			},
		},
		{
			// Operands are parsed by the Pratt loop; a rule called with a level reports it.
			name: "pratt levels",
			src: `
def main = e(sum) $$
def e = pratt {
    operand n
    level { infix left "-" }
    level sum { infix left "+" }
    level { infix left "*" }
}
def n = @(?0-9)`,
			input: "1*2",
			want: []string{
				`> main@0`,
				` > e@0:1`,
				`  > n@0`,
				`  < n@0 ok 1 "1" examined=1`,
				`  > n@2`,
				`  < n@2 ok 3 "2" examined=3`,
				` < e@0:1 ok 3 "1*2" examined=4 ! 3 syntax error: expected "*", "+", "-"`,
				`< main@0 ok 3 "1*2" examined=4 ! 3 syntax error: expected "*", "+", "-"`,
			},
		},
		{
			name: "lookahead and syntax error",
			src: `
def main = !kw word $$
def kw = "if"
def word = @(?a-z)+`,
			input: "if",
			want: []string{
				`> main@0`,
				` > kw@0 lookahead`,
				` < kw@0 lookahead ok 2 "if" examined=2`,
				`< main@0 fail examined=2`,
			},
			err: "1:1: syntax error",
		},
		{
			// A recovered error is reported by the call that recovered, which matches.
			name: "recover",
			src: `
def main = stmt* $$
def stmt = (word ";") #recover(skip=(?^;)+ ";")
def word = @(?a-z)+`,
			input: "a;1;",
			want: []string{
				`> main@0`,
				` > stmt@0`,
				`  > word@0`,
				`  < word@0 ok 1 "a" examined=2 ! 1 syntax error: expected (?a-z)`,
				` < stmt@0 ok 2 "a;" examined=2 ! 1 syntax error: expected (?a-z)`,
				` > stmt@2`,
				`  > word@2`,
				`  < word@2 fail examined=3 ! 2 syntax error: expected (?a-z)`,
				` < stmt@2 ok 4 "1;" examined=4 ! 3 syntax error: expected (?^;)`,
				` > stmt@4`,
				`  > word@4`,
				`  < word@4 fail examined=5 ! 4 syntax error: expected (?a-z)`,
				` < stmt@4 fail examined=5 ! 4 syntax error: expected (?^;), (?a-z)`,
				`< main@0 ok 4 "a;1;" examined=5 ! 4 syntax error: expected (?^;), (?a-z)`,
			},
			err: "1:3: syntax error: expected (?a-z)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := compile(t, tt.src)
			for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
				lines, res := traceLines(prog, "main", tt.input, ParseOptions{Backend: b})
				if got, want := strings.Join(lines, "\n"), strings.Join(tt.want, "\n"); got != want {
					t.Errorf("%v: trace\n%s\nwant\n%s", b, got, want)
				}
				if !strings.Contains(res, tt.err) {
					t.Errorf("%v: result %s, want error %q", b, res, tt.err)
				}
			}
			checkTrace(t, prog, "main", tt.input, ParseOptions{})
		})
	}
}

// TestTraceStreamAndDocument checks tracing in stream parses and incremental documents.
func TestTraceStreamAndDocument(t *testing.T) {
	prog := compile(t, `
def main = line* #stream $$
def line = @(?a-z)* "\n"`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		var lines, traced []string
		emit := func(dst *[]string) func(*Node) error {
			return func(n *Node) error { *dst = append(*dst, n.String()); return nil }
		}
		err1 := prog.ParseStreamWith("main", strings.NewReader("ab\ncd\n"), emit(&lines), ParseOptions{Backend: b})
		calls := 0
		err2 := prog.ParseStreamWith("main", strings.NewReader("ab\ncd\n"), emit(&traced), ParseOptions{Backend: b, Trace: func(e TraceEvent) {
			if e.Kind == TraceExit && e.Rule == "line" && e.Matched {
				calls++
			}
		}})
		if fmt.Sprint(lines, err1) != fmt.Sprint(traced, err2) || calls != 2 {
			t.Errorf("%v: stream %v %v, traced %v %v, %d line calls", b, lines, err1, traced, err2, calls)
		}

		var evals, memo int
		d, err := prog.NewDocumentWith("main", "ab\ncd\n", ParseOptions{Backend: b, Trace: func(e TraceEvent) {
			if e.Kind == TraceExit {
				evals += e.Evals
				if e.Memo {
					memo++
				}
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		n1, _ := d.Parse()
		if evals != d.Stats().Evaluated {
			t.Errorf("%v: traced %d evaluations, stats %d", b, evals, d.Stats().Evaluated)
		}
		if err := d.Edit(0, 1, "x"); err != nil {
			t.Fatal(err)
		}
		evals, memo = 0, 0
		n2, _ := d.Parse()
		plain, _ := prog.ParseWith("main", "xb\ncd\n", ParseOptions{Backend: b})
		if n2.String() != plain.String() || n1 == nil {
			t.Errorf("%v: document %v, parse %v", b, n2, plain)
		}
		if evals != d.Stats().Evaluated || memo != d.Stats().Reused || memo == 0 {
			t.Errorf("%v: traced %d evaluations and %d memo hits, stats %+v", b, evals, memo, d.Stats())
		}
	}
}

// TestTracePanic checks that a panic in the trace function propagates out of the parse with its
// own value on every backend, rather than being reported as a fault of the parse (a runtime error
// on the bytecode backends would be "invalid bytecode").
func TestTracePanic(t *testing.T) {
	prog := compile(t, `def main = @(?a-z)+ $$`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		var got any
		var err error
		func() {
			defer func() { got = recover() }()
			var m map[string]int
			_, err = prog.ParseWith("main", "abc", ParseOptions{Backend: b, Trace: func(TraceEvent) {
				m["x"]++ // a runtime error
			}})
		}()
		if re, ok := got.(interface{ RuntimeError() }); !ok || err != nil {
			t.Errorf("%v: recovered %#v (%v), error %v", b, got, re, err)
		}
	}
}

// TestTraceRecovered checks that exit events give the errors recovered during the call, which
// are not part of Failure.
func TestTraceRecovered(t *testing.T) {
	prog := compile(t, `
def main = stmt* $$
def stmt = (word ";") #recover(skip=(?^;)+ ";")
def word = @(?a-z)+`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		var got []string
		_, err := prog.ParseWith("main", "ab1;cd;", ParseOptions{Backend: b, Trace: func(e TraceEvent) {
			if e.Kind == TraceExit && len(e.Recovered()) > 0 {
				got = append(got, fmt.Sprintf("%s@%d %v; failure %v", e.Rule, e.Pos, e.Recovered(), e.Failure()))
			}
		}})
		want := []string{
			`stmt@0 [1:3: syntax error: expected ";", (?a-z)]; failure 1:4: syntax error: expected (?^;)`,
			`main@0 [1:3: syntax error: expected ";", (?a-z)]; failure 1:8: syntax error: expected (?^;), (?a-z)`,
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") || err == nil {
			t.Errorf("%v: %v\n%s\nwant\n%s", b, err, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}
