package complexitytest

import "strings"

// yamlMaps returns n block mappings nested one space deeper each, with value at the bottom.
func yamlMaps(n int, value string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(rep(" ", i) + "k:\n")
	}
	b.WriteString(rep(" ", n) + "k: " + value + "\n")
	return b.String()
}

// triageYaml is the triage table of yaml.
func triageYaml() []TriageGroup {
	return []TriageGroup{
		{Grammar: "yaml", ID: "separation", Witness: true, Rules: []string{
			"s_separate_in_line", "s_separate_lines", "s_l_comments", "l_empty_block", "l_empty_flow", "block_out_node",
			"s_l_block_node", "s_l_flow_in_block", "block_collection_start", "block_sequence_start", "seq_space",
			"l_block_sequence", "l_block_mapping",
		}, Pattern: `examined`,
			Shape:  "the spaces, tabs, comments and empty lines between nodes, read by a lookahead or failed alternative that tests what follows them and again by the separation that the next alternative reads",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "long trailing spaces and comments", Build: list("", "k%d:"+rep(" ", 20)+"v"+rep(" ", 20)+"# "+rep("c", 20), "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
				{Name: "values on the next line after long spaces", Build: list("", "k%d:"+rep(" ", 30)+"\n  v", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
				{Name: "empty lines between entries", Build: list("", "- a\n"+rep(rep(" ", 10)+"\n", 3), "", ""), Sizes: doubling(200, 3), Order: Linear},
				{Name: "comment lines before a document", Build: func(n int) string { return rep("  # "+rep("c", 20)+"\n", n) + "a: 1\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "folded flow scalars with empty lines", Build: func(n int) string { return "k: \"" + rep("a\n   \n  ", n) + "b\"\n" }, Sizes: doubling(500, 3), Order: Linear},
				{Name: "sequences and mappings after long spaces", Build: list("", "k%d:"+rep(" ", 20)+"\n- a\n- b", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "yaml", ID: "block-scalars", Witness: true, Rules: []string{"s_l_block_scalar"}, Pattern: `examined`,
			Shape:  "the indentation and trailing empty lines of a literal or folded block scalar, read by the chomping and keep tests and again by the lines that follow",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "block scalars with trailing empty lines", Build: list("", "k%d: |+\n  a\n"+rep(rep(" ", 6)+"\n", 3), "", ""), Sizes: doubling(200, 3), Order: Linear},
				{Name: "a block scalar with n empty lines", Build: func(n int) string { return "k: >-\n  a\n" + rep("   \n", n) + "  b\n" }, Sizes: doubling(1000, 3), Order: Linear},
				{Name: "block scalars with comments after them", Build: list("", "k%d: |\n  a\n # "+rep("c", 20), "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "yaml", ID: "directives-and-tags", Witness: true, Rules: []string{"ns_directive_parameter", "c_ns_tag_property", "c_tag_handle"}, Pattern: `examined`,
			Shape:  "the parameters of a reserved directive and the characters of a tag, read by an alternative that fails at the end and again by the next alternative",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "reserved directives with long parameters", Build: func(n int) string { return list("", "%%FOO p%d"+rep("p", 30)+" "+rep("q", 30), "\n", "\n---\na\n")(n) }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "long tags", Build: list("", "- !"+rep("t", 30)+" a\n- !<"+rep("u", 30)+"> b\n- !!"+rep("h", 30)+" c", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
				{Name: "one long tag handle", Build: func(n int) string { return "!" + rep("h", n) + " a\n" }, Sizes: doubling(2000, 3), Order: Linear},
			}},
		{Grammar: "yaml", ID: "scalars", Witness: true, Rules: []string{"nb_ns_plain_in_line_out", "nb_ns_plain_in_line_in", "nb_ns_double_in_line", "nb_ns_single_in_line"}, Pattern: `examined`,
			Shape:  "the characters of a plain, double-quoted or single-quoted scalar line, read by a repetition whose next iteration fails at a space run and again by the folding of the next line",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "multi-line plain scalars", Build: func(n int) string { return "k: " + list("", rep("w", 10)+rep(" ", 10), "\n  ", "")(n) + "\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "multi-line plain scalars in flow", Build: func(n int) string { return "[" + list("", rep("w", 10)+rep(" ", 10), "\n ", "")(n) + "]\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "multi-line quoted scalars", Build: func(n int) string {
					return "k: \"" + list("", rep("w", 10)+rep(" ", 10), "\n  ", "")(n) + "\"\nj: '" + list("", rep("w", 10)+rep(" ", 10), "\n  ", "")(n) + "'\n"
				}, Sizes: doubling(300, 3), Order: Linear},
				{Name: "one long plain line with spaces", Build: func(n int) string { return "k: " + rep("w ", n) + "w\n" }, Sizes: doubling(1000, 3), Order: Linear},
			}},
		{Grammar: "yaml", ID: "indentation-bindings", Witness: true, Rules: []string{"seq_space", "compact_collection"}, Pattern: `the cost can grow exponentially: the binding n :=`,
			Shape:  "the indentation n set from the spaces before an entry; each binding reads the earlier n after reading a dash or the spaces of a deeper entry, so the calls of a nesting are at increasing positions and a position sees the few indentations of its enclosing entries",
			Assign: "F101", Order: Linear, Families: []TriageFamily{
				{Name: "nested compact sequences", Build: func(n int) string { return rep("- ", n) + "a\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested compact mappings in sequences", Build: func(n int) string {
					var b strings.Builder
					for i := range n {
						b.WriteString(rep("  ", i) + "- k:\n")
					}
					b.WriteString(rep("  ", n) + "- a\n")
					return b.String()
				}, Sizes: []int{24, 48, 96}, Order: Linear},
				{Name: "nested block sequences in mappings", Build: func(n int) string {
					var b strings.Builder
					for i := range n {
						b.WriteString(rep(" ", i) + "k:\n" + rep(" ", i) + "-\n")
					}
					b.WriteString(rep(" ", n) + "a\n")
					return b.String()
				}, Sizes: []int{24, 48, 96}, Order: Linear},
			}},
		{Grammar: "yaml", ID: "stream-prefix", Rules: []string{"l_yaml_stream"}, Pattern: `contains the repetition l_comment\*`,
			Shape:  "the comment lines of a document prefix against the prefixes that follow them",
			Assign: "F97", Order: Linear, Families: []TriageFamily{
				{Name: "documents separated by comment lines", Build: list("", "a%d\n...\n# "+rep("c", 20)+"\n# x", "\n", "\n"), Sizes: doubling(200, 3), Order: Linear},
			}},
		{Grammar: "yaml", ID: "nested", Pattern: nestedPattern,
			Shape:  "units of block and flow nodes nested through the recursion of block collections and flow collections, every call on it memoized under the values of n and c",
			Assign: "F100", Order: Linear, Families: []TriageFamily{
				{Name: "nested block mappings", Build: func(n int) string { return yamlMaps(n, "v") }, Sizes: []int{48, 96, 192}, Order: Linear},
				{Name: "nested flow sequences", Build: func(n int) string { return nest("[a, ", "b", "]")(n) + "\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested flow mappings", Build: func(n int) string { return nest("{a: ", "b", "}")(n) + "\n" }, Sizes: doubling(300, 3), Order: Linear},
				{Name: "nested flow pairs with properties", Build: func(n int) string { return nest("[&a !t ", "b", " : c]")(n) + "\n" }, Sizes: doubling(200, 3), Order: Linear},
				{Name: "flow nodes inside nested blocks", Build: func(n int) string { return yamlMaps(n, "[a, {b: c}]") }, Sizes: []int{48, 96, 192}, Order: Linear},
			}},
	}
}
