package gentest

// TextCompareGrammar compares string fields of structs with each other, with literals and in
// predicates.
const TextCompareGrammar = `type Name = { Text string }
type Tag = { Text string; N int }
type Ops = { Eq bool; Ne bool; Lt bool; Le bool; Gt bool; Ge bool }
type Out = { Items node[_] }
rule main = items:(-" "* item)* $$ => Out{Items: $items}
rule item = cmp / mixed / pred
rule cmp = a:name "," b:name => Ops{Eq: $a.Text == $b.Text, Ne: $a.Text != $b.Text, Lt: $a.Text < $b.Text, Le: $a.Text <= $b.Text, Gt: $a.Text > $b.Text, Ge: $a.Text >= $b.Text}
rule mixed = "%" a:name ":" t:tag => Ops{Eq: $a.Text == $t.Text, Ne: $t.Text != "x", Lt: "m" < $a.Text, Le: $a.Text <= "m", Gt: $t.Text > $a.Text, Ge: $t.N >= 1}
rule pred = "?" a:name {$a.Text != "no"} {"a" <= $a.Text} => $a
rule tag = "<" t:([a-z]+ #atomic) ">" => Tag{Text: text($t), N: 1}
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}`

// TextCompareInputs are the inputs of TextCompareGrammar.
var TextCompareInputs = []string{"", "a,b", "b,a", "ab,ab", "%ab:<ab>", "%z:<a>", "%a:<x>", "?no", "?yes", "a,b %c:<d> ?x", "?", "a,"}
