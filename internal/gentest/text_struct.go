package gentest

// TextStructGrammar has rules whose actions make structs with a field Text from the text they
// match.
const TextStructGrammar = `type Name = { Text string }
type Num = { Text string }
type Pair = { Text string; N int }
type Other = { Text string }
type Out = { Items node[_] }
rule main = items:(-" "* item)* $$ => Out{Items: $items}
rule item = tag / name / num / pair / plain / spaced / other
rule tag = "[" a:name "]" b:name {$b.Text == $a.Text} => $b
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}
rule num = t:([0-9]+ ("." [0-9]+)?) #atomic => Num{Text: text($t)}
rule pair = "<" t:([a-z]+ #atomic) ">" => Pair{Text: text($t)}
rule plain = t:"!" => Other{Text: text($t)}
rule spaced Name = t:("#" [a-z]*) #atomic => Name{Text: text($t) + "#"}
rule other = t:("@" [a-z]*) #atomic => Other{Text: text($t)}`
