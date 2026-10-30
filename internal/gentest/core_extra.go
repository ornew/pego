package gentest

// CoreExtraGrammars are grammars with term forms that the typed rules of the corpus lack: text
// comparisons of two captures and comparisons of fields.
var CoreExtraGrammars = []Case{
	{Name: "text equality", Src: `rule main = a:[a-z]+ "=" b:[a-z]+ => text($a) != text($b) || text($a) == text($b)
`},
	{Name: "field comparison", Src: `type P = { N int; S string }
type R = { A bool; B bool; C bool }
rule main R = p:pair q:pair => R{A: $p.N == 1, B: $p.S != "x", C: $p.N < $q.N}
rule pair P = n:[0-9]+ s:[a-z] => P{N: len($n), S: text($s)}
`},
}
