package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

// node[string] is the type of a string node, in fields, rules and casts, and is checked like one.
func TestNodeStringType(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"type P = { K node[string] }\nrule main P = k:[a-z]+ #atomic => P{K: $k}",
		"rule main node[string] = \"a\"",
		"rule main = x:\"a\" => $x.(node[string])",
		"type W = { V int }\ntype T ~ node[string] | W\nrule main T = \"a\"",
	} {
		for _, opts := range []compile.Options{{}} {
			g, err := parseTest(src)
			if err != nil {
				t.Fatalf("%q: %v", src, err)
			}
			p, err := Compile(g, opts)
			if err != nil {
				t.Fatalf("%q: %v", src, err)
			}
			if _, err := p.Parse("main", "a"); err != nil {
				t.Fatalf("%q: parse: %v", src, err)
			}
		}
	}
	// A value that is not a string node does not fit: statically, and in a cast at run time.
	for _, tc := range []struct{ src, want string }{
		{"type P = { K node[string] }\nrule main P = k:(\"a\" \"b\") => P{K: $k}", "cannot use"},
		{"rule main node[string] = \"a\" \"b\"", "not assignable"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	g, err := parseTest("rule main = k:(\"a\" \"b\") => $k.(node[_]).(node[string])")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse("main", "ab"); err == nil {
		t.Fatal("a tuple node was accepted as node[string] at run time")
	}
}

// node[_] is a node of any value: it accepts and rejects what node does, in a field, a rule's
// declared type, a cast and an optional, on every backend, and reports the same type errors.
func TestDynamicNodeType(t *testing.T) {
	t.Parallel()
	const src = `type S = { F TYPE }
rule main S = v:val => S{F: $v}
rule val = x:("a" / "b" "c" / "d"*) => $x
rule cast TYPE = x:val => $x.(TYPE)
rule opt *TYPE = x:val? => $x`
	run := func(typ, in string) string {
		prog := mustCompile(t, strings.ReplaceAll(src, "TYPE", typ))
		var out []string
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, start := range []string{"main", "cast", "opt"} {
				got := ""
				if n, err := prog.ParseWith(start, in, ParseOptions{Backend: b}); err != nil {
					got = "error: " + err.Error()
				} else {
					got = n.String()
				}
				out = append(out, start+" "+b.String()+": "+got)
			}
		}
		return strings.Join(out, "\n")
	}
	// Every node fits node[_], in a field, a cast and an optional rule type, on every backend.
	for _, in := range []string{"a", "bc", "dd", "", "x"} {
		for _, line := range strings.Split(run("node[_]", in), "\n") {
			if strings.Contains(line, "error:") && (in == "a" || in == "bc" || in == "dd" || strings.Contains(line, " type ")) {
				t.Errorf("%q: %s", in, line)
			}
		}
	}
	// node without a type argument is not a type: the grammar API and earlier syntax reach the
	// checker with it.
	for _, typ := range []string{"node", "*node", "[]node"} {
		g, err := parseLegacyTest(strings.ReplaceAll(src, "TYPE", typ))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "node takes a type argument: write node[T] with the type T of the node's value") {
			t.Errorf("%s: got %v", typ, err)
		}
	}
	// A struct and an int do not fit either.
	const bad = `type S = { F TYPE }
type T = { Text string }
rule main = "t" => S{F: T{Text: "t"}}
rule other TYPE = "i" => T{Text: "i"}
rule cast = x:"a" => S{F: 1}`
	errs := map[string]string{}
	for _, typ := range []string{"node[_]"} {
		g, err := parseTest(strings.ReplaceAll(bad, "TYPE", typ))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Compile(g, compile.Options{})
		if err == nil {
			t.Fatalf("%s: accepted", typ)
		}
		errs[typ] = err.Error()
	}
	for _, want := range []string{"cannot use T as node[_] in field F of S", "rule other produces T, which is not assignable to node[_]", "cannot use int as node[_] in field F of S"} {
		if !strings.Contains(errs["node[_]"], want) {
			t.Errorf("errors %s: want %q", errs["node[_]"], want)
		}
	}
	for _, typ := range []string{"node[_]"} {
		if got, _ := mustCompile(t, "rule main = x:\"a\" => $x.("+typ+")").RuleType("main"); got != "node[_]" {
			t.Errorf("cast to %s: rule type %s", typ, got)
		}
	}
}
