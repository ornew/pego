package runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// The typed runtime, appended to generated parsers made with pego gen -types. It parses like the
// runtime in runtime.go, but builds the values of the grammar's types directly, as Go values,
// instead of Node trees: the value of an expression is an any holding a pointer to a struct
// type, *TMatch (a string node), *TError (an error value), an int, string or bool, nil,
// or a *TSeq: a tuple node, a list node, or a tuple or list value. Values other than
// nodes need no carrier, as they do in Node trees. Every step mirrors runtime.go, so the result is
// that of converting the Node tree that Parse returns.
//
// TParser embeds the parser and redefines the methods that handle values; the generated
// methods of the typed rules (trules) run on it.

// Span is the range of input a typed value covers, in the parse's position unit (End is exclusive).
//
// Of its fields and methods, TSetSpan, TSpan are for generated parsers; not a stable API.
type Span struct{ Start, End int }

// Range returns the start and the end (exclusive) of the span.
func (s Span) Range() (start, end int) { return s.Start, s.End }

func (s *Span) TSpan() (int, int)       { return s.Start, s.End }
func (s *Span) TSetSpan(start, end int) { s.Start, s.End = start, end }

// TNode is a node whose Value has type T: a string node (TNode[string]) holds the text a
// literal, a character class, ., a #atomic or _ matched, and a tuple or list node holds its
// elements. A node of a statically unknown value is a TNode[any] whose Value is a string, or a
// []any of the elements of a tuple or list.
type TNode[T any] struct {
	Span
	Value T
}

// Error is input skipped by error recovery (#recover).
type Error struct {
	Span
	Text    string
	Message string
}

// TMatch and TError are a string node and an Error while the typed runtime handles them: whether
// the value is fresh (see Node.RawFresh) is part of the state of the parse, not of the result. The
// result holds pointers to the embedded TNode[string] and Error (TPub). A string node of a named
// node type has nt, the type's registered index (TRegisterNodeType; 0 for a plain string node).
//
// TMatch is for generated parsers; not a stable API.
type TMatch struct {
	TNode[string]
	fresh bool
	Nt    uint16
}

// tnodeType is a registered named node type: its name, and pub, which returns the public form of
// a value x of the type with the generated code's chunks a (a *tslabs, or nil).
type tnodeType struct {
	name string
	pub  func(a any, x TVal) any
}

// tnodeTypes holds the registered named node types of all grammars by their index (nt) in TMatch
// and TSeq; index 0 is no named node type. Registration copies the table, so readers never
// lock.
var (
	tnodeTypes   atomic.Pointer[[]tnodeType]
	tnodeTypesMu sync.Mutex
)

// TRegisterNodeType registers a named node type of a grammar and returns its index. Types of
// different grammars have different indexes even when their names are the same.
//
// TRegisterNodeType is for generated parsers; not a stable API.
func TRegisterNodeType(name string, pub func(a any, x TVal) any) uint16 {
	tnodeTypesMu.Lock()
	defer tnodeTypesMu.Unlock()
	old := []tnodeType{{}}
	if p := tnodeTypes.Load(); p != nil {
		old = *p
	}
	if len(old) > 1<<16-1 {
		panic("too many named node types")
	}
	types := append(old[:len(old):len(old)], tnodeType{name, pub})
	tnodeTypes.Store(&types)
	return uint16(len(types) - 1)
}

// tnodeTypeOf returns the registered named node type nt (not 0).
func tnodeTypeOf(nt uint16) *tnodeType {
	return &(*tnodeTypes.Load())[nt]
}

// TError is for generated parsers; not a stable API.
type TError struct {
	Error
	fresh bool
}

// NewTError returns a fresh error value for the input range [start, end) that recovery skipped.
//
// NewTError is for generated parsers; not a stable API.
func NewTError(start, end int, text, message string) *TError {
	return &TError{Error{Span: Span{Start: start, End: end}, Text: text, Message: message}, true}
}

// TPub returns the public form of a value: *TNode[string] and *Error for TMatch and TError, a
// *TNode[any] for a tuple or list node and a []any for a tuple or list value, with the public
// forms of their elements.
//
// TPub is for generated parsers; not a stable API.
func TPub(h *TPubHooks, v any) any { return TPubIn(h, nil, v) }

// TPubIn is TPub with the generated code's chunks a (a *tslabs, or nil), which the public forms of
// values of named node types (tnodeType.pub) are made from and remembered in.
//
// TPubIn is for generated parsers; not a stable API.
func TPubIn(h *TPubHooks, a any, v any) any {
	switch x := v.(type) {
	case *TMatch:
		if x.Nt != 0 {
			return tnodeTypeOf(x.Nt).pub(a, x)
		}
		return h.Match(&x.TNode)
	case *TError:
		return h.Error(&x.Error)
	case *TSeq:
		if x.nt != 0 {
			return tnodeTypeOf(x.nt).pub(a, x)
		}
		return tpubElems(h, a, x)
	}
	return v
}

// TPubHooks makes the public forms of the values the typed runtime builds that the generated
// package defines types for: a string node, an error value and a node of a statically unknown
// value are public values of the package's own types, which implement the package's unions.
// Back is the inverse: it returns the runtime's form of such a public value, and any other value
// unchanged.
//
// TPubHooks is for generated parsers; not a stable API.
type TPubHooks struct {
	Match func(*TNode[string]) any
	Error func(*Error) any
	Node  func(*TNode[any]) any
	Back  func(any) any
}

// tpubElems returns the public form of a tuple or list node, or a tuple or list value, x as a
// plain node or value (also for a node of a named node type).
func tpubElems(h *TPubHooks, a any, x *TSeq) any {
	elems := tpubKids(h, a, x)
	if x.Typ == TypeTupleValue || x.Typ == TypeListValue {
		return elems
	}
	return h.Node(&TNode[any]{x.Span, elems})
}

// tpubKids returns the public forms of the elements of x.
func tpubKids(h *TPubHooks, a any, x *TSeq) []any {
	elems := make([]any, len(x.Kids))
	for i, k := range x.Kids {
		elems[i] = TPubIn(h, a, k)
	}
	return elems
}

// TPubNode returns the public form of a node of a statically unknown value: a TNode[any], also for
// a node of a named node type, as the node it was converted from.
//
// TPubNode is for generated parsers; not a stable API.
func TPubNode(h *TPubHooks, v any) *TNode[any] { return TPubNodeIn(h, nil, v) }

// TPubNodeIn is TPubNode with the chunks a (see TPubIn).
//
// TPubNodeIn is for generated parsers; not a stable API.
func TPubNodeIn(h *TPubHooks, a any, v any) *TNode[any] {
	switch x := v.(type) {
	case *TMatch:
		return &TNode[any]{x.Span, x.Value}
	case *TSeq:
		if x.Typ != TypeTupleValue && x.Typ != TypeListValue {
			return &TNode[any]{x.Span, tpubKids(h, a, x)}
		}
	}
	return nil
}

// TMatchAs returns the public form of x, a string node of a named node type whose Go type N is
// struct{ TNode[string] }: N has the layout of TNode[string] (the generated code asserts their
// sizes are equal), so the public value is x's own TNode seen as an N. A node so publishes as one
// value wherever it appears, as a plain string node does, without allocating or remembering it.
//
// TMatchAs is for generated parsers; not a stable API.
func TMatchAs[N any](x *TMatch) *N {
	return (*N)(unsafe.Pointer(&x.TNode))
}

// TPubMatch is for generated parsers; not a stable API.
func TPubMatch(v any) *TNode[string] {
	if x, ok := v.(*TMatch); ok {
		return &x.TNode
	}
	return nil
}

// TPubError is for generated parsers; not a stable API.
func TPubError(v any) *Error {
	if x, ok := v.(*TError); ok {
		return &x.Error
	}
	return nil
}

// workControlSetter is implemented by the output values a typed parse hands its work control to
// (the generated converters).
//
// SetWorkControl is for generated parsers; not a stable API.
type workControlSetter interface{ SetWorkControl(*WorkControl) }

// tfieldWorker is implemented by struct values that read a field while charging work.
//
// TFieldWork is for generated parsers; not a stable API.
type tfieldWorker interface {
	TFieldWork(*TParser, string) (any, bool)
}

// stepCharger charges steps to a work control.
//
// Step is for generated parsers; not a stable API.
type stepCharger interface{ Step(uint64) }

// TVal is a node value of the typed runtime: a value of a struct type, *TMatch,
// *TError or *TSeq.
//
// TVal is for generated parsers; not a stable API.
type TVal interface {
	TSpan() (int, int)
	TSetSpan(start, end int)
	TName() string                  // type name
	TText() (string, bool)          // text of a string node or an error value
	TKids() []any                   // children of a CST node
	TField(name string) (any, bool) // field value, and whether the type has the field (structs)
	TStruct() bool                  // a struct type
	TFresh() bool
	TSetFresh(bool)
}

func (m *TMatch) TName() string {
	if m.Nt != 0 {
		return tnodeTypeOf(m.Nt).name
	}
	return "string"
}
func (m *TMatch) TText() (string, bool)     { return m.Value, true }
func (m *TMatch) TKids() []any              { return nil }
func (m *TMatch) TField(string) (any, bool) { return nil, true }
func (m *TMatch) TStruct() bool             { return false }
func (m *TMatch) TFresh() bool              { return m.fresh }
func (m *TMatch) TSetFresh(f bool)          { m.fresh = f }
func (e *TError) TName() string             { return "error" }
func (e *TError) TText() (string, bool)     { return e.Text, true }
func (e *TError) TKids() []any              { return nil }
func (e *TError) TStruct() bool             { return false }
func (e *TError) TFresh() bool              { return e.fresh }
func (e *TError) TSetFresh(f bool)          { e.fresh = f }
func (e *TError) TField(name string) (any, bool) {
	if name == "message" {
		return e.Message, true
	}
	return nil, true
}

// TSeq is a tuple node, a list node, a tuple value (TypeTupleValue) or a list value
// (TypeListValue) of the typed runtime. A tuple node's fields are its captures. A tuple or list
// node of a named node type has nt, as TMatch does; typ stays "tuple" or "list".
//
// TSeq is for generated parsers; not a stable API.
type TSeq struct {
	Span
	Typ    string
	Kids   []any
	fields []tfield
	fresh  bool
	nt     uint16
}

type tfield struct {
	name string
	val  any
}

func (n *TSeq) TName() string {
	if n.nt != 0 {
		return tnodeTypeOf(n.nt).name
	}
	return n.Typ
}
func (n *TSeq) TText() (string, bool) { return "", false }
func (n *TSeq) TKids() []any          { return n.Kids }
func (n *TSeq) TStruct() bool         { return false }
func (n *TSeq) TFresh() bool          { return n.fresh }
func (n *TSeq) TSetFresh(f bool)      { n.fresh = f }
func (n *TSeq) TField(name string) (any, bool) {
	for _, f := range n.fields {
		if f.name == name {
			return f.val, true
		}
	}
	return nil, true
}

func (n *TSeq) set(name string, v any) {
	for i := range n.fields {
		if n.fields[i].name == name {
			n.fields[i].val = v
			return
		}
	}
	n.fields = append(n.fields, tfield{name, v})
}

// tshape is Node.shape for the values of the typed runtime that are TVal.
func tshape(n TVal) nodeShape {
	switch x := n.(type) {
	case *TError:
		return shapeError
	case *TSeq:
		switch x.Typ {
		case "list":
			return shapeList
		case TypeTupleValue:
			return shapeTupleValue
		case TypeListValue:
			return shapeListValue
		}
		return shapeTuple
	}
	if n.TStruct() {
		return shapeStruct
	}
	return shapeString
}

// AsTval returns v as a node value, or nil. Values never hold nil pointers: nil is always the
// untyped nil.
//
// AsTval is for generated parsers; not a stable API.
func AsTval(v any) TVal {
	n, _ := v.(TVal)
	return n
}

// tisNode reports whether n is a node of the value model (a string node, a tuple or list node,
// or an error value) rather than a struct or a list or tuple value; see Node.isNode.
func tisNode(n TVal) bool {
	if t, ok := n.(*TSeq); ok {
		return t.Typ == "tuple" || t.Typ == "list"
	}
	return !n.TStruct()
}

// nodeValue returns the Value of the node n: its text for a string node or an error value, and a
// tuple or list value sharing its elements otherwise (see parser.nodeValue).
func (p *TParser) nodeValue(n TVal) any {
	if text, ok := n.TText(); ok {
		return text
	}
	s, e := n.TSpan()
	typ := TypeTupleValue
	if t, ok := n.(*TSeq); ok && t.Typ == "list" {
		typ = TypeListValue
	}
	v := p.NewNode(typ, s, e, n.TKids())
	v.fresh = false
	return v
}

// --- Rules ---

// RuleSpec is the compact form of the analysis results of a rule, which generated parsers keep as
// static data. Its Id is its position in the table. Scope and Vars index the string lists given to
// NewRules, 0 for none; a nil Value is the zero Shaper.
//
// For generated parsers; not a stable API.
type RuleSpec struct {
	Name  string
	Value *Shaper
	Flags uint8
	Seen  int32
	Scope uint16
	Vars  uint16
}

// The flags of a RuleSpec set the fields of a Rule of the same names.
const (
	RuleMemo uint8 = 1 << iota
	RuleLeader
	RuleNovalue
	RuleDocMemo
	RuleSplice
)

// RuleExtra holds the fields of the Rule with the Id that are rarely set.
//
// For generated parsers; not a stable API.
type RuleExtra struct {
	Id         int
	Where      string
	NodeType   string
	OpNodeType string
}

// NewRules builds the rule table of the specs, with the string lists their Scope and Vars index and
// the rare fields in extras.
//
// For generated parsers; not a stable API.
func NewRules(specs []RuleSpec, strs [][]string, extras []RuleExtra) []*Rule {
	table := make([]Rule, len(specs))
	rules := make([]*Rule, len(specs))
	for i, s := range specs {
		r := &table[i]
		rules[i] = r
		r.Id, r.Name, r.Seen = i, s.Name, int(s.Seen)
		if s.Value != nil {
			r.Value = *s.Value
		}
		r.Memo, r.Leader, r.Novalue = s.Flags&RuleMemo != 0, s.Flags&RuleLeader != 0, s.Flags&RuleNovalue != 0
		r.DocMemo, r.Splice = s.Flags&RuleDocMemo != 0, s.Flags&RuleSplice != 0
		r.Scope, r.Vars = strs[s.Scope], strs[s.Vars]
	}
	for _, e := range extras {
		r := rules[e.Id]
		r.Where, r.NodeType, r.OpNodeType = e.Where, e.NodeType, e.OpNodeType
	}
	return rules
}

// NewTRules returns the typed rules that share the analysis results of rules, one for one.
//
// For generated parsers; not a stable API.
func NewTRules(rules []*Rule) []*TRule {
	table := make([]TRule, len(rules))
	trules := make([]*TRule, len(rules))
	for i, r := range rules {
		table[i].Rule = r
		trules[i] = &table[i]
	}
	return trules
}

// TRule is a rule of the typed runtime: the rule's analysis results and its typed body.
//
// TRule is for generated parsers; not a stable API.
type TRule struct {
	*Rule
	Body          func(p *TParser, min int) (any, bool)
	Action        func(c *TActCtx) any
	OperatorsRule *TOperatorsRule
	// Entry is the generated method that calls the rule as call(r, 0) does (u<id> or s<id>). The
	// generated parser sets it only for the start rules, which TParse runs through it; every other
	// rule is called through the generated methods that name it. A direct rule (see gen_direct.go)
	// has no body or action and is called only through it.
	Entry func(p *TParser) (any, bool)
	// Nt is the registered index of the named node type the rule's value is converted to
	// (rule.NodeType), or 0.
	Nt uint16
	// OpNt is the registered index of the named node type every operator node of an Operators
	// Rule is converted to (rule.OpNodeType), or 0.
	OpNt uint16
}

// TAs returns v as a T, or the zero T (nil) if it is not one.
//
// TAs is for generated parsers; not a stable API.
func TAs[T any](v any) T {
	t, _ := v.(T)
	return t
}

// TPtr and TDeref return the value of a field of a pointer type as a value of the typed runtime.
//
// TPtr is for generated parsers; not a stable API.
func TPtr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return p
}

// TBack returns the value of a field of an open type (a string node, a tuple or list node, an
// error value, a union, node or any) as a value of the typed runtime: the inverse of
// TPub, except that a tuple or list node becomes a tuple node and a []any a tuple value.
//
// TBack is for generated parsers; not a stable API.
func TBack(h *TPubHooks, v any) any {
	v = h.Back(v)
	switch x := v.(type) {
	case *TNode[string]:
		if x == nil {
			return nil
		}
		return &TMatch{TNode: *x}
	case *Error:
		if x == nil {
			return nil
		}
		return &TError{Error: *x}
	case *TNode[any]:
		if x == nil {
			return nil
		}
		switch y := x.Value.(type) {
		case string:
			return &TMatch{TNode: TNode[string]{x.Span, y}}
		case []any:
			return &TSeq{Span: x.Span, Typ: "tuple", Kids: tbackAll(h, y)}
		}
		return nil
	case []any:
		return &TSeq{Typ: TypeTupleValue, Kids: tbackAll(h, x)}
	}
	return v
}

// TKid returns element i of a tuple's elements (nil if there is none).
//
// TKid is for generated parsers; not a stable API.
func TKid(kids []any, i int) any {
	if i < len(kids) {
		return kids[i]
	}
	return nil
}

// tnodeValue returns the Value of a tuple or list node as a tuple or list value.
func tnodeValue(n *TSeq) any {
	typ := TypeTupleValue
	if n.Typ == "list" {
		typ = TypeListValue
	}
	return &TSeq{Span: n.Span, Typ: typ, Kids: n.Kids}
}

func tbackAll(h *TPubHooks, vs []any) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = TBack(h, v)
	}
	return out
}

func TDeref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// TListOf returns a list value of kids, for reading a list field of a struct in an action.
//
// TListOf is for generated parsers; not a stable API.
func TListOf(kids []any) any {
	n := &TSeq{Typ: TypeListValue, Kids: kids}
	first := true
	for _, it := range kids {
		t := AsTval(it)
		if t == nil {
			continue
		}
		s, e := t.TSpan()
		if first || s < n.Start {
			n.Start = s
		}
		if first || e > n.End {
			n.End = e
		}
		first = false
	}
	return n
}

// TParse parses the whole input with the typed rule r, and returns the result converted by conv
// (before the parser's scratch memory, which the result may be in, is cleared). ext holds the
// generated code's chunks.
//
// TParse is for generated parsers; not a stable API.
func TParse[T any](r *TRule, input string, opts []ParseOption, ext any, conv func(any) T) (T, error) {
	o, err := ResolveOptions(r.g, opts)
	if err != nil {
		var zero T
		return zero, err
	}
	return TParseResolved(r, input, o, ext, conv)
}

// TParseResolved consumes options already applied by the public entry point.
//
// TParseResolved is for generated parsers; not a stable API.
func TParseResolved[T any](r *TRule, input string, o parseOptions, ext any, conv func(any) T) (res T, err error) {
	defer recoverParse(&res, &err)
	o = prepareOptions(o)
	p, _ := tpool.Get().(*TParser)
	if p == nil {
		p = &TParser{Parser: &Parser{}}
	}
	defer func() { p.recycle(); tpool.Put(p) }()
	p.Control = o.control
	p.Observation = newObservation(&o.observe)
	if o.control != nil {
		if target, ok := ext.(workControlSetter); ok {
			target.SetWorkControl(o.control)
			defer target.SetWorkControl(nil)
		}
	}
	v, err := p.run(r, input, o, ext)
	if v == nil {
		return res, err
	}
	o.control.Check()
	res = conv(v)
	o.control.Check()
	return res, err
}

// run parses the whole input with the typed rule r.
func (p *TParser) run(r *TRule, input string, o parseOptions, ext any) (v any, err error) {
	p.MaxDepth = o.maxDepth
	p.Ext = ext
	p.setGrammar(r.g)
	if o.unit == Bytes {
		// p.In stays empty: direct rules read code points from it while p.Pos < len(p.In).
		p.unit, p.bs, p.n, p.In = Bytes, input, len(input), p.In[:0]
	} else {
		p.setSource(input)
	}
	if p.TypedDocument != nil {
		p.Document = p.TypedDocument.owner
	}
	p.memo.sparse = p.memo.stride > 64 && p.n > 1024
	defer func() {
		if x := recover(); x != nil {
			switch x := x.(type) {
			case fatal:
				v, err = nil, x.err
			case evalError: // in an action (predicates recover their own)
				v, err = nil, p.wrapEvalError(p.Where, x.msg)
			default:
				panic(x)
			}
		}
	}()
	var ok bool
	if r.Entry != nil {
		v, ok = r.Entry(p)
	} else {
		v, ok = p.Call(r, 0)
	}
	// Normal completion polls even for a nil typed value. Fatal unwinding
	// returns through the recovery above without replacing its established error.
	p.Control.Check()
	if ok && p.Pos == p.n {
		if len(p.Recovered) > 0 {
			errors := p.copyErrors(nil, p.Recovered)
			p.Control.Check()
			return v, SyntaxErrors(errors)
		}
		return v, nil
	}
	if ok {
		p.Expect(p.Pos, IdEndInput)
	}
	e := p.MakeError(p.farthest, p.exp[p.expBase:])
	p.Control.Check()
	return nil, e
}

// tpool keeps parsers of the typed runtime, with their scratch memory, for later parses. What a
// parse returns never refers to that memory: CST nodes, value lists, frames and memo entries are
// consumed by the parse itself (typed values come from the generated code's chunks, ext).
var tpool sync.Pool

// recycle clears the parser for the next parse, keeping its scratch memory.
func (p *TParser) recycle() {
	q := p.Parser
	memo := memoTable{seen: q.memo.seen, calls: q.memo.calls, pages: q.memo.pages, bitPages: q.memo.bitPages}
	// Retain useful sparse pages for large inputs, capped at 32 MiB of backing
	// storage. The next small parse trims this cache when it finishes.
	memo.resetSeen(max(1<<20, min(q.n, 4<<20)))
	in, offs := q.In[:0], q.offs[:0]
	if cap(in) > 1<<20 { // do not keep the buffers of a large input for every later parse
		in, offs = nil, nil
	}
	*q = Parser{In: in, offs: offs, exp: q.exp[:0], arena: q.arena[:0]}
	q.memo = memo
	p.nodes.reset()
	p.vals.reset()
	p.frames.reset()
	p.fields.reset()
	p.memoSlab.reset()
	clear(p.Trail[:cap(p.Trail)])
	clear(p.KidStack)
	clear(p.saved[:cap(p.saved)])
	clear(p.memoSlots)
	free := p.free
	for i := range free {
		free[i] = free[i][:0]
	}
	*p = TParser{Parser: q, Trail: p.Trail[:0], KidStack: p.KidStack[:0], saved: p.saved[:0], memoSlots: p.memoSlots[:0],
		nodes: p.nodes, vals: p.vals, frames: p.frames, fields: p.fields, memoSlab: p.memoSlab, free: free}
}

// tarena allocates values of type T in chunks that are kept from one parse to the next (tpool):
// reset clears what was used and starts again at the first chunk.
type tarena[T any] struct {
	chunks [][]T
	ci     int // the chunk in use
	off    int // the next free element in it
}

const tchunk = 512

// slice returns n zero elements (capacity n).
func (a *tarena[T]) slice(n int) []T {
	if n > tchunk/4 {
		return make([]T, n)
	}
	if a.ci < len(a.chunks) && a.off+n > len(a.chunks[a.ci]) {
		a.ci++
		a.off = 0
	}
	if a.ci == len(a.chunks) {
		a.chunks = append(a.chunks, make([]T, tchunk))
	}
	s := a.chunks[a.ci][a.off : a.off+n : a.off+n]
	a.off += n
	return s
}

func (a *tarena[T]) one() *T { return &a.slice(1)[0] }

func (a *tarena[T]) reset() {
	for i := 0; i <= a.ci && i < len(a.chunks); i++ {
		clear(a.chunks[i])
	}
	a.ci, a.off = 0, 0
}

// TParser is the parser state of the typed runtime.
//
// TParser is for generated parsers; not a stable API.
type TParser struct {
	*Parser
	Ext      any    // the generated code's allocation chunks
	Where    string // the rule whose action is being evaluated
	Frame    *TCaptureFrame
	Trail    []tundo
	Created  []TVal // struct values made by the action being evaluated
	KidStack []any
	ac       TActCtx
	saved    []any
	matches  []TMatch // returned in values, so not kept for the next parse
	// Scratch memory, kept for the next parse (recycle).
	nodes     tarena[TSeq]
	vals      tarena[any]
	frames    tarena[TCaptureFrame]
	fields    tarena[tfield]
	memoSlab  tarena[tmemoEntry]
	memoSlots []*tmemoEntry // memo entries by position (chains)
	// TypedDocument is set only by typed Document parses (TypedDocumentParse).
	TypedDocument *typedDocumentState
	free          [8][]*TCaptureFrame // frames freed by FreeFrame, by size
}

// TCaptureFrame is for generated parsers; not a stable API.
type TCaptureFrame struct{ Vals []any }

// FreeFrame makes the frame f, which nothing refers to any more, available to NewFrame. A rule's
// frame is dead when the rule returns: the values have been copied into its result, and the
// trail entries that refer to it are gone.
func (p *TParser) FreeFrame(f *TCaptureFrame) {
	n := len(f.Vals)
	if n == 0 || n >= len(p.free) {
		return
	}
	clear(f.Vals)
	p.free[n] = append(p.free[n], f)
}

// EmptyTFrame is for generated parsers; not a stable API.
var EmptyTFrame = &TCaptureFrame{}

type tundo struct {
	f    *TCaptureFrame
	slot int
	old  any
}

func (p *TParser) Mark() Mark {
	return Mark{Pos: p.Pos, env: p.Env, Trail: len(p.Trail), recovered: len(p.Recovered)}
}

func (p *TParser) Reset(m Mark) {
	p.Pos = m.Pos
	p.Env = m.env
	p.Recovered = p.Recovered[:m.recovered]
	p.undoCaptures(m.Trail)
	p.DropTrail(m.Trail)
}

// DropTrail discards undo history whose effects are already restored or whose
// frame is returning. Clear its references before truncating the backing slice.
func (p *TParser) DropTrail(base int) {
	clear(p.Trail[base:])
	p.Trail = p.Trail[:base]
}

func (p *TParser) SetCapture(slot int, v any) {
	p.StepWork()
	p.Trail = append(p.Trail, tundo{f: p.Frame, slot: slot, old: p.Frame.Vals[slot]})
	p.Frame.Vals[slot] = v
}

// SetCaptureOrdinary is SetCapture in an ordinary parse (parser.ExpectOrdinary); so are
// NewNodeOrdinary and NewMatchOrdinary.
func (p *TParser) SetCaptureOrdinary(slot int, v any) {
	p.Trail = append(p.Trail, tundo{f: p.Frame, slot: slot, old: p.Frame.Vals[slot]})
	p.Frame.Vals[slot] = v
}

// --- Allocation ---

func (p *TParser) NewNode(typ string, start, end int, kids []any) *TSeq {
	p.StepWork()
	n := p.nodes.one()
	*n = TSeq{Span: Span{start, end}, Typ: typ, Kids: kids, fresh: true}
	return n
}

func (p *TParser) NewMatch(start, end int, text string, fresh bool) *TMatch {
	p.StepWork()
	if len(p.matches) == 0 {
		p.matches = make([]TMatch, nodeChunk)
	}
	m := &p.matches[0]
	p.matches = p.matches[1:]
	*m = TMatch{TNode: TNode[string]{Span{start, end}, text}, fresh: fresh}
	return m
}

func (p *TParser) NewNodeOrdinary(typ string, start, end int, kids []any) *TSeq {
	n := p.nodes.one()
	*n = TSeq{Span: Span{start, end}, Typ: typ, Kids: kids, fresh: true}
	return n
}

func (p *TParser) NewMatchOrdinary(start, end int, text string, fresh bool) *TMatch {
	if len(p.matches) == 0 {
		p.matches = make([]TMatch, nodeChunk)
	}
	m := &p.matches[0]
	p.matches = p.matches[1:]
	*m = TMatch{TNode: TNode[string]{Span{start, end}, text}, fresh: fresh}
	return m
}

// NewVals returns a value list of length and capacity n.
func (p *TParser) NewVals(n int) []any {
	if n == 0 {
		return []any{}
	}
	return p.vals.slice(n)
}

func (p *TParser) tfields(n int) []tfield {
	return p.fields.slice(n)[:0]
}

// Kids returns p.KidStack[base:] as a value list and pops it.
func (p *TParser) Kids(base int) []any {
	ks := p.NewVals(len(p.KidStack) - base)
	copyWork(p.Parser, ks, p.KidStack[base:])
	p.DropKids(base)
	return ks
}

func (p *TParser) DropKids(base int) {
	clear(p.KidStack[base:])
	p.KidStack = p.KidStack[:base]
}

func (p *TParser) NewFrame(n int) *TCaptureFrame {
	if n == 0 {
		return EmptyTFrame
	}
	if n < len(p.free) {
		if fs := p.free[n]; len(fs) > 0 {
			f := fs[len(fs)-1]
			p.free[n] = fs[:len(fs)-1]
			return f
		}
	}
	f := p.frames.one()
	f.Vals = p.NewVals(n)
	return f
}

// --- Memo ---

// The memo of typed results (TParser.memoSlots, chains of entries by position) is keyed like
// memoTable, whose FirstCall bookkeeping the typed runtime shares.
type tmemoEntry struct {
	val      any
	errs     []*SyntaxError
	expected []ExpID
	next     *tmemoEntry
	env      []any
	end      int
	far      int
	rule     int32
	min      int32
	ok       bool
	silent   bool
	growing  bool
}

func (p *TParser) memoGet(rule, pos, min int, env []any) *tmemoEntry {
	if WorkControlsEnabled && p.Control != nil {
		return p.memoGetControlled(rule, pos, min, env)
	}
	if pos >= len(p.memoSlots) {
		return nil
	}
	for e := p.memoSlots[pos]; e != nil; e = e.next {
		if e.rule == int32(rule) && e.min == int32(min) && sameValues(e.env, env) {
			return e
		}
	}
	return nil
}

func (p *TParser) memoGetControlled(rule, pos, min int, key []any) *tmemoEntry {
	p.StepWork()
	if pos >= len(p.memoSlots) {
		return nil
	}
	for e := p.memoSlots[pos]; e != nil; e = e.next {
		p.StepWork()
		if e.rule == int32(rule) && e.min == int32(min) && p.memoKeyEqual(e.env, key) {
			return e
		}
	}
	return nil
}

func (p *TParser) memoPut(rule, pos, min int, env []any, e *tmemoEntry) {
	if WorkControlsEnabled && p.Control != nil {
		p.memoPutControlled(rule, pos, min, env, e)
		return
	}
	if pos >= len(p.memoSlots) {
		if pos < cap(p.memoSlots) {
			p.memoSlots = p.memoSlots[:pos+1]
		} else {
			grown := make([]*tmemoEntry, pos+1, max(2*cap(p.memoSlots), pos+1, 64))
			copy(grown, p.memoSlots)
			p.memoSlots = grown
		}
	}
	e.rule, e.min, e.env = int32(rule), int32(min), env
	link := &p.memoSlots[pos]
	for x := *link; x != nil; link, x = &x.next, x.next {
		if x.rule == e.rule && x.min == e.min && sameValues(x.env, env) {
			e.next = x.next
			*link = e
			return
		}
	}
	e.next = p.memoSlots[pos]
	p.memoSlots[pos] = e
}

func (p *TParser) memoPutControlled(rule, pos, min int, key []any, e *tmemoEntry) {
	p.StepWork()
	slots := p.memoSlots
	if pos >= len(slots) {
		if pos < cap(slots) {
			slots = slots[:pos+1]
		} else {
			grown := make([]*tmemoEntry, pos+1, max(2*cap(slots), pos+1, 64))
			copyWork(p.Parser, grown, slots)
			slots = grown
		}
	}
	link := &slots[pos]
	var replaced *tmemoEntry
	for x := *link; x != nil; link, x = &x.next, x.next {
		p.StepWork()
		if x == e || x.rule == int32(rule) && x.min == int32(min) && p.memoKeyEqual(x.env, key) {
			replaced = x
			break
		}
	}
	p.StepWork()
	p.memoSlots = slots
	e.rule, e.min, e.env = int32(rule), int32(min), key
	if replaced != nil {
		e.next = replaced.next
		*link = e
		return
	}
	e.next = slots[pos]
	slots[pos] = e
}

// --- Typed Document memo ---

// typedDocumentState owns private typed memo values of a typed Document (TypedDocumentParse). The
// fresh invocation arenas that contain those values must never be reset or returned to the batch
// pool.
type typedDocumentState struct {
	owner *documentState
	slots []*tmemoEntry
	meta  map[*tmemoEntry]typedDocumentMeta
}

type typedDocumentMeta struct {
	examined    int
	provisional bool
}

// invalidate drops the memo entries that an edit at start can change, relinking the chains in place.
// No parse runs during an edit (Document.parsing rejects it), and published values are copies, so no
// TParser or result refers to the chains while they change. Entries whose examined frontier reaches
// start, growing entries and provisional entries are dropped, as are all entries from start onward.
func (d *typedDocumentState) invalidate(start int) {
	kept := min(start, len(d.slots))
	for pos := kept; pos < len(d.slots); pos++ {
		for e := d.slots[pos]; e != nil; e = e.next {
			delete(d.meta, e)
		}
		d.slots[pos] = nil
	}
	for pos := range kept {
		link := &d.slots[pos]
		for e := *link; e != nil; e = *link {
			m, complete := d.meta[e]
			if complete && !e.growing && !m.provisional && m.examined <= start {
				link = &e.next
				continue
			}
			delete(d.meta, e)
			*link = e.next
		}
	}
	d.slots = d.slots[:kept]
}

// DocumentCall memoizes typed rule entries (rule.DocMemo), including generated
// direct wrappers. Examined and provisional metadata stays outside batch memo
// entries.
func (p *TParser) DocumentCall(r *TRule, min int, inv func(*TParser) (any, bool)) (any, bool) {
	p.StepWork()
	start := p.Pos
	oldHigh, oldProvisional := p.docHigh, p.docProvisional
	p.docHigh, p.docProvisional = start+1, false
	defer func() { p.docHigh = max(oldHigh, p.docHigh); p.docProvisional = oldProvisional || p.docProvisional }()
	if !r.DocMemo {
		rec := len(p.Recovered)
		var v any
		var ok bool
		if inv != nil {
			p.Document.stats.Evaluated++
			v, ok = inv(p)
		} else {
			v, ok = p.invoke(r, min)
		}
		if !ok {
			p.Pos = start
			p.Recovered = p.Recovered[:rec]
		}
		return v, ok
	}
	env := p.memoEnv(r.Rule)
	old := p.memoGet(r.Id, start, min, env)
	if old != nil && (old.growing || (!old.silent || p.Silent > 0) && (len(old.errs) == 0 || old.silent == (p.Silent > 0))) {
		m := p.TypedDocument.meta[old]
		p.documentTouch(m.examined)
		p.docProvisional = p.docProvisional || old.growing || m.provisional
		p.Document.stats.Reused++
		p.Observation.memoHit()
		if !old.growing {
			p.MergeExpected(old.far, old.expected)
		}
		if !old.ok {
			return nil, false
		}
		p.Pos = old.end
		p.Recovered = p.copyErrors(p.Recovered, old.errs)
		return old.val, true
	}
	// A context-incompatible entry may be replaced. Do not keep it alive through
	// stale metadata once its memo chain no longer refers to it.
	if old != nil {
		delete(p.TypedDocument.meta, old)
	}
	rec := len(p.Recovered)
	m := p.Isolate(start)
	var v any
	var ok bool
	var e *tmemoEntry
	if r.Leader {
		v, ok = p.grow(r, min, env)
		e = p.memoGet(r.Id, start, min, env)
	} else {
		if inv != nil {
			p.Document.stats.Evaluated++
			v, ok = inv(p)
		} else {
			v, ok = p.invoke(r, min)
		}
		if !ok {
			p.Recovered = p.Recovered[:rec]
		}
		e = p.memoSlab.one()
		*e = tmemoEntry{val: v, ok: ok, end: p.Pos, silent: p.Silent > 0, errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, min, env, e)
	}
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.documentTouch(p.Pos + 1)
	p.TypedDocument.meta[e] = typedDocumentMeta{examined: p.docHigh, provisional: p.docProvisional || r.Leader}
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

// --- Rule calls ---

func (p *TParser) Call(r *TRule, min int) (any, bool) {
	if p.Observation != nil {
		return p.callObserved(r, min)
	}
	return p.callBody(r, min)
}

// callObserved is call in an observed parse.
func (p *TParser) callObserved(r *TRule, min int) (any, bool) {
	p.Observation.Enter(r.Name, p.Pos, min)
	h := p.ExaminedBegin()
	v, ok := p.callBody(r, min)
	x, known := p.ExaminedEnd(h)
	p.Observation.Exit(ok, p.Pos, x, known)
	return v, ok
}

// callBody is call without observation.
func (p *TParser) callBody(r *TRule, min int) (any, bool) {
	if p.TypedDocument != nil {
		return p.DocumentCall(r, min, nil)
	}
	p.StepWork()
	start := p.Pos
	if !r.Memo && !r.Leader || p.FirstCall(r.Rule) {
		if len(r.Scope) == 0 {
			return p.invokePlainBody(r, min)
		}
		rec := len(p.Recovered)
		v, ok := p.invoke(r, min)
		if !ok {
			p.Pos = start
			p.Recovered = p.Recovered[:rec]
		}
		return v, ok
	}
	env := p.memoEnv(r.Rule)
	if e := p.memoGet(r.Id, start, min, env); e != nil && (e.growing || (!e.silent || p.Silent > 0) && (len(e.errs) == 0 || e.silent == (p.Silent > 0))) {
		p.Observation.memoHit()
		if !e.growing {
			p.MergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.Pos = e.end
		p.Recovered = p.copyErrors(p.Recovered, e.errs)
		return e.val, true
	}
	rec := len(p.Recovered)
	m := p.Isolate(start)
	var v any
	var ok bool
	var e *tmemoEntry
	if r.Leader {
		v, ok = p.grow(r, min, env)
		e = p.memoGet(r.Id, start, min, env)
	} else {
		v, ok = p.invoke(r, min)
		if !ok {
			p.Recovered = p.Recovered[:rec]
		}
		e = p.memoSlab.one()
		*e = tmemoEntry{val: v, ok: ok, end: p.Pos, silent: p.Silent > 0,
			errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, min, env, e)
	}
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

// CallMemo is the memoized part of call, for the rule r invoked by inv (the generated i<id>):
// the rule is called at the position again, or its memoization is no longer deferred.
func (p *TParser) CallMemo(r *TRule, inv func(*TParser) (any, bool)) (any, bool) {
	if p.TypedDocument != nil {
		return p.DocumentCall(r, 0, inv)
	}
	start := p.Pos
	env := p.memoEnv(r.Rule)
	if e := p.memoGet(r.Id, start, 0, env); e != nil && (e.growing || (!e.silent || p.Silent > 0) && (len(e.errs) == 0 || e.silent == (p.Silent > 0))) {
		p.Observation.memoHit()
		if !e.growing {
			p.MergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.Pos = e.end
		p.Recovered = p.copyErrors(p.Recovered, e.errs)
		return e.val, true
	}
	rec := len(p.Recovered)
	m := p.Isolate(start)
	v, ok := inv(p)
	if !ok {
		p.Recovered = p.Recovered[:rec]
	}
	e := p.memoSlab.one()
	*e = tmemoEntry{val: v, ok: ok, end: p.Pos, silent: p.Silent > 0,
		errs: p.copyErrors(nil, p.Recovered[rec:])}
	p.memoPut(r.Id, start, 0, env, e)
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

func (p *TParser) grow(r *TRule, min int, env []any) (any, bool) {
	start := p.Pos
	rec := len(p.Recovered)
	best := p.memoSlab.one()
	*best = tmemoEntry{end: start, growing: true}
	p.memoPut(r.Id, start, min, env, best)
	for {
		v, ok := p.invoke(r, min)
		if !ok || (best.ok && p.Pos <= best.end) {
			break
		}
		best = p.memoSlab.one()
		*best = tmemoEntry{val: v, ok: true, end: p.Pos, growing: true, errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, min, env, best)
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
	}
	best.growing = false
	best.silent = p.Silent > 0
	p.Pos = best.end
	p.Recovered = p.copyErrors(p.Recovered[:rec], best.errs)
	return best.val, best.ok
}

func (p *TParser) invoke(r *TRule, min int) (any, bool) {
	if p.TypedDocument != nil {
		p.Document.stats.Evaluated++
	}
	f := p.NewFrame(len(r.Scope))
	prevFrame, prevEnv, prevCut, trail := p.Frame, p.Env, p.Cut, len(p.Trail)
	p.Frame, p.Cut = f, false
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.TooDeep()
	}
	start := p.Pos
	p.Observation.Evaluated()
	v, ok := r.Body(p, min)
	p.Depth--
	p.Frame, p.Cut = prevFrame, prevCut
	p.DropTrail(trail)
	if ok {
		v = p.Finish(r, f, v, start)
	}
	p.Env = prevEnv
	p.FreeFrame(f)
	return v, ok
}

func (p *TParser) InvokePlain(r *TRule, min int) (any, bool) {
	if p.Observation != nil {
		return p.invokePlainObserved(r, min)
	}
	return p.invokePlainBody(r, min)
}

// invokePlainObserved is InvokePlain in an observed parse.
func (p *TParser) invokePlainObserved(r *TRule, min int) (any, bool) {
	p.Observation.Enter(r.Name, p.Pos, min)
	h := p.ExaminedBegin()
	v, ok := p.invokePlainBody(r, min)
	x, known := p.ExaminedEnd(h)
	p.Observation.Exit(ok, p.Pos, x, known)
	return v, ok
}

// invokePlainBody is InvokePlain without observation.
func (p *TParser) invokePlainBody(r *TRule, min int) (any, bool) {
	start, rec, trail := p.Pos, len(p.Recovered), len(p.Trail)
	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.TooDeep()
	}
	p.Observation.Evaluated()
	v, ok := r.Body(p, min)
	p.Depth--
	p.Cut = prevCut
	p.DropTrail(trail)
	if ok {
		v = p.Finish(r, EmptyTFrame, v, start)
	} else {
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
	}
	p.Env = prevEnv
	return v, ok
}

// Finish makes the rule's value from the value of its body, and checks it against the rule's
// declared type if needed.
func (p *TParser) Finish(r *TRule, f *TCaptureFrame, v any, start int) any {
	var body any
	if r.Nt != 0 && r.Action == nil {
		body = TFreshNode(v)
	}
	v = p.finishValue(r, f, v, start)
	if r.Nt != 0 {
		v = p.TConvertResultOwned(r, v, body)
	}
	return v
}

// TFreshNode returns v if it is a fresh string, tuple or list node, and otherwise nil. Taken from
// the value of a rule's body before the rule finishes it, it is the node the body made and nothing
// else holds yet (see TConvertResultOwned).
//
// TFreshNode is for generated parsers; not a stable API.
func TFreshNode(v any) any {
	switch x := v.(type) {
	case *TMatch:
		if x.fresh {
			return x
		}
	case *TSeq:
		if x.fresh && (x.Typ == "tuple" || x.Typ == "list") {
			return x
		}
	}
	return nil
}

// TConvertResultOwned converts v, the value of rule r, to the rule's named node type, like
// TConvertResult. If v is body, the fresh node of the rule's body (TFreshNode), the rule returns
// the node it made itself, which nothing else holds, so it gets the type in place instead of
// being copied: the copy tconvert makes keeps a node that others hold as it is.
func (p *TParser) TConvertResultOwned(r *TRule, v, body any) any {
	if body != nil && v == body {
		switch x := v.(type) {
		case *TMatch:
			x.Nt = r.Nt
			return x
		case *TSeq:
			x.nt = r.Nt
			return x
		}
	}
	return p.TConvertResult(r, v)
}

// tnamed reports whether n is a node of a named node type.
func tnamed(n TVal) bool {
	switch x := n.(type) {
	case *TMatch:
		return x.Nt != 0
	case *TSeq:
		return x.nt != 0
	}
	return false
}

// tconvert is nodeTypeKinds.Convert for the values of the typed runtime: it returns the node of
// the named node type nt that v converts to (a copy of v with the same range and content, or v
// itself if it already has the type), and reports false if v is not a string, tuple or list node.
func (p *TParser) tconvert(nt uint16, v any) (any, bool) {
	switch x := v.(type) {
	case *TMatch:
		if x.Nt == nt {
			return x, true
		}
		m := &TMatch{TNode: x.TNode, Nt: nt}
		return m, true
	case *TSeq:
		if x.Typ != "tuple" && x.Typ != "list" {
			return nil, false
		}
		if x.nt == nt {
			return x, true
		}
		c := p.nodes.one()
		*c = *x
		c.nt, c.fresh = nt, false
		if x.fresh {
			// The rule that made x may still change it; the copy keeps what it holds now.
			c.Kids = append([]any(nil), x.Kids...)
		}
		c.fields = append([]tfield(nil), x.fields...)
		return c, true
	}
	return nil, false
}

// TConvertResult converts v, the value of rule r, to the rule's named node type.
func (p *TParser) TConvertResult(r *TRule, v any) any {
	n, ok := p.tconvert(r.Nt, v)
	if !ok {
		panic(fatal{fmt.Errorf("cannot convert %s to %s in result of rule %s", ttypeName(v), r.NodeType, r.where())})
	}
	return n
}

// Convert evaluates a node type constructor T{v} (ActCtx.Convert): it converts v to the named node
// type with the index nt.
func (c *TActCtx) Convert(nt uint16, v any) any {
	n, ok := c.P.tconvert(nt, v)
	if !ok {
		c.P.evalErrorf("cannot convert %s to %s", ttypeName(v), tnodeTypeOf(nt).name)
	}
	return n
}

// TSetNodeType gives v, a string, tuple or list node that back made, the named node type nt.
//
// TSetNodeType is for generated parsers; not a stable API.
func TSetNodeType(v any, nt uint16) any {
	switch x := v.(type) {
	case *TMatch:
		x.Nt = nt
	case *TSeq:
		x.nt = nt
	}
	return v
}

func (p *TParser) finishValue(r *TRule, f *TCaptureFrame, v any, start int) any {
	if r.Novalue {
		return nil
	}
	if r.Action != nil {
		c := p.UseCtx(TActCtx{P: p, F: f, Start: start, End: p.Pos, Cbase: len(p.Created)})
		return c.Result(r.Action, r.where())
	}
	if r.Splice {
		if n := AsTval(v); n != nil {
			n.TSetFresh(false)
		}
		return v
	}
	v = p.ScopeValue(r.Value, v, start)
	if len(r.Scope) > 0 {
		v = p.AttachCaptures(v, r.Scope, f.Vals, start, p.Pos)
	}
	n := AsTval(v)
	if n == nil {
		// A value other than a node passes through as it is: the result of an action, of an
		// Operators Rule's lines or of a rule the body calls, such as an alternative of a
		// left-recursion leader. The engine passes it on in a box.
		return v
	}
	n.TSetFresh(false)
	return n
}

// AttachCaptures attaches the non-nil captures vals (named names) as fields of the value (a CST
// node), first wrapping it in a tuple node unless it is a fresh one that is not itself captured.
func (p *TParser) AttachCaptures(v any, names []string, vals []any, start, end int) any {
	n := AsTval(v)
	found := false
	wrap := n == nil || !n.TFresh()
	for _, c := range vals {
		p.StepWork()
		if c != nil {
			found = true
		}
		if c != nil && n != nil && c == any(n) {
			wrap = true
		}
	}
	if !found {
		return v
	}
	t, isNode := n.(*TSeq)
	if wrap || !isNode {
		kids := p.NewVals(1)
		kids[0] = v
		t = p.NewNode("tuple", start, end, kids)
	}
	if t.fields == nil {
		t.fields = p.tfields(len(names))
	}
	for i, name := range names {
		p.StepWork()
		if vals[i] != nil {
			p.setValueField(t, name, vals[i])
		}
	}
	return t
}

// --- Shapes of values (see runtime.go) ---

// shapeTuple returns the tuple node of the result v of a match of [start, p.Pos).
func (p *TParser) shapeTuple(s Shaper, v any, start int) any {
	if s.Elems {
		if n, _ := v.(*TSeq); s.Shared && n != nil {
			return p.NewNode("tuple", n.Start, n.End, n.Kids)
		}
		return v
	}
	var kids []any
	if s.K == 1 {
		kids = p.NewVals(1)
		kids[0] = v
	}
	return p.NewNode("tuple", start, p.Pos, kids)
}

// tone returns the one element of the result v.
func tone(s Shaper, v any) any {
	if s.Elems {
		n, _ := v.(*TSeq)
		if n == nil || len(n.Kids) == 0 {
			return nil
		}
		return n.Kids[0]
	}
	return v
}

// ShapeSingle returns the value of the result v where the expression takes one element; ok is
// false when it has no value.
func (p *TParser) ShapeSingle(s Shaper, v any, start int) (any, bool) {
	switch s.Mode {
	case singlePass:
		return tone(s, v), true
	case singleTuple:
		return p.shapeTuple(s, v, start), true
	}
	return nil, false
}

// ShapeCapture is ShapeSingle for a capture or a recovered expression, whose value is the unit
// node of the range matched when the expression has no value.
func (p *TParser) ShapeCapture(s Shaper, v any, start int) any {
	v, ok := p.ShapeSingle(s, v, start)
	if !ok {
		v = p.NewNode("tuple", start, p.Pos, nil)
	}
	return v
}

// ScopeValue returns the value of a scope whose body matched [start, p.Pos) with the result v.
func (p *TParser) ScopeValue(s Shaper, v any, start int) any {
	if s.Tuple {
		return p.shapeTuple(s, v, start)
	}
	return tone(s, v)
}

// --- Terminals ---

func (p *TParser) MatchLiteral(rs []rune, text string, desc ExpID, build bool) (any, bool) {
	start := p.Pos
	if _, ok := p.Parser.MatchLiteral(rs, text, desc, false); !ok {
		return nil, false
	}
	if !build {
		return nil, true
	}
	return p.NewMatch(start, p.Pos, p.LiteralText(start, text), true), true
}

// MatchLiteralFold is MatchLiteral for a case-insensitive literal (see Parser.MatchLiteralFold).
func (p *TParser) MatchLiteralFold(rs []rune, desc ExpID, build bool) (any, bool) {
	start := p.Pos
	if _, ok := p.Parser.MatchLiteralFold(rs, desc, false); !ok {
		return nil, false
	}
	if !build {
		return nil, true
	}
	return p.NewMatch(start, p.Pos, p.Text(start, p.Pos), true), true
}

// MatchLiteralOrdinary is MatchLiteral in an ordinary parse (parser.ExpectOrdinary).
func (p *TParser) MatchLiteralOrdinary(rs []rune, text string, desc ExpID, build bool) (any, bool) {
	start := p.Pos
	if _, ok := p.Parser.MatchLiteralOrdinary(rs, text, desc, false); !ok {
		return nil, false
	}
	if !build {
		return nil, true
	}
	return p.NewMatchOrdinary(start, p.Pos, p.LiteralText(start, text), true), true
}

// MatchAnyOrdinary is MatchAny in an ordinary parse (parser.ExpectOrdinary).
func (p *TParser) MatchAnyOrdinary(build bool) (any, bool) {
	_, size, ok := p.PeekOrdinary()
	if !ok {
		p.ExpectOrdinary(p.Pos, IdAny)
		return nil, false
	}
	return p.Single(size, build)
}

func (p *TParser) MatchAny(build bool) (any, bool) {
	_, size, ok := p.Peek()
	if !ok {
		p.Expect(p.Pos, IdAny)
		return nil, false
	}
	return p.Single(size, build)
}

func (p *TParser) Single(size int, build bool) (any, bool) {
	start := p.Pos
	p.Pos += size
	if !build {
		return nil, true
	}
	return p.NewMatch(start, p.Pos, p.Text(start, p.Pos), true), true
}

func (p *TParser) Anchor(ok bool, desc ExpID) (any, bool) {
	if ok {
		return nil, true
	}
	p.Expect(p.Pos, desc)
	return nil, false
}

// --- Attributes ---

type tmatcher = func(p *TParser) (any, bool)

func (p *TParser) tracked(m tmatcher) (v any, ok bool, far int, expected []ExpID) {
	mark := p.Isolate(p.Pos)
	v, ok = m(p)
	far, expected = p.Unisolate(mark)
	return v, ok, far, expected
}

func (p *TParser) ErrorAttr(m tmatcher, msg ExpID) (any, bool) {
	v, ok, far, expected := p.tracked(m)
	if ok {
		p.MergeExpected(far, expected)
		return v, true
	}
	p.Expect(far, msg)
	return nil, false
}

func (p *TParser) DiagnosticAttr(m tmatcher, name ExpID) (any, bool) {
	v, ok, far, expected := p.tracked(m)
	if ok {
		p.MergeExpected(far, expected)
		return v, true
	}
	p.MergeExpected(far, RenamedExpected(p.Parser, expected, name))
	return nil, false
}

func (p *TParser) RecoverAttr(m, skip tmatcher, build bool) (any, bool) {
	m0 := p.Mark()
	v, ok, far, expected := p.tracked(m)
	if ok {
		p.MergeExpected(far, expected)
		return v, true
	}
	expected = p.Keep(expected)
	p.Reset(m0)
	if _, ok := skip(p); !ok || p.Pos == m0.Pos {
		p.Reset(m0)
		p.MergeExpected(far, expected)
		return nil, false
	}
	e := p.MakeError(far, expected)
	p.Recovered = append(p.Recovered, e)
	if !build {
		return nil, true
	}
	return &TError{Error{Span: Span{m0.Pos, p.Pos}, Text: p.Text(m0.Pos, p.Pos), Message: p.RecoveryErrorText(e)}, true}, true
}

// --- Actions and predicates ---

// TActCtx is the context of evaluating an action or a predicate (ActCtx in the typed runtime).
//
// TActCtx is for generated parsers; not a stable API.
type TActCtx struct {
	P            *TParser
	F            *TCaptureFrame
	Start, End   int
	Cbase        int
	Lhs, Rhs, Op any
}

func (p *TParser) UseCtx(c TActCtx) *TActCtx {
	p.ac = c
	return &p.ac
}

// Result evaluates an action. Its errors fail the parse: tparse reports them with the rule
// name, which result records in p.Where instead of deferring a recovery for each action.
func (c *TActCtx) Result(action func(*TActCtx) any, where string) (n any) {
	prev := c.P.Where
	c.P.Where = where
	n = c.Finish(action(c))
	c.P.Where = prev
	return n
}

// Finish checks and finishes the value v of an action (see result): a struct the action made
// takes the rule's range, and the value is no longer fresh. A value other than a node needs no
// carrier here.
func (c *TActCtx) Finish(v any) any {
	t := AsTval(v)
	if t != nil {
		for _, x := range c.P.Created[c.Cbase:] {
			c.P.StepWork()
			if x == t {
				t.TSetSpan(c.Start, c.End)
				break
			}
		}
	}
	c.P.DropCreated(c.Cbase)
	if v == nil {
		return nil
	}
	if t == nil {
		if _, isFn := v.(func(...any) any); isFn {
			c.P.evalErrorf("result must be a value, got a function")
		}
		return v
	}
	t.TSetFresh(false)
	return t
}

func (p *TParser) DropCreated(base int) {
	clear(p.Created[base:])
	p.Created = p.Created[:base]
}

func (p *TParser) Predicate(t func(*TActCtx) any) (ok bool) {
	base, kids := len(p.Created), len(p.KidStack)
	defer func() {
		p.DropCreated(base)
		if x := recover(); x != nil {
			if _, isEval := x.(evalError); !isEval {
				panic(x)
			}
			p.DropKids(kids)
			ok = false
		}
	}()
	c := p.UseCtx(TActCtx{P: p, F: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
	v := t(c)
	if b, isBool := v.(bool); isBool && !b {
		return false
	}
	return true
}

// Bind is Parser.Bind for the typed runtime.
func (p *TParser) Bind(name string, t func(*TActCtx) any) (ok bool) {
	base, kids := len(p.Created), len(p.KidStack)
	defer func() {
		p.DropCreated(base)
		if x := recover(); x != nil {
			if _, isEval := x.(evalError); !isEval {
				panic(x)
			}
			p.DropKids(kids)
			ok = false
		}
	}()
	c := p.UseCtx(TActCtx{P: p, F: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
	v := t(c)
	if _, isFn := v.(func(...any) any); isFn {
		return false
	}
	p.bind(name, v)
	return true
}

// tvalOrNil is the typed runtime's nodeOrNil: values are already any, with the untyped nil.
func tvalOrNil(v any) any { return v }

func (c *TActCtx) Cap(slot int) any { return tvalOrNil(c.F.Vals[slot]) }

func (c *TActCtx) Lookup(name string) any {
	if WorkControlsEnabled && c.P.Control != nil {
		if v, ok := c.P.lookupEnvControlled(name); ok {
			return v
		}
		c.P.evalErrorf("variable %s is not defined", name)
		return nil
	}

	for e := c.P.Env; e != nil; e = e.next {
		if e.name == name {
			return e.val
		}
	}
	c.P.evalErrorf("variable %s is not defined", name)
	return nil
}

// ttypeName is typeName for values of the typed runtime.
func ttypeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	}
	if t, ok := v.(TVal); ok {
		if tnamed(t) {
			return t.TName()
		}
		return valueTypeName(tshape(t), t.TName(), len(t.TKids()))
	}
	return fmt.Sprintf("%T", v)
}

// FieldSpanOf widens the range [*start, *end) to cover the field value v if it is a node (first is
// set until a node has been seen), as NewStruct does.
//
// FieldSpanOf is for generated parsers; not a stable API.
func FieldSpanOf(v any, first *bool, start, end *int) {
	child := AsTval(v)
	if child == nil {
		return
	}
	s, e := child.TSpan()
	if *first || s < *start {
		*start = s
	}
	if *first || e > *end {
		*end = e
	}
	*first = false
}

// Made finishes a struct value n made by an action with the given range (see NewStruct).
func (c *TActCtx) Made(n TVal, first bool, start, end int) any {
	if first {
		start, end = c.Start, c.End
	}
	n.TSetSpan(start, end)
	c.P.Created = append(c.P.Created, n)
	return n
}

func (c *TActCtx) NewList(items []any) any {
	kids := c.P.NewVals(len(items))
	copyWork(c.P.Parser, kids, items)
	return c.listNode(kids)
}

// listNode returns a list value with the child list kids (which it takes over). Its range, which
// the value model does not show, covers the elements that are nodes.
func (c *TActCtx) listNode(kids []any) *TSeq { return c.valueNode(TypeListValue, kids) }

// Tuple returns a tuple value of the arguments, as a tuple literal builds it.
func (c *TActCtx) Tuple(args ...any) any {
	base := len(c.P.KidStack)
	for _, a := range args {
		c.P.StepWork()
		c.P.KidStack = append(c.P.KidStack, c.asNode("tuple", a))
	}
	return c.valueNode(TypeTupleValue, c.P.Kids(base))
}

func (c *TActCtx) valueNode(typ string, kids []any) *TSeq {
	n := c.P.NewNode(typ, c.Start, c.Start, kids)
	n.fresh = false
	first := true
	for _, it := range kids {
		c.P.StepWork()
		t := AsTval(it)
		if t == nil {
			continue
		}
		s, e := t.TSpan()
		if first || s < n.Start {
			n.Start = s
		}
		if first || e > n.End {
			n.End = e
		}
		first = false
	}
	return n
}

// tstrFields is implemented by the generated struct types: TStrField returns the value of a
// string field without boxing it.
//
// TStrField is for generated parsers; not a stable API.
type tstrFields interface {
	TStrField(name string) (string, bool)
}

// FieldOperand reads x.Name as member does, keeping a string field of a struct unboxed.
func (c *TActCtx) FieldOperand(x any, name string) TOperand {
	// A string field of a struct costs no steps to read under work controls either: see TFieldWork.
	if n, ok := x.(tstrFields); ok {
		if s, ok := n.TStrField(name); ok {
			return TOperand{Str: s, IsStr: true}
		}
	}
	return TOperand{V: c.Member(x, name)}
}

// Compare applies a comparison operator as binary does, comparing two strings directly.
func (c *TActCtx) Compare(op string, l, r TOperand) any {
	if !l.IsStr {
		l.Str, l.IsStr = l.V.(string)
	}
	if !r.IsStr {
		r.Str, r.IsStr = r.V.(string)
	}
	if l.IsStr && r.IsStr {
		if v, ok := c.P.compareStrings(op, l.Str, r.Str); ok {
			return v
		}
	}
	return c.Binary(op, l.value(), r.value())
}

func (c *TActCtx) Member(x any, name string) any {
	n := AsTval(x)
	if n == nil {
		c.P.evalErrorf("cannot access .%s of %s", name, ttypeName(x))
	}
	if tisNode(n) {
		switch name {
		case "Start":
			s, _ := n.TSpan()
			return s
		case "End":
			_, e := n.TSpan()
			return e
		case "Value":
			return c.P.nodeValue(n)
		}
	}
	if len(name) > 1 && name[0] == '$' {
		capture := name[1:]
		// x.$y reads a capture, which struct values do not have; an unmatched capture is nil.
		v, ok := c.P.valueField(n, capture)
		if n.TStruct() {
			c.P.evalErrorf("%s has no capture %s", n.TName(), capture)
		}
		if !ok {
			return nil
		}
		return tvalOrNil(v)
	}
	v, ok := c.P.valueField(n, name)
	if !ok {
		c.P.evalErrorf("%s has no field %s", n.TName(), name)
	}
	return tvalOrNil(v)
}

func trtUnary(op string, x any) any {
	switch op {
	case "-":
		if i, ok := x.(int); ok {
			return -i
		}
	case "!":
		if b, ok := x.(bool); ok {
			return !b
		}
	}
	evalErrorf("invalid operand %s for unary %s", ttypeName(x), op)
	return nil
}

func trtBool(op string, v any) bool {
	b, ok := v.(bool)
	if !ok {
		evalErrorf("invalid operand %s for %s", ttypeName(v), op)
	}
	return b
}

func trtAnd(l any, r func() any) any {
	if !trtBool("&&", l) {
		return false
	}
	return trtBool("&&", r())
}

func trtOr(l any, r func() any) any {
	if trtBool("||", l) {
		return true
	}
	return trtBool("||", r())
}

func trtEqual(a, b any) bool {
	switch a.(type) {
	case int, string, bool:
		return a == b
	case nil:
		return b == nil
	}
	an, ok := a.(TVal)
	if !ok {
		return false
	}
	bn, ok := b.(TVal)
	return ok && an == bn
}

func trtBinary(op string, l, r any) any {
	switch op {
	case "==":
		return trtEqual(l, r)
	case "!=":
		return !trtEqual(l, r)
	case "[]":
		items := tlistItems("[]", l)
		return items[rtIndex(nil, r, len(items), ttypeName)]
	}
	switch l := l.(type) {
	case int:
		if r, ok := r.(int); ok {
			switch op {
			case "+":
				return l + r
			case "-":
				return l - r
			case "*":
				return l * r
			case "/", "%":
				if r == 0 {
					evalErrorf("division by zero")
				}
				if op == "/" {
					return l / r
				}
				return l % r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	case string:
		if r, ok := r.(string); ok {
			switch op {
			case "+":
				return l + r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	}
	evalErrorf("invalid operands %s and %s for %s", ttypeName(l), ttypeName(r), op)
	return nil
}

func (c *TActCtx) Length(x any) any {
	switch x := x.(type) {
	case string:
		return c.P.unitLen(x)
	case nil:
		return 0
	}
	if n, ok := x.(TVal); ok {
		if text, ok := n.TText(); ok {
			return c.P.unitLen(text)
		}
		return len(n.TKids())
	}
	c.P.evalErrorf("len: invalid argument %s", ttypeName(x))
	return nil
}

func (c *TActCtx) Text(x any) any { return c.TextStr(x) }

func (c *TActCtx) TextStr(x any) string {
	switch x := x.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	if n, ok := x.(TVal); ok {
		if text, ok := n.TText(); ok {
			return text
		}
		s, e := n.TSpan()
		return c.P.Text(s, e)
	}
	c.P.evalErrorf("text: invalid argument %s", ttypeName(x))
	return ""
}

func tlistItems(fn string, v any) []any {
	if v == nil {
		return nil
	}
	if n, ok := v.(TVal); ok {
		return n.TKids()
	}
	evalErrorf("%s: expected a list, got %s", fn, ttypeName(v))
	return nil
}

func tasNode(fn string, v any) any {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case TVal, int, string, bool:
		return v
	}
	evalErrorf("%s: elements must be values, got %s", fn, ttypeName(v))
	return nil
}

func trtFold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := tlistItems(fn, list)
	for i := range items {
		it := items[i]
		if right {
			it = items[len(items)-1-i]
		}
		acc = f(acc, tvalOrNil(it))
	}
	return acc
}

func (c *TActCtx) MapList(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	c.PushMap(list, f)
	return c.EndList(base)
}

func (c *TActCtx) FilterList(list any, f func(item any) any) any {
	base := len(c.P.KidStack)
	for _, item := range c.ListItems("filter", list) {
		c.P.StepWork()
		value := f(tvalOrNil(item))
		keep, ok := value.(bool)
		if !ok {
			c.P.evalErrorf("filter: predicate must produce a bool, got %s", ttypeName(value))
		}
		if keep {
			c.P.KidStack = append(c.P.KidStack, item)
		}
	}
	return c.EndList(base)
}

func (c *TActCtx) List(args ...any) any {
	base := len(c.P.KidStack)
	c.PushList(args...)
	return c.EndList(base)
}

func (c *TActCtx) Concat(args ...any) any {
	base := len(c.P.KidStack)
	for _, a := range args {
		c.P.StepWork()
		c.PushItems(a)
	}
	return c.EndList(base)
}

func (c *TActCtx) PushMap(list any, f func(item any) any) {
	for _, it := range c.ListItems("map", list) {
		c.P.StepWork()
		c.P.KidStack = append(c.P.KidStack, c.asNode("map", f(tvalOrNil(it))))
	}
}

func (c *TActCtx) PushList(args ...any) {
	for _, a := range args {
		c.P.StepWork()
		c.P.KidStack = append(c.P.KidStack, c.asNode("list", a))
	}
}

func (c *TActCtx) PushItems(v any) {
	c.P.KidStack = appendWork(c.P.Parser, c.P.KidStack, c.ListItems("concat", v))
}

func (c *TActCtx) EndList(base int) any {
	return c.listNode(c.P.Kids(base))
}

// --- Operators Rule expressions ---

// TOperatorsRule is for generated parsers; not a stable API.
type TOperatorsRule struct {
	Skip     tmatcher
	Operands []*TOperatorsRuleLine
	Prefix   []*TOperatorsRuleOp
	Led      []*TOperatorsRuleOp
}

// TOperatorsRuleLine is for generated parsers; not a stable API.
type TOperatorsRuleLine struct {
	Scope  []string
	M      tmatcher
	Action func(*TActCtx) any
	// Value makes the value of an operand line without an action, and of an operator line's
	// operator, from the value of the line's expression.
	Value Shaper
}

// TOperatorsRuleOp is for generated parsers; not a stable API.
type TOperatorsRuleOp struct {
	Id    int
	Kind  string
	Assoc string
	Level int
	Line  *TOperatorsRuleLine
}

type toperatorsRuleAttempt struct {
	op         *TOperatorsRuleOp
	frame      *TCaptureFrame
	v          any
	start, end int
	env        *Env
	cut        bool
	errs       []*SyntaxError
}

func (p *TParser) skipTrivia(pr *TOperatorsRule) {
	if pr.Skip == nil {
		return
	}
	m := p.Mark()
	p.Silent++
	_, ok := pr.Skip(p)
	p.Silent--
	if !ok {
		p.Reset(m)
	}
}

func (p *TParser) longest(ops []*TOperatorsRuleOp) (best toperatorsRuleAttempt, cutFailed bool) {
	m0 := p.Mark()
	for _, op := range ops {
		p.StepWork()
		prevFrame, prevCut := p.Frame, p.Cut
		f := p.NewFrame(len(op.Line.Scope))
		p.Frame, p.Cut = f, false
		v, ok := op.Line.M(p)
		cut := p.Cut
		p.Frame, p.Cut = prevFrame, prevCut
		if ok && p.Pos == m0.Pos && op.Kind != "infix" {
			ok = false
		}
		if ok && (best.op == nil || p.Pos > best.end) {
			best = toperatorsRuleAttempt{op: op, frame: f, v: v, start: m0.Pos, end: p.Pos, env: p.Env, cut: cut,
				errs: p.copyErrors(nil, p.Recovered[m0.recovered:])}
		}
		if !ok && cut {
			cutFailed = true
		}
		if len(f.Vals) == 0 {
			p.Reset(m0)
			continue
		}
		p.saved = appendWork(p.Parser, p.saved[:0], f.Vals)
		p.Reset(m0)
		copyWork(p.Parser, f.Vals, p.saved)
	}
	return best, cutFailed
}

func (p *TParser) apply(a *toperatorsRuleAttempt) {
	p.Pos, p.Env = a.end, a.env
	if len(a.errs) != 0 {
		p.applyErrors(a)
	}
}

// applyErrors is the recovery-error replay of apply, kept out of line so that apply inlines.
//
//go:noinline
func (p *TParser) applyErrors(a *toperatorsRuleAttempt) {
	p.Recovered = p.copyErrors(p.Recovered, a.errs)
}

// OperatorsRuleParse is parser.OperatorsRuleParse for typed values.
func (p *TParser) OperatorsRuleParse(r *TRule, min int) (any, bool) {
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.TooDeep()
	}
	v, ok := p.operatorsRuleExpr(r, min)
	p.Depth--
	return v, ok
}

func (p *TParser) operatorsRuleExpr(r *TRule, min int) (any, bool) {
	pr := r.OperatorsRule
	lhs, lstart, ok := p.operatorsRuleNud(r)
	if !ok {
		return nil, false
	}
	lastNone := -1
	for {
		m0 := p.Mark()
		p.skipTrivia(pr)
		a, cutFailed := p.longest(pr.Led)
		if a.op == nil {
			p.Reset(m0)
			if cutFailed {
				return nil, false
			}
			break
		}
		if a.op.Level <= min || a.op.Assoc == "none" && a.op.Level == lastNone {
			p.Reset(m0)
			break
		}
		p.apply(&a)
		if a.op.Kind == "postfix" {
			lhs, lstart = p.operatorsRuleBuild(r, &a, lhs, lstart, nil)
			lastNone = -1
			if p.Pos == m0.Pos {
				break
			}
			continue
		}
		rmin := a.op.Level
		if a.op.Assoc == "right" {
			rmin--
		}
		rhs, ok := p.OperatorsRuleParse(r, rmin)
		if !ok {
			if a.cut {
				return nil, false
			}
			p.Reset(m0)
			break
		}
		lhs, lstart = p.operatorsRuleBuild(r, &a, lhs, lstart, rhs)
		lastNone = -1
		if a.op.Assoc == "none" {
			lastNone = a.op.Level
		}
		if p.Pos == m0.Pos { // as in runtime.go
			break
		}
	}
	return lhs, true
}

// operatorsRuleNud parses an operand or a prefix application. Besides its value it returns where
// the value starts: a value that is not a node has no span of its own, so the start travels
// beside it, as the range of the box a value other than a node travels in (see Parser.operatorsRuleNud).
func (p *TParser) operatorsRuleNud(r *TRule) (any, int, bool) {
	pr := r.OperatorsRule
	p.skipTrivia(pr)
	m0 := p.Mark()
	if a, cutFailed := p.longest(pr.Prefix); a.op != nil {
		p.apply(&a)
		rhs, ok := p.OperatorsRuleParse(r, a.op.Level)
		if ok {
			v, start := p.operatorsRuleBuild(r, &a, nil, 0, rhs)
			return v, start, true
		}
		if a.cut {
			return nil, 0, false
		}
		p.Reset(m0)
	} else if cutFailed {
		return nil, 0, false
	}
	for _, o := range pr.Operands {
		p.StepWork()
		prevFrame, prevCut := p.Frame, p.Cut
		f := p.NewFrame(len(o.Scope))
		p.Frame, p.Cut = f, false
		v, ok := o.M(p)
		cut := p.Cut
		p.Frame, p.Cut = prevFrame, prevCut
		if ok {
			return p.lineResult(r, o, f, v, m0.Pos), m0.Pos, true
		}
		p.Reset(m0)
		if cut {
			return nil, 0, false
		}
	}
	return nil, 0, false
}

func (p *TParser) lineResult(r *TRule, l *TOperatorsRuleLine, f *TCaptureFrame, v any, start int) any {
	if r.Novalue {
		return nil
	}
	return p.lineValue(r, l, f, v, start)
}

func (p *TParser) lineValue(r *TRule, l *TOperatorsRuleLine, f *TCaptureFrame, v any, start int) any {
	if l.Action != nil {
		c := p.UseCtx(TActCtx{P: p, F: f, Start: start, End: p.Pos, Cbase: len(p.Created)})
		return c.Result(l.Action, r.where())
	}
	v = p.ScopeValue(l.Value, v, start)
	if len(l.Scope) > 0 {
		v = p.AttachCaptures(v, l.Scope, f.Vals, start, p.Pos)
	}
	if n := AsTval(v); n != nil {
		n.TSetFresh(false)
		return n
	}
	// A value other than a node, such as the result of a rule's action, stays as it is.
	return v
}

// operatorsRuleBuild applies the operator of a to lhs, which starts at lstart if it is not a node,
// and rhs. Besides the value it returns where the value starts, as operatorsRuleNud does.
func (p *TParser) operatorsRuleBuild(r *TRule, a *toperatorsRuleAttempt, lhs any, lstart int, rhs any) (any, int) {
	if r.Novalue {
		return nil, a.start
	}
	start, end := a.start, p.Pos
	if l := AsTval(lhs); l != nil {
		start, _ = l.TSpan()
	} else if lhs != nil {
		start = lstart
	}
	if a.op.Kind == "postfix" {
		end = a.end
	}
	l := a.op.Line
	// The operator's value is its one element, or the node of its tuple, which has the line's
	// captures.
	opv, ok := p.ShapeSingle(l.Value, a.v, a.start)
	if !ok {
		opv = p.NewNode("tuple", a.start, a.end, nil)
	}
	if l.Value.Mode == singleTuple && len(l.Scope) > 0 {
		opv = p.AttachCaptures(opv, l.Scope, a.frame.Vals, a.start, a.end)
	}
	if n := AsTval(opv); n != nil {
		n.TSetFresh(false)
	}
	if l.Action != nil {
		c := p.UseCtx(TActCtx{P: p, F: a.frame, Start: start, End: end, Cbase: len(p.Created),
			Op: opv, Lhs: lhs, Rhs: rhs})
		v := c.Result(l.Action, r.where())
		if r.OpNt != 0 {
			v = p.convertOperator(r, v)
		}
		return v, start
	}
	// Without an action, the application is the node of the tuple (lhs, op, rhs) of the line's
	// kind, with those captures.
	var kids []any
	var fields []tfield
	switch a.op.Kind {
	case "prefix":
		kids = p.NewVals(2)
		kids[0], kids[1] = opv, rhs
		fields = []tfield{{"op", opv}, {"rhs", rhs}}
	case "postfix":
		kids = p.NewVals(2)
		kids[0], kids[1] = lhs, opv
		fields = []tfield{{"lhs", lhs}, {"op", opv}}
	default:
		kids = p.NewVals(3)
		kids[0], kids[1], kids[2] = lhs, opv, rhs
		fields = []tfield{{"lhs", lhs}, {"op", opv}, {"rhs", rhs}}
	}
	n := p.NewNode("tuple", start, end, kids)
	n.fresh = false
	n.fields = fields
	n.nt = r.OpNt // made with the named node type of the operators, if any
	return n, start
}

// convertOperator converts v, the value of an operator of rule r, to the named node type of the
// rule's operators.
func (p *TParser) convertOperator(r *TRule, v any) any {
	n, ok := p.tconvert(r.OpNt, v)
	if !ok {
		panic(fatal{fmt.Errorf("cannot convert %s to %s in result of rule %s", ttypeName(v), r.OpNodeType, r.where())})
	}
	return n
}

func (c *TActCtx) Binary(op string, l, r any) any {
	if v, ok := c.P.stringBinary(op, l, r); ok {
		return v
	}
	if WorkControlsEnabled && c.P.Control != nil {
		return trtBinaryWork(c.P.Parser, op, l, r)
	}
	return trtBinary(op, l, r)
}

func (p *TParser) PushKid(v any) { p.StepWork(); p.KidStack = append(p.KidStack, v) }
func (p *TParser) setValueField(n *TSeq, name string, v any) {
	if WorkControlsEnabled && p.Control != nil {
		p.setValueFieldControlled(n, name, v)
		return
	}
	n.set(name, v)
}

func (p *TParser) setValueFieldControlled(n *TSeq, name string, v any) {
	for i := range n.fields {
		p.StepWork()
		if p.rawValueEqual(n.fields[i].name, name) {
			n.fields[i].val = v
			return
		}
	}
	p.StepWork()
	n.fields = append(n.fields, tfield{name, v})
}
func (p *TParser) undoCaptures(from int) {
	for i := len(p.Trail) - 1; i >= from; i-- {
		p.StepWork()
		u := p.Trail[i]
		u.f.Vals[u.slot] = u.old
	}
}

func (p *TParser) valueField(n TVal, name string) (any, bool) {
	if WorkControlsEnabled && p.Control != nil {
		return p.valueFieldControlled(n, name)
	}
	return n.TField(name)
}

func (p *TParser) valueFieldControlled(n TVal, name string) (any, bool) {
	t, transient := n.(*TSeq)
	if !transient {
		if controlled, ok := n.(tfieldWorker); ok {
			return controlled.TFieldWork(p, name)
		}
		return n.TField(name)
	}
	for _, field := range t.fields {
		p.StepWork()
		if p.rawValueEqual(field.name, name) {
			return field.val, true
		}
	}
	return nil, true
}
func (c *TActCtx) Fold(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.foldControlled(fn, right, acc, list, f)
	}
	return trtFold(fn, right, acc, list, f)
}

func (c *TActCtx) foldControlled(fn string, right bool, acc, list any, f func(acc, item any) any) any {
	items := c.ListItems(fn, list)
	for i := range items {
		c.P.StepWork()
		item := items[i]
		if right {
			item = items[len(items)-1-i]
		}
		acc = f(acc, tvalOrNil(item))
	}
	return acc
}

// ListField materializes a typed struct's list field during an action read.
// The ordinary TField path remains independent of a parser or work controller.
func (p *TParser) ListField(kids []any) any {
	n := &TSeq{Typ: TypeListValue, Kids: kids}
	first := true
	for _, it := range kids {
		p.StepWork()
		t := AsTval(it)
		if t == nil {
			continue
		}
		start, end := t.TSpan()
		if first || start < n.Start {
			n.Start = start
		}
		if first || end > n.End {
			n.End = end
		}
		first = false
	}
	return n
}

func trtUnaryWork(p *Parser, op string, x any) any {
	switch op {
	case "-":
		if i, ok := x.(int); ok {
			return -i
		}
	case "!":
		if b, ok := x.(bool); ok {
			return !b
		}
	}
	p.evalErrorf("invalid operand %s for unary %s", ttypeName(x), op)
	return nil
}

func trtBoolWork(p *Parser, op string, v any) bool {
	b, ok := v.(bool)
	if !ok {
		p.evalErrorf("invalid operand %s for %s", ttypeName(v), op)
	}
	return b
}

func trtBinaryWork(p *Parser, op string, l, r any) any {
	switch op {
	case "==":
		return trtEqual(l, r)
	case "!=":
		return !trtEqual(l, r)
	case "[]":
		items := tlistItemsWork(p, "[]", l)
		return items[rtIndex(p, r, len(items), ttypeName)]
	}
	switch l := l.(type) {
	case int:
		if r, ok := r.(int); ok {
			switch op {
			case "+":
				return l + r
			case "-":
				return l - r
			case "*":
				return l * r
			case "/", "%":
				if r == 0 {
					p.evalErrorf("division by zero")
				}
				if op == "/" {
					return l / r
				}
				return l % r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	case string:
		if r, ok := r.(string); ok {
			switch op {
			case "+":
				return l + r
			case "<":
				return l < r
			case "<=":
				return l <= r
			case ">":
				return l > r
			case ">=":
				return l >= r
			}
		}
	}
	p.evalErrorf("invalid operands %s and %s for %s", ttypeName(l), ttypeName(r), op)
	return nil
}

func tlistItemsWork(p *Parser, fn string, v any) []any {
	if v == nil {
		return nil
	}
	if n, ok := v.(TVal); ok {
		return n.TKids()
	}
	p.evalErrorf("%s: expected a list, got %s", fn, ttypeName(v))
	return nil
}

func tasNodeWork(p *Parser, fn string, v any) any {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case TVal, int, string, bool:
		return v
	}
	p.evalErrorf("%s: elements must be values, got %s", fn, ttypeName(v))
	return nil
}

func (c *TActCtx) Unary(op string, v any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.unaryControlled(op, v)
	}
	return trtUnary(op, v)
}

func (c *TActCtx) unaryControlled(op string, v any) any {
	return trtUnaryWork(c.P.Parser, op, v)
}
func (c *TActCtx) And(l any, r func() any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.andControlled(l, r)
	}
	return trtAnd(l, r)
}

func (c *TActCtx) andControlled(l any, r func() any) any {
	if !trtBoolWork(c.P.Parser, "&&", l) {
		return false
	}
	return trtBoolWork(c.P.Parser, "&&", r())
}
func (c *TActCtx) Or(l any, r func() any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.orControlled(l, r)
	}
	return trtOr(l, r)
}

func (c *TActCtx) orControlled(l any, r func() any) any {
	if trtBoolWork(c.P.Parser, "||", l) {
		return true
	}
	return trtBoolWork(c.P.Parser, "||", r())
}
func (c *TActCtx) ListItems(fn string, v any) []any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.listItemsControlled(fn, v)
	}
	return tlistItems(fn, v)
}

func (c *TActCtx) listItemsControlled(fn string, v any) []any {
	return tlistItemsWork(c.P.Parser, fn, v)
}
func (c *TActCtx) asNode(fn string, v any) any {
	if WorkControlsEnabled && c.P.Control != nil {
		return c.asNodeControlled(fn, v)
	}
	return tasNode(fn, v)
}

func (c *TActCtx) asNodeControlled(fn string, v any) any {
	return tasNodeWork(c.P.Parser, fn, v)
}
