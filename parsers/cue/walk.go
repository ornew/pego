package cue

// Inspect traverses the syntax tree of n in depth-first order, as cuelang.org/go/cue/ast.Inspect does: it calls
// f(n), and if f returns true it calls Inspect for each child of n, then f(nil). The children of a node are the
// nodes that its fields hold (not the operators and brackets, which are [Match] values), in source order.
//
// n is a pointer to a type of the syntax tree, such as the *File that [ParseAST] returns.
func Inspect(n any, f func(node any) bool) {
	if !f(n) {
		return
	}
	eachChild(n, func(ch any) { Inspect(ch, f) })
	f(nil)
}
