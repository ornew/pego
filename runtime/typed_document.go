package runtime

import "fmt"

// The typed Document runtime, appended to generated parsers made with both pego gen -types and
// -document: Document.ParseAST parses directly into typed values, which a typed Document keeps
// privately (typedDocumentState) and publishes as read-only values that later results may share.
// The generated CloneAST copies a published value.

// TypedDocumentCloneBase is the state of a CloneAST copy of a published typed Document value. The
// generated typedDocumentClone embeds it and adds the copies of the grammar's types.
//
// TypedDocumentCloneBase is for generated parsers; not a stable API.
type TypedDocumentCloneBase struct {
	Control *WorkControl
	Values  map[any]any
	Slices  map[DocumentSliceKey]any
	Tasks   []TypedDocumentCloneTask
}

// TypedDocumentCloneTask is a value whose copy (target) has yet to be filled from source.
//
// TypedDocumentCloneTask is for generated parsers; not a stable API.
type TypedDocumentCloneTask struct{ Source, Target any }

func (c *TypedDocumentCloneBase) Begin(control *WorkControl) { c.Control = control }

func (c *TypedDocumentCloneBase) Step(n uint64) {
	if WorkControlsEnabled && c.Control != nil {
		c.Control.Charge(0, n)
	}
}

func (c *TypedDocumentCloneBase) Remember(from, to any) {
	if c.Values == nil {
		c.Values = make(map[any]any)
	}
	c.Values[from] = to
}

func (c *TypedDocumentCloneBase) ReleaseBase() {
	c.Control, c.Values, c.Slices, c.Tasks = nil, nil, nil, nil
}

// TypedDocumentParse parses the Document d with the typed rule root and returns the value that publish
// makes of the parse result. The values of a typed Document parse are write-once: they are published
// as they are, and later parses reuse them, so publish must not write to them. a is the generated
// code's allocation chunks.
//
// TypedDocumentParse is for generated parsers; not a stable API.
func TypedDocumentParse[T any](d *Document, root *TRule, o parseOptions, a workControlSetter, publish func(any) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.State.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	o = prepareOptions(o)
	cache, ok := d.typedMemo.(*typedDocumentState)
	if !ok {
		cache = &typedDocumentState{owner: d.State, meta: make(map[*tmemoEntry]typedDocumentMeta)}
		d.typedMemo = cache
	}
	// Admission leaves reusable state untouched. Once execution can begin,
	// discard private memo values on interruption or caller-context panic.
	completed := false
	defer func() {
		if !completed {
			d.typedMemo = nil
		}
	}()
	p := &TParser{Parser: &Parser{Control: o.control}, TypedDocument: cache, memoSlots: cache.slots}
	p.Observation = newObservation(&o.observe)
	defer p.releaseObservation()
	// Do not recycle p: memo values and expectations may refer into its arenas.
	a.SetWorkControl(o.control)
	defer a.SetWorkControl(nil)
	d.State.stats = documentStats{}
	v, parseErr := p.run(root, d.State.text, o, a)
	// Context implementations may return any public error type. Fatal work
	// interruption must not be mistaken for a completed syntax failure.
	if o.control != nil && o.control.interrupted {
		return res, parseErr
	}
	if parseErr != nil {
		switch parseErr.(type) {
		case *SyntaxError, SyntaxErrors:
		default:
			return res, parseErr
		}
	}
	o.control.Check()
	if v != nil {
		res = publish(v)
	}
	err = typedDocumentError(parseErr, o.control)
	cache.slots = p.memoSlots
	completed = true
	return res, err
}

// typedDocumentError copies the diagnostics of err, charging c. Diagnostics are private memo data
// too; publishing their original pointers or Expected/Messages slices would let caller mutation
// alter a later parse.
func typedDocumentError(err error, control *WorkControl) error {
	clone := func(e *SyntaxError) *SyntaxError {
		control.Charge(0, 1)
		if e == nil {
			return nil
		}
		out := new(SyntaxError)
		*out = *e
		if e.Expected != nil {
			control.Charge(0, uint64(len(e.Expected)))
			out.Expected = make([]string, len(e.Expected))
			for i, s := range e.Expected {
				control.Charge(0, 1)
				out.Expected[i] = s
			}
		}
		if e.Messages != nil {
			control.Charge(0, uint64(len(e.Messages)))
			out.Messages = make([]string, len(e.Messages))
			for i, s := range e.Messages {
				control.Charge(0, 1)
				out.Messages[i] = s
			}
		}
		return out
	}
	switch e := err.(type) {
	case *SyntaxError:
		return clone(e)
	case SyntaxErrors:
		control.Charge(0, uint64(len(e)))
		out := make(SyntaxErrors, len(e))
		for i, item := range e {
			out[i] = clone(item)
		}
		return out
	}
	return err
}
