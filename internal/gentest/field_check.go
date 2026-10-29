package gentest

import "sort"

// FieldCheckValues are the dynamically typed values that fieldCheckGrammar passes to a struct
// field, by the first input character: nodes, nil and scalars from the rule val of type _.
var FieldCheckValues = map[string]string{
	"m": "Match", "t": "Int", "l": "[]Int", "k": "[]Match", "e": "[]nil", "n": "empty List",
	"q": "Box", "?": "Error", "w": "[][]Int", "p": "[]*Int", "z": "nil", "i": "int", "s": "string", "b": "bool",
}

// FieldCheckFields maps the declared types of fieldCheckGrammar's field F to the values they
// accept. A match is a string node and a value of the text struct Int is a node of its own type,
// lists built by actions are list values, which are not nodes, and an error value fits only a type
// that admits error.
var FieldCheckFields = map[string]string{
	"Alias":                    "t",
	"*Int":                     "tz",
	"[]Int":                    "ln",
	"[]*Int":                   "lnep",
	"Int | string":             "ts",
	"int":                      "i",
	"string":                   "s",
	"bool":                     "b",
	"_":                        "mtlkenq?wpzisb",
	"node[_]":                  "m",
	"[][]Int":                  "wn",
	"*(Box | bool)":            "qzb",
	"node[string]":             "m",
	"*[]*Int":                  "lnepz",
	"Int | error":              "t?",
	"[](node[string] | error)": "kn",
}

// FieldCheckGrammar gives S a field F of type typ, set from the dynamically typed value of val, which
// the grammar must cast to typ. Constructing S succeeds with an accepted value (input
// "<value>.") and is an action error of the cast otherwise; in a predicate (input "<value>!")
// the rejected value makes the predicate fail, so the rule no matches instead.
func FieldCheckGrammar(typ string) string {
	return `type Int = { Text string }
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type Result ~ S | Int
rule main Result = nodeCase / nodePred / no
rule nodeCase = v:val "." => S{F: $v.(` + typ + `)}
rule nodePred = v:val {S{F: $v.(` + typ + `)} != nil} "!" => Int{Text: "yes"}
rule no = [a-z!.?;]+ => Int{Text: "no"}
rule val _ = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz / vi / vs / vb
rule vm = "m"
rule vt = "t" => Int{Text: "t"}
rule vl = "l" => []Int{Int{Text: "a"}}
rule vk = k:"k" => list($k)
rule ve = "e" => list(nil)
rule vn = "n" => list()
rule vq = "q" => Box{Value: Int{Text: "q"}}
rule vr = ("r" ";") #recover(skip="?")
rule vw = "w" => [][]Int{[]Int{Int{Text: "w"}}}
rule vp = "p" => list(Int{Text: "a"}, nil)
rule vz = "z" => nil
rule vi = "i" => 1
rule vs = "s" => "s"
rule vb = "b" => true
`
}

func FieldCheckTypes() []string { return FieldCheckTypesOf(FieldCheckFields) }

func FieldCheckTypesOf(fields map[string]string) []string {
	var types []string
	for typ := range fields {
		types = append(types, typ)
	}
	sort.Strings(types)
	return types
}

func FieldCheckInputs() []string {
	var in []string
	for v := range FieldCheckValues {
		in = append(in, v+".")
	}
	sort.Strings(in)
	return append(in, "l!", "k!", "z!", "i!", "s!", "?!")
}

func FieldCheckCases() []Case {
	var cases []Case
	for _, typ := range FieldCheckTypes() {
		cases = append(cases, Case{"field check " + typ, FieldCheckGrammar(typ), FieldCheckInputs()})
	}
	return cases
}
