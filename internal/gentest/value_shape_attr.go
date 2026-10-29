package gentest

// AttributeShapeCases are grammars whose rule body is a sequence, a lookahead sequence or a
// splice call under an attribute other than #recover. The attribute only labels or marks
// the expression, so the value of every rule must equal the value without the attribute.
// Each is also written without the attribute, so a difference between the pair shows in the
// comparison of every backend with the engine.
var AttributeShapeCases = []Case{
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
