//go:build !pego_work_controls_reference

package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// Compare the public parse result and exported diagnostic records with and
// without a generous work limit. Fixtures are drawn from attrs_test.go,
// gen_direct_recovery_test.go, and trace_lr_test.go.
func TestWorkDiagnosticPublicParity(t *testing.T) {
	tests := []struct {
		name, grammar string
		inputs        []string
		wantMemo      bool
		wantRecovered bool
	}{
		{
			name: "nested error labels",
			grammar: `rule main = declaration @error(message="outer statement")

rule declaration = "let" " " name ";"

rule name = [a-z]+ @error(message="inner name") #atomic`,
			inputs: []string{"let alice;", "let 1;", "let alice"},
		},
		{
			name: "lookahead with nested alternatives",
			grammar: `rule main = ("s" &(wide "!") wide "!" / "m" (wide "!" / wide "?") / "e" wide @error(message="expected a word")) $$

rule wide = ("é" "0" / "é" "1") #atomic`,
			inputs: []string{"sé0!", "sé@", "mé0?", "mé0@", "eé@"},
		},
		{
			name: "recovery succeeds",
			grammar: `type Doc = {
    Text *node[string]
    N    int
}

var n int
rule main Doc = {n := 0} e:item $$ => $e

rule item Doc = v:(x:"é" #atomic {n := 1} "!") #recover(skip="é" {n := n + 2} "?") => Doc{Text: $x, N: n}`,
			inputs:        []string{"é!", "é?", "é"},
			wantRecovered: true,
		},
		{
			name: "recovery skip fails",
			grammar: `type Doc = {
    Text node[string]
    N    int
}

var n int
rule main Doc = {n := 0} x:("ab" #recover(skip={n := 8} "x" "!") / {n == 0} "x" "?") #atomic $$ => Doc{Text: $x, N: n}`,
			inputs:        []string{"x!", "x?", "x", ""},
			wantRecovered: true,
		},
		{
			name: "zero progress recovery",
			grammar: `type Atom = { Text string }

rule main Atom = t:("ab" #recover(skip="x"?) $$) #atomic => Atom{Text: text($t)}`,
			inputs:        []string{"ab", "x", "y", ""},
			wantRecovered: true,
		},
		{
			name: "memoized recovery",
			grammar: `rule main = (r "!" / r "?") $$

rule r = item*

rule item = ("a" #atomic ";") #recover(skip=[^;]+ ";")`,
			inputs:        []string{"a;!", "x;?", "x;!"},
			wantMemo:      true,
			wantRecovered: true,
		},
		{
			name: "left recursive recovery",
			grammar: `type Doc = {
    Text node[string]
}

rule main Doc = x:expr #atomic $$ => Doc{Text: $x}

rule expr = (expr "+" #cut "a") #recover(skip="a?") / "a"`,
			inputs:        []string{"a", "a+a", "a?", "a?+a", "a+", ""},
			wantRecovered: true,
		},
		{
			name: "OperatorsRule operator recovery",
			grammar: `type Atom = { Text string }

type Post = {
    X    Expr
    Text node[_] | error
}

type Expr ~ Atom | Post

rule main Expr = e:expr $$ => $e

rule expr Expr = #operators {
    skip (" " "!") #recover(skip=" " "?")
    operand atom
    level {
        prefix ("-" #cut "z") #recover(skip="-?") => Post{X: $rhs, Text: $op}
        postfix ("[" x:"é" #atomic "]") #recover(skip="[?]") => Post{X: $lhs, Text: $op}
    }
}

rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}`,
			inputs:        []string{"a", "-za", "-?a", "a[é]", "a[?]", " !a", " ?a", "-a", "a[", "-?", "", "a ?[?]"},
			wantRecovered: true,
		},
	}

	type result struct {
		Node                 *Node
		Err                  error
		ErrorText, ErrorType string
	}
	marshal := func(t *testing.T, node *Node, err error) []byte {
		t.Helper()
		got := result{Node: node, Err: err}
		if err != nil {
			got.ErrorText, got.ErrorType = err.Error(), fmt.Sprintf("%T", err)
		}
		b, jsonErr := json.Marshal(got)
		if jsonErr != nil {
			t.Fatal(jsonErr)
		}
		return b
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prog := compile(t, tc.grammar)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{Bytes, CodePoints} {
					for _, recognize := range []bool{false, true} {
						t.Run(fmt.Sprintf("%v/%v/recognize=%t", backend, unit, recognize), func(t *testing.T) {
							memoObserved, recoveredObserved := false, false
							trace := func(e TraceEvent) {
								// Force eager recording for this fixture, on both paths.
								if e.Kind == TraceEnter {
									e.p.deferMemo = false
								}
								if e.Kind == TraceExit && e.Memo && e.Rule == "r" {
									memoObserved = true
								}
							}
							for _, input := range tc.inputs {
								base := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
								if tc.wantMemo {
									base.Trace = trace
								}
								wantNode, wantErr := prog.ParseWith("main", input, base)
								var recovered SyntaxErrors
								if errors.As(wantErr, &recovered) && len(recovered) > 0 {
									recoveredObserved = true
								}
								want := marshal(t, wantNode, wantErr)
								limited := base
								limited.WorkLimits = WorkLimits{MaxInputWork: 1 << 50, MaxSteps: 1 << 50}
								gotNode, gotErr := prog.ParseWith("main", input, limited)
								got := marshal(t, gotNode, gotErr)
								if !bytes.Equal(got, want) {
									t.Errorf("input %q: result differs\n got: %s\nwant: %s", input, got, want)
								}
								if after := marshal(t, wantNode, wantErr); !bytes.Equal(after, want) {
									t.Fatal("controlled retry changed retained baseline result")
								}
							}
							if tc.wantMemo && !memoObserved {
								t.Fatal("fixture did not produce an r memo hit")
							}
							if tc.wantRecovered && !recoveredObserved {
								t.Fatal("fixture did not produce recovered SyntaxErrors")
							}
						})
					}
				}
			}
		})
	}
}
