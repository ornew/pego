package gentest

// A hidden rule without an action makes no node of its own: a call of it contributes what its
// expression would, written in its place as a group. Each case gives the grammar written with
// the hidden rule and the same grammar with its expression written where it is called.
type SpliceCase struct {
	Name    string
	Hidden  string
	Inlined string
	Inputs  []string
}

func SpliceCases() []SpliceCase {
	return []SpliceCase{
		{"sequence", `rule main = "a" _m "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c") "d"`, []string{"abcd", "abd", "acd", "ad", "abc", ""}},
		{"one element", `rule main = "a" _m "d"
rule _m = "b"`, `rule main = "a" ("b") "d"`, []string{"abd", "ad", "abcd", ""}},
		{"choice", `rule main = "a" _m "d"
rule _m = "b" / "c" "e"`, `rule main = "a" ("b" / "c" "e") "d"`, []string{"abd", "aced", "acd", "ad", ""}},
		{"nested", `rule main = "a" _m "d"
rule _m = "b" _n
rule _n = "c" "e"`, `rule main = "a" ("b" ("c" "e")) "d"`, []string{"abced", "abcd", "ad", ""}},
		{"repetition", `rule main = "a" _m* "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c")* "d"`, []string{"ad", "abcd", "abcbcd", "abd", ""}},
		{"optional", `rule main = "a" _m? "d"
rule _m = "b" "c"`, `rule main = "a" ("b" "c")? "d"`, []string{"ad", "abcd", "abd", "abcbcd", ""}},
		{"whole body", `rule main = _m
rule _m = "b" "c"`, `rule main = ("b" "c")`, []string{"bc", "b", ""}},
		{"hidden captures", `rule main = "a" _m "d"
rule _m = x:"b" "c"`, `rule main = "a" ("b" "c") "d"`, []string{"abcd", "abd", ""}},
		{"captured call", `rule main = x:_m "d"
rule _m = "b" "c"`, `rule main = x:("b" "c") "d"`, []string{"bcd", "bd", ""}},
		{"no value", `rule main = "a" _m "b"
rule _m = &"b"`, `rule main = "a" (&"b") "b"`, []string{"ab", "a", ""}},
		{"lookahead in body", `rule main = "a" _m "c"
rule _m = !"x" "b"`, `rule main = "a" (!"x" "b") "c"`, []string{"abc", "axc", "ac", ""}},
		{"used twice", `rule main = _m "-" _m
rule _m = "a" "b"`, `rule main = ("a" "b") "-" ("a" "b")`, []string{"ab-ab", "ab-a", "ab", ""}},
		{"in a choice", `rule main = _m / "x"
rule _m = "a" "b"`, `rule main = ("a" "b") / "x"`, []string{"ab", "x", "a", ""}},
		{"in a repetition body", `rule main = (_m "-")*
rule _m = "a" "b"`, `rule main = (("a" "b") "-")*`, []string{"", "ab-", "ab-ab-", "ab"}},
		{"recovery", `rule main = (_m ";")* $$ #recover(skip=[^;]+ ";")
rule _m = [a-z]+ #atomic`, `rule main = (([a-z]+ #atomic) ";")* $$ #recover(skip=[^;]+ ";")`, []string{"ab;cd;", "ab;1x;cd;", "1x;", "ab"}},
	}
}

// SpliceGenCases returns the hidden-rule cases for the corpus that compares the backends and the
// generated parsers.
func SpliceGenCases() []Case {
	var cases []Case
	for _, tc := range SpliceCases() {
		cases = append(cases, Case{"hidden rule: " + tc.Name, tc.Hidden, tc.Inputs})
	}
	return append(cases,
		Case{"hidden rule: right recursion", `rule main = _l
rule _l = "x" _l / "x"`, []string{"x", "xxx", "", "xy"}},
		Case{"hidden rule: left recursion", `rule main = _e $$
rule _e = _e "+" "a" / "a"`, []string{"a", "a+a+a", "a+", ""}},
		Case{"hidden rule: optional recursion", `rule main = _t
rule _t = "(" _t? ")"`, []string{"()", "(())", "(()", ""}},
		Case{"hidden rule: cut", `rule main = _m / "a" "c"
rule _m = "a" #cut "b"`, []string{"ab", "ac", "a", ""}},
	)
}
