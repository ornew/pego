package complexitytest

// sel wraps an expression in a SELECT statement.
func sel(expr func(n int) string) func(n int) string {
	return func(n int) string { return "SELECT " + expr(n) + ";\n" }
}

// triageDuckdb is the triage table of duckdb.
func triageDuckdb() []TriageGroup {
	return []TriageGroup{
		{Grammar: "duckdb", ID: "operator-tails", Witness: true, Rules: []string{"pm_end", "op_special", "core"}, Pattern: `examined`,
			Shape:  "the operator lookahead &pm_end, and the op_special and core scans, read the run of + and - that follows an operator character; a prefix operator at each character of a run reads the rest of the run again",
			Assign: "O75", Order: Poly(2), Families: []TriageFamily{
				{Name: "a run of prefix plus signs", Build: sel(func(n int) string { return rep("+", n) + "1" }), Sizes: doubling(200, 3), Order: Poly(2)},
				{Name: "a run of alternating signs", Build: sel(func(n int) string { return rep("+-", n) + "1" }), Sizes: doubling(100, 3), Order: Poly(2)},
				{Name: "prefix signs separated by spaces", Build: sel(func(n int) string { return rep("+ ", n) + "1" }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "one long generic operator", Build: sel(func(n int) string { return "1 *" + rep("+*", n) + " 2" }), Sizes: doubling(500, 3), Order: Linear},
				{Name: "many comparisons", Build: sel(list("", "a <= b", " AND ", "")), Sizes: doubling(500, 3), Order: Linear},
			}},
		{Grammar: "duckdb", ID: "strings", Witness: true, Rules: []string{"sq_string", "e_body", "dq_close", "bit_string"}, Pattern: `examined`,
			Shape:  "a string continuation (strings separated by a newline) or a dollar-quote end that fails after the body was scanned, and the alternative or repetition that reads it again",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "continued standard strings", Build: sel(list("", "'"+rep("a", 20)+"'", "\n", "")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "continued escape strings", Build: sel(list("E", "'"+rep("\\n", 10)+"'", "\n", "")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "continued bit strings", Build: sel(list("B", "'0101'", "\n", "")), Sizes: doubling(500, 3), Order: Linear},
				{Name: "dollar string with inner tags", Build: sel(func(n int) string { return "$t$" + rep("$aaaa", n) + "$t$" }), Sizes: doubling(1000, 3), Order: Linear},
				{Name: "time zone strings", Build: list("", "SET TIME ZONE '"+rep("z", 20)+"'\n'"+rep("y", 20)+"'", ";\n", ";\n"), Sizes: doubling(300, 3), Order: Linear},
			}},
		{Grammar: "duckdb", ID: "nested", Pattern: nestedPattern,
			Shape:  "units of statements, expressions, types and table references nested through the recursion of expressions, subqueries, joins and types, every call on it memoized",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested parentheses", Build: sel(nest("(", "1", ")")), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested scalar subqueries", Build: sel(nest("(SELECT ", "1", ")")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested function calls", Build: sel(nest("f(a, ", "1", ")")), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested lists", Build: sel(nest("[1, ", "2", "]")), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested structs", Build: sel(nest("{'a': ", "1", "}")), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested CASE", Build: sel(nest("CASE WHEN a THEN ", "1", " END")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested BETWEEN", Build: sel(nest("a BETWEEN (", "b", ") AND c")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested NOT and minus", Build: sel(func(n int) string { return rep("NOT - ", n) + "a" }), Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested derived tables", Build: func(n int) string { return "SELECT * FROM " + nest("(SELECT * FROM ", "t", ") AS s")(n) + ";\n" }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested joins", Build: func(n int) string { return "SELECT * FROM " + nest("a JOIN (", "b JOIN c ON y", ") ON x")(n) + ";\n" }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested struct types", Build: func(n int) string { return "CREATE TABLE t (c " + nest("STRUCT(a ", "INT", ")")(n) + ");\n" }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested array types", Build: func(n int) string { return "CREATE TABLE t (c INT" + rep("[]", n) + ");\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested lambdas", Build: sel(func(n int) string {
					return "list_transform(l, " + rep("x -> list_transform(x, ", n) + "y -> y" + rep(")", n) + ")"
				}), Sizes: doubling(100, 3), Order: Linear},
				{Name: "nested IN lists", Build: sel(nest("a IN (", "1", ")")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested casts", Build: sel(nest("CAST(", "1", " AS INT)")), Sizes: doubling(200, 3), Order: Linear},
				{Name: "nested window functions", Build: sel(nest("sum(", "x", ") OVER (PARTITION BY a ORDER BY b)")), Sizes: doubling(100, 3), Order: Linear},
			}},
	}
}
