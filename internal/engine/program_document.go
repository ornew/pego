package engine

import (
	"fmt"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/document"
)

// NewDocument creates a Document that parses text with the rule start. Positions are in code
// points.
func (prog *Program) NewDocument(start, text string) (*Document, error) {
	return prog.NewDocumentWith(start, text, ParseOptions{})
}

// NewDocumentWith creates a Document with options such as the position unit. Positions passed
// to Edit use the same unit.
func (prog *Program) NewDocumentWith(start, text string, o ParseOptions) (d *Document, err error) {
	if prog.HasMissing || o.MissingTokens != 0 {
		return nil, compile.MissingUnsupported("Document parsing")
	}
	if err := core.ValidateStreamDiagnostics(o, false); err != nil {
		return nil, err
	}
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(core.Fatal); ok {
				d, err = nil, f.Err
			} else if cp, ok := x.(core.ContextPanic); ok {
				panic(cp.Value)
			} else {
				panic(x)
			}
		}
	}()
	if o.Recognize {
		return nil, fmt.Errorf("recognition is not supported for documents")
	}
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, true); err != nil {
		return nil, err
	}
	_, _, c, err := prog.admitParse(start, o, false)
	if err != nil {
		return nil, err
	}
	c.Check()
	in := core.NewControlledDocumentInput(text, o.Unit, c)
	c.Check()
	in.Control = nil // construction counters never become parse defaults
	return document.New(prog, start, prog.backend(o.Backend), o, in), nil
}

// NewChangeDocumentWith creates a Document with NewDocumentWith and enables its
// change notifications with EnableChanges(queueLimit).
func (p *Program) NewChangeDocumentWith(start, text string, o ParseOptions, queueLimit int) (*ChangeDocument, error) {
	d, err := p.NewDocumentWith(start, text, o)
	if err != nil {
		return nil, err
	}
	if err := d.EnableChanges(queueLimit); err != nil {
		return nil, err
	}
	return &ChangeDocument{Document: d}, nil
}

// SelectBackend returns the backend that a request for b runs on.
func (prog *Program) SelectBackend(b Backend) Backend { return prog.backend(b) }

// Shared returns the program part that a parser for prog reads.
func (prog *Program) Shared() *core.SharedProgram { return &prog.sharedProgram }

// PrepareRun returns the program that parses a Document on backend b under the budget control c.
func (prog *Program) PrepareRun(b Backend, c *core.BudgetControl) (document.Runner, error) {
	active, err := prog.prepareControlledProgram(b, c)
	if err != nil {
		return nil, err
	}
	return active, nil
}
