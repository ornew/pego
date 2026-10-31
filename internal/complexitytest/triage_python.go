package complexitytest

import "strings"

// pyDeep is the deepest bracket nesting that the python grammar accepts (lv % 512 < 400 with
// two per level); bracket families stay below it.
const pyDeep = 192

// pyBlocks returns n nested if blocks, each indented one more space, with body at the bottom.
// The python grammar accepts at most 98 levels.
func pyBlocks(n int, body string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(rep(" ", i) + "if a:\n")
	}
	b.WriteString(rep(" ", n) + body + "\n")
	return b.String()
}

// triagePython is the triage table of python.
func triagePython() []TriageGroup {
	return []TriageGroup{
		{Grammar: "python", ID: "whitespace", Witness: true, Rules: []string{"ws", "blank", "indentation"}, Pattern: `examined`,
			Shape:  "whitespace, blank-line and indentation scans that a failed alternative, option or lookahead gives back to a continuation that starts with the same whitespace",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long whitespace inside brackets", Build: func(n int) string { return "x = (" + list("", "1"+rep(" ", 20)+","+rep(" ", 20), "", "")(n) + "2)\n" }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "continuation lines", Build: list("x = 1", " + \\\n    2", "", "\n"), Sizes: doubling(500, 3), Order: Linear},
				{Name: "blank lines with spaces", Build: list("", "x = 1\n"+rep(rep(" ", 10)+"\n", 3), "", ""), Sizes: doubling(300, 3), Order: Linear},
				{Name: "match statements with nested patterns", Build: list("match x:\n", "    case ( a , [ b , c ] , { 'k' : d } ) :\n        pass\n", "", ""), Sizes: doubling(200, 3), Order: Linear},
				{Name: "try statements with many handlers and blanks", Build: list("try:\n    pass\n", "except E:\n    pass\n   \n", "", ""), Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "tokens", Witness: true, Rules: []string{"ident", "digitpart", "str_body", "raw_bytes_body", "fesc", "lparam"}, Pattern: `examined`,
			Shape:  "identifier, number, string and escape scans inside token rules, and the copy of a lambda parameter name, that a later alternative or iteration reads again",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long attribute chains", Build: func(n int) string { return "x = " + list("a", ".b", "", "")(n) + "\n" }, Sizes: doubling(1000, 3), Order: Linear},
				{Name: "long numbers", Build: func(n int) string {
					return "x = " + rep("1", n) + "j + " + rep("2", n) + ".5e3 + " + rep("3", n) + "\n"
				}, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "long strings", Build: func(n int) string {
					return "x = '" + rep("a", n) + "' '''" + rep("b'c", n) + "'''\ny = rb'" + rep("c", n) + "'\n"
				}, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "f-string with named escapes", Build: func(n int) string { return "x = f'" + rep("\\N{DASH}", n) + "'\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "lambda with many parameters", Build: func(n int) string { return "f = lambda " + list("", "a%d", ", ", "")(n) + ": 0\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "global names and calls", Build: list("", "global "+rep("abc, ", 10)+"d\nf"+rep("g", 30)+"(x for x in y)\n", "", ""), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "bounded-variables", Witness: true, Pattern: `the cost can grow exponentially: the binding (lv|ind|isdict) :=`,
			Shape:  "bindings of lv (bracket and f-string state), ind (indentation) and isdict that read their earlier value; every value lies in a bounded set (lv % 512 < 400, at most 98 indentation levels, a boolean), so the memo key takes few values at a position",
			Assign: "F101", Order: Linear, Families: []TriageFamily{
				{Name: "nested brackets", Build: func(n int) string { return "x = " + nest("([{", "1", "}])")(n) + "\n" }, Sizes: []int{16, 32, 64}, Order: Linear},
				{Name: "nested calls and subscripts", Build: func(n int) string { return "x = " + nest("f(a[", "1", "])")(n) + "\n" }, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "nested f-strings", Build: func(n int) string { return "x = " + nest("f'{", "1", "}'")(n) + "\n" }, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "nested blocks", Build: func(n int) string { return pyBlocks(n, "pass") }, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "nested patterns", Build: func(n int) string {
					return "match x:\n    case " + nest("[(a, b, c, d, ", "a", ")]")(n) + ":\n        pass\n"
				}, Sizes: []int{24, 48, 96}, Order: Linear},
			}},
		{Grammar: "python", ID: "fold-copies", Witness: true, Pattern: `copies the fold accumulator acc$`,
			Shape:  "a fold that copies its accumulator in each step: a dotted import name built by acc + \".\" + text, the starred arguments of a call and the parts of an implicitly joined string built by concat(acc, ...)",
			Assign: "O84", Order: Poly(2), Families: []TriageFamily{
				{Name: "dotted import name", Build: func(n int) string { return "import " + list("a", ".b", "", "")(n) + "\n" }, Sizes: doubling(1000, 3), Order: Poly(2)},
				{Name: "relative from import", Build: func(n int) string { return "from " + list("a", ".b", "", "")(n) + " import c\n" }, Sizes: doubling(1000, 3), Order: Poly(2)},
				{Name: "alternating keyword and starred arguments", Build: func(n int) string { return "f(" + list("", "a%d=1, *b", ", ", "")(n) + ")\n" }, Sizes: doubling(500, 3), Order: Poly(2)},
				{Name: "implicitly joined f-strings", Build: func(n int) string { return "x = " + list("", "f'a{b}' 'c'", " ", "")(n) + "\n" }, Sizes: doubling(500, 3), Order: Poly(2)},
			}},
		{Grammar: "python", ID: "field-text", Witness: true, Rules: []string{"ffield", "tfield"}, Pattern: `copies \$s, which holds a call on a recursion`,
			Shape:  "the text of a replacement field, which holds the nested f-strings of the field, taken at every level; the runtime takes the text of a capture without copying it",
			Assign: "F103", Order: Linear, Families: []TriageFamily{
				{Name: "nested f-strings", Build: func(n int) string { return "x = " + nest("f'{", "1", "}'")(n) + "\n" }, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "many fields", Build: func(n int) string { return "x = f'" + rep("{a!r:>10}", n) + "'\n" }, Sizes: doubling(500, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "except-star-handlers", Rules: []string{"try_stmt"}, Pattern: `copies, for each element`,
			Shape:  "the action of try_stmt maps the except* handlers inside a fold over the same handlers",
			Assign: "M27", Order: Poly(2), Families: []TriageFamily{
				{Name: "many except* handlers", Build: list("try:\n    pass\n", "except* E:\n    pass\n", "", ""), Sizes: doubling(300, 3), Order: Poly(2)},
				{Name: "many except handlers", Build: list("try:\n    pass\n", "except E:\n    pass\n", "", ""), Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "bounded-lambda-copies", Rules: []string{"params", "lambda_params", "brace_items"}, Pattern: `copies, for each element`,
			Shape:  "a fold over a list of at most one element (the / marker of a parameter list, the #repeat(,1) value of a dictionary display) copies a list from outside its lambda",
			Assign: "F102", Order: Linear, Families: []TriageFamily{
				{Name: "parameters with a slash", Build: func(n int) string { return "def f(" + list("", "a%d", ", ", "")(n) + ", /, b=1):\n    pass\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "lambda parameters with a slash", Build: func(n int) string { return "f = lambda " + list("", "a%d", ", ", "")(n) + ", /: 0\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "large dictionary", Build: func(n int) string { return "x = {" + list("", "%d: 1", ", ", "")(n) + "}\n" }, Sizes: doubling(500, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "module-whitespace", Rules: []string{"main", "first_stmts", "module_end"}, Pattern: `contains the repetition`,
			Shape:  "the blank lines and whitespace at the start and end of a module against the same whitespace after them",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "leading and trailing blank lines", Build: func(n int) string { return rep("   \n", n) + "x = 1\n" + rep("   \n", n) + rep(" ", n) }, Sizes: doubling(1000, 3), Order: Linear},
			}},
		{Grammar: "python", ID: "nested", Pattern: nestedPattern,
			Shape:  "units of statements, expressions, targets and patterns nested through the recursion of expressions, blocks and patterns",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested parentheses", Build: func(n int) string { return "x = " + nest("(", "1", ")")(n) + "\n" }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested lists and tuples", Build: func(n int) string { return "x = " + nest("[1, (", "2", ",)]")(n) + "\n" }, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "nested lambdas", Build: func(n int) string { return "f = " + rep("lambda: ", n) + "0\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested conditional expressions", Build: func(n int) string { return "x = " + rep("a if b else ", n) + "c\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested not", Build: func(n int) string { return "x = " + rep("not ", n) + rep("-", n) + "a\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested comprehensions", Build: func(n int) string { return "x = " + nest("[", "y", " for y in z]")(n) + "\n" }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested starred targets", Build: func(n int) string { return nest("[", "a", ", *b]")(n) + " = c\n" }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested subscripts and slices", Build: func(n int) string { return "x = " + nest("a[1:", "2", "]")(n) + "\n" }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested class patterns", Build: func(n int) string { return "match x:\n    case " + nest("C(", "a", ", k=1)")(n) + ":\n        pass\n" }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested function definitions", Build: func(n int) string { return pyDefs(n) }, Sizes: []int{24, 48, 96}, Order: Linear},
			}},
	}
}

// pyDefs returns n nested function definitions, each indented one more space.
func pyDefs(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(rep(" ", i) + "def f(a, b=1, *c, d, **e):\n")
	}
	b.WriteString(rep(" ", n) + "pass\n")
	return b.String()
}
