package complexitytest

// goFile wraps statements in a function of a Go source file.
func goFile(body func(n int) string) func(n int) string {
	return func(n int) string { return "package p\n\nfunc f() {\n" + body(n) + "\n}\n" }
}

// triageGolang is the triage table of golang.
func triageGolang() []TriageGroup {
	return []TriageGroup{
		{Grammar: "golang", ID: "comments", Witness: true, Rules: []string{"sp", "ws"}, Pattern: `examined`,
			Shape:  "the hand-expanded comment and line-directive scans of sp and ws, rules without memoization that call no rule, rescanned by later alternatives and lookaheads of the same comment",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long block comments between tokens", Build: goFile(list("", "x := 1 /* "+rep("c", 60)+" */ + 2", "\n", "")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "one long block comment", Build: goFile(func(n int) string { return "x := 1 /*" + rep("a*", n) + "*/ + 2" }), Sizes: doubling(4000, 3), Order: Linear},
				{Name: "block line directive with many colons", Build: goFile(func(n int) string { return "x := 1 /*line " + rep("a:", n) + "1:1*/ + 2" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "line directive with many colons", Build: func(n int) string { return "package p\n//line " + rep("a:", n) + "1:1\nvar x = 1\n" }, Sizes: doubling(2000, 3), Order: Linear},
				{Name: "block line directive with long numbers", Build: goFile(func(n int) string { return "x := 1 /*line a:" + rep("0", n) + "1:" + rep("0", n) + "1*/ + 2" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "many line comments", Build: goFile(list("", "x := 1 // "+rep("c", 40), "\n", "")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "long space runs", Build: goFile(list("", "x"+rep(" ", 30)+":="+rep(" ", 30)+"f"+rep(" ", 30)+"(1)", "\n", "")), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "golang", ID: "line-directives", Witness: true, Rules: []string{"dirSegBlock", "dirNum", "dirUint"}, Pattern: `examined`,
			Shape:  "the line-directive rules of a block comment that ends a statement (semi), whose alternatives rescan its segments and numbers",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "statements ended by block directives", Build: goFile(list("", "x = 1 /*line a\nb:c:1:1*/", "", "")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "a block directive with many colons ending a statement", Build: goFile(func(n int) string { return "x := 1 /*line " + rep("a:", n) + "0001:00001*/\ny := 2" }), Sizes: doubling(2000, 3), Order: Linear},
			}},
		{Grammar: "golang", ID: "identifiers", Witness: true, Rules: []string{"idRest"}, Pattern: `examined`,
			Shape:  "the !ident lookahead of unnamedType rescans an identifier that the type names read again",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long type names", Build: func(n int) string { return "package p\n\nvar x " + rep("T", n) + "\n" }, Sizes: doubling(4000, 3), Order: Linear},
				{Name: "many typed parameters", Build: list("package p\n\nfunc f(", "a []T, b map[K]V, c *T", ", ", ") {}\n"), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "golang", ID: "numbers", Witness: true, Rules: []string{"decDigits", "hexDigits"}, Pattern: `examined`,
			Shape:  "the float, imaginary and integer literal alternatives rescan the same digits",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long numbers", Build: goFile(func(n int) string {
					return "x := " + rep("1", n) + "i + 0x" + rep("f", n) + "i + " + rep("2", n) + " + 0x1p" + rep("3", n)
				}), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "many imaginary literals", Build: goFile(list("x := ", "123_456i", " + ", "")), Sizes: doubling(500, 3), Order: Linear},
			}},
		{Grammar: "golang", ID: "nested", Pattern: nestedPattern,
			Shape:  "units of statements, expressions, types and literals nested through the recursion of expressions, types and blocks, every call on it memoized",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested parentheses", Build: goFile(func(n int) string { return "x := " + nest("(", "1", ")")(n) }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested calls", Build: goFile(func(n int) string { return "x := " + nest("f(a, ", "1", ")")(n) }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested composite literals", Build: goFile(func(n int) string { return "x := " + nest("T{a: ", "1", "}")(n) }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested index expressions", Build: goFile(func(n int) string { return "x := " + nest("a[", "1", "]")(n) }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested function literals", Build: goFile(func(n int) string { return "x := " + nest("func() int { return ", "1", " }()")(n) }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested blocks", Build: goFile(nest("{\n", "x++", "\n}")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested if statements", Build: goFile(nest("if a {\n", "x++", "\n}")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested slice and map types", Build: func(n int) string { return "package p\n\nvar x " + rep("[]map[string]", n) + "int\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested generic types", Build: func(n int) string { return "package p\n\nvar x " + nest("T[", "int", "]")(n) + "\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested struct types", Build: func(n int) string { return "package p\n\nvar x " + nest("struct { a ", "int", " }")(n) + "\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "nested conversions and receives", Build: goFile(func(n int) string { return "x := " + nest("(<-chan int)(", "c", ")")(n) }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested unary operators", Build: goFile(func(n int) string { return "x := " + rep("-^!&*", n) + "a" }), Sizes: doubling(500, 3), Order: Linear},
			}},
	}
}
