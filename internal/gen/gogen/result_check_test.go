package gogen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestRuleResultChecksAreSelective checks that a result is never checked when it runs: a result
// not statically known to fit the rule's declared type is rejected when the grammar is compiled,
// whether or not the rule has an action, and a cast is the only check, which compiled files and
// generated parsers keep.
func TestRuleResultChecksAreSelective(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testRuleResultChecksAreSelective)
}

func testRuleResultChecksAreSelective(t *testing.T) {
	const decls = `type Int = { Text string }
type S = { F Int }
type O = { X Int }
rule static Int = "s" => Int{Text: "s"}
rule val _ = "v" => Int{Text: "v"}
`
	for _, tc := range []struct{ name, rules, want string }{
		{"an action", "rule main = s:static d:dynamic => S{F: $d}\nrule dynamic Int = v:val => $v\n",
			"rule dynamic produces _, which is not assignable to Int: the type of the value is not known; convert it with a cast, as in value.(Int)"},
		{"no action", "rule main = s:static d:dynamic => S{F: $d}\nrule dynamic Int = val\n",
			"rule dynamic produces _, which is not assignable to Int"},
		{"a node type", "rule main = s:static p:pass => S{F: Int{Text: \"\"}}\nrule pass node[_] = val\n",
			"rule pass produces _, which is not assignable to node[_]"},
		{"an Operators Rule operand", "rule main = s:static o:ops => S{F: Int{Text: \"\"}}\nrule ops Int | O = #operators {\n    operand val\n    operand \"o\" => Int{Text: \"o\"}\n    level { infix left \"+\" => Int{Text: \"+\"} }\n}\n",
			"produces _, which is not assignable to Int | O"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileMessage(t, decls+tc.rules); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q; want an error containing %q", got, tc.want)
			}
		})
	}
	src := decls + `rule main = s:static d:dynamic p:pass o:ops => S{F: $d}
rule dynamic Int = v:val => $v.(Int)
rule pass node[_] = v:val => $v.(node[_])
rule ops Int | O = #operators {
    operand v:val => $v.(Int | O)
    operand "o" => Int{Text: "o"}
    level { infix left "+" => Int{Text: "+"} }
}
`
	prog := mustCompile(t, src)
	d := prog.Module().Disassemble()
	if n := strings.Count(d, "check="); n != 0 {
		t.Fatalf("a result is checked when it runs:\n%s", d)
	}
	if n := strings.Count(d, "ECAST"); n != 3 {
		t.Fatalf("disassembly has %d casts, want 3:\n%s", n, d)
	}
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := engine.LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"svvv", "svvo+o", "svvv+o"} {
		want := resultWith(prog, in, engine.ParseOptions{})
		if strings.Contains(want, `"err"`) {
			t.Fatalf("%q: %s", in, want)
		}
		for _, b := range []engine.Backend{engine.Closure, engine.Bytecode} {
			got := resultWith(loaded, in, engine.ParseOptions{Backend: b})
			if b == engine.Closure {
				if !strings.Contains(got, "closure backend needs the grammar source") {
					t.Fatalf("%q loaded %s: %s, want the closure backend refused", in, b, got)
				}
				continue
			}
			if got != want {
				t.Fatalf("%q loaded %s: %s\nwant %s", in, b, got, want)
			}
		}
	}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(code), "CheckResult("); n != 0 {
		t.Fatalf("generated %d result checks", n)
	}
	if n := strings.Count(string(code), "c.Cast("); n != 3 {
		t.Fatalf("generated %d casts, want 3", n)
	}
}
