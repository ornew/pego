package engine

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorAttribute(t *testing.T) {
	check(t, `def main = "hello" / _|_ #error(message="expect 'hello'")`,
		ok("hello", `"hello"@main`),
		fails("bye", `1:1: expect 'hello'`),
	)
	// The message is reported at the farthest position reached within the expression.
	check(t, `
def main = "let" " " name #error(message="expected a name") ";"
def name = @(?a-z)+ (?0-9)*`,
		fails("let 1;", `1:5: expected a name`),
		fails("let a", `1:6: syntax error: expected ";", (?0-9), (?a-z)`),
	)
	// On success, the expectations within the expression remain as usual.
	check(t, `def main = ("a"+ #error(message="as")) "b"`,
		fails("aac", `1:3: syntax error: expected "a", "b"`),
	)
}

func TestRecoverAttribute(t *testing.T) {
	prog := compile(t, `
def main = stmt* $$
def stmt = (@(?a-z)+ ";") #recover(skip=(?^;)+ ";")`)
	n, err := prog.Parse("main", "ab;1x;cd;9;")
	if n == nil {
		t.Fatalf("no tree: %v", err)
	}
	want := "(Seq [(Seq \"ab\" \";\")@stmt Error\"1x;\"{message=`1:4: syntax error: expected (?a-z)`}@stmt (Seq \"cd\" \";\")@stmt Error\"9;\"{message=`1:10: syntax error: expected (?a-z)`}@stmt])@main"
	if got := n.String(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	var errs SyntaxErrors
	if !errors.As(err, &errs) || len(errs) != 2 {
		t.Fatalf("got %v", err)
	}
	if errs[0].Line != 1 || errs[0].Col != 4 || errs[1].Col != 10 {
		t.Errorf("positions %v", err)
	}

	// If the whole parse fails, only the final error is returned.
	_, err = prog.Parse("main", "1x;ab")
	var se *SyntaxError
	if !errors.As(err, &se) || se.Error() != `1:6: syntax error: expected ";", (?^;), (?a-z)` {
		t.Errorf("got %v", err)
	}
}

// TestRecoverBacktracking checks that recovered errors undone by backtracking are not reported.
func TestRecoverBacktracking(t *testing.T) {
	src := `
def main = item "!" / item "?"
def item = (@"a" ";") #recover(skip=(?^;)+ ";")`
	for _, opts := range []Options{{}, {DisableMemo: true}} {
		prog := compile(t, src, opts)
		n, err := prog.Parse("main", "x;?")
		if n == nil {
			t.Fatalf("no tree: %v", err)
		}
		var errs SyntaxErrors
		// "x;" is reported only once (no duplicate from the memoized second item).
		if !errors.As(err, &errs) || len(errs) != 1 {
			t.Errorf("memo=%v: got %v", !opts.DisableMemo, err)
		}
	}
}

func TestRecoverTyping(t *testing.T) {
	// The value of a recovered expression may be an Error, but an Error can be placed where any
	// node type is expected.
	compile(t, `
type Stmt struct { Name Match }
def main = ss:stmt* $$ -> $ss
def stmt: Stmt = s:good #recover(skip=(?^;)+ ";") -> $s
def good: Stmt = n:@(?a-z)+ ";" -> new Stmt{Name: $n}
`)
}

func TestAttributeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`def main = "a" #error`, `#error requires a message argument`},
		{`def main = "a" #error(message=b)
def b = "b"`, `#error: message must be a string`},
		{`def main = "a" #recover`, `#recover requires a skip argument`},
		{`def main = "a" #recover(skip="x", until="y")`, `#recover has no argument until`},
		{`def main = "a" #nope`, `unknown attribute #nope`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestRecoverInPrattOperator checks that errors recovered inside a Pratt operator part are also
// reported.
func TestRecoverInPrattOperator(t *testing.T) {
	prog := compile(t, `
def main = e $$
def e = pratt {
    operand @(?0-9)+
    level { postfix "(" (@(?0-9)+ #recover(skip=(?^\)\n)+)) ")" }
}`)
	n, err := prog.Parse("main", "1(x)")
	var errs SyntaxErrors
	if n == nil || !errors.As(err, &errs) || len(errs) != 1 || errs[0].Col != 3 {
		t.Errorf("got %v, %v", n, err)
	}
}
