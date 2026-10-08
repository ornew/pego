// Package cue parses CUE.
package cue

import (
	"unicode/utf8"
)

// ParseFile parses a CUE file as cuelang.org/go/cue/parser.ParseFile does, and returns the first error, if
// any: ParseAST, then Check, and an error for input that is not UTF-8 (which the parser reads as U+FFFD).
func ParseFile(input string, unit ...Unit) (*File, error) {
	if !utf8.ValidString(input) {
		i := 0
		for j, r := range input {
			if r == utf8.RuneError {
				if _, w := utf8.DecodeRuneInString(input[j:]); w == 1 {
					break
				}
			}
			i = j
		}
		return nil, locate(input, &SemanticError{Span: Span{Start: i + 1, End: i + 1}, Msg: "illegal UTF-8 encoding"}, unit)
	}
	f, err := ParseAST(input, unit...)
	if err != nil {
		return nil, err
	}
	if err := f.Check(); err != nil {
		return nil, locate(input, err, unit)
	}
	return f, nil
}

// Valid reports whether input is a CUE file that ParseFile accepts.
func Valid(input string) bool {
	_, err := ParseFile(input)
	return err == nil
}

// locate sets the line and column of a SemanticError, whose position is in the unit of the parse.
func locate(input string, err error, unit []Unit) error {
	e, ok := err.(*SemanticError)
	if !ok {
		return err
	}
	bytes := len(unit) > 0 && unit[0] == Bytes
	e.Line, e.Col = 1, 1
	i := 0
	for j, r := range input {
		if bytes && j >= e.Start || !bytes && i >= e.Start {
			break
		}
		if r == '\n' {
			e.Line, e.Col = e.Line+1, 1
		} else {
			e.Col++
		}
		i++
	}
	return e
}
