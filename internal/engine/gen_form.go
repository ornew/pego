package engine

import (
	"fmt"
	"strings"
)

// The generator prints a body once and writes the places where the forms of a body differ as
// fragments: a parser with work controls or Documents gives each execution function a branch for
// ordinary parses next to the controlled body, and both are printed from the same traversal, so
// that the names of the generated variables and tables agree.
//
// A fragment is written
//
//	formStart controlled formOrdinary ordinary [formRest rest] formEnd
//
// with control characters that no generated text contains. A form of the text is made by choosing
// the alternative of each fragment (selectForm). The rest form is the body that stays for a Document
// in a parser without work controls; it is the controlled alternative unless a fragment has its own.
const (
	formStart    = "\x01"
	formOrdinary = "\x02"
	formRest     = "\x04"
	formEnd      = "\x03"
)

// formMutant is set by tests to break the ordinary alternative of a fragment and see that the
// checks on the generated branches notice.
var formMutant func(controlled, ordinary string) string

// textForm is a form of generated text.
type textForm int

const (
	formControlled textForm = iota
	formOrdinaryText
	formRestText
)

// form returns the text that differs between the controlled and the ordinary form of a body: the
// controlled one for the controlled body, the ordinary one for the ordinary branch. Without a branch
// there is one text.
func (g *generator) form(controlled, ordinary string) string {
	return g.formRest(controlled, ordinary, controlled)
}

// formRest is form with the text that stays in a Document's form of a parser without work controls.
func (g *generator) formRest(controlled, ordinary, rest string) string {
	if formMutant != nil {
		ordinary = formMutant(controlled, ordinary)
	}
	switch {
	case g.ordinary:
		return ordinary
	case !g.pairs:
		return controlled
	case controlled == ordinary && controlled == rest:
		return controlled
	case rest == controlled:
		return formStart + controlled + formOrdinary + ordinary + formEnd
	}
	return formStart + controlled + formOrdinary + ordinary + formRest + rest + formEnd
}

// selectForm returns the text of form f: each fragment is replaced by its alternative. Fragments
// nest, as the text of an alternative may contain fragments of its own. It panics with an internal
// error on a fragment without its separator or end.
func selectForm(text string, f textForm) string {
	if !strings.Contains(text, formStart) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	selectFormInto(&b, text, f)
	return b.String()
}

func selectFormInto(b *strings.Builder, text string, f textForm) {
	for {
		i := strings.Index(text, formStart)
		if i < 0 {
			b.WriteString(text)
			return
		}
		b.WriteString(text[:i])
		// Find the end of the fragment and its separators at its own depth.
		depth, ord, rest, end := 0, -1, -1, -1
		for j := i; j < len(text) && end < 0; j++ {
			switch text[j] {
			case formStart[0]:
				depth++
			case formEnd[0]:
				if depth--; depth == 0 {
					end = j
				}
			case formOrdinary[0]:
				if depth == 1 {
					ord = j
				}
			case formRest[0]:
				if depth == 1 {
					rest = j
				}
			}
		}
		if end < 0 || ord < 0 {
			// The printer writes whole fragments; a broken one is a bug in a site that wrote it.
			panic(fmt.Sprintf("internal error: malformed text fragment (end %d, ordinary separator %d) in %q", end, ord, text[i:min(len(text), i+80)]))
		}
		controlled, ordinary, restText := text[i+1:ord], "", ""
		if rest >= 0 {
			ordinary, restText = text[ord+1:rest], text[rest+1:end]
		} else {
			ordinary = text[ord+1 : end]
			restText = controlled
		}
		switch f {
		case formOrdinaryText:
			selectFormInto(b, ordinary, f)
		case formRestText:
			selectFormInto(b, restText, f)
		default:
			selectFormInto(b, controlled, f)
		}
		text = text[end+1:]
	}
}
