package gentest

const TypedFilterIdentityGrammar = `type Word = { Text string }
type Alias ~ Word
type Result = { First *Alias; Items []Alias; Optional []*Alias; Empty []Alias; NilInput []Alias }
rule main Result = w:word $$ => Result{
    First: $w,
    Items: filter(list($w, $w), (item) => true),
    Optional: filter(list(nil, $w, nil, $w), (item) => true),
    Empty: filter(list($w), (item) => false),
    NilInput: filter(nil, (item) => true)
}
rule empty Result = $$ => Result{
    First: nil,
    Items: filter(nil, (item) => true),
    Optional: filter(list(nil, nil), (item) => true),
    Empty: filter(nil, (item) => true),
    NilInput: filter(nil, (item) => true)
}
rule word Word = t:([a-zé]+) #atomic => Word{Text: text($t)}`
