package runtime

import (
	"fmt"
	"unicode/utf8"
)

// Document incrementally parses the configured start rule. It belongs to one
// goroutine; returned nodes may be shared with later parses.
//
// Of its fields and methods, BeginParse, EndParse, ResolveParseOptions, State are for generated parsers; not a stable API.
type Document struct {
	State     *documentState
	defaults  parseOptions
	typedMemo interface{ invalidate(int) }
	// parsing is set while a parse runs, whose observation callbacks must not
	// parse or edit the Document again (BeginParse).
	parsing bool
}

// DocumentBusy returned by a Parse, ParseAST or Edit of a Document that is
// already parsing. Only a trace callback of that parse can make one; parsers
// generated with observability export it as ErrDocumentBusy.
//
// DocumentBusy is for generated parsers; not a stable API.
var DocumentBusy = fmt.Errorf("document is already parsing")

func (d *Document) BeginParse() error {
	if d.parsing {
		return DocumentBusy
	}
	d.parsing = true
	return nil
}

func (d *Document) EndParse() { d.parsing = false }

// ParseStats counts rule evaluations and reused memo results in the last Parse.
type ParseStats struct {
	Evaluated int
	Reused    int
}

// MakeDocument is for generated parsers; not a stable API.
func MakeDocument(root *Rule, text string, opts []ParseOption) (d *Document, err error) {
	o, err := ResolveOptions(root.g, opts)
	if err != nil {
		return nil, err
	}
	defer recoverParse(&d, &err)
	o = prepareOptions(o)
	// Construction performs the same input decoding and work checks as a parse,
	// but its counters do not carry into subsequent invocations.
	p := &Parser{unit: o.unit, Control: o.control}
	p.setGrammar(root.g)
	if o.unit == CodePoints {
		p.setSource(text)
	} else if o.control != nil {
		p.InputWork(uint64(len(text)))
	}
	o.control.Check()
	o.control, o.prepared = nil, false
	return &Document{State: newDocumentState(root, text, o.unit), defaults: o}, nil
}

// Text returns the current exact input, including invalid UTF-8 bytes.
func (d *Document) Text() string { return d.State.text }

// Edit replaces [start,end) in the Document's position unit.
func (d *Document) Edit(start, end int, text string) error {
	if d.parsing {
		return DocumentBusy
	}
	reuse, err := d.State.editReuse(start, end, text)
	if err != nil {
		return err
	}
	if d.typedMemo != nil {
		d.typedMemo.invalidate(reuse)
	}
	return nil
}

// Parse parses the current text, applying invocation options after the
// Document's construction defaults. An invocation cannot change its unit.
func (d *Document) Parse(opts ...ParseOption) (*Node, error) {
	if err := d.BeginParse(); err != nil {
		return nil, err
	}
	defer d.EndParse()
	o, err := d.ResolveParseOptions(opts)
	if err != nil {
		return nil, err
	}
	return d.State.parse(o)
}

func (d *Document) ResolveParseOptions(opts []ParseOption) (parseOptions, error) {
	o := d.defaults
	for _, opt := range opts {
		opt(&o)
	}
	if o.maxDepth < 0 {
		return o, fmt.Errorf("max depth must be non-negative: %d", o.maxDepth)
	}
	if o.maxDepth == 0 {
		o.maxDepth = d.State.root.g.maxDepth
	}
	if !WorkControlsEnabled && (o.context != nil || o.limits != (WorkLimits{})) {
		return o, fmt.Errorf("work controls are disabled in this generated reference parser")
	}
	if err := o.validateDiagnostics(); err != nil {
		return o, err
	}
	if err := o.validateObservation(d.State.root.g); err != nil {
		return o, err
	}
	return o, nil
}

// Stats returns the evaluation and reuse counts of the last Parse.
func (d *Document) Stats() ParseStats {
	s := d.State.stats
	return ParseStats{Evaluated: s.Evaluated, Reused: s.Reused}
}

// Clone copies a Node graph while preserving shared Node identities and repeated
// []any or []*Node slice views. It does not clone arbitrary mutable action values.
func (n *Node) Clone() *Node {
	state := documentCloneState{
		nodes:  make(map[*Node]*Node),
		slices: make(map[DocumentSliceKey]any),
	}
	return cloneDocumentNode(n, &state)
}

type documentCloneState struct {
	nodes  map[*Node]*Node
	slices map[DocumentSliceKey]any
}

// A slice's first element, length and capacity identify an exact slice view.
// The element pointer keeps the backing array visible to the collector while
// cloning. Empty slices without capacity have no elements or mutable backing
// array to share.
//
// DocumentSliceKey is for generated parsers; not a stable API.
type DocumentSliceKey struct {
	First    any
	Length   int
	Capacity int
}

func cloneDocumentNode(n *Node, state *documentCloneState) *Node {
	if n == nil {
		return nil
	}
	if c := state.nodes[n]; c != nil {
		return c
	}
	c := new(Node)
	*c = *n
	state.nodes[n] = c
	if n.RawChildren != nil {
		c.RawChildren = cloneDocumentValue(n.RawChildren, state).([]*Node)
	}
	if n.RawFields != nil {
		c.RawFields = make(Fields, len(n.RawFields))
		for i, field := range n.RawFields {
			field.Value = cloneDocumentValue(field.Value, state)
			c.RawFields[i] = field
		}
	}
	return c
}

func cloneDocumentValue(v any, state *documentCloneState) any {
	switch v := v.(type) {
	case *Node:
		return cloneDocumentNode(v, state)
	case []*Node:
		if v == nil {
			return v
		}
		if cap(v) > 0 {
			key := DocumentSliceKey{First: &v[:1][0], Length: len(v), Capacity: cap(v)}
			if out, ok := state.slices[key]; ok {
				return out
			}
			// Source capacity identifies this view, but is not live output.
			out := make([]*Node, len(v))
			state.slices[key] = out
			for i, n := range v {
				out[i] = cloneDocumentNode(n, state)
			}
			return out
		}
		out := make([]*Node, len(v))
		for i, n := range v {
			out[i] = cloneDocumentNode(n, state)
		}
		return out
	case []any:
		if v == nil {
			return v
		}
		if cap(v) > 0 {
			key := DocumentSliceKey{First: &v[:1][0], Length: len(v), Capacity: cap(v)}
			if out, ok := state.slices[key]; ok {
				return out
			}
			// Source capacity identifies this view, but is not live output.
			out := make([]any, len(v))
			state.slices[key] = out // publish before walking elements to break cycles
			for i, item := range v {
				out[i] = cloneDocumentValue(item, state)
			}
			return out
		}
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneDocumentValue(item, state)
		}
		return out
	default:
		return v
	}
}

// newDocumentState parses only the configured start rule.
func newDocumentState(root *Rule, text string, unit PositionUnit) *documentState {
	return &documentState{root: root, text: text, unit: unit}
}

func (d *documentState) edit(start, end int, replacement string) error {
	_, err := d.editReuse(start, end, replacement)
	return err
}

// editReuse applies an edit and returns the position before which memo
// results remain valid.
func (d *documentState) editReuse(start, end int, replacement string) (int, error) {
	length := len(d.text)
	if d.unit != Bytes {
		length = utf8.RuneCountInString(d.text)
	}
	if start < 0 || end < start || end > length {
		return start, fmt.Errorf("invalid range [%d,%d) for text of length %d", start, end, length)
	}
	startByte, endByte := start, end
	if d.unit == Bytes {
		if err := documentByteBoundary(d.text, start); err != nil {
			return start, err
		}
		if err := documentByteBoundary(d.text, end); err != nil {
			return start, err
		}
	} else {
		startByte = documentByteOffset(d.text, start)
		endByte = documentByteOffset(d.text, end)
	}
	// The replacement can complete an incomplete UTF-8 sequence before start,
	// turning invalid bytes, each a separate character, into one character.
	// Such a sequence has at most UTFMax-1 bytes, each one position in either
	// unit, so results that examined them are not retained.
	reuse := start
	for i := 0; i < utf8.UTFMax-1 && i < startByte; i++ {
		if r, size := utf8.DecodeLastRuneInString(d.text[:startByte-i]); r != utf8.RuneError || size != 1 {
			break
		}
		reuse--
	}

	// Retain only entries wholly before the edit. This intentionally does not
	// shift suffix entries; that optimization needs shared-node movement and
	// positional-value invalidation. A retained entry's input and positions
	// are unchanged, and a future parse can still reuse it.
	var kept memoTable
	for pos := 0; pos < len(d.memo.slots) && pos < reuse; pos++ {
		for e := d.memo.slots[pos]; e != nil; e = e.next {
			if e.examined > reuse || e.growing || e.provisional {
				continue
			}
			copyEntry := kept.alloc()
			*copyEntry = *e
			copyEntry.next = nil
			kept.put(int(e.rule), pos, int(e.min), e.env, copyEntry)
		}
	}
	// Build the replacement before changing either text or memo. Invalid UTF-8
	// bytes are preserved, and a replacement may complete an existing prefix.
	next := d.text[:startByte] + replacement + d.text[endByte:]
	d.text, d.memo = next, kept
	return reuse, nil
}

func documentByteOffset(text string, index int) int {
	if index == 0 {
		return 0
	}
	count := 0
	for off := range text {
		if count == index {
			return off
		}
		count++
	}
	return len(text)
}

func documentByteBoundary(text string, pos int) error {
	if pos < len(text) && !utf8.RuneStart(text[pos]) {
		for start := pos - 1; start >= max(0, pos-utf8.UTFMax+1); start-- {
			if utf8.RuneStart(text[start]) {
				_, size := utf8.DecodeRuneInString(text[start:])
				if start+size > pos {
					return fmt.Errorf("position %d is not at a character boundary", pos)
				}
				break
			}
		}
	}
	return nil
}

// parse retains completed memo entries after ordinary syntax failures. A
// panic or work interruption discards all entries because they may depend on
// unfinished left-recursion growth or partially evaluated actions.
func (d *documentState) parse(o parseOptions) (*Node, error) {
	return DocumentParseConverted(d, o, func(n *Node, _ *WorkControl, _ error) *Node { return n })
}

// DocumentParseConverted keeps Node parsing and final typed conversion inside
// the same work-controlled invocation and memo cleanup boundary.
//
// DocumentParseConverted is for generated parsers; not a stable API.
func DocumentParseConverted[T any](d *documentState, o parseOptions, convert func(*Node, *WorkControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	if d.root == nil {
		return res, fmt.Errorf("document start rule is missing")
	}
	o = prepareOptions(o)
	p := &Parser{unit: d.unit, MaxDepth: o.maxDepth, Control: o.control}
	p.setGrammar(d.root.g)
	p.Observation = newObservation(&o.observe)
	defer p.releaseObservation()
	if d.unit == Bytes {
		p.bs, p.n = d.text, len(d.text)
	} else {
		p.setSource(d.text)
	}
	p.Document, p.memo = d, d.memo
	d.stats = documentStats{}
	defer func() {
		if x := recover(); x != nil {
			d.memo = memoTable{}
			panic(x)
		}
		d.memo = p.memo
	}()
	p.Control.Check()
	v, ok := p.Call(d.root, 0)
	p.Control.Check()
	var n *Node
	if ok && p.Pos == p.n {
		n = v
		if len(p.Recovered) > 0 {
			err = SyntaxErrors(p.Recovered)
		}
	} else {
		if ok {
			p.Expect(p.Pos, IdEndInput)
		}
		err = p.MakeError(p.farthest, p.exp[p.expBase:])
	}
	err = publishDocumentError(p, err)
	res = convert(n, o.control, err)
	p.Control.Check()
	return res, err
}

// publishDocumentError separates caller-owned diagnostics from private memo
// entries. Strings are immutable, but the error objects and slice containers
// must not be shared with a later invocation.
func publishDocumentError(p *Parser, err error) error {
	switch e := err.(type) {
	case SyntaxErrors:
		if e == nil {
			return e
		}
		p.valueWork(len(e))
		out := make(SyntaxErrors, len(e))
		for i, item := range e {
			out[i] = cloneDocumentError(p, item)
		}
		return out
	}
	// Ordinary failures are freshly assembled by MakeError, not memoized.
	return err
}

func cloneDocumentError(p *Parser, e *SyntaxError) *SyntaxError {
	p.StepWork()
	if e == nil {
		return nil
	}
	out := new(SyntaxError)
	*out = *e
	if e.Expected != nil {
		p.valueWork(len(e.Expected))
		out.Expected = make([]string, len(e.Expected))
		copyWork(p, out.Expected, e.Expected)
	}
	if e.Messages != nil {
		p.valueWork(len(e.Messages))
		out.Messages = make([]string, len(e.Messages))
		copyWork(p, out.Messages, e.Messages)
	}
	return out
}
