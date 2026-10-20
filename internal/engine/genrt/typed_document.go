package genrt

import "fmt"

// The typed Document runtime, appended to generated parsers made with both pego gen -types and
// -document: Document.ParseAST parses directly into typed values, which a typed Document keeps
// privately (typedDocumentState) and publishes as independent copies.

// typedDocumentCloneBase is the state of a publication of the private values of a typed Document
// parse. The generated typedDocumentClone embeds it and adds the copies of the grammar's types.
type typedDocumentCloneBase struct {
	control *workControl
	values  map[any]any
	slices  map[documentSliceKey]any
	tasks   []typedDocumentCloneTask
}

// typedDocumentCloneTask is a value whose copy (target) has yet to be filled from source.
type typedDocumentCloneTask struct{ source, target any }

// typedDocumentPublisher publishes the values of a typed Document parse (a *typedDocumentClone).
type typedDocumentPublisher interface {
	begin(c *workControl)
	step(n uint64)
	finish()
	release()
}

func (c *typedDocumentCloneBase) begin(control *workControl) { c.control = control }

func (c *typedDocumentCloneBase) step(n uint64) {
	if workControlsEnabled && c.control != nil {
		c.control.charge(0, n)
	}
}

func (c *typedDocumentCloneBase) remember(from, to any) {
	if c.values == nil {
		c.values = make(map[any]any)
	}
	c.values[from] = to
}

func (c *typedDocumentCloneBase) releaseBase() {
	c.control, c.values, c.slices, c.tasks = nil, nil, nil, nil
}

// typedDocumentParse parses the Document d with the typed rule root, publishing the value with
// publish, which copies it with c. a is the generated code's allocation chunks.
func typedDocumentParse[T any](d *Document, root *trule, o parseOptions, a interface{ setWorkControl(*workControl) }, c typedDocumentPublisher, publish func(any) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.state.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	o = prepareOptions(o)
	cache, ok := d.typedMemo.(*typedDocumentState)
	if !ok {
		cache = &typedDocumentState{owner: d.state, meta: make(map[*tmemoEntry]typedDocumentMeta)}
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
	p := &tparser{parser: &parser{control: o.control}, typedDocument: cache, memoSlots: cache.slots}
	p.observation = newObservation(&o.observe)
	defer p.releaseObservation()
	// Do not recycle p: memo values and expectations may refer into its arenas.
	a.setWorkControl(o.control)
	defer a.setWorkControl(nil)
	c.begin(o.control)
	defer c.release()
	d.state.stats = documentStats{}
	v, parseErr := p.run(root, d.state.text, o, a)
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
	o.control.check()
	if v != nil {
		res = publish(v)
	}
	err = typedDocumentError(parseErr, c)
	c.finish()
	cache.slots = p.memoSlots
	completed = true
	return res, err
}

// typedDocumentError copies the diagnostics of err, charging c. Diagnostics are private memo data
// too; publishing their original pointers or Expected/Messages slices would let caller mutation
// alter a later parse.
func typedDocumentError(err error, c interface{ step(uint64) }) error {
	clone := func(e *SyntaxError) *SyntaxError {
		c.step(1)
		if e == nil {
			return nil
		}
		out := new(SyntaxError)
		*out = *e
		if e.Expected != nil {
			c.step(uint64(len(e.Expected)))
			out.Expected = make([]string, len(e.Expected))
			for i, s := range e.Expected {
				c.step(1)
				out.Expected[i] = s
			}
		}
		if e.Messages != nil {
			c.step(uint64(len(e.Messages)))
			out.Messages = make([]string, len(e.Messages))
			for i, s := range e.Messages {
				c.step(1)
				out.Messages[i] = s
			}
		}
		return out
	}
	switch e := err.(type) {
	case *SyntaxError:
		return clone(e)
	case SyntaxErrors:
		c.step(uint64(len(e)))
		out := make(SyntaxErrors, len(e))
		for i, item := range e {
			out[i] = clone(item)
		}
		return out
	}
	return err
}
