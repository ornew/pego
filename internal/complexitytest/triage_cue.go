package complexitytest

// cueField makes a field a: expr of a CUE file.
func cueField(expr func(n int) string) func(n int) string {
	return func(n int) string { return "a: " + expr(n) + "\n" }
}

// interpRests are the rules of cue that build the elements of an interpolation.
var interpRests = []string{"dq_rest", "sq_rest", "gsl_rest", "gml_rest"}

// triageCue is the triage table of cue.
func triageCue() []TriageGroup {
	return []TriageGroup{
		{Grammar: "cue", ID: "whitespace", Witness: true, Rules: []string{"sp", "nlws", "ws"}, Pattern: `examined`,
			Shape:  "the space and comment scans after a keyword used as a label, an identifier, a comprehension clause or an embedding, read again by the alternative or lookahead that follows",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "keywords as labels before long spaces", Build: list("", "for"+rep(" ", 20)+": 1\nif"+rep(" ", 20)+": 2\nlet"+rep(" ", 20)+": 3\nx%d"+rep(" ", 20)+"?: 4", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
				{Name: "comprehensions with long spaces", Build: cueField(func(n int) string {
					return "[" + list("", "for x in y"+rep(" ", 20)+"if z"+rep(" ", 20)+"{b: x}", ", ", "")(n) + "]"
				}), Sizes: doubling(200, 3), Order: Linear},
				{Name: "embeddings with long spaces", Build: func(n int) string { return list("", "x"+rep(" ", 30)+"\n(y)"+rep(" ", 30)+"\n", "", "")(n) }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "list elements before newlines and commas", Build: cueField(func(n int) string { return "[" + list("", "1"+rep(" ", 10)+"\n"+rep(" ", 10)+",", " ", "")(n) + "2]" }), Sizes: doubling(200, 3), Order: Linear},
				{Name: "aliases after long comments", Build: list("", "f%d"+rep(" ", 20)+"~ // "+rep("c", 30)+"\n x: 1", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "definitions", Witness: true, Rules: []string{"hashrest"}, Pattern: `examined`,
			Shape:  "the name of a definition #name read by the !(sp ident) lookahead of a let keyword and again by the label or embedding that follows",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long definition names", Build: list("", "let L%d"+rep("x", 40)+" = #"+rep("D", 40), "\n", "\n#"+rep("D", 40)+": 1\n"), Sizes: doubling(200, 3), Order: Linear},
				{Name: "one long definition", Build: func(n int) string { return "#" + rep("D", n) + ": 1\nlet\n" }, Sizes: doubling(2000, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "numbers", Witness: true, Rules: []string{"based", "d10", "dec_ip"}, Pattern: `examined`,
			Shape:  "the integer, decimal, based and multiplier alternatives of a number literal, and the number tokens of an attribute, rescan the same digits",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long decimal integers", Build: cueField(func(n int) string { return rep("1", n) }), Sizes: doubling(2000, 4), Order: Linear},
				{Name: "long hexadecimal integers", Build: cueField(func(n int) string { return "0x" + rep("f", n) }), Sizes: doubling(2000, 4), Order: Linear},
				{Name: "long decimals", Build: cueField(func(n int) string { return rep("2", n) + ".5e3" }), Sizes: doubling(2000, 4), Order: Linear},
				{Name: "long integers with multipliers", Build: cueField(func(n int) string { return rep("3", n) + "Ki" }), Sizes: doubling(2000, 4), Order: Linear},
				{Name: "long numbers in attributes", Build: func(n int) string {
					return "a: 1 @x(" + rep("1", n) + ", 0x" + rep("f", n) + ", " + rep("2", n) + "Ki, " + rep("3", n) + ".5)\n"
				}, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "many attribute numbers", Build: func(n int) string { return "a: 1 @x(" + list("", "123_456Mi", ", ", "")(n) + ")\n" }, Sizes: doubling(500, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "string-text", Witness: true, Rules: []string{"sel_string", "dq_plain", "sq_plain", "import_name"}, Pattern: `copying operation text`,
			Shape:  "the text of a string, a string selector or an import name copied by an atomic token rule, which a later alternative reads again",
			Assign: "F103", Order: Linear, Families: []TriageFamily{
				{Name: "long string selectors", Build: cueField(func(n int) string { return "b.\"" + rep("s", n) + "\"" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "chains of string selectors", Build: cueField(func(n int) string { return "b" + rep(".\""+rep("s", 20)+"\"", n) }), Sizes: doubling(200, 3), Order: Linear},
				{Name: "long strings", Build: cueField(func(n int) string { return "\"" + rep("s", n) + "\" + '" + rep("t", n) + "'" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "strings in attributes", Build: func(n int) string { return "a: 1 @x(\"" + rep("s", n) + "\", '" + rep("t", n) + "')\n" }, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "many imports with long names and paths", Build: func(n int) string {
					return "import (\n" + list("", "\tn%[1]d"+rep("m", 30)+" \"p"+rep("q", 30)+"%[1]d\"", "\n", "\n")(n) + ")\n"
				}, Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "file-clauses", Rules: []string{"file"}, Pattern: `^an iteration of the repetition at 8[56]:[0-9]+ in rule file contains`,
			Shape:  "an iteration of the package clause or the import list holds a copy of a name or a string that the next part of the file evaluates again at the same position through memoized rules",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "long package name", Build: func(n int) string { return "package " + rep("p", n) + "\n\na: 1\n" }, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "many import declarations", Build: func(n int) string {
					return list("package p\n\n", "import (\"a/b%[1]d\")\nimport n%[1]d \"c\"", "\n", "\na: 1\n")(n)
				}, Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "raw-strings", Witness: true, Rules: []string{"gtriple", "gs_quote", "gsl_interp", "gsl_interp_s", "ml_line", "ml_frag", "gml_interp", "gml_interp_s"}, Pattern: `examined`,
			Shape:  "the hashes that open a raw string, the quotes inside its body and the lines of a multi-line string, read by an alternative of gstring that fails and again by the next",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "raw strings with many hashes", Build: cueField(func(n int) string {
					h := rep("#", n)
					return h + "\"a\"" + h + " + " + h + "'b'" + h + " + " + h + "\"\\" + h + "(1)\"" + h
				}), Sizes: doubling(1000, 3), Order: Linear},
				{Name: "quotes inside a raw string", Build: cueField(func(n int) string { return "##\"" + rep("\"#'", n) + "\"##" }), Sizes: doubling(1000, 3), Order: Linear},
				{Name: "many lines of a multi-line string", Build: cueField(func(n int) string { return "\"\"\"\n" + rep("\t  line\n", n) + "\t\"\"\"" }), Sizes: doubling(1000, 3), Order: Linear},
				{Name: "multi-line strings with escapes", Build: cueField(func(n int) string { return "#\"\"\"\n" + rep("\tx\\#n \\\"\n", n) + "\t\"\"\"#" }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested interpolations", Build: cueField(nest("\"\\(", "1", ")\"")), Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "hash-count", Witness: true, Rules: []string{"hq"}, Pattern: `the cost can grow exponentially: the binding sv :=`,
			Shape:  "the closing hashes of a raw string counted by the recursion of hq, which lowers sv by 2 after each hash it reads; every call is at a new position",
			Assign: "F101", Order: Linear, Families: []TriageFamily{
				{Name: "raw string with n hashes", Build: cueField(func(n int) string { h := rep("#", n); return h + "\"a\"" + h }), Sizes: doubling(1000, 3), Order: Linear},
				{Name: "multi-line raw string with n hashes", Build: cueField(func(n int) string { h := rep("#", n); return h + "'''\n\ta\n\t'''" + h }), Sizes: doubling(1000, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "multi-line-indentation", Rules: []string{"gml_plain"}, Pattern: `copies, for each element`,
			Shape:  "the fold of gml_plain compares the text of the closing indentation with the indentation of each line, a comparison that stops at the shorter string",
			Assign: "F103", Order: Linear, Families: []TriageFamily{
				{Name: "lines indented like a long closing indentation", Build: cueField(func(n int) string {
					ind := rep(" ", 40)
					return "\"\"\"\n" + rep(ind+"x\n", n) + ind + "\"\"\""
				}), Sizes: doubling(200, 3), Order: Linear},
				{Name: "one long closing indentation", Build: cueField(func(n int) string {
					ind := rep(" ", n)
					return "\"\"\"\n" + rep(ind+"x\n\n", 8) + ind + "\"\"\""
				}), Sizes: doubling(1000, 3), Order: Linear},
			}},
		{Grammar: "cue", ID: "interpolation-lists", Rules: interpRests, Pattern: nestedPattern,
			Shape:  "the elements of an interpolation built by a right recursion (dq_rest, sq_rest, gsl_rest, gml_rest) whose action concat(list($e, $m), $t) copies the list of the rest at every level",
			Assign: "M28", Order: Poly(2), Families: []TriageFamily{
				{Name: "many interpolations", Build: cueField(func(n int) string {
					return "\"" + list("", "\\(%d) ", "", "")(n) + "\" + '" + list("", "\\(%d) ", "", "")(n) + "'"
				}), Sizes: doubling(300, 3), Order: Poly(2)},
				{Name: "many raw interpolations", Build: cueField(func(n int) string { return "#\"" + list("", "\\#(%d) ", "", "")(n) + "\"#" }), Sizes: doubling(300, 3), Order: Poly(2)},
				{Name: "many multi-line interpolations", Build: cueField(func(n int) string {
					return "#\"\"\"\n" + list("", "\tx\\#(%d) "+rep("y", 20), "\n", "\n")(n) + "\t\"\"\"#"
				}), Sizes: doubling(300, 3), Order: Poly(2)},
			}},
		{Grammar: "cue", ID: "nested", Except: interpRests, Pattern: nestedPattern,
			Shape:  "units of declarations, expressions, labels and comprehensions nested through the recursion of structs, lists, expressions and interpolations, every call on it memoized under the value of sv",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested parentheses", Build: cueField(nest("(", "1", ")")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested structs", Build: cueField(nest("{b: ", "1", "}")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested lists", Build: cueField(nest("[1, ", "2", "]")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested calls and indexes", Build: cueField(nest("f(b[", "1", "])")), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested comprehensions", Build: cueField(nest("[for x in ", "y", " {x}]")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested field chains", Build: func(n int) string { return rep("b: ", n) + "1\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested unary and disjunctions", Build: cueField(func(n int) string { return rep("*!-", n) + "x | " + rep("(y | ", n) + "z" + rep(")", n) }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested raw interpolations", Build: cueField(nest("#\"\\#(", "1", ")\"#")), Sizes: doubling(300, 3), Order: Linear},
			}},
	}
}
