package engine

// operatorsRuleOperandActionCases are Operators Rules whose operands have actions. The value of
// an operand with an action is the action's result, and an operator node that applies to it starts
// where the operand starts, not where the operand's action result says (a result that is not a node
// has no span).
var operatorsRuleOperandActionCases = []genCase{
	{"operatorsRule operand action", `rule main = v:e $$ => $v
rule e = #operators {
    operand "i" => "o"
    level { infix left "+" }
}`,
		[]string{"i", "i+i", "i+i+i", "+", "i+", ""}},
	{"operatorsRule operand action prefix", `rule main = v:e $$ => $v
rule e = #operators {
    operand "i" => "o"
    level { prefix "-" }
    level { postfix "!" }
}`,
		[]string{"i", "-i", "i!", "--i!!", "-i!", "-", "i-", "é"}},
	{"operatorsRule operand action and captures", `rule main = v:e $$ => $v
rule e = #operators {
    operand x:"i" #atomic => $x
    operand "é" => 1
    level { infix left "+" => ($lhs, $op, $rhs) }
    level { prefix "-" => ($op, $rhs) }
    level { postfix "!" => ($lhs, $op) }
}`, []string{"i", "é", "i+é", "-i+é!", "é!", "-é", "i+", "é+é+i"}},
	{"operatorsRule operand rule call with scalar result", `rule main = v:e $$ => $v
rule e = #operators {
    operand n
    level { infix left "+" }
    level { postfix "!" }
}
rule n = "i" => 7`, []string{"i", "i+i", "i!", "i+i!", "+", ""}},
	{"operatorsRule scalar results of operators", `rule main = v:e $$ => $v
rule e = #operators {
    skip " "*
    operand "i" => "o"
    operand "n" => nil
    level { infix right "^" => "p" }
    level { infix left "+" => $lhs }
    level { prefix "-" => 2 }
    level { postfix "!" => $lhs }
}`, []string{"i", " i", "i^i^i", "i + i", " i + i!", "n+i", "i+n", "-i", "- i!", "--i", "i!!+i", "n!", " n ^ i", "i^"}},
	{"operatorsRule scalar operand actions and typed results", `type Atom = { Text node[string] }
type Sum = { L Expr; R Expr; At int }
type Expr ~ Atom | Sum
rule main Expr = v:e $$ => $v
rule e Expr = #operators {
    operand x:"i" #atomic => Atom{Text: $x}
    level { infix left "+" => Sum{L: $lhs, R: $rhs, At: 1} }
}`, []string{"i", "i+i", "i+i+i", "i+"}},
}
