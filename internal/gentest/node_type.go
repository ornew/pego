package gentest

// NodeTypeGrammar uses named node types: rule conversions, constructors, unions of named
// node types, and the node members and functions on them.
const NodeTypeGrammar = `type Num = node[string]
type Ident = node[string]
type Pair = node[(Num, Ident)]
type Nums = node[[]Num]
type Any = node[_]
type Expr ~ Num | Ident
type Out = { Items []Expr; Pair *Pair; Len int; Text string; Start int; Nums *Nums; Any *Any }
rule main Out = items:spaced* p:pair? ns:nums? a:anything? $$ => Out{
  Items: $items.Value, Pair: $p, Len: foldl(0, $items, (n, x) => n + len(x)),
  Text: foldl("", $items, (s, x) => s + text(x)), Start: foldl(0, $items, (n, x) => x.Start), Nums: $ns, Any: $a
}
rule spaced Expr = -" "* x:item => $x
rule item Expr = num / ident
rule num Num = [0-9]+ #atomic
rule ident Ident = t:([a-z]+ #atomic) => Ident{$t}
rule pair Pair = "<" t:(num -"," ident) ">" => Pair{$t}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule anything Any = "!" x:("a" / "bc") => Any{$x}
`
