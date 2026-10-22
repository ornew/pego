package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Value-free terminal expressions in lean typed Go parsers.
//
// A value-free expression made only of literals, character classes, sequences, choices, options
// and negative lookaheads (a terminal expression) needs no state but the position. Written out as
// straight-line code, a large one, such as a keyword list spelled as a trie of case-insensitive
// character classes, takes tens of kilobytes. The generator lowers such an expression to a static
// runtime.TerminalProgram that the runtime's SkipTerminal interprets. SkipTerminal makes the same
// calls as the code it replaces (see dgen.expr and dgen.choice): it consumes the same input and
// records the same expectations at the same positions. Where it departs from the order of that
// code it does so only where the departure is unobservable: a guard that admits the next
// character also matches the class that begins its alternative, and where ExpectOrdinary records
// nothing at the position of a choice, alternatives whose guards do not admit the character are
// passed over without testing them (runtime/terminal.go).

// minTerminalTable is the smallest run of guarded alternatives at the start of a choice for
// which the runtime builds a dispatch table instead of testing the guards in turn.
const minTerminalTable = 4

// minTerminalLeaves is the smallest number of literals and classes for which a terminal
// expression with a choice is lowered to a program. Smaller ones stay straight-line code, which
// costs less to run than the interpreter's calls.
const minTerminalLeaves = 16

// terminalLeaves returns the number of literals and classes of e, and whether e is a terminal
// expression containing a choice. It returns ok false for any other expression.
func terminalLeaves(e grammar.Expr) (leaves int, choice, ok bool) {
	switch e := e.(type) {
	case *grammar.Literal, *grammar.CharClass:
		return 1, false, true
	case *grammar.Seq:
		for _, it := range e.Items {
			n, c, ok := terminalLeaves(it)
			if !ok {
				return 0, false, false
			}
			leaves, choice = leaves+n, choice || c
		}
		return leaves, choice, true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			n, _, ok := terminalLeaves(alt)
			if !ok {
				return 0, false, false
			}
			leaves += n
		}
		return leaves, true, true
	case *grammar.Optional:
		return terminalLeaves(e.Expr)
	case *grammar.Not:
		return terminalLeaves(e.Expr)
	case *grammar.Discard:
		return terminalLeaves(e.Expr)
	case *grammar.Atomic:
		return terminalLeaves(e.Expr)
	}
	return 0, false, false
}

// terminalTable returns the package variable that holds e lowered to a TerminalProgram, if e is
// a value-free terminal expression worth lowering in a lean typed parser, declaring it on first
// use.
func (g *generator) terminalTable(e grammar.Expr) (string, bool) {
	if !g.leanTyped() || g.observability || g.disableTerminalPrograms {
		return "", false
	}
	leaves, choice, ok := terminalLeaves(e)
	if !ok || !choice || leaves < minTerminalLeaves {
		return "", false
	}
	t := &terminalProgram{g: g, classes: map[string]int{}, literals: map[string]int{}}
	t.emit(e)
	if t.overflow {
		return "", false
	}
	words := make([]string, len(t.code))
	for i, w := range t.code {
		words[i] = strconv.Itoa(int(w))
	}
	body := fmt.Sprintf("{Code: []uint16{%s}, Classes: []*Class{%s}", strings.Join(words, ", "), strings.Join(t.classNames, ", "))
	if len(t.literalItems) > 0 {
		body += fmt.Sprintf(", Literals: []TerminalText{%s}", strings.Join(t.literalItems, ", "))
	}
	body += "}"
	key := "term" + body
	if name := g.workClassNames[key]; name != "" {
		return name, true
	}
	name := g.name("term")
	g.workClassNames[key] = name
	g.workClassKeys = append(g.workClassKeys, key)
	// The program is static data; Prepare builds its dispatch tables at initialization.
	fmt.Fprintf(&g.vars, "var %sData = TerminalProgram%s\nvar %[1]s = %[1]sData.Prepare()\n", name, body)
	return name, true
}

// Operation codes of runtime.TerminalProgram.
const (
	terminalClass uint16 = iota
	terminalLiteral
	terminalSeq
	terminalChoice
	terminalOptional
	terminalNot
	terminalGuard
	terminalGuardClass
)

// terminalNoTable marks a choice without a dispatch table (runtime.TerminalNoTable).
const terminalNoTable = 0xffff

// terminalProgram assembles the code of a runtime.TerminalProgram.
type terminalProgram struct {
	g                 *generator
	code              []uint16
	classes, literals map[string]int
	classNames        []string
	literalItems      []string
	tables            int
	overflow          bool
}

// word appends n, noting a value that does not fit.
func (t *terminalProgram) word(n int) {
	if n < 0 || n > 0xffff {
		t.overflow = true
	}
	t.code = append(t.code, uint16(n))
}

// open starts an operation whose length is set by close.
func (t *terminalProgram) open(op uint16) int {
	at := len(t.code)
	t.code = append(t.code, op, 0)
	return at
}

func (t *terminalProgram) close(at int) {
	n := len(t.code) - at
	if n > 0xffff {
		t.overflow = true
	}
	t.code[at+1] = uint16(n)
}

func (t *terminalProgram) class(e *grammar.CharClass) int {
	name := t.g.classTable(e)
	if i, ok := t.classes[name]; ok {
		return i
	}
	t.classes[name] = len(t.classNames)
	t.classNames = append(t.classNames, "&"+name)
	return t.classes[name]
}

// emit appends the operation of e.
func (t *terminalProgram) emit(e grammar.Expr) {
	g := t.g
	switch e := e.(type) {
	case *grammar.Literal:
		value := literalValue(e.Value)
		i, ok := t.literals[value]
		if !ok {
			i = len(t.literalItems)
			t.literals[value] = i
			t.literalItems = append(t.literalItems, fmt.Sprintf("{Runes: %s, Text: %q}", g.literal(value), value))
		}
		t.code = append(t.code, terminalLiteral, 4)
		t.word(i)
		t.word(int(g.desc(strconv.Quote(e.Value))))
	case *grammar.CharClass:
		t.code = append(t.code, terminalClass, 4)
		t.word(t.class(e))
		t.word(int(g.desc(charClassString(e))))
	case *grammar.Seq:
		at := t.open(terminalSeq)
		for _, it := range e.Items {
			t.emit(it)
		}
		t.close(at)
	case *grammar.Choice:
		at := t.open(terminalChoice)
		run := 0
		for run < len(e.Alts) {
			if _, _, ok := t.first(e.Alts[run]); !ok {
				break
			}
			run++
		}
		if run >= minTerminalTable && t.tables < terminalNoTable {
			t.word(t.tables)
			t.tables++
		} else {
			t.word(terminalNoTable)
		}
		for _, alt := range e.Alts {
			// Each alternative gets the guard that dgen.choice gives it (generator.first).
			if first, id, ok := t.first(alt); ok {
				if class, rest, ok := leadingClass(alt); ok && class == first {
					// The guard tests the class that the alternative begins with.
					guard := t.open(terminalGuardClass)
					t.word(t.class(first))
					t.word(int(id))
					for _, it := range rest {
						t.emit(it)
					}
					t.close(guard)
					continue
				}
				guard := t.open(terminalGuard)
				t.word(t.class(first))
				t.word(int(id))
				t.emit(alt)
				t.close(guard)
				continue
			}
			t.emit(alt)
		}
		t.close(at)
	case *grammar.Optional:
		at := t.open(terminalOptional)
		t.emit(e.Expr)
		t.close(at)
	case *grammar.Not:
		at := t.open(terminalNot)
		t.emit(e.Expr)
		t.close(at)
	case *grammar.Discard:
		t.emit(e.Expr)
	case *grammar.Atomic:
		t.emit(e.Expr)
	default:
		panic(fmt.Sprintf("terminalProgram.emit: %T", e))
	}
}

// first returns the class and expectation of the guard of an alternative, as generator.first
// finds them for a terminal expression: the first literal or class that the alternative must
// match first, a literal standing for its first code point.
func (t *terminalProgram) first(e grammar.Expr) (*grammar.CharClass, expID, bool) {
	g := t.g
	switch e := e.(type) {
	case *grammar.Literal:
		if e.Value == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(e.Value)
		return &grammar.CharClass{Ranges: []grammar.CharRange{{Lo: r, Hi: r}}}, g.desc(strconv.Quote(e.Value)), true
	case *grammar.CharClass:
		return e, g.desc(charClassString(e)), true
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return t.first(e.Items[0])
		}
	case *grammar.Atomic:
		return t.first(e.Expr)
	case *grammar.Discard:
		return t.first(e.Expr)
	}
	return nil, 0, false
}

// leadingClass returns the character class that e matches first and the expressions it then
// matches in turn, if e is a class or a sequence that begins with one (through discards and
// atomic groups, which match as their expressions in a terminal expression).
func leadingClass(e grammar.Expr) (*grammar.CharClass, []grammar.Expr, bool) {
	switch e := e.(type) {
	case *grammar.CharClass:
		return e, nil, true
	case *grammar.Seq:
		if len(e.Items) > 0 {
			if c, rest, ok := leadingClass(e.Items[0]); ok {
				return c, append(rest[:len(rest):len(rest)], e.Items[1:]...), true
			}
		}
	case *grammar.Atomic:
		return leadingClass(e.Expr)
	case *grammar.Discard:
		return leadingClass(e.Expr)
	}
	return nil, nil, false
}
