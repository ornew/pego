package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

// compileMessage compiles src and returns the error message, or "" if it compiles.
func compileMessage(t *testing.T, src string, opts ...compile.Options) string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err = Compile(g, o)
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestVarInitialValues checks that every parse starts from the declared initial values, through
// every entry point and every backend, and that a grammar can read them before it binds.
func TestVarInitialValues(t *testing.T) {
	t.Parallel()
	const src = `type R = { N int; S string; B bool; M int; Z int }
var n int = 7
var s = "init"
var b bool
var m = -3
var z int
rule main R = "a" => R{N: n, S: s, B: b, M: m, Z: z}
rule other R = {n := n + 1} "b" => R{N: n, S: s, B: b, M: m, Z: z}
`
	prog := mustCompile(t, src)
	const want = `R{B: false, M: -3, N: 7, S: "init", Z: 0}`
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			opts := ParseOptions{Backend: backend, Unit: unit}
			n, err := prog.ParseWith("main", "a", opts)
			if err != nil || n.String() != want {
				t.Errorf("%v/%v main: got %v, %v; want %s", backend, unit, n, err, want)
			}
			// Another start rule starts from the same values.
			n, err = prog.ParseWith("other", "b", opts)
			if err != nil || n.String() != `R{B: false, M: -3, N: 8, S: "init", Z: 0}` {
				t.Errorf("%v/%v other: got %v, %v", backend, unit, n, err)
			}
			// A parse that follows another one is not affected by its bindings.
			n, err = prog.ParseWith("main", "a", opts)
			if err != nil || n.String() != want {
				t.Errorf("%v/%v main again: got %v, %v", backend, unit, n, err)
			}
		}
	}
	// A document and a stream start from the initial values as well.
	doc, err := prog.NewDocument("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != want {
		t.Errorf("document: got %v, %v", n, err)
	}
	// A stream starts from them too, and the bindings of one element do not reach the next.
	streamed := mustCompile(t, `type E = { N int }
var n int = 7
rule main = (item)* #stream $$
rule item E = {n := n + 1} "a" => E{N: n}
`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		var got []string
		err := streamed.ParseStreamWith("main", strings.NewReader("aaa"), func(n *value.Node) error {
			got = append(got, n.String())
			return nil
		}, ParseOptions{Backend: backend})
		if err != nil || strings.Join(got, " ") != "E{N: 8} E{N: 8} E{N: 8}" {
			t.Errorf("stream %v: got %v, %v", backend, got, err)
		}
	}
	// A saved program keeps them in each mode that builds values.
	for _, mode := range valueModes {
		data, err := prog.Marshal("main", mode)
		if err != nil {
			t.Fatal(err)
		}
		loaded, start, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := loaded.ParseWith(start, "a", servedBy(mode, ParseOptions{Backend: backend}))
			if backend == Closure {
				wantClosureRefused(t, err)
				continue
			}
			if err != nil || n.String() != want {
				t.Errorf("loaded (%s mode) %v: got %v, %v", mode, backend, n, err)
			}
		}
	}
	// The module lists the variables with their types and values.
	vars := prog.Module().Vars
	if len(vars) != 5 {
		t.Fatalf("module variables: %+v", vars)
	}
}

// TestVarTypedReads checks that reads and bindings have the declared type: a read can be used
// where its type is accepted, and the checks do not apply without the type checker.
func TestVarTypedReads(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type V = { N int; S string; B bool }
var d int
var tag string = "t"
var on bool = true
rule main V = {d := d + 2} {tag := tag + "!"} "a" => V{N: d * 3, S: tag, B: on && d == 2}
`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err != nil || n.String() != `V{B: true, N: 6, S: "t!"}` {
			t.Errorf("%v: got %v, %v", backend, n, err)
		}
	}
	if got := compileMessage(t, "rule main = {x := 1} \"a\""); !strings.Contains(got, "undefined variable x") {
		t.Errorf("undeclared variable: %q", got)
	}
}

// TestGrammarWithoutVariables checks that a grammar without variable declarations has no
// variable table and no variables in its module.
func TestGrammarWithoutVariables(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "a" ("b" / "c")* $$`)
	if prog.Vars != nil || len(prog.Module().Vars) != 0 {
		t.Fatalf("variables: %v, %v", prog.Vars, prog.Module().Vars)
	}
	for _, in := range []string{"abc", "a", "ab?"} {
		a, errA := prog.ParseWith("main", in, ParseOptions{Backend: Closure})
		b, errB := prog.ParseWith("main", in, ParseOptions{Backend: Bytecode})
		if gentest.ResultJSON(a, errA) != gentest.ResultJSON(b, errB) {
			t.Errorf("%q: %s / %s", in, gentest.ResultJSON(a, errA), gentest.ResultJSON(b, errB))
		}
	}
}

// TestVarBindingsAreStatic checks that a binding is accepted only when the type checker proves its
// value fits the variable: a value of an unknown type is rejected with a hint to cast it, and a
// cast makes it acceptable. Nothing is checked when the binding runs.
func TestVarBindingsAreStatic(t *testing.T) {
	t.Parallel()
	const prelude = `var x int
rule dyn _ = di / ds
rule di = "i" => 1
rule ds = "s" => "str"
`
	const hint = "the type of the value is not known; convert it with a cast, as in value.(int)"
	for _, tc := range []struct{ name, src, want string }{
		{"a rule value of unknown type", prelude + "rule main = d:dyn {x := $d} \"z\" => x\n", "cannot use _ as int in the binding of variable x: " + hint},
		{"a list element of []_", prelude + "type L = { Items []_ }\nrule main = d:dyn {x := list($d)[0]} \"z\" => x\n", "in the binding of variable x: " + hint},
		{"a field of type _", prelude + "type F = { V _ }\nrule main = d:dyn {x := F{V: $d}.V} \"z\" => x\n", "cannot use _ as int in the binding of variable x: " + hint},
		{"a wrong type", prelude + "rule main = {x := \"s\"} \"a\"\n", "cannot use string as int in the binding of variable x"},
		{"a literal of the wrong type", "var s string\nrule main = {s := 1} \"a\"\n", "cannot use int as string in the binding of variable s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileMessage(t, tc.src); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q; want an error containing %q", got, tc.want)
			}
		})
	}

	// A cast is an explicit conversion: it is accepted, and its own failure is an evaluation error,
	// which makes the binding fail like any other evaluation error in a predicate.
	cast := mustCompile(t, prelude+"rule main = (d:dyn {x := $d.(int)} \"z\" / \"s\" \"y\") => x\n")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if n, err := cast.ParseWith("main", "iz", ParseOptions{Backend: backend}); err != nil || n.String() != "1" {
			t.Errorf("%v: got %v, %v; want 1", backend, n, err)
		}
		if _, err := cast.ParseWith("main", "sz", ParseOptions{Backend: backend}); err == nil {
			t.Errorf("%v: a failed cast in a binding was accepted", backend)
		}
	}
	checkBackends(t, cast, "main", "iz")
	checkBackends(t, cast, "main", "sz")

	// The generated bindings carry no check either: ASSIGN has no operand to ask for one.
	for _, in := range cast.Module().Code {
		if in.Op == bytecode.OpBind && in.C != 0 {
			t.Errorf("ASSIGN with operand C = %d", in.C)
		}
	}
	data, err := cast.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, start, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := loaded.ParseWith(start, "iz", ParseOptions{Backend: Bytecode}); err != nil || n.String() != "1" {
		t.Errorf("loaded: got %v, %v; want 1", n, err)
	}
}
