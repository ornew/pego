package engine

import (
	"io"
	"sort"
	"strconv"
	"strings"
)

// GNode is a node of the value model: the range of input a rule or a valued subexpression
// matched, and the value it produced. The value is a string, an int, a bool, nil, a Tuple, a
// List, a *Struct, an *ErrorValue or another *GNode. A node exposes methods only, so that the
// engine may change how it stores trees.
//
// With source information, a node also records the rule that produced it and where its captures
// are inside its value.
type GNode struct {
	start, end int32
	value      any
	src        *nodeSource
	// gen is the number of Document edits that the positions account for.
	gen uint32
}

// nodeSource is the opt-in source information of a node.
type nodeSource struct {
	rule string
	caps []nodeCapture
}

// nodeCapture is where a capture of a node is: a path of tuple indexes inside the node's value,
// or, for a capture in a positive lookahead, its value beside the tuple.
type nodeCapture struct {
	name  string
	path  []int
	side  bool
	value any
}

// newGNode returns a node of the range [start, end) with the value v.
func newGNode(start, end int32, v any) *GNode {
	return &GNode{start: start, end: end, value: v}
}

// Start returns the start of the node's range, in the parse's position unit.
func (n *GNode) Start() int32 { return n.start }

// End returns the exclusive end of the node's range.
func (n *GNode) End() int32 { return n.end }

// Value returns the node's value.
func (n *GNode) Value() any { return n.value }

// Len returns the number of elements of the node's value when it is a Tuple or a List, and 0
// otherwise.
func (n *GNode) Len() int {
	switch v := n.value.(type) {
	case Tuple:
		return v.Len()
	case List:
		return v.Len()
	}
	return 0
}

// Elem returns the element i of the node's value, which must be a Tuple or a List.
func (n *GNode) Elem(i int) any {
	switch v := n.value.(type) {
	case Tuple:
		return v.At(i)
	case List:
		return v.At(i)
	}
	panic("pego: Elem of a node whose value is neither a tuple nor a list")
}

// Rule returns the name of the rule that produced the node, or "" without source information.
func (n *GNode) Rule() string {
	if n.src == nil {
		return ""
	}
	return n.src.rule
}

// CaptureNames returns the names of the node's captures in source information, in order.
func (n *GNode) CaptureNames() []string {
	if n.src == nil {
		return nil
	}
	names := make([]string, len(n.src.caps))
	for i, c := range n.src.caps {
		names[i] = c.name
	}
	return names
}

// Capture returns the value of the capture name, using source information. ok is false when the
// node has no source information or no such capture. A capture inside an optional that did not
// match is nil.
func (n *GNode) Capture(name string) (v any, ok bool) {
	if n.src == nil {
		return nil, false
	}
	for _, c := range n.src.caps {
		if c.name != name {
			continue
		}
		if c.side {
			return c.value, true
		}
		return valueAt(n.value, c.path), true
	}
	return nil, false
}

// valueAt follows a path of tuple indexes from v. A missing element on the way is nil.
func valueAt(v any, path []int) any {
	for _, i := range path {
		t, ok := v.(Tuple)
		if !ok || i < 0 || i >= t.Len() {
			return nil
		}
		v = t.At(i)
	}
	return v
}

// Tuple is a fixed sequence of values of possibly different types: the value of a sequence.
type Tuple struct{ elems []any }

// newTuple returns a tuple of the elements; it keeps the slice.
func newTuple(elems ...any) Tuple { return Tuple{elems} }

// Len returns the number of elements.
func (t Tuple) Len() int { return len(t.elems) }

// At returns the element i.
func (t Tuple) At(i int) any { return t.elems[i] }

// List is a sequence of values of one type: the value of a repetition or a list literal.
type List struct{ elems []any }

// newList returns a list of the elements; it keeps the slice.
func newList(elems ...any) List { return List{elems} }

// Len returns the number of elements.
func (l List) Len() int { return len(l.elems) }

// At returns the element i.
func (l List) At(i int) any { return l.elems[i] }

// Struct is a value of a struct type defined in the grammar.
type Struct struct {
	typ    string
	fields []structField
}

type structField struct {
	name  string
	value any
}

// Type returns the name of the struct's type.
func (s *Struct) Type() string { return s.typ }

// NumField returns the number of fields.
func (s *Struct) NumField() int { return len(s.fields) }

// FieldAt returns the name and value of the field i, in the order of the type definition.
func (s *Struct) FieldAt(i int) (name string, value any) {
	return s.fields[i].name, s.fields[i].value
}

// Field returns the value of the field name.
func (s *Struct) Field(name string) (any, bool) {
	for _, f := range s.fields {
		if f.name == name {
			return f.value, true
		}
	}
	return nil, false
}

// sortedFields returns the fields sorted by name, for output.
func (s *Struct) sortedFields() []structField {
	fs := append([]structField(nil), s.fields...)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].name < fs[j].name })
	return fs
}

// ErrorValue is a value produced by error recovery: a range skipped, or a missing token.
type ErrorValue struct {
	start, end int32
	msg        string
	// expected is the expected token of a missing token, or "".
	expected string
}

// Start returns the start of the range the error covers.
func (e *ErrorValue) Start() int32 { return e.start }

// End returns the exclusive end of the range the error covers.
func (e *ErrorValue) End() int32 { return e.end }

// Message returns the error message.
func (e *ErrorValue) Message() string { return e.msg }

// Expected returns the expected token of a missing token, or "".
func (e *ErrorValue) Expected() string { return e.expected }

// valueFrame is a step of the iterative value writers. Trees can be deeper than the goroutine
// stack should grow for output, so the writers keep their own stack.
type valueFrame struct {
	v     any
	i     int
	phase uint8
	// owner and path locate a tuple inside the value of the node owner, for capture labels.
	owner *GNode
	path  []int
	// closeSide reports that the value of a lookahead capture was written and its object needs
	// closing.
	closeSide bool
}

const (
	frameEnter uint8 = iota
	frameElems
	frameCaptures
	frameExit
)

// writeValueJSON writes v in the JSON form of the value model: a node is an object with the
// members start, end and value, plus rule and captures with source information; a tuple and a
// list are arrays; nil is null; a struct is an object with the members type and fields; an error
// is an object with the members start, end, error and, for a missing token, expected.
func writeValueJSON(w io.Writer, v any) error {
	out := jsonOutput{w: w, buf: make([]byte, 0, 512)}
	stack := []valueFrame{{v: v}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		var push *valueFrame
		var err error
		switch v := f.v.(type) {
		case *GNode:
			if v == nil {
				err = out.raw("null")
				f.phase = frameExit
				break
			}
			switch f.phase {
			case frameEnter:
				if err = out.raw(`{"start":`); err == nil {
					err = out.number(v.start)
				}
				if err == nil {
					err = out.raw(`,"end":`)
				}
				if err == nil {
					err = out.number(v.end)
				}
				if err == nil {
					err = out.raw(`,"value":`)
				}
				f.phase = frameElems
				push = &valueFrame{v: v.value}
			case frameElems:
				if v.src != nil && v.src.rule != "" {
					if err = out.raw(`,"rule":`); err == nil {
						err = out.encoded(v.src.rule)
					}
				}
				f.phase = frameCaptures
				if err == nil && v.src != nil && len(v.src.caps) > 0 {
					err = out.raw(`,"captures":{`)
				} else {
					f.phase = frameExit
					if err == nil {
						err = out.raw("}")
					}
				}
			case frameCaptures:
				if f.closeSide {
					f.closeSide = false
					if err = out.raw("}"); err != nil {
						break
					}
				}
				if f.i == len(v.src.caps) {
					err = out.raw("}}")
					f.phase = frameExit
					break
				}
				c := v.src.caps[f.i]
				if f.i > 0 {
					err = out.raw(",")
				}
				f.i++
				if err == nil {
					err = out.encoded(c.name)
				}
				if err != nil {
					break
				}
				if c.side {
					if err = out.raw(`:{"value":`); err == nil {
						f.closeSide = true
						push = &valueFrame{v: c.value}
					}
					break
				}
				err = out.raw(`:{"path":`)
				if err == nil {
					err = out.encoded(pathOrEmpty(c.path))
				}
				if err == nil {
					err = out.raw("}")
				}
			}
		case Tuple, List:
			elems := valueElems(v)
			if f.phase == frameEnter {
				err = out.raw("[")
				f.phase = frameElems
			}
			if err != nil {
				break
			}
			if f.i == len(elems) {
				err = out.raw("]")
				f.phase = frameExit
				break
			}
			if f.i > 0 {
				err = out.raw(",")
			}
			push = &valueFrame{v: elems[f.i]}
			f.i++
		case *Struct:
			if v == nil {
				err = out.raw("null")
				f.phase = frameExit
				break
			}
			fields := v.sortedFields()
			if f.phase == frameEnter {
				if err = out.raw(`{"type":`); err == nil {
					err = out.encoded(v.typ)
				}
				if err == nil {
					err = out.raw(`,"fields":{`)
				}
				f.phase = frameElems
			}
			if err != nil {
				break
			}
			if f.i == len(fields) {
				err = out.raw("}}")
				f.phase = frameExit
				break
			}
			if f.i > 0 {
				err = out.raw(",")
			}
			if err == nil {
				err = out.encoded(fields[f.i].name)
			}
			if err == nil {
				err = out.raw(":")
			}
			push = &valueFrame{v: fields[f.i].value}
			f.i++
		case *ErrorValue:
			if v == nil {
				err = out.raw("null")
			} else {
				err = writeErrorJSON(&out, v)
			}
			f.phase = frameExit
		default:
			err = out.encoded(v)
			f.phase = frameExit
		}
		if err != nil {
			return err
		}
		if f.phase == frameExit {
			stack = stack[:len(stack)-1]
		}
		if push != nil {
			stack = append(stack, *push)
		}
	}
	return out.flush()
}

func pathOrEmpty(p []int) []int {
	if p == nil {
		return []int{}
	}
	return p
}

func writeErrorJSON(out *jsonOutput, e *ErrorValue) error {
	if err := out.raw(`{"start":`); err != nil {
		return err
	}
	if err := out.number(e.start); err != nil {
		return err
	}
	if err := out.raw(`,"end":`); err != nil {
		return err
	}
	if err := out.number(e.end); err != nil {
		return err
	}
	if err := out.raw(`,"error":`); err != nil {
		return err
	}
	if err := out.encoded(e.msg); err != nil {
		return err
	}
	if e.expected != "" {
		if err := out.raw(`,"expected":`); err != nil {
			return err
		}
		if err := out.encoded(e.expected); err != nil {
			return err
		}
	}
	return out.raw("}")
}

func valueElems(v any) []any {
	switch v := v.(type) {
	case Tuple:
		return v.elems
	case List:
		return v.elems
	}
	return nil
}

// valueString renders v as an S-expression of the value's shape, without positions: a tuple is
// ( ), a list [ ], a string is quoted, nil is nil, a struct is Name{Field: value}, and an error
// is error("message"). With source information, a node is followed by @rule, an element that a
// capture names is preceded by name:, and a capture in a lookahead is written after the elements
// as &name:value.
func valueString(v any) string {
	var b strings.Builder
	stack := []valueFrame{{v: v}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		var push *valueFrame
		switch v := f.v.(type) {
		case *GNode:
			if v == nil {
				b.WriteString("nil")
				f.phase = frameExit
				break
			}
			switch f.phase {
			case frameEnter:
				f.phase = frameElems
				writeCaptureLabels(&b, v, []int{})
				push = &valueFrame{v: v.value, owner: v, path: []int{}}
			case frameElems:
				if v.src != nil && v.src.rule != "" {
					b.WriteString("@" + v.src.rule)
				}
				f.phase = frameExit
			}
		case Tuple, List:
			elems := valueElems(v)
			_, isTuple := v.(Tuple)
			open, close := "[", "]"
			if isTuple {
				open, close = "(", ")"
			}
			if f.phase == frameEnter {
				b.WriteString(open)
				f.phase = frameElems
			}
			if f.i == len(elems) {
				if isTuple && f.owner != nil && len(f.path) == 0 {
					writeSideCaptures(&b, f.owner, f.i > 0)
				}
				b.WriteString(close)
				f.phase = frameExit
				break
			}
			if f.i > 0 {
				b.WriteString(" ")
			}
			next := valueFrame{v: elems[f.i]}
			if isTuple && f.owner != nil {
				p := append(append([]int(nil), f.path...), f.i)
				writeCaptureLabels(&b, f.owner, p)
				next.owner, next.path = f.owner, p
			}
			push = &next
			f.i++
		case *Struct:
			if v == nil {
				b.WriteString("nil")
				f.phase = frameExit
				break
			}
			fields := v.sortedFields()
			if f.phase == frameEnter {
				b.WriteString(v.typ + "{")
				f.phase = frameElems
			}
			if f.i == len(fields) {
				b.WriteString("}")
				f.phase = frameExit
				break
			}
			if f.i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fields[f.i].name + ": ")
			push = &valueFrame{v: fields[f.i].value}
			f.i++
		case *ErrorValue:
			if v == nil {
				b.WriteString("nil")
			} else if v.expected != "" {
				b.WriteString("error(" + strconv.Quote(v.msg) + " expected " + strconv.Quote(v.expected) + ")")
			} else {
				b.WriteString("error(" + strconv.Quote(v.msg) + ")")
			}
			f.phase = frameExit
		case nil:
			b.WriteString("nil")
			f.phase = frameExit
		case string:
			b.WriteString(strconv.Quote(v))
			f.phase = frameExit
		case int:
			b.WriteString(strconv.Itoa(v))
			f.phase = frameExit
		case bool:
			b.WriteString(strconv.FormatBool(v))
			f.phase = frameExit
		default:
			b.WriteString("?")
			f.phase = frameExit
		}
		if f.phase == frameExit {
			stack = stack[:len(stack)-1]
		}
		if push != nil {
			// Elements of a node's value are not nodes' own tuples: only the node's direct
			// value carries the owner for labels.
			if _, isNode := push.v.(*GNode); isNode {
				push.owner, push.path = nil, nil
			}
			stack = append(stack, *push)
		}
	}
	return b.String()
}

// writeCaptureLabels writes the labels of the captures of owner located at path.
func writeCaptureLabels(b *strings.Builder, owner *GNode, path []int) {
	if owner.src == nil {
		return
	}
	for _, c := range owner.src.caps {
		if !c.side && equalPath(c.path, path) {
			b.WriteString(c.name + ":")
		}
	}
}

// writeSideCaptures writes the lookahead captures of owner after its elements.
func writeSideCaptures(b *strings.Builder, owner *GNode, sep bool) {
	if owner.src == nil {
		return
	}
	for _, c := range owner.src.caps {
		if !c.side {
			continue
		}
		if sep {
			b.WriteString(" ")
		}
		sep = true
		b.WriteString("&" + c.name + ":" + valueString(c.value))
	}
}

func equalPath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
