package engine

import "github.com/ornew/pego/grammar"

// textArg returns the argument of t if t is a call text(x).
func textArg(t grammar.Term) (grammar.Term, bool) {
	if call, ok := t.(*grammar.Call); ok && call.Func == "text" && len(call.Args) == 1 {
		return call.Args[0], true
	}
	return nil, false
}

// fusable reports whether every argument of the concat call t is itself a call of list, map or a
// fusable concat. Their elements can then be pushed directly, without building the intermediate
// lists (concat(list($first), map($rest, ...)) is the common way to build a list), and errors
// still arise in the same order as when the arguments are evaluated first: each call checks its
// elements as it makes them, and concat has nothing left to check. With any other argument,
// concat would check it only after all arguments are evaluated.
func fusable(t *grammar.Call) bool {
	for _, a := range t.Args {
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list", ok && call.Func == "map" && len(call.Args) == 2:
		case ok && call.Func == "concat" && fusable(call):
		default:
			return false
		}
	}
	return true
}
