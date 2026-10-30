package gentest

// NamedASTGrammar is the grammar of the named node types and the AST they build.
const NamedASTGrammar = `type T = { Text string }
type S = { T T }
type U ~ S | T
type Alias ~ T
type Mixed ~ S | node[_]
type Number ~ int
type Values = { N Number; Text string; Flag bool }
type Recovery = { T T | error }
type Items ~ []S
type ParseRuleAST = { T T }
type ParseRuleAST_ ~ ParseRuleAST
rule main = t:term => ParseRuleAST{T: $t}
rule term T = t:("é") #atomic => T{Text: text($t)}
rule aliased Alias = t:("é") #atomic => T{Text: text($t)}
rule mixed Mixed = cst
rule structure = t:term => S{T: $t}
rule values = "v" => Values{N: 42, Text: "yes", Flag: true}
rule optional = t:term? => $t
rule items Items = xs:(s:structure)* => map($xs, (x) => x.$s)
rule union U = structure / term
rule cst = "x" "y"
rule open node[_] = "z"
rule arbitrary _ = "a" => S{}
rule abstract node[string] = "t"
rule recursive S = "(" s:recursive ")" / s:structure => $s
rule abort = x:"a"* #atomic => Values{N: 10 / len($x)}
rule recoverStruct S | error = structure #recover(skip="?")
rule recoverTerminal = t:term #recover(skip="?") => Recovery{T: $t}
rule recoverUnion U | error = union #recover(skip="?")
rule nonemptyItems Items = xs:(s:structure)+ => map($xs, (x) => x.$s)
rule recoverList Items | error = nonemptyItems #recover(skip="?")
rule recoveryElements [](S | error) = xs:(s:structure #recover(skip="?"))+ => map($xs, (x) => x.$s)
`
