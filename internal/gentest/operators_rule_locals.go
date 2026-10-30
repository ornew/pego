package gentest

// OperatorsRuleLocalCases are grammars whose Operators Rules read a scalar value from a local: an
// operator's $lhs or $rhs, or the folded $value of a whole-rule action. A scalar travels boxed, with
// the range of the action that made it, and a local reads it as its value, as an ordinary capture
// does, on every backend.
var OperatorsRuleLocalCases = []Case{
	{"Operators Rule operator action on scalar operands", `rule main = e $$
rule e = #operators {
    operand "(" n:[0-9] ")" => text($n)
    operand [0-9]
    level { infix left "+" => text($lhs) }
}`, []string{"(1)", "1", "(1)+2", "1+2", "(1)+(2)", "(1)+2+3", "1+(2)+3", "(1)+", ""}},
	{"whole Operators Rule action on scalar operands", `rule main = v:e $$ => $v
rule e = #operators {
    operand "(" n:[0-9] ")" => text($n)
    operand [0-9]
    level { infix none "+" }
} => text($value)`, []string{"(1)", "1", "(1)+2", "1+2", "(1)+(2)", "(1)+", ""}},
	{"whole Operators Rule action passing a scalar fold", `rule main = e $$
rule e = #operators {
    operand "(" n:[0-9] ")" => text($n)
    operand [0-9]
    level { infix left "+" }
} => $value`, []string{"(1)", "1", "(1)+2", "1+2", ""}},
}
