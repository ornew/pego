package lsp

// Built-in names of the PEGO language, for hover and completion. The descriptions follow the
// specification (spec/).

type builtin struct {
	name string
	// signature is shown in code (for example "len(x) int").
	signature string
	doc       string
	// snippet is the text inserted by completion, in the snippet syntax.
	snippet string
}

var builtinTypes = []builtin{
	{name: "int", doc: "An integer. Positions in the input are also `int` values."},
	{name: "string", doc: "A string. `len` measures its length in the position unit."},
	{name: "bool", doc: "A boolean."},
	{name: "node", doc: "Any node. `node[string]` is a string node: the node of a literal, a character class, `.` or an atomic expression."},
	{name: "error", doc: "The value of error recovery: an error recovered by `#recover`, or a token inserted by `#missing`."},
	{name: "_", doc: "The top type: every value. A `_` value needs a cast `x.(T)` to be used as a `T`."},
	{name: "_|_", doc: "The bottom type: no value. A location of this type is an error."},
	{name: "Missing", doc: "Reserved node type: an opt-in #missing literal insertion with an empty source range."},
}

var builtinFuncs = []builtin{
	{name: "len", signature: "len(x) int", snippet: "len($1)",
		doc: "The length of a string or a string node in the position unit; for a tuple or list, the number of elements; `0` for `nil`."},
	{name: "text", signature: "text(x) string", snippet: "text($1)",
		doc: "The input text that the node `x` covers (for a string node, its text); `\"\"` for `nil`."},
	{name: "foldl", signature: "foldl(init, list, (acc, item) => ...)", snippet: "foldl(${1:init}, ${2:list}, (${3:acc}, ${4:item}) => $0)",
		doc: "Folds `list` from the left: applies the function to each element, from the first to the last, starting with `init`."},
	{name: "foldr", signature: "foldr(init, list, (acc, item) => ...)", snippet: "foldr(${1:init}, ${2:list}, (${3:acc}, ${4:item}) => $0)",
		doc: "Folds `list` from the right: applies the function to each element, from the last to the first, starting with `init`."},
	{name: "map", signature: "map(list, (item) => ...) []T", snippet: "map(${1:list}, (${2:item}) => $0)",
		doc: "A list of the results of applying the function to each element. The function must return a node or `nil`."},
	{name: "filter", signature: "filter(list, (item) => bool) []T", snippet: "filter(${1:list}, (${2:item}) => $0)",
		doc: "Keeps the original elements whose predicate returns `true`, preserving order and node identity. The result keeps the input list's static element type; a `nil` input returns an empty list. Predicate errors follow the surrounding action: they fail a predicate action or abort a value action."},
	{name: "list", signature: "list(a, b, ...) []T", snippet: "list($1)",
		doc: "A list of the arguments, which must be nodes or `nil`."},
	{name: "concat", signature: "concat(l1, l2, ...) []T", snippet: "concat($1)",
		doc: "The concatenation of the lists; a `nil` argument, and an error recovered by `#recover`, count as empty lists."},
}

var builtinMembers = []builtin{
	{name: "Start", signature: "Start int", doc: "The start position of the node in the position unit."},
	{name: "End", signature: "End int", doc: "The end position of the node (exclusive)."},
	{name: "Value", signature: "Value T", doc: "The value of the node: for a string node, the text it matched; for a sequence, the tuple of its elements; for a repetition, the list of its elements."},
}

var attributes = []builtin{
	{name: "diagnostic", signature: "@diagnostic(name=\"...\")", snippet: "diagnostic(name=\"$1\")",
		doc: "Names the ordinary expected items when the wrapped expression fails; explicit @error messages remain visible."},
	{name: "error", signature: "@error(message=\"...\")", snippet: "error(message=\"$1\")",
		doc: "Replaces the expected items of a syntax error inside the expression with a message."},
}

var pragmas = []builtin{
	{name: "atomic", signature: "#atomic", snippet: "atomic",
		doc: "Matches the wrapped input as one string node without building child values."},
	{name: "cut", signature: "#cut", snippet: "cut",
		doc: "Stands in place of an expression and commits the innermost enclosing choice of the rule to the current alternative."},
	{name: "repeat", signature: "#repeat(min,max)", snippet: "repeat(${1:min},${2:max})",
		doc: "Repeats the preceding expression. One count is exact; an omitted bound is zero or unbounded."},
	{name: "missing", signature: "#missing", snippet: "missing",
		doc: "Permits insertion of a nonempty literal when Closure parsing explicitly enables missing tokens. No source text is fabricated."},
	{name: "recover", signature: "#recover(skip=e)", snippet: "recover(skip=$1)",
		doc: "When the expression fails, skips input matching e and continues. The skipped range becomes an `error` value."},
	{name: "stream", signature: "#stream", snippet: "stream",
		doc: "In a stream parse, hands each element of a repetition to the caller as soon as it matches."},
}

// Keywords offered by completion where a parsing expression or a definition may start.
var statementKeywords = []string{"rule", "type"}

var bodyKeywords = []string{"operators"}

var operatorsRuleKeywords = []string{"skip", "operand", "level", "prefix", "postfix", "infix"}

var termKeywords = []string{"true", "false", "nil"}

func findBuiltin(list []builtin, name string) *builtin {
	for i := range list {
		if list[i].name == name {
			return &list[i]
		}
	}
	return nil
}
