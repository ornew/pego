package gentest

var TypedOperatorsRuleBodyCases = []Case{
	{"typed/OperatorsRule_associativity", `type Number = { Text string }
type Binary = { Left Expr; Right Expr; Op node[string] }
type Unary = { X Expr; Op node[string] }
type Expr ~ Number | Binary | Unary
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    skip " "*
    operand number
    operand "(" e:expr ")" => $e
    level { infix none "=" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { infix left "+" / "-" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { prefix "-" => Unary{X: $rhs, Op: $op} }
    level { infix right "^" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { postfix "!" => Unary{X: $lhs, Op: $op} }
}
rule number Number = t:([0-9]+) #atomic => Number{Text: text($t)}
`, []string{"1+2-3", "1^2^3", "-1^2!", "(1+2)!", "1=2", "1=2=3", "1+", "-", "1!+", "(1+2", " 1 + 2", ""}},
	{"typed/OperatorsRule_longest_ties", `type Atom = { Text string }
type Prefix = { X Expr; Tag int }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    level {
        prefix "-" => Prefix{X: $rhs, Tag: 1}
        prefix "--" => Prefix{X: $rhs, Tag: 2}
        prefix "--" => Prefix{X: $rhs, Tag: 3}
        prefix "¬" => Prefix{X: $rhs, Tag: 4}
    }
}
rule atom Atom = t:("-x" / "a" / "é") #atomic => Atom{Text: text($t)}
`, []string{"a", "-a", "--a", "---a", "--", "-x", "¬é", "¬", "--x", ""}},
	{"typed/OperatorsRule_element_captures", `type Atom = { Text string }
type Post = { X Expr; Items []node[string]; N int }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    level {
        postfix "[" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(1, 3) "]" => Post{X: $lhs, Items: map($items, (e) => e.$x), N: len($items)}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, []string{"a", "a[b,]", "a[b,c,][d,]", "a[]", "a[b,", "a[b,c,d,e,]", "a[é,]", ""}},
	{"typed/OperatorsRule_positional_actions", `type Atom = { Text string }
type Doc = { Text node[string]; Items []node[_] }
type Expr ~ Atom | Doc
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    operand v1:"[" "é" #atomic "]" => Doc{Text: $v1, Items: list($v1)}
    level { postfix "(" x:"x" #atomic ")" => Doc{Text: $x, Items: list($op)} }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, []string{"a", "[é]", "[é](x)", "a(x)", "[é", "a(x", "a(y)", ""}},
	{"typed/OperatorsRule_environment_rollback", `type Atom = { Text string }
type Prefix = { X Expr; Tag int; Text node[string] }
type Expr ~ Atom | Prefix
var n int
rule main Expr = {n := 0} e:expr $$ => $e
rule expr Expr = #operators {
    skip " " {n := 8} "!" / " "*
    operand atom
    level {
        prefix o:"-" #atomic {n := 1} "x" "z" => Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:"-" #atomic {n := 2} "x" => Prefix{X: $rhs, Tag: n, Text: $o}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, []string{"a", "-xa", "-xza", " -xa", " !a", "-x", "-xz", " -x", "", "-x?a"}},
	{"typed/OperatorsRule_lookahead_memo", `type Atom = { Text string }
type Prefix = { X Expr; Text node[string] }
type Expr ~ Atom | Prefix
rule main Expr = &(expr "!") e:expr "!" $$ => $e
rule expr Expr = #operators {
    operand atom
    level { prefix &x:"¬" #atomic x:"¬" #atomic => Prefix{X: $rhs, Text: $x} }
}
rule atom Atom = t:("é") #atomic => Atom{Text: text($t)}
`, []string{"é!", "¬é!", "¬¬é!", "¬!", "¬é?", "é!!", ""}},
	{"typed/OperatorsRule_nullable_parts", `type Atom = { Text string }
type Prefix = { X Expr }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    level { prefix "" => Prefix{X: $rhs} }
}
rule atom Atom = t:("a"?) #atomic => Atom{Text: text($t)}
`, []string{"", "a", "aa", "b"}},
	{"typed/OperatorsRule_framed_cuts", `type Doc = { Text node[_] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand x:"a" #atomic #cut => Doc{Text: $x}
    level { prefix "-" #cut "z" => Doc{Text: $op} }
}
`, []string{"a", "-za", "-a", "-z", "a?", "", "-x"}},
	{"typed/OperatorsRule_recovery", `type Doc = { Text node[string] | error }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand x:"a" #recover(skip=";") #atomic => Doc{Text: $x}
    level { prefix "-" #recover(skip=";") => Doc{Text: $op} }
}
`, []string{"a", ";", "-a", "a?", "", "-;"}},
	{"typed/OperatorsRule_unreachable_capture", `type Doc = { Text *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand x:"a" #atomic / _|_ ignored:"b" #atomic => Doc{Text: $ignored}
}
`, []string{"a", "b", "", "a?"}},
	{"typed/OperatorsRule_mixed_routes", `type Doc = { Text *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = #operators {
    operand x:"a" #atomic => Doc{Text: $x}
    operand x:"b" #atomic / _|_ ignored:"c" #atomic => Doc{Text: $ignored}
    operand x:"é" #atomic => Doc{Text: $x}
}
`, []string{"a", "b", "é", "c", "", "é?"}},
	{"typed/OperatorsRule_error_isolation", `type Atom = { Text string }
type Prefix = { X Expr; Text node[string] }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    operand atom
    level { prefix (x:"-" #atomic "z") @error(message="prefix marker") => Prefix{X: $rhs, Text: $x} }
}
rule atom Atom = t:("a" @error(message="atom")) #atomic => Atom{Text: text($t)}
`, []string{"a", "-za", "-z", "-a", "-", "b", ""}},
}
