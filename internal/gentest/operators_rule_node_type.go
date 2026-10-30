package gentest

// OperatorsNamedNodeCases declare an Operators Rule with a named node type: every prefix, infix and
// postfix node the rule builds, at every nesting level, is converted to that type, whether it is
// the tuple node of an operator without an action or the node that an action returns. The operands
// come from other rules that convert their own values.
func OperatorsNamedNodeCases() []Case {
	return []Case{
		{Name: "operators named tuple", Src: `type Expr = node[_]
type Other = node[string]
rule main Expr = e:expr ws $$ => $e
rule expr Expr = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { infix left "*" => Other{$op} }
    level { prefix "-" }
    level {
        postfix "!" => $lhs
        postfix "[" i:expr ws "]"
    }
    level { infix right "^" }
}
rule num Expr = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "1+2*3", "-1+2", "1+-2", "2!", "(1+2)[3]*4", "1+2!+3", "1 + (2 - 3) * 4", "2^3^4", "-2^3", "1+", "-", "1[2][3]!", "((1))"}},
		{Name: "operators without a named type", Src: `type Expr ~ node[_]
rule main = e:expr ws $$ => $e
rule expr Expr = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { prefix "-" }
    level { postfix "!" }
}
rule num = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "-1+2!", "(1+2)!"}},
		{Name: "operators named through an alias", Src: `type Expr = node[_]
type Alias ~ Expr
rule main Alias = e:expr ws $$ => $e
rule expr Alias = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { prefix "-" }
    level { postfix "!" }
}
rule num Alias = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "-1+2!", "(1+2)!", "1+"}},
	}
}
