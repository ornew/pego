package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// actionTextError holds only owned text, never evaluator or parser state. Leaf
// errors are built eagerly because predicates may discard them without calling
// Error. The marker avoids invoking arbitrary formatters for engine-owned text.
type actionTextError string

func (e actionTextError) Error() string { return string(e) }

func (p *parser) actionError(parts ...string) error {
	if !workControlsEnabled || p == nil || p.control == nil {
		return errors.New(strings.Join(parts, ""))
	}
	return actionTextError(p.actionErrorText(parts...))
}

// actionErrorText reserves each fragment visit, including empty fragments, and
// each bounded byte copy before writing. The local builder owns the final text.
func (p *parser) actionErrorText(parts ...string) string {
	size := 0
	for _, part := range parts {
		p.stepWork()
		size = checkedDiagnosticSize(size, len(part))
	}
	var out strings.Builder
	out.Grow(size)
	for _, part := range parts {
		p.stepWork()
		p.writeDiagnosticText(&out, part)
	}
	return out.String()
}

func (p *parser) actionIndexError(index, length int) error {
	return p.actionError("$", strconv.Itoa(index), " is out of range (", strconv.Itoa(length), " elements)")
}

// failAction preserves the flat public error and native %v semantics. Only
// engine-owned causes use bounded copying. Private opaque causes can execute
// caller code while formatting; check cancellation on both sides of that
// nonpreemptible boundary, without changing fmt's panic handling.
func (p *parser) failAction(where string, err error) {
	if !workControlsEnabled || p.control == nil {
		p.fail("action in %s: %v", where, err)
	}
	text, owned := err.(actionTextError)
	if !owned {
		p.control.check()
		text = actionTextError(fmt.Sprintf("%v", err))
		p.control.check()
	}
	panic(fatal{err: errors.New(p.actionErrorText("action in ", where, ": ", string(text)))})
}
