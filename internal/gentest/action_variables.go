package gentest

// Four alternatives force reuse both with deferred batch memoization and with
// Document's first-call memoization. No predicate reads x on behalf of r.
var ActionVariableCases = []Case{
	{"rule action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = leaf => V{N: x}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"transitive action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = inner
rule inner = leaf => V{N: x}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"operatorsRule operand action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf => V{N: x}
}
rule leaf = "a"`, []string{"a", "ab", "ac", "ad"}},
	{"operatorsRule prefix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { prefix "-" => V{N: x} }
}
rule leaf = "a"`, []string{"-a", "-ab", "-ac", "-ad"}},
	{"operatorsRule infix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { infix left "+" => V{N: x} }
}
rule leaf = "a"`, []string{"a+a", "a+ab", "a+ac", "a+ad"}},
	{"operatorsRule postfix action variables", `type V = { N int }
var x int
rule main = {x := 1} r "b" / {x := 2} r "c" / {x := 3} r "d" / {x := 4} r
rule r = #operators {
    operand leaf
    level { postfix "!" => V{N: x} }
}
rule leaf = "a"`, []string{"a!", "a!b", "a!c", "a!d"}},
}
