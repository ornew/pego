package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
)

func TestErrorAttribute(t *testing.T) {
	t.Parallel()
	check(t, `rule main = "hello" / _|_ @error(message="expect 'hello'")`,
		ok("hello", `"hello"@main`),
		fails("bye", `1:1: expect 'hello'`),
	)
	// The message is reported at the farthest position reached within the expression.
	check(t, `rule main = "let" " " name @error(message="expected a name") ";"
rule name = [a-z]+ #atomic [0-9]*`,
		fails("let 1;", `1:5: expected a name`),
		fails("let a", `1:6: syntax error: expected ";", [0-9], [a-z]`),
	)
	// On success, the expectations within the expression remain as usual.
	check(t, `rule main = "a"+ @error(message="as") "b"`,
		fails("aac", `1:3: syntax error: expected "a", "b"`),
	)
}

func TestRecoverAttribute(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`)
	n, err := prog.ParseWith("main", "ab;1x;cd;9;", ParseOptions{Expectations: true})
	if n == nil {
		t.Fatalf("no tree: %v", err)
	}
	want := `([("ab" ";")@stmt error("1:4: syntax error: expected [a-z]") ("cd" ";")@stmt error("1:10: syntax error: expected [a-z]")])@main`
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
	_, err = prog.ParseWith("main", "1x;ab", ParseOptions{Expectations: true})
	var se *SyntaxError
	if !errors.As(err, &se) || se.Error() != `1:6: syntax error: expected ";", [^;], [a-z]` {
		t.Errorf("got %v", err)
	}
}

// TestRecoverBacktracking checks that recovered errors undone by backtracking are not reported.
func TestRecoverBacktracking(t *testing.T) {
	t.Parallel()
	src := `rule main = item "!" / item "?"
rule item = ("a" #atomic ";") #recover(skip=[^;]+ ";")`
	for _, opts := range []compile.Options{{}, {DisableMemo: true}} {
		prog := mustCompile(t, src, opts)
		n, err := prog.ParseWith("main", "x;?", ParseOptions{Expectations: true})
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

// TestRecoverInLookahead checks that a result with errors recovered outside a lookahead is not
// reused inside one, or the other way round: errors recovered inside a lookahead carry no
// expectations, so the reused errors would differ from a parse without memoization.
func TestRecoverInLookahead(t *testing.T) {
	t.Parallel()
	check(t, `rule main = r1 "ab" / r1 _
rule r1 = &p0 p0
rule p0 = #operators {
    operand "a"
    operand "(" p0 ")"
    level { postfix "!" ";" #recover(skip=[^;]* ";") }
}`,
		ok("a!a;", "error: 1:3: syntax error\n1:3: syntax error: expected \";\""),
	)
}

// TestRecoverWithoutValue checks that #recover on an expression without a value (here a
// lookahead) gives no value after a recovery, on every backend, inside a sequence and as a
// rule body.
func TestRecoverWithoutValue(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"rule main = r0 \"b\"\nrule r0 = c0:_ (&\"a\") #recover(skip=[^;]* \";\")",
		"rule main = r0 \"b\"\nrule r0 = (&\"a\") #recover(skip=[^;]* \";\")",
	} {
		prog := mustCompile(t, src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := prog.ParseWith("main", "x;b", ParseOptions{Expectations: true, Backend: b})
			var errs SyntaxErrors
			if n == nil || !errors.As(err, &errs) || len(errs) != 1 {
				t.Errorf("%v: got %v, %v\n%s", b, n, err, src)
				continue
			}
			if s := n.String(); strings.Contains(s, "Error") {
				t.Errorf("%v: the recovery made a value: %s", b, s)
			}
		}
		checkBackends(t, prog, "main", "x;b")
	}
}

// TestRecoverInOperatorsRuleOperator checks that errors recovered inside an Operators Rule operator part are also
// reported.
func TestRecoverInOperatorsRuleOperator(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = e $$
rule e = #operators {
    operand [0-9]+ #atomic
    level { postfix "(" [0-9]+ #recover(skip=[^)\n]+) #atomic ")" }
}`)
	n, err := prog.ParseWith("main", "1(x)", ParseOptions{Expectations: true})
	var errs SyntaxErrors
	if n == nil || !errors.As(err, &errs) || len(errs) != 1 || errs[0].Col != 3 {
		t.Errorf("got %v, %v", n, err)
	}
}
