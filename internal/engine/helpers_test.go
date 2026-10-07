package engine

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// compile compiles PEGO source.
func compile(t *testing.T, src string, opts ...Options) *Program {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	prog, err := Compile(g, o)
	if err != nil {
		t.Fatalf("compile error: %v\n%s", err, src)
	}
	return prog
}

// compileError returns the compile error message.
func compileError(t *testing.T, src string) string {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	_, err = Compile(g, Options{})
	if err == nil {
		t.Fatalf("expected a compile error\n%s", src)
	}
	return err.Error()
}

// result returns the parse result as an S-expression, or "error: ..." on error.
func result(prog *Program, input string) string {
	return resultWith(prog, input, ParseOptions{})
}

func resultWith(prog *Program, input string, o ParseOptions) string {
	n, err := prog.ParseWith("main", input, o)
	if err != nil {
		return "error: " + err.Error()
	}
	return n.String()
}

type parseCase struct {
	input, want string
}

// check checks pairs of inputs and expected results against the grammar src. It also checks
// that the results are the same with and without memoization.
func check(t *testing.T, src string, cases ...parseCase) {
	t.Helper()
	prog := compile(t, src)
	noMemo := compile(t, src, Options{DisableMemo: true})
	for _, c := range cases {
		got := result(prog, c.input)
		if got != c.want && !(strings.HasPrefix(c.want, "error: ") && strings.Contains(got, c.want[len("error: "):]) && strings.HasPrefix(got, "error: ")) {
			t.Errorf("input %q\n got  %s\n want %s", c.input, got, c.want)
		}
		if got2 := result(noMemo, c.input); got2 != got {
			t.Errorf("input %q: result differs without memoization\n memo    %s\n no memo %s", c.input, got, got2)
		}
		checkBackends(t, prog, "main", c.input)
		checkBackends(t, noMemo, "main", c.input)
		checkUnits(t, prog, "main", c.input, Closure)
		checkUnits(t, prog, "main", c.input, Bytecode)
		checkUnits(t, prog, "main", c.input, BytecodeIterative)
	}
}

func ok(input, want string) parseCase { return parseCase{input, want} }

// fails expects a syntax error. want is a string contained in the error message.
func fails(input, want string) parseCase {
	return parseCase{input, "error: " + want}
}

func TestMain(m *testing.M) {
	m.Run()
}

func lines(s ...string) string { return strings.Join(s, "\n") }

func mustParse(src string) *grammar.Grammar {
	g, err := syntax.Parse(src)
	if err != nil {
		panic(err)
	}
	return g
}

// checkUnits checks that the result of parsing in bytes, with positions converted to code
// points, matches the result of parsing in code points.
func checkUnits(t *testing.T, prog *Program, start, input string, b Backend) {
	t.Helper()
	n1, err1 := prog.ParseWith(start, input, ParseOptions{Unit: CodePoints, Backend: b})
	n2, err2 := prog.ParseWith(start, input, ParseOptions{Unit: Bytes, Backend: b})
	n2, err2 = toCodePoints(input, n2, err2)
	if a, b := resultJSON(n1, err1), resultJSON(n2, err2); a != b {
		t.Errorf("input %q: results differ between units\n codepoints %s\n bytes      %s", input, a, b)
	}
}

// checkBackends checks that every backend produces the same result (value, positions, errors).
func checkBackends(t *testing.T, prog *Program, start, input string) {
	t.Helper()
	for _, u := range []Unit{CodePoints, Bytes} {
		n1, err1 := prog.ParseWith(start, input, ParseOptions{Unit: u})
		want := resultJSON(n1, err1)
		checkRecognize(t, prog, start, input, u, err1)
		for _, b := range []Backend{Bytecode, BytecodeIterative} {
			n2, err2 := prog.ParseWith(start, input, ParseOptions{Unit: u, Backend: b})
			if got := resultJSON(n2, err2); got != want {
				t.Errorf("input %q (unit %v): %v differs from closure\n closure %s\n %v %s", input, u, b, want, b, got)
			}
		}
	}
}

// checkRecognize checks that a parse that builds no tree (recognition) returns the same syntax
// error as an ordinary parse (or succeeds if it does). Results are not compared when the
// ordinary parse fails with a runtime error in an action (recognition does not evaluate
// actions).
func checkRecognize(t *testing.T, prog *Program, start, input string, u Unit, want error) {
	t.Helper()
	switch want.(type) {
	case nil, *SyntaxError, SyntaxErrors:
	default:
		return
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith(start, input, ParseOptions{Unit: u, Backend: b, Recognize: true})
		if n != nil || fmt.Sprint(err) != fmt.Sprint(want) {
			t.Errorf("input %q (unit %v, %v): recognition gives %v, %v; want %v", input, u, b, n, err, want)
		}
	}
}

// toCodePoints converts the positions in a result parsed in bytes to code points.
func toCodePoints(input string, n *Node, err error) (*Node, error) {
	// Map byte offsets to code point positions (a byte in the middle of a character maps to that
	// character's position).
	cp := make([]int, len(input)+1)
	i := 0
	for b := 0; b < len(input); i++ {
		_, size := utf8.DecodeRuneInString(input[b:])
		for k := 0; k < size; k++ {
			cp[b+k] = i
		}
		b += size
	}
	cp[len(input)] = i
	runes := []rune(input)
	lineCol := func(pos int) (int, int) {
		line, col := 1, 1
		for _, r := range runes[:min(pos, len(runes))] {
			if r == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
		return line, col
	}
	// Convert the column (in bytes) of "line:col: ..." to code points.
	fixMessage := func(msg string) string {
		var line, col int
		var rest string
		if k, _ := fmt.Sscanf(msg, "%d:%d:", &line, &col); k != 2 {
			return msg
		}
		rest = msg[strings.Index(msg[strings.Index(msg, ":")+1:], ":")+strings.Index(msg, ":")+1:]
		off := 0
		for l := 1; l < line; l++ {
			off += strings.Index(input[off:], "\n") + 1
		}
		_, c := lineCol(cp[off+col-1])
		return fmt.Sprintf("%d:%d%s", line, c, rest)
	}
	seen := map[*Node]*Node{}
	var conv func(*Node) *Node
	conv = func(n *Node) *Node {
		if n == nil {
			return nil
		}
		if c, ok := seen[n]; ok {
			return c
		}
		c := *n
		seen[n] = &c
		c.Start, c.End = cp[n.Start], cp[n.End]
		if n.Children != nil {
			c.Children = make([]*Node, len(n.Children))
			for i, ch := range n.Children {
				c.Children[i] = conv(ch)
			}
		}
		if n.Fields != nil {
			c.Fields = make(Fields, len(n.Fields))
			for i, f := range n.Fields {
				switch v := f.Value.(type) {
				case *Node:
					f.Value = conv(v)
				case string:
					if n.Type == TypeError && f.Name == "message" {
						f.Value = fixMessage(v)
					}
				}
				c.Fields[i] = f
			}
		}
		return &c
	}
	convErr := func(e *SyntaxError) *SyntaxError {
		c := *e
		c.Pos = cp[e.Pos]
		c.Line, c.Col = lineCol(c.Pos)
		return &c
	}
	switch e := err.(type) {
	case *SyntaxError:
		err = convErr(e)
	case SyntaxErrors:
		l := make(SyntaxErrors, len(e))
		for i, x := range e {
			l[i] = convErr(x)
		}
		err = l
	}
	return conv(n), err
}
