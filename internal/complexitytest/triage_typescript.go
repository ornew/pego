package complexitytest

// tsStmt wraps an expression in a statement.
func tsStmt(expr func(n int) string) func(n int) string {
	return func(n int) string { return "x = " + expr(n) + ";\n" }
}

// tsx starts a TSX source: the typescript grammar selects JSX with a leading U+0000.
func tsx(src func(n int) string) func(n int) string {
	return func(n int) string { return "\x00" + src(n) }
}

// tsNestings are the nesting families of the typescript grammar: the recursion of expressions,
// types, functions, classes and JSX, which the nested reasons and the ctx bindings follow.
func tsNestings() []TriageFamily {
	return []TriageFamily{
		{Name: "nested parentheses", Build: tsStmt(nest("(", "a", ")")), Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested arrow functions", Build: tsStmt(func(n int) string { return rep("(a) => ", n) + "a" }), Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested arrow defaults", Build: tsStmt(nest("(a = ", "b", ") => a")), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested comma lists", Build: tsStmt(nest("(a, ", "b", ")")), Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested calls with type arguments", Build: tsStmt(nest("f<T>(", "a", ")")), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested comparisons in parentheses", Build: tsStmt(nest("a < (b", "", ")")), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested object literals", Build: tsStmt(nest("{a: ", "1", "}")), Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested arrays and spreads", Build: tsStmt(nest("[...a, ", "1", "]")), Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested function expressions", Build: tsStmt(nest("function* () { yield ", "1", "; }")), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested async arrows with await", Build: tsStmt(nest("async (a) => (await a, ", "a", ")")), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested generic types", Build: func(n int) string { return "let x: " + nest("A<", "B", ">")(n) + ";\n" }, Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested conditional types", Build: func(n int) string { return "let x: " + rep("A extends B ? ", n) + "C" + rep(" : D", n) + ";\n" }, Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested function types", Build: func(n int) string { return "let x: " + rep("(a: A) => ", n) + "B;\n" }, Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested object types", Build: func(n int) string { return "let x: " + nest("{ a: ", "B", " }")(n) + ";\n" }, Sizes: doubling(500, 3), Order: Linear},
		{Name: "nested classes", Build: tsStmt(nest("class extends B { m() { return ", "1", "; } }")), Sizes: doubling(200, 3), Order: Linear},
		{Name: "nested blocks and for loops", Build: nest("for (let i = 0; i < n; i++) { ", "f();", " }\n"), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested destructuring parameters", Build: tsStmt(func(n int) string { return "(" + nest("{a: [", "b", "]}")(n) + ") => 0" }), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested JSX elements", Build: tsx(func(n int) string { return "x = " + nest("<a b={1}>", "t", "</a>")(n) + ";\n" }), Sizes: doubling(300, 3), Order: Linear},
		{Name: "nested JSX in arrows", Build: tsx(func(n int) string { return "x = " + nest("<a f={() => ", "1", "} />")(n) + ";\n" }), Sizes: doubling(300, 3), Order: Linear},
	}
}

// triageTypescript is the triage table of typescript.
func triageTypescript() []TriageGroup {
	return []TriageGroup{
		{Grammar: "typescript", ID: "identifiers", Witness: true, Rules: []string{"identifierPartEscape", "identifierStartEscape", "unicodeBraced", "identifierRest"}, Pattern: `examined`,
			Shape:  "the leading zeros of a braced escape \\u{0...} and the characters of an identifier, read by the !idPart, !idStart and start lookaheads after keywords, literals and list elements and again by the identifier that follows",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "an escape with n leading zeros", Build: tsStmt(func(n int) string { return "\\u{" + rep("0", n) + "61}" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "identifiers of many escapes", Build: tsStmt(func(n int) string { return "a" + rep("\\u{00062}", n) }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "object literal with escaped keys", Build: tsStmt(func(n int) string { return "{" + list("", "\\u{0000061}%d: 1", ", ", "")(n) + "}" }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "import and export specifiers with escapes", Build: func(n int) string {
					return "import {" + list("", "\\u{00061}%[1]d as b%[1]d", ", ", "")(n) + "} from 'm';\nexport {" + list("", "\\u{00062}%[1]d as type", ", ", "")(n) + "};\n"
				}, Sizes: doubling(200, 3), Order: Linear},
				{Name: "class members with escaped names", Build: func(n int) string { return "class C {\n" + list("", "  \\u{000063}%d = 1;", "\n", "")(n) + "\n}\n" }, Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "typescript", ID: "whitespace-and-templates", Witness: true, Rules: []string{"ws", "sp", "rawTemplateCharacters"}, Pattern: `examined`,
			Shape:  "whitespace and comment scans before an export modifier, and the characters of a tagged template, read again by a later alternative",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long comments before modifiers", Build: list("", "export /* "+rep("c", 40)+" */ declare /* */ const x%d: number;", "\n", ""), Sizes: doubling(200, 3), Order: Linear},
				{Name: "long tagged templates", Build: tsStmt(func(n int) string { return "tag`" + rep("a$", n) + "${b}`" }), Sizes: doubling(2000, 3), Order: Linear},
				{Name: "nested tagged templates", Build: tsStmt(nest("tag`a${", "b", "}c`")), Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "typescript", ID: "ctx-bindings", Witness: true, Pattern: `the cost can grow exponentially: the binding ctx :=`,
			Shape:  "bindings of the context flags ctx that read its earlier value; every binding keeps ctx in [0, 128) (bits of yield, await, in, decorator, conditional type, JSX), so the memo key of a rule takes at most 128 values at a position, and the values that occur are fixed by the enclosing constructs",
			Assign: "F101", Order: Linear, Families: tsNestings()},
		{Grammar: "typescript", ID: "nested", Pattern: nestedPattern,
			Shape:  "units of statements, expressions, types and JSX nested through their recursion, every call on it memoized under the ctx value of its context",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested parentheses and arrows", Build: tsStmt(func(n int) string { return rep("((a) => ", n) + "a" + rep(")", n) }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested ternaries", Build: tsStmt(func(n int) string { return rep("a ? (b) : ", n) + "c" }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested type assertions and satisfies", Build: tsStmt(nest("(<T>(", "a", " as U) satisfies V)")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested indexed and optional chains", Build: tsStmt(nest("a?.[", "b", "]!")), Sizes: doubling(300, 3), Order: Linear},
			}},
	}
}
