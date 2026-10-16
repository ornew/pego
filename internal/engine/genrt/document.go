package genrt

import (
	"fmt"
	"unicode/utf8"
)

// Document incrementally parses the configured start rule. It belongs to one
// goroutine; returned nodes may be shared with later parses.
type Document struct {
	state     *documentState
	defaults  parseOptions
	typedMemo interface{ invalidate(int) }
}

// ParseStats counts rule evaluations and reused memo results in the last Parse.
type ParseStats struct {
	Evaluated int
	Reused    int
}

func makeDocument(root *rule, text string, opts []ParseOption) (d *Document, err error) {
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	defer recoverParse(&d, &err)
	o = prepareOptions(o)
	// Construction performs the same input decoding and work checks as a parse,
	// but its counters do not carry into subsequent invocations.
	p := &parser{unit: o.unit, control: o.control}
	if o.unit == CodePoints {
		p.setSource(text)
	} else if o.control != nil {
		p.inputWork(uint64(len(text)))
	}
	o.control.check()
	o.control, o.prepared = nil, false
	return &Document{state: newDocumentState(root, text, o.unit), defaults: o}, nil
}

// Text returns the current exact input, including invalid UTF-8 bytes.
func (d *Document) Text() string { return d.state.text }

// Edit replaces [start,end) in the Document's position unit.
func (d *Document) Edit(start, end int, text string) error {
	reuse, err := d.state.editReuse(start, end, text)
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
	o, err := d.resolveParseOptions(opts)
	if err != nil {
		return nil, err
	}
	return d.state.parse(o)
}

func (d *Document) resolveParseOptions(opts []ParseOption) (parseOptions, error) {
	o := d.defaults
	for _, opt := range opts {
		opt(&o)
	}
	if o.maxDepth < 0 {
		return o, fmt.Errorf("max depth must be non-negative: %d", o.maxDepth)
	}
	if o.maxDepth == 0 {
		o.maxDepth = defaultMaxDepth
	}
	if !workControlsEnabled && (o.context != nil || o.limits != (WorkLimits{})) {
		return o, fmt.Errorf("work controls are disabled in this generated reference parser")
	}
	return o, nil
}

// Stats returns the evaluation and reuse counts of the last Parse.
func (d *Document) Stats() ParseStats {
	s := d.state.stats
	return ParseStats{Evaluated: s.Evaluated, Reused: s.Reused}
}

// Clone copies a Node graph while preserving shared Node identities and repeated
// []any or []*Node slice views. It does not clone arbitrary mutable action values.
func (n *Node) Clone() *Node {
	state := documentCloneState{
		nodes:  make(map[*Node]*Node),
		slices: make(map[documentSliceKey]any),
	}
	return cloneDocumentNode(n, &state)
}

type documentCloneState struct {
	nodes  map[*Node]*Node
	slices map[documentSliceKey]any
}

// A slice's first element, length and capacity identify an exact slice view.
// The element pointer keeps the backing array visible to the collector while
// cloning. Empty slices without capacity have no elements or mutable backing
// array to share.
type documentSliceKey struct {
	first    any
	length   int
	capacity int
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
	if n.Children != nil {
		c.Children = cloneDocumentValue(n.Children, state).([]*Node)
	}
	if n.Fields != nil {
		c.Fields = make(Fields, len(n.Fields))
		for i, field := range n.Fields {
			field.Value = cloneDocumentValue(field.Value, state)
			c.Fields[i] = field
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
			key := documentSliceKey{first: &v[:1][0], length: len(v), capacity: cap(v)}
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
			key := documentSliceKey{first: &v[:1][0], length: len(v), capacity: cap(v)}
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
func newDocumentState(root *rule, text string, unit Unit) *documentState {
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
	return documentParseConverted(d, o, func(n *Node, _ *workControl, _ error) *Node { return n })
}

// documentParseConverted keeps Node parsing and final typed conversion inside
// the same work-controlled invocation and memo cleanup boundary.
func documentParseConverted[T any](d *documentState, o parseOptions, convert func(*Node, *workControl, error) T) (res T, err error) {
	defer recoverParse(&res, &err)
	if o.unit != d.unit {
		return res, fmt.Errorf("document parse cannot change unit")
	}
	if d.root == nil {
		return res, fmt.Errorf("document start rule is missing")
	}
	o = prepareOptions(o)
	p := &parser{unit: d.unit, maxDepth: o.maxDepth, control: o.control}
	if d.unit == Bytes {
		p.bs, p.n = d.text, len(d.text)
	} else {
		p.setSource(d.text)
	}
	p.document, p.memo = d, d.memo
	d.stats = documentStats{}
	defer func() {
		if x := recover(); x != nil {
			d.memo = memoTable{}
			panic(x)
		}
		d.memo = p.memo
	}()
	p.control.check()
	v, ok := p.call(d.root, 0)
	p.control.check()
	var n *Node
	if ok && p.pos == p.n {
		n = v
		if len(p.recovered) > 0 {
			err = SyntaxErrors(p.recovered)
		}
	} else {
		if ok {
			p.expect(p.pos, idEndInput)
		}
		err = p.makeError(p.farthest, p.exp[p.expBase:])
	}
	err = publishDocumentError(p, err)
	res = convert(n, o.control, err)
	p.control.check()
	return res, err
}

// publishDocumentError separates caller-owned diagnostics from private memo
// entries. Strings are immutable, but the error objects and slice containers
// must not be shared with a later invocation.
func publishDocumentError(p *parser, err error) error {
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
	// Ordinary failures are freshly assembled by makeError, not memoized.
	return err
}

func cloneDocumentError(p *parser, e *SyntaxError) *SyntaxError {
	p.stepWork()
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
