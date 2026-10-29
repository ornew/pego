package gentest

const NilActionStream = `type V = { N int }
rule main = item* #stream $$
rule item = "a" => foldl(nil, list(V{N: 1}), (acc, v) => nil)`

var ActionLifecycleCases = []Case{
	{"nil allocating action", NilActionStream, []string{"", "a", "aaa", "ab"}},
	{"nil OperatorsRule action", `type V = { N int }
rule main = e $$
rule e = #operators {
    operand "a" => foldl(nil, list(V{N: 1}), (acc, v) => nil)
    level { postfix "!" => foldl(nil, list(V{N: 2}), (acc, v) => nil) }
}`, []string{"a", "a!", "a!!", "a?"}},
}
