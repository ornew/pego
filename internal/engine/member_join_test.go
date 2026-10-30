package engine

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestMemberAccessOnJoinTypeErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		// No member declares the field.
		{gentest.JoinMemberRules + "rule main = x:u => $x.Nope", "has no field Nope"},
		{gentest.JoinMemberRules + "rule main = x:(n / w) => $x.F", "has no field F"},
		// Nested: no member of the join of the first access declares the field.
		{gentest.JoinMemberRules + "rule main = x:v => $x.In.Nope", "has no field Nope"},
		// A field that only some members declare is rejected, node members included.
		{gentest.JoinMemberRules + "rule main = x:(n / a) => $x.Start", "A lacks it"},
		{gentest.JoinMemberRules + "rule main = x:(n / a) => $x.Value", "A lacks it"},
		{gentest.JoinMemberRules + "rule main = x:u => $x.X", "B lacks it"},
		{gentest.JoinMemberRules + "rule main = x:(a / n) => $x.F", "N lacks it"},
		{gentest.JoinMemberRules + "rule main = x:u => $x.X", "A | B has no field X in every member"},
		{gentest.JoinMemberRules + "rule main = x:u => $x.X", "x.(T).X"},
		// A member that may be nil is rejected.
		{gentest.JoinMemberRules + "rule main = x:u? => $x.F", "may be nil"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestMemberAccessOnJoinAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.JoinMemberCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, input := range fixture.Inputs {
				baseline := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

func TestMemberAccessOnJoinValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, input, want string }{
		{gentest.JoinMemberRules + "rule main = x:s $$ => $x.F\n", "c", `"c"`},
		{gentest.JoinMemberRules + "rule main = x:u $$ => $x.F\n", "b", `2`},
		{gentest.JoinMemberRules + "rule main = x:u $$ => $x.F\n", "a", `"a"`},
		{gentest.JoinMemberRules + "rule main = x:v $$ => $x.In.F\n", "d", `4`},
		{gentest.JoinMemberRules + "rule main = x:v $$ => $x.In.F\n", "c", `"ca"`},
		{gentest.JoinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "a", `1`},
		{gentest.JoinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "a", `1`},
		{gentest.JoinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "b", `cannot cast`},
	} {
		program := mustCompile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := gentest.ResultJSON(program.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: %s, want %s", backend, tc.input, got, tc.want)
			}
		}
	}
}

func TestMemberAccessOnJoinGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, append(gentest.JoinMemberCases(), gentest.TypedJoinMemberCases()...), gen.GenOptions{})
}
