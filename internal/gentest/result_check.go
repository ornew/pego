package gentest

// ResultCheckGrammar declares the rules rA to rE with the type rt and passes them the dynamically
// typed values of fieldCheckValues, which each rule casts to typ: input "<value><route>" reaches
// rA (route "."), whose action forwards the value; rB (","), which does so with a second capture;
// the Operators Rule operand lines rC (":") and rD ("+"); rE, which forwards a scalar value (input
// "<i|s|b>="); and, through a predicate that reads the value, rA and rB ("!" and "~"). S gives the
// type typ a field, for the expected messages; W wraps the result in a field of type rt, so that
// generated parsers use the typed runtime when rt is representable.
func ResultCheckGrammar(typ, rt string) string {
	return `type Int = { Text string }
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type W = { V ` + rt + ` }
rule main W = ma / mb / mc / md / me / mp / mq
rule ma W = &(. ".") v:rA "." => W{V: $v}
rule mb W = &(. ",") v:rB "," => W{V: $v}
rule mc W = &(. ":") v:rC ":" => W{V: $v}
rule md W = &(. "+") v:rD "+" => W{V: $v}
rule me W = &(. "=") v:rE => W{V: $v}
rule mp W = &(. "!") v:rA {$v == nil || $v != nil} "!" => W{V: $v}
rule mq W = &(. "~") v:rB {$v == nil || $v != nil} "~" => W{V: $v}
rule rA ` + rt + ` = v:val => $v.(` + typ + `)
rule rB ` + rt + ` = v:val => $v.(` + typ + `)
rule rC ` + rt + ` = #operators {
    operand v:val => $v.(` + typ + `)
}
rule rD ` + rt + ` = #operators {
    operand v:val => $v.(` + typ + `)
}
rule rE ` + rt + ` = v:sc "=" => $v.(` + typ + `)
rule sc _ = si / ss / sb
rule si = "i" => 1
rule ss = "s" => "s"
rule sb = "b" => true
rule val _ = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz
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
`
}

func ResultCheckInputs() []string {
	var in []string
	for _, v := range "mtlkenq?wpz" {
		for _, r := range ".,:+!~" {
			in = append(in, string(v)+string(r))
		}
	}
	return append(in, "i=", "s=", "b=")
}

// ResultCheckTypes are the types of fieldCheckFields that a rule can declare.
func ResultCheckTypes() []string { return ResultCheckTypesOf(FieldCheckFields) }

func ResultCheckTypesOf(fields map[string]string) []string {
	var types []string
	for _, typ := range FieldCheckTypesOf(fields) {
		switch typ {
		case "int", "string", "bool", "Int | string", "*(Box | bool)":
		default:
			types = append(types, typ)
		}
	}
	return types
}

// OperatorResultCheckGrammar has operator lines with actions whose values are dynamically typed:
// input "o<value>^" applies the postfix operator, "!<value>o" the prefix one and "o+<value>o" the
// infix one, each forwarding the value it captures. Input "<<value>#" reaches the left-recursive
// rule lr, which generated typed parsers call like Operators Rules, through their rule table, and
// "@<value>@" the rule rr, whose recovery expression keeps it from a direct typed body. Input
// "%<value>%" constructs W in a rule with a recovery expression, which compiling copies.
const OperatorResultCheckGrammar = `type Int = { Text string }
type W = { V []Int }
rule main W = ml / mr / mf / me
rule mf W = "%" v:val ("%" #recover(skip="%")) => W{V: $v.([]Int)}
rule mr W = "@" v:rr => W{V: $v}
rule rr []Int = v:val ("@" #recover(skip="@")) => $v.([]Int)
rule ml W = "<" v:lr => W{V: $v}
rule me W = v:e => W{V: $v}
rule lr []Int = (v:lr "^" / v:val "#") => $v.([]Int)
rule e []Int = #operators {
    operand "o" => []Int{Int{Text: "o"}}
    level { infix left "+" v:val => $v.([]Int) }
    level { prefix "!" v:val => $v.([]Int) }
    level { postfix v:val "^" => $v.([]Int) }
}
rule val _ = vm / vt / vl / vz
rule vm = "m"
rule vt = "t" => Int{Text: "t"}
rule vl = "l" => []Int{Int{Text: "l"}}
rule vz = "z" => nil
`

var OperatorResultCheckInputs = []string{"om^", "ot^", "ol^", "oz^", "!mo", "!lo", "!zo", "o+mo", "o+lo", "o+to", "ol^+lo", "<l#", "<m#", "<l#^", "<t#^", "<z#", "@l@", "@m@", "@t@", "%l%", "%m%", "%z%"}

// TextStructOperatorsRuleGrammar is the text struct counterpart of terminalOperatorsRuleGrammar:
// the Operators Rule produces the text struct through its actions.
const TextStructOperatorsRuleGrammar = `type Int = { Text string }
type S = { F Int }
rule main = v:e => S{F: $v}
rule e Int = #operators {
    operand v:val => $v
    level { infix left "+" v:val => $v }
}
rule val Int = t:"m" #atomic => Int{Text: text($t)}
`

var TerminalOperatorsRuleInputs = []string{"m", "m+m", "m+", ""}

func ResultCheckCases() []Case {
	var cases []Case
	for _, typ := range ResultCheckTypes() {
		cases = append(cases, Case{"result check " + typ, ResultCheckGrammar(typ, typ), ResultCheckInputs()})
	}
	return append(cases,
		Case{"operator result check", OperatorResultCheckGrammar, OperatorResultCheckInputs},
		Case{"text struct Operators Rule", TextStructOperatorsRuleGrammar, TerminalOperatorsRuleInputs})
}
