package engine

import (
	"regexp"
	"testing"
)

// attributeShapeCases are grammars whose rule body is a sequence, a lookahead sequence or a
// splice call under an attribute other than #recover. The attribute only labels or marks
// the expression, so the value of every rule must equal the value without the attribute.
// Each is also written without the attribute, so a difference between the pair shows in the
// comparison of every backend with the engine.
var attributeShapeCases = []genCase{
	{"attribute shape/diagnostic over seq with predicates", `rule main = (!"x" [a-z]+ #atomic) @diagnostic(name="x")`, []string{"abc", "xbc", ""}},
	{"attribute shape/plain over seq with predicates", `rule main = (!"x" [a-z]+ #atomic)`, []string{"abc", "xbc", ""}},
	{"attribute shape/diagnostic over lookahead seq", `rule main = (&"a" "abc") @diagnostic(name="x")`, []string{"abc", "abd", ""}},
	{"attribute shape/plain over lookahead seq", `rule main = (&"a" "abc")`, []string{"abc", "abd", ""}},
	{"attribute shape/diagnostic in a field", `rule main = x:(("a" "b") @diagnostic(name="ab")) "c"`, []string{"abc", "ab", "c"}},
	{"attribute shape/diagnostic in an optional", `rule main = (("a" "b") @diagnostic(name="ab"))? "c"`, []string{"abc", "c", "ac"}},
	{"attribute shape/diagnostic in an alternative", `rule main = (("a" "b") @diagnostic(name="ab")) / "c"`, []string{"ab", "c", "a"}},
	{"attribute shape/diagnostic in a repetition", `rule main = (("a" "b") @diagnostic(name="ab"))*`, []string{"abab", "", "aba"}},
	{"attribute shape/error over seq", `rule main = ("a" "b") @error("want ab")`, []string{"ab", "a", ""}},
	{"attribute shape/diagnostic over splice call", `rule main = (_pair) @diagnostic(name="pair") "c"
rule _pair = "a" "b"`, []string{"abc", "ab", "c"}},
	{"attribute shape/diagnostic over splice body", `rule main = _pair "c"
rule _pair = ("a" "b") @diagnostic(name="pair")`, []string{"abc", "ab", "c"}},
}

// attributeRE matches the @diagnostic and @error attributes of attributeShapeCases.
var attributeRE = regexp.MustCompile(` ?@(diagnostic\(name="[^"]*"\)|error\("[^"]*"\))`)

// TestAttributeKeepsValueShape checks that @diagnostic and @error do not change the value of
// an expression: every backend gives the same result for a grammar as for the grammar with its
// attributes removed.
func TestAttributeKeepsValueShape(t *testing.T) {
	for _, c := range attributeShapeCases {
		plainSrc := attributeRE.ReplaceAllString(c.src, "")
		if plainSrc == c.src {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			attr, plain := compile(t, c.src), compile(t, plainSrc)
			for _, backend := range []Backend{Default, Bytecode, BytecodeIterative} {
				for _, input := range c.inputs {
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
	for _, src := range []string{
		`rule main = ("a")* #stream @diagnostic(name="x")`,
		`rule main = (("a")* @diagnostic(name="x")) #stream`,
		`rule main = ("a" @diagnostic(name="x"))* #stream`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if _, err := Generate(g, GenOptions{Start: "main", Module: "x/y", Types: true, Stream: true}); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}
