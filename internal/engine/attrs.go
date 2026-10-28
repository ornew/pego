package engine

import "github.com/ornew/pego/grammar"

func validArg(attr, arg string) bool {
	switch attr {
	case "error":
		return arg == "message"
	case "diagnostic":
		return arg == "name"
	case "recover":
		return arg == "skip"
	}
	return true
}

func (c *compiler) stringArg(a *grammar.Attribute, name string) (string, bool) {
	v, ok := a.Arg(name)
	if !ok {
		c.errorf(a.Diagnostic(), "@%s requires a %s argument", a.Name, name)
		return "", false
	}
	lit, ok := v.(*grammar.Literal)
	if !ok {
		c.errorf(a.Diagnostic(), "@%s: %s must be a string", a.Name, name)
		return "", false
	}
	return lit.Value, true
}
