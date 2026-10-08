package cel

// The standard macros of CEL (has, all, exists, exists_one, map and filter) are written as calls, and the grammar
// parses them as calls: an application of CEL chooses which macros it supports, and expands them after parsing. What
// the parser of cel-go with the standard macros adds is that it rejects a macro call whose arguments are not what the
// macro takes; CheckMacros reports the same calls.

// IsMacroCall reports whether c is a call that cel-go's parser expands as one of the standard macros: its name, its
// arguments and whether it has a receiver are those of the macro. It does not check that the arguments are valid;
// CheckMacros does.
func IsMacroCall(c *Call) bool {
	if c.Func.Rooted() {
		return false
	}
	name, n := c.Func.Text, len(c.Args)
	if c.Target == nil {
		return name == "has" && n == 1
	}
	switch name {
	case "all", "exists", "exists_one", "existsOne", "filter":
		return n == 2
	case "map":
		return n == 2 || n == 3
	}
	return false
}

// unparen returns the expression inside its parentheses: cel-go does not keep them.
func unparen(e Expr) Expr {
	for {
		p, ok := e.(*Paren)
		if !ok {
			return e
		}
		e = p.X
	}
}

// checkMacros reports the first macro call in e that cel-go's parser rejects with the standard macros: has(x) of
// something that is not a selection of a field, and a quantifier or map or filter whose first argument is not a name,
// or is the name of the accumulator.
func checkMacros(e Expr) error {
	var err error
	Inspect(e, func(e Expr) bool {
		if err != nil {
			return false
		}
		c, ok := e.(*Call)
		if !ok || !IsMacroCall(c) {
			return true
		}
		arg := unparen(c.Args[0])
		if c.Target == nil { // has
			// A selection of a field; or a has() itself, which expands to the selection that tests the field.
			switch a := arg.(type) {
			case *Select:
				if !a.Optional {
					return true
				}
			case *Call:
				if a.Target == nil && IsMacroCall(a) {
					return true
				}
			}
			err = &CheckError{Span: SpanOf(arg), Message: "invalid argument to has() macro"}
			return true
		}
		id, ok := arg.(*Ident)
		switch {
		case !ok && c.Func.Text == "map" || !ok && c.Func.Text == "filter":
			err = &CheckError{Span: SpanOf(arg), Message: "argument is not an identifier"}
		case !ok:
			err = &CheckError{Span: SpanOf(arg), Message: "argument must be a simple name"}
		case !id.Rooted() && id.Name() == "__result__":
			err = &CheckError{Span: SpanOf(arg), Message: "iteration variable overwrites accumulator variable"}
		}
		return true
	})
	return err
}
