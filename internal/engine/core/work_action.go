package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ActionTextError holds only owned text, never evaluator or parser state. Leaf
// errors are built eagerly because predicates may discard them without calling
// Error. The marker avoids invoking arbitrary formatters for engine-owned text.
type ActionTextError string

func (e ActionTextError) Error() string { return string(e) }

func (p *Parser) ActionError(parts ...string) error {
	if !WorkControlsEnabled || p == nil || p.Control == nil {
		return errors.New(strings.Join(parts, ""))
	}
	return ActionTextError(p.actionErrorText(parts...))
}

// actionErrorText reserves each fragment visit, including empty fragments, and
// each bounded byte copy before writing. The local builder owns the final text.
func (p *Parser) actionErrorText(parts ...string) string {
	size := 0
	for _, part := range parts {
		p.Work()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.Work()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *Parser) ActionIndexError(index, length int) error {
	return p.ActionError("$", strconv.Itoa(index), " is out of range (", strconv.Itoa(length), " elements)")
}

// FailAction preserves the flat public error and native %v semantics. Only
// engine-owned causes use bounded copying. Private opaque causes can execute
// caller code while formatting; check cancellation on both sides of that
// nonpreemptible boundary, without changing fmt's panic handling.
func (p *Parser) FailAction(where string, err error) {
	if !WorkControlsEnabled || p.Control == nil {
		p.Fail("action in %s: %v", where, err)
	}
	text, owned := err.(ActionTextError)
	if !owned {
		p.Control.Check()
		text = ActionTextError(fmt.Sprintf("%v", err))
		p.Control.Check()
	}
	panic(Fatal{Err: errors.New(p.actionErrorText("action in ", where, ": ", string(text)))})
}
