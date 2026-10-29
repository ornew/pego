package gentest

var NodeDirectCases = []Case{
	{"node/direct_memo_action", `type Pair = { Left node[string]; Right node[string] }
rule main = pair "!" $$ / pair "?" $$
rule pair Pair = left:[a-z] #atomic "-" right:number => Pair{Left: $left, Right: $right}
rule number = [0-9]+ #atomic
`, []string{"a-12!", "a-12?", "a-x?", "a-12", "é-12?", ""}},
	{"node/direct_nested_frames", `type Item = { Name node[string]; Value node[string] }
type Doc = { Items []Item }
rule main Doc = items:item #repeat(1, 3) $$ => Doc{Items: $items.Value}
rule item Item = name:[a-z]+ #atomic ":" value:[0-9]+ #atomic ";" => Item{Name: $name, Value: $value}
`, []string{"a:1;b:22;", "a:1;", "a:1;b:x;", "a:1;b:2;c:3;d:4;", ""}},
	{"node/direct_expectations", `rule main = item ";" $$
rule item = "let" " " name @error(message="expected a name") / "let" " " "_" name
rule name = [a-z]+ #atomic
`, []string{"let abc;", "let ;", "let _abc;", "let 1;", "let abc", ""}},
}
