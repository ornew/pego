package gentest

// CaptureAccessCases returns the grammars and inputs of the capture access tests, which every backend runs.
func CaptureAccessCases() []Case {
	return []Case{
		{Name: "map elements", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => map($xs, (i) => i.$y)`, Inputs: []string{"a1b2", "a1", "", "a"}},
		{Name: "unmatched alternative", Src: `rule main = xs:(y:"a" / z:"b")+ $$ => filter($xs, (i) => i.$y != nil)`, Inputs: []string{"abab", "bb", ""}},
		{Name: "both captures", Src: `rule main = xs:(y:"a" z:"b")+ $$ => list($xs[0].$z, $xs[0].$y)`, Inputs: []string{"ab", "abab", "a", ""}},
		{Name: "predicate", Src: `rule main = xs:(y:"a"? "b")+ ({$xs[0].$y != nil} "!" / "?") $$`, Inputs: []string{"ab!", "b?", "b!", "ab?"}},
		{Name: "chained", Src: `rule main = xs:(ys:(z:"a" "b")+ "c")+ $$ => $xs[0].$ys[0].$z`, Inputs: []string{"abc", "ababc", "c", ""}},
		{Name: "indexed", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => $xs[len($xs) - 1].$y`, Inputs: []string{"a1b2", ""}},
		{Name: "struct field", Src: "type Out = { Y *node[string]; Z *node[string] }\nrule main = xs:(y:\"a\"? z:\"b\")+ $$ => Out{Y: $xs[0].$y, Z: $xs[0].$z}", Inputs: []string{"ab", "b", ""}},
		{Name: "sequence capture", Src: `rule main = x:(y:"a" "b") $$ => $x.$y`, Inputs: []string{"ab", "a", ""}},
		{Name: "struct list", Src: "type Out = { Ys []node[string] }\nrule main = xs:(-\",\" y:[a-z])* $$ => Out{Ys: map($xs, (i) => i.$y)}", Inputs: []string{",a,b", "", ",a,"}},
	}
}
