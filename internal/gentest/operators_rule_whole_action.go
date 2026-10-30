package gentest

// OperatorsRuleWholeActionCases are grammars whose Operators Rules have a whole-rule action, which
// runs once per call of the rule on the folded value ($value): after the complete fold, never on
// an intermediate reduction, and again for each call of the rule from an operand or operator part
// and for each level-restricted call.
var OperatorsRuleWholeActionCases = []Case{
	{"whole Operators Rule action", `type Wrap = { Value _ }
rule main = w:e $$ => $w
rule e Wrap = #operators {
    skip " "*
    operand [a-z]
    operand "(" x:e " "* ")" => $x
    level { infix left "+" }
    level { infix right "^" }
    level { prefix "-" }
    level { postfix "!" }
} => Wrap{Value: $value}`, []string{"a", " a", "a+b+c", "a ^ b ^ c", "-a!", "(a+b)+c", "a+(b+(c))", "-(a)!+b", "a+", "", "(a", "a b", "-"}},
	{"whole Operators Rule action on level-restricted calls", `type W = { V _ }
rule main = first:level(e, mul) rest:("," x:e)* $$ => concat(list($first), map($rest, (r) => r.$x))
rule e W = #operators {
    operand [0-9]
    level { infix left "," }
    level add { infix left "+" }
    level mul { infix left "*" }
} => W{V: $value}`, []string{"1", "1*2", "1+2", "1*2,3+4", "1,2,3", ","}},
	{"whole Operators Rule action with a named node type", `type Expr = node[_]
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand n:[0-9]+ #atomic => $n
    operand "(" x:expr ")" => $x
    level { infix left "+" }
    level { prefix "-" }
} => Expr{$value}`, []string{"1", "1+2", "-1+(2+3)", "1+", ""}},
	{"whole Operators Rule action passing the folded value", `rule main = e $$
rule e = #operators {
    operand [0-9]
    level { infix left "+" => $rhs }
    level { postfix "!" }
} => $value`, []string{"1", "1+2", "1!+2!", "1+2+3", "+"}},
	{"whole Operators Rule action error", `type R = { N int }
rule main = e $$
rule e R = #operators {
    operand [0-9]
    level { infix left "+" }
} => R{N: 10 / (len(text($value)) - 3)}`, []string{"1", "1+2", "1+2+3", "1+"}},
	{"whole Operators Rule action with a recovered operand", `type W = { V _ }
rule main = v:e $$ => $v
rule e W = #operators {
    operand ("i" ";") #recover(skip="?")
    level { infix left "+" }
} => W{V: $value}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", ""}},
}
