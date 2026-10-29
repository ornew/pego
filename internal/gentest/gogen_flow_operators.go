package gentest

// OperatorsLineRecoveryCases are Operators Rules with a line that recovers, which the Flow route prints as it
// prints the other lines; the parsers must match the engine (they are in the generated corpus). rule is the
// Operators Rule with the line.
var OperatorsLineRecoveryCases = []struct {
	Case
	Rule string
}{
	{Case{"Operators Rule operand with recovery", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", ""}}, "e"},
	{Case{"Operators Rule operand with a cut in the recovered body", `rule main = v:e $$ => $v
rule e = #operators {
    operand ("i" #cut ";") #recover(skip="?")
    level { infix left "+" }
}`, []string{"i;", "i;+i;", "i?+i;", "?", "i;+", "i", "ix", ""}}, "e"},
}
