package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

const joinMemberRules = `type A = { F string; X int }
type B = { F int }
type C = { F string; In A }
type D = { In B }
type N = node[string]
type U ~ A | B
type S ~ A | C
type V ~ C | D
rule a = "a" => A{F: "a", X: 1}
rule b = "b" => B{F: 2}
rule c = "c" => C{F: "c", In: A{F: "ca", X: 3}}
rule d = "d" => D{In: B{F: 4}}
rule n N = [0-9]+ #atomic
rule w = [a-z]+ #atomic
rule u U = a / b
rule s S = a / c
rule v V = c / d
`

func joinMemberCases() []genCase {
	return []genCase{
		{"same type", joinMemberRules + "rule main = x:s $$ => $x.F\n", []string{"a", "c", "x"}},
		{"different types", joinMemberRules + "rule main = x:u $$ => $x.F\n", []string{"a", "b", "x"}},
		{"inline choice", joinMemberRules + "rule main = x:(a / b / c) $$ => $x.F\n", []string{"a", "b", "c"}},
		{"nested", joinMemberRules + "rule main = x:v $$ => $x.In.F\n", []string{"c", "d", "x"}},
		{"node members", joinMemberRules + "rule main = x:(n / w) $$ => $x.Start + $x.End\n", []string{"12", "ab", ""}},
		{"node and struct", joinMemberRules + "rule main = x:(n / a) $$ => $x.(N).Start\n", []string{"12", "a"}},
		{"predicate", joinMemberRules + "rule main = x:u ({$x.F == $x.F} \"!\" / \"?\") $$\n", []string{"a!", "b?", "a?"}},
		{"constructor", joinMemberRules + "type R = { V string | int }\nrule main = x:u $$ => R{V: $x.F}\n", []string{"a", "b"}},
		{"lambda", joinMemberRules + "rule main = xs:u+ $$ => map($xs.Value, (i) => i.F)\n", []string{"ab", "ba", "a"}},
	}
}

func TestMemberAccessOnJoinTypes(t *testing.T) {
	for _, tc := range []struct{ src, rule, want string }{
		{joinMemberRules + "rule main = x:s => $x.F", "main", `string`},
		{joinMemberRules + "rule main = x:u => $x.F", "main", `string | int`},
		{joinMemberRules + "rule main = x:v => $x.In", "main", `A | B`},
		{joinMemberRules + "rule main = x:v => $x.In.F", "main", `string | int`},
		{joinMemberRules + "rule main = x:(n / w) => $x.Start", "main", `int`},
		{joinMemberRules + "rule main = x:(n / w) => $x.Value", "main", `string`},
		// A cast to a member that has the field is accepted.
		{joinMemberRules + "rule main = x:u => $x.(A).X", "main", `int`},
	} {
		types, errs := valueTypesOf(t, tc.src)
		if len(errs) > 0 {
			t.Errorf("%q: %v", tc.src, errs)
			continue
		}
		if got := types[tc.rule].String(); got != tc.want {
			t.Errorf("%q: %s, want %s", tc.src, got, tc.want)
		}
	}
}

func TestMemberAccessOnJoinTypeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// No member declares the field.
		{joinMemberRules + "rule main = x:u => $x.Nope", "has no field Nope"},
		{joinMemberRules + "rule main = x:(n / w) => $x.F", "has no field F"},
		// Nested: no member of the join of the first access declares the field.
		{joinMemberRules + "rule main = x:v => $x.In.Nope", "has no field Nope"},
		// A field that only some members declare is rejected, node members included.
		{joinMemberRules + "rule main = x:(n / a) => $x.Start", "A lacks it"},
		{joinMemberRules + "rule main = x:(n / a) => $x.Value", "A lacks it"},
		{joinMemberRules + "rule main = x:u => $x.X", "B lacks it"},
		{joinMemberRules + "rule main = x:(a / n) => $x.F", "N lacks it"},
		{joinMemberRules + "rule main = x:u => $x.X", "A | B has no field X in every member"},
		{joinMemberRules + "rule main = x:u => $x.X", "x.(T).X"},
		// A member that may be nil is rejected.
		{joinMemberRules + "rule main = x:u? => $x.F", "may be nil"},
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
	for _, fixture := range joinMemberCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := mustCompile(t, fixture.src)
			for _, input := range fixture.inputs {
				baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

func TestMemberAccessOnJoinValues(t *testing.T) {
	for _, tc := range []struct{ src, input, want string }{
		{joinMemberRules + "rule main = x:s $$ => $x.F\n", "c", `"c"`},
		{joinMemberRules + "rule main = x:u $$ => $x.F\n", "b", `2`},
		{joinMemberRules + "rule main = x:u $$ => $x.F\n", "a", `"a"`},
		{joinMemberRules + "rule main = x:v $$ => $x.In.F\n", "d", `4`},
		{joinMemberRules + "rule main = x:v $$ => $x.In.F\n", "c", `"ca"`},
		{joinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "a", `1`},
		{joinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "a", `1`},
		{joinMemberRules + "rule main = x:u $$ => $x.(A).X\n", "b", `cannot cast`},
	} {
		program := mustCompile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultJSON(program.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: %s, want %s", backend, tc.input, got, tc.want)
			}
		}
	}
}

// typedJoinMemberCases read a scalar field every member of the join declares with the same type
// and every constructor sets, which the typed runtime can read back.
func typedJoinMemberCases() []genCase {
	rules := "type A = { F string; X int }\ntype C = { F string }\ntype S ~ A | C\n" +
		"rule a = \"a\" => A{F: \"a\", X: 1}\nrule c = \"c\" => C{F: \"c\"}\nrule s S = a / c\n"
	return []genCase{
		{"typed same field", rules + "rule main = x:s $$ => $x.F\n", []string{"a", "c", "x"}},
		{"typed constructor", rules + "type R = { V string }\nrule main = x:s $$ => R{V: $x.F}\n", []string{"a", "c"}},
		{"typed list", rules + "rule main = xs:s+ $$ => map($xs.Value, (i) => i.F)\n", []string{"ac", "ca"}},
		{"typed predicate", rules + "rule main = x:s ({$x.F == \"a\"} \"!\" / \"?\") $$\n", []string{"a!", "c?", "a?"}},
	}
}

func TestMemberAccessOnJoinGeneratedGoParity(t *testing.T) {
	eachMode(t, testMemberAccessOnJoinGeneratedGoParity)
}

func testMemberAccessOnJoinGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, append(joinMemberCases(), typedJoinMemberCases()...), gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, joinMemberCases(), gen.GenOptions{ConvertTypes: true}, 0)
	testGeneratedTypesCorpus(t, goBin, typedJoinMemberCases(), gen.GenOptions{ConvertTypes: true}, 0)
}

func TestMemberAccessOnJoinGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, append(joinMemberCases(), typedJoinMemberCases()...), gen.GenOptions{})
}
