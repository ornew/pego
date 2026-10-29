package gentest

// IncrementalGrammar is a grammar with left recursion, an Operators Rule expression, line starts (which
// examine the preceding character), captures, and position-dependent actions.
const IncrementalGrammar = `type Pos = { At int; Name node[string] }
rule main = (line / blank)* $$
rule blank = "\n"
rule line = ^ (assign / list / pos) "\n"
rule assign = n:name " = " e:expr
rule list = list "," item / item
rule item = [a-z]+ #atomic
rule pos Pos = "@" n:name => Pos{At: $n.Start, Name: $n}
rule name = [a-z]+ #atomic
rule expr = #operators {
    operand [0-9]+ #atomic
    operand "(" x:expr ")" => $x
    level { infix left "+" / "-" }
    level { infix left "*" }
    level { prefix "-" }
}`
