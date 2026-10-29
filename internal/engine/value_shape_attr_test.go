package engine

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// attributeRE matches the @diagnostic and @error attributes of attributeShapeCases.
var attributeRE = regexp.MustCompile(` ?@(diagnostic\(name="[^"]*"\)|error\("[^"]*"\))`)

// TestAttributeKeepsValueShape checks that @diagnostic and @error do not change the value of
// an expression: every backend gives the same result for a grammar as for the grammar with its
// attributes removed.
func TestAttributeKeepsValueShape(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.AttributeShapeCases {
		plainSrc := attributeRE.ReplaceAllString(c.Src, "")
		if plainSrc == c.Src {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			attr, plain := mustCompile(t, c.Src), mustCompile(t, plainSrc)
			for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
				for _, input := range c.Inputs {
					opts := ParseOptions{Backend: backend}
					got, gerr := attr.ParseWith("main", input, opts)
					want, werr := plain.ParseWith("main", input, opts)
					if gerr != nil || werr != nil {
						// Only the diagnostics of a failure may differ.
						if (gerr == nil) != (werr == nil) {
							t.Errorf("backend=%v input=%q: attribute error %v, plain error %v", backend, input, gerr, werr)
						}
						continue
					}
					if g, w := resultJSON(got, nil), resultJSON(want, nil); g != w {
						t.Errorf("backend=%v input=%q: with attribute %s, without %s", backend, input, g, w)
					}
				}
			}
		})
	}
}

// TestAttributeAroundStreamRepetition checks that a typed parser with a stream is generated
// when an attribute sits between the group and the #stream repetition, or inside the element.
func TestAttributeAroundStreamRepetition(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = ("a")* #stream @diagnostic(name="x")`,
		`rule main = (("a")* @diagnostic(name="x")) #stream`,
		`rule main = ("a" @diagnostic(name="x"))* #stream`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if _, err := gentest.Generate(t, g, gen.GenOptions{Start: "main", Module: "x/y", Types: true, Stream: true}); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// TestStreamSeesThroughGroupsAndAttributes checks that #stream placement looks through groups
// and through attributes other than #recover and #missing, on every backend.
func TestStreamSeesThroughGroupsAndAttributes(t *testing.T) {
	t.Parallel()
	same := [][2]string{
		{`rule main = (("a")* #stream "b") @diagnostic(name="x")`, `rule main = ("a"* #stream "b")`},
		{`rule main = (("a")* #stream "b") @diagnostic(name="x")`, `rule main = ("a")* #stream "b"`},
		{`rule main = (x:("a")* #stream) @diagnostic(name="x") "b"`, `rule main = x:("a")* #stream "b"`},
		{`rule main = ((("a")* #stream) @diagnostic(name="x") "b") @error("want b")`, `rule main = ("a")* #stream "b"`},
	}
	inputs := []string{"aaab", "b", "aab"}
	for _, c := range same {
		attr, plain := mustCompile(t, c[0]), mustCompile(t, c[1])
		for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
			for _, in := range inputs {
				run := func(p *Program) (string, error) {
					var got []string
					err := p.ParseStreamWith("main", strings.NewReader(in), func(n *value.Node) error {
						got = append(got, fmt.Sprint(n))
						return nil
					}, ParseOptions{Backend: backend})
					return strings.Join(got, ","), err
				}
				g, gerr := run(attr)
				w, werr := run(plain)
				if in == "aaab" && g == "" {
					t.Errorf("%s backend=%v input=%q: no elements", c[0], backend, in)
				}
				if g != w || (gerr == nil) != (werr == nil) {
					t.Errorf("%s backend=%v input=%q: elements %q err %v, plain %q err %v", c[0], backend, in, g, gerr, w, werr)
				}
			}
		}
	}
	for _, src := range []string{
		`rule main = ("a"* #stream) #recover(skip=[^;]+ ";") "b"`,
		`rule main = (("a"* #stream) #recover(skip=[^;]+ ";") "b") @diagnostic(name="x")`,
		`rule main = ("a"* #stream "b") / "c"`,
		`rule main = ("a"* #stream / "c") "b"`,
		`rule main = (("a"* #stream) "b")* "c"`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Errorf("%s: compiled, want a #stream placement error", src)
		}
	}
}
