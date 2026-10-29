package gentest

// IntegerBoundaryGrammar computes integers beyond the 64-bit range from the
// length of the input, so that they are not constants.
const IntegerBoundaryGrammar = `type I = { Add int; Sub int; Mul int; Neg int; Quo int; Rem int; Big int }
rule main = x:"a"* #atomic => I{
    Add: 9223372036854775807 + len($x),
    Sub: 0 - 9223372036854775807 - 1 - len($x),
    Mul: 9223372036854775807 * (len($x) + 1),
    Neg: -(0 - 9223372036854775807 - len($x)),
    Quo: (0 - 9223372036854775807 - 1) / (0 - len($x)),
    Rem: (0 - 9223372036854775807 - 1) % (0 - len($x)),
    Big: 9223372036854775807 - len($x),
}`

const ActionEnvironmentGrammar = `type Item = { Depth int; Tag string; Size int; Name node[string]; Note *node[string] }
type Out = { Items []Item; Count int; Sum int; Tag string }
var depth int
var tag string
var last string
rule main Out = {depth := 0} {tag := "top"} peek? xs:item* $$ => Out{Items: $xs.Value, Count: len($xs), Sum: foldl(depth, $xs, (s, i) => s + i.Depth * 10 + i.Size), Tag: tag}
rule peek = {depth := 5} (item "%" / plain "%")
rule item Item = nested / tagged / deep / shout / listed / late / plain
rule nested Item = "(" {depth := depth + 1} i:item ")" => $i
rule tagged Item = "#" w:word {tag := tag + "/" + text($w)} ":" i:item => $i
rule deep Item = {depth > 1 && depth < 5 && (tag != "top" || last != "")} n:name => Item{Depth: depth, Tag: tag, Size: 100, Name: $n}
rule shout Item = n:name {tag := "shadow"} m:"!"? "!" => Item{Depth: depth, Tag: tag, Size: 0 - len(text($n)), Name: $n, Note: $m}
rule listed Item = "[" (n:name {last := text($n)} ","?)* m:"]" => Item{Depth: depth, Tag: last, Size: len(last), Name: $m}
rule late Item = "~" n:name => Item{Depth: len(last), Tag: tag, Size: 0, Name: $n}
rule plain Item = n:name m:"?"? => Item{Depth: depth, Tag: tag, Size: len(text($n)) * (depth + 1), Name: $n, Note: $m}
rule name = [a-z]+ #atomic
rule word = [a-z]+ #atomic`
