// Package value defines Node, the value that parsing produces, with its kinds, fields, JSON form
// and traversal. The parsers, the Document, the compiler and the structural query build and read
// nodes through D, which gives the representation of a node; the public API exposes Node through
// an alias and only its methods.
package value

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Names of the reserved node types.
const (
	TypeString  = "string"  // a node whose value is its text (literal, character class, atomic, and so on)
	TypeTuple   = "tuple"   // a sequence, or an operator of an operators rule without an action
	TypeList    = "list"    // a repetition
	TypeMissing = "missing" // literal inserted at an empty source range
	TypeError   = "error"   // range skipped by error recovery

	// Kinds of the value model that are not nodes. Their names cannot be type names.
	typeBox        = "(box)"   // a value other than a node, carried where a node pointer is expected
	TypeTupleValue = "(tuple)" // a tuple value: the Value of a tuple node
	TypeListValue  = "(list)"  // a list value: the Value of a list node, or a list made by an action
)

// Node is a value produced by parsing. It is a CST node, a terminal, a list, or a struct built by
// an action. In JSON it is an object with the members type, rule (if any), start, end, text (if
// any), children (if any) and fields (if any).
type Node struct {
	d Data
}

// Data is the representation of a Node. The packages of the engine read and change it through
// D; it is not part of the public Node API, which is the methods of Node.
type Data struct {
	// Kind holds the type and rule names (Type, Rule). Nodes share it: names are interned.
	Kind *NodeKind
	// Start and End are the range in the input, in the parse's position unit (End is exclusive).
	Start, End int32
	// Text is the string value of a string node.
	Text string
	// Children are the elements of a tuple or list value. An optional that did not match is nil.
	Children []*Node
	// Fields are the struct fields and captures. Each value is a *Node, int, string, bool, or nil.
	Fields Fields

	// Fresh reports that the node was created in the rule body currently being evaluated and has not
	// yet been returned to any rule. That rule may freely modify a fresh node.
	Fresh bool
	// Terminal reports that the node is a terminal.
	Terminal bool
	// Labeled reports that the captures in Fields are output (source information was requested).
	Labeled bool
	// Gen is the number of Document edits that the positions account for.
	Gen uint32
	// Box is the value of an action that is not a node (a KindValue node).
	Box any
}

// D returns the representation of n.
func D(n *Node) *Data { return &n.d }

// Of returns the node with the representation d.
func Of(d Data) Node { return Node{d: d} }

// Ptr returns a new node with the representation d.
func Ptr(d Data) *Node { return &Node{d: d} }

// Type returns the node's type name: the kind of a node of the value model (string, tuple, list,
// error, or Missing for an inserted literal), or otherwise the name of a type defined in the grammar.
func (n *Node) Type() string { return n.d.Kind.TypeName() }

// Rule returns the name of the rule that produced the node, if it was produced by a rule without
// an action, or "".
func (n *Node) Rule() string { return n.d.Kind.RuleName() }

// MarshalJSON encodes the node as an object (see Node).
func (n *Node) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	if err := n.WriteJSON(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// NodeKind is the pair of type and rule names of nodes. Kinds are interned (KindOf), so that a node
// holds one pointer for both names.
type NodeKind struct {
	Type, Rule string
	Reserved   int // the index of a reserved type in reservedTypes with no rule, or -1
	// Base is the index in reservedTypes of what the nodes are: the reserved type itself, with or
	// without a rule, or for a named node type the string, tuple or list node it converts. It is
	// -1 for a struct.
	Base int
}

// TypeName returns the type name of k, or "" for a nil kind.
func (k *NodeKind) TypeName() string {
	if k == nil {
		return ""
	}
	return k.Type
}

// IsNodeType reports whether k is the kind of a named node type.
func (k *NodeKind) IsNodeType() bool {
	return k != nil && k.Base >= 0 && k.Type != reservedTypes[k.Base]
}

// RuleName returns the rule name of k, or "" for a nil kind.
func (k *NodeKind) RuleName() string {
	if k == nil {
		return ""
	}
	return k.Rule
}

var reservedTypes = [...]string{TypeString, TypeTuple, TypeList, TypeError, TypeMissing, typeBox, TypeTupleValue, TypeListValue}

// Kinds of the reserved types without a rule.
var (
	MatchKind   = KindOf(TypeString, "")
	SeqKind     = KindOf(TypeTuple, "")
	ListKind    = KindOf(TypeList, "")
	ErrorKind   = KindOf(TypeError, "")
	MissingKind = KindOf(TypeMissing, "")

	BoxKind        = KindOf(typeBox, "")
	TupleValueKind = KindOf(TypeTupleValue, "")
	ListValueKind  = KindOf(TypeListValue, "")
)

// IsNode reports whether n is a node of the value model (a string node, a tuple or list node, a
// node of a named node type, or an error value) rather than a struct, a box or a list or tuple
// value.
func IsNode(n *Node) bool {
	if k := n.d.Kind; k != nil && k.Base >= 0 && k.Base <= baseMissing {
		return true
	}
	return n.d.Terminal
}

// UnboxNode returns the value that the carrier n stands for: the box's value for a box, nil for
// nil, and n otherwise.
func UnboxNode(n *Node) any {
	if n == nil {
		return nil
	}
	if n.d.Kind == BoxKind {
		return n.d.Box
	}
	return n
}

// UnboxValue is UnboxNode for a value that may be a carrier.
func UnboxValue(v any) any {
	if n, ok := v.(*Node); ok {
		return UnboxNode(n)
	}
	return v
}

var kinds sync.Map // [2]string → *NodeKind

// KindOf returns the interned kind of the type and rule names. It is meant for compilation and
// loading; parsing uses kinds prepared then.
func KindOf(typ, rule string) *NodeKind {
	key := [2]string{typ, rule}
	if k, ok := kinds.Load(key); ok {
		return k.(*NodeKind)
	}
	k := &NodeKind{Type: typ, Rule: rule, Reserved: -1, Base: -1}
	for i, t := range reservedTypes {
		if t == typ {
			k.Base = i
			if rule == "" {
				k.Reserved = i
			}
		}
	}
	actual, _ := kinds.LoadOrStore(key, k)
	return actual.(*NodeKind)
}

// The indexes in reservedTypes of the bases of node kinds.
const (
	baseString = iota
	BaseTuple
	BaseList
	baseError
	baseMissing
)

// NodeTypeKinds are the kinds of the nodes of a named node type, by the base (baseString,
// baseTuple, baseList) of the node converted to it. They have no rule.
type NodeTypeKinds [BaseList + 1]*NodeKind

// NewNodeTypeKinds returns the interned kinds of the named node type typ.
func NewNodeTypeKinds(typ string) *NodeTypeKinds {
	var ks NodeTypeKinds
	for base := range ks {
		// The key cannot be that of a rule, whose name has no NUL.
		key := [2]string{typ, "\x00" + reservedTypes[base]}
		k, ok := kinds.Load(key)
		if !ok {
			k, _ = kinds.LoadOrStore(key, &NodeKind{Type: typ, Reserved: -1, Base: base})
		}
		ks[base] = k.(*NodeKind)
	}
	return &ks
}

// Convert returns the node of the named node type of ks that the value v converts to: a copy of
// v with the same range and content, or v itself if it already has the type. It reports false if
// v is not a string, tuple or list node.
func (ks *NodeTypeKinds) Convert(v any) (*Node, bool) {
	n, ok := v.(*Node)
	if !ok || n == nil || n.d.Kind == nil || n.d.Kind.Base < 0 || n.d.Kind.Base > BaseList {
		return nil, false
	}
	k := ks[n.d.Kind.Base]
	if n.d.Kind == k {
		return n, true
	}
	c := *n
	c.d.Kind, c.d.Fresh = k, false
	if n.d.Fresh {
		// The rule that made n may still change it; the copy keeps what it holds now.
		c.d.Children = slices.Clone(n.d.Children)
	}
	c.d.Fields = slices.Clone(n.d.Fields)
	return &c, true
}

// ConvertOwned gives n, a node that nothing else holds, the named node type of ks in place. It
// reports false if n is not a string, tuple or list node.
func (ks *NodeTypeKinds) ConvertOwned(n *Node) bool {
	if n == nil || n.d.Kind == nil || n.d.Kind.Base < 0 || n.d.Kind.Base > BaseList {
		return false
	}
	n.d.Kind = ks[n.d.Kind.Base]
	return true
}

// RuleKinds are the kinds of a rule's nodes: those of the reserved types, which the rule gives its
// name to (finish).
type RuleKinds struct {
	reserved [len(reservedTypes)]*NodeKind
}

func NewRuleKinds(rule string) RuleKinds {
	var rk RuleKinds
	for i, t := range reservedTypes {
		rk.reserved[i] = KindOf(t, rule)
	}
	return rk
}

// Named returns the kind k with the rule's name. A node of a named node type keeps no rule.
func (rk *RuleKinds) Named(k *NodeKind, rule string) *NodeKind {
	if k.Reserved >= 0 {
		return rk.reserved[k.Reserved]
	}
	if k.Base >= 0 {
		return k
	}
	return KindOf(k.Type, rule)
}

// NodeField is a field of a node (a struct field or a capture).
type NodeField struct {
	Name  string
	Value any
}

// Fields is the list of a node's fields. Each name appears at most once.
// In JSON it is an object with names in lexicographic order.
type Fields []NodeField

// Get returns the value of the field name.
func (fs Fields) Get(name string) (any, bool) {
	for i := range fs {
		if fs[i].Name == name {
			return fs[i].Value, true
		}
	}
	return nil, false
}

// SetField sets the field name of fs to v (adding it if absent).
func SetField(fs *Fields, name string, v any) {
	for i := range *fs {
		if (*fs)[i].Name == name {
			(*fs)[i].Value = v
			return
		}
	}
	*fs = append(*fs, NodeField{name, v})
}

// sorted returns a copy sorted lexicographically by name.
func (fs Fields) sorted() Fields {
	c := append(Fields(nil), fs...)
	sort.Slice(c, func(i, j int) bool { return c[i].Name < c[j].Name })
	return c
}

// MarshalJSON encodes the fields as an object with names in lexicographic order.
func (fs Fields) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fs.sorted() {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Name)
		b.Write(k)
		b.WriteByte(':')
		if err := writeJSONValue(&b, f.Value); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Clone returns a deep copy of the tree n. Subtrees shared within the tree stay shared in the
// copy. A Document moves the nodes it reuses after an edit, which changes trees returned by its
// earlier parses; a clone keeps a tree as it was.
func (n *Node) Clone() *Node {
	return cloneNode(n, map[*Node]*Node{})
}

func cloneNode(n *Node, seen map[*Node]*Node) *Node {
	if n == nil {
		return nil
	}
	if c, ok := seen[n]; ok {
		return c
	}
	c := new(Node)
	*c = *n
	seen[n] = c
	if n.d.Children != nil {
		c.d.Children = make([]*Node, len(n.d.Children))
		for i, ch := range n.d.Children {
			c.d.Children[i] = cloneNode(ch, seen)
		}
	}
	if n.d.Fields != nil {
		c.d.Fields = make(Fields, len(n.d.Fields))
		for i, f := range n.d.Fields {
			if vn, ok := f.Value.(*Node); ok {
				f.Value = cloneNode(vn, seen)
			}
			c.d.Fields[i] = f
		}
	}
	return c
}

// IsTerminal reports whether the node holds text: a string node or an error value.
func (n *Node) IsTerminal() bool { return n != nil && n.d.Terminal }

// Kind is what a Node stands for in the value model.
type Kind uint8

const (
	// KindString is a node whose value is a string, the text it matched: Text.
	KindString Kind = iota
	// KindTuple is a node whose value is a tuple: Len and Elem.
	KindTuple
	// KindList is a node whose value is a list: Len and Elem.
	KindList
	// KindError is an error value of recovery: Text is its message.
	KindError
	// KindStruct is a struct built by an action: Type and Field.
	KindStruct
	// KindTupleValue is a tuple built by an action, without a range: Len and Elem.
	KindTupleValue
	// KindListValue is a list built by an action, without a range: Len and Elem.
	KindListValue
	// KindValue is any other value built by an action, such as an int or a bool: Value.
	KindValue
)

var kindNames = [...]string{"string", "tuple", "list", "error", "struct", "tuple value", "list value", "value"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

// Kind returns what the node stands for.
func (n *Node) Kind() Kind {
	switch ShapeOf(n) {
	case ShapeString:
		return KindString
	case ShapeTuple:
		return KindTuple
	case ShapeList:
		return KindList
	case ShapeError:
		return KindError
	case ShapeBox:
		return KindValue
	case ShapeTupleValue:
		return KindTupleValue
	case ShapeListValue:
		return KindListValue
	}
	return KindStruct
}

// Start returns the start of the range of the input that the node spans, in the position unit of
// the parse.
func (n *Node) Start() int { return int(n.d.Start) }

// End returns the end of the range of the input that the node spans (exclusive), in the position
// unit of the parse.
func (n *Node) End() int { return int(n.d.End) }

// Text returns the string value of a KindString node, and the message of a KindError value; it
// returns "" for other nodes and nil.
func (n *Node) Text() string {
	if n == nil {
		return ""
	}
	switch ShapeOf(n) {
	case ShapeString:
		return n.d.Text
	case ShapeError:
		msg, _ := n.d.Fields.Get("message")
		s, _ := msg.(string)
		return s
	}
	return ""
}

// Len returns the number of elements of a tuple or a list (KindTuple, KindList, KindTupleValue,
// KindListValue), and 0 for other nodes and nil.
func (n *Node) Len() int {
	if n == nil {
		return 0
	}
	switch ShapeOf(n) {
	case ShapeTuple, ShapeList, ShapeTupleValue, ShapeListValue:
		return len(n.d.Children)
	}
	return 0
}

// Elem returns the element i of a tuple or a list: a node, or nil for an optional that did not
// match. It panics if i is out of the range [0, Len()).
func (n *Node) Elem(i int) *Node {
	if i < 0 || i >= n.Len() {
		panic(fmt.Sprintf("pego: element %d of a node with %d elements", i, n.Len()))
	}
	return n.d.Children[i]
}

// Elems returns the elements of a tuple or a list in order, as Elem does.
func (n *Node) Elems() iter.Seq2[int, *Node] {
	return func(yield func(int, *Node) bool) {
		for i := range n.Len() {
			if !yield(i, n.d.Children[i]) {
				return
			}
		}
	}
}

// Value returns the value of a KindValue node (such as an int, a string or a bool) and the string
// of a KindString node; it returns nil for other nodes and nil.
func (n *Node) Value() any {
	if n == nil {
		return nil
	}
	switch ShapeOf(n) {
	case ShapeBox:
		return n.d.Box
	case ShapeString:
		return n.d.Text
	}
	return nil
}

// Field returns the value of the field name of a struct, and, with source information, the value
// of the capture name of a node: a *Node, an int, a string, a bool, or nil.
func (n *Node) Field(name string) any {
	if !n.fieldsVisible() {
		return nil
	}
	v, _ := FieldOf(n, name)
	return v
}

// FieldOf returns the value of the field name of n.
func FieldOf(n *Node, name string) (any, bool) {
	return n.d.Fields.Get(name)
}

// fieldsVisible reports whether the fields of n are public: those of a struct, and the captures
// of a node with source information.
func (n *Node) fieldsVisible() bool {
	return n != nil && (n.d.Labeled || ShapeOf(n) == ShapeStruct)
}

// Fields returns the fields of a struct, and, with source information, the captures of a node,
// in the order the action wrote them.
func (n *Node) Fields() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		if !n.fieldsVisible() {
			return
		}
		for _, f := range n.d.Fields {
			if !yield(f.Name, f.Value) {
				return
			}
		}
	}
}

// Shape is what a Node stands for in the value model.
type Shape uint8

const (
	ShapeString     Shape = iota // a node whose value is a string (Text)
	ShapeTuple                   // a node whose value is a tuple (Children)
	ShapeList                    // a node whose value is a list (Children)
	ShapeError                   // an error value of recovery (Error or Missing)
	ShapeBox                     // a box carrying a value other than a node
	ShapeTupleValue              // a tuple value (Children)
	ShapeListValue               // a list value (Children)
	ShapeStruct                  // a struct value (Fields)
)

// baseShapes are the shapes of the nodes of the kinds of each base (NodeKind.Base).
var baseShapes = [len(reservedTypes)]Shape{ShapeString, ShapeTuple, ShapeList, ShapeError, ShapeError, ShapeBox, ShapeTupleValue, ShapeListValue}

// ShapeOf returns what n stands for in the value model.
func ShapeOf(n *Node) Shape {
	if k := n.d.Kind; k != nil && k.Base >= 0 {
		return baseShapes[k.Base]
	}
	if n.d.Terminal {
		return ShapeString
	}
	return ShapeStruct
}

// ValueTypeName names the type of a value of the given shape in messages, in the terms of the
// value model: node[string] for a string node, node[(_, _)] and node[[]_] for tuple and list
// nodes (a tuple has elems elements), error for an error value, (_, _) and []_ for tuple and list
// values, and name for a struct.
func ValueTypeName(shape Shape, name string, elems int) string {
	switch shape {
	case ShapeString:
		return "node[string]"
	case ShapeTuple:
		return "node[" + tupleTerm(elems) + "]"
	case ShapeList:
		return "node[[]_]"
	case ShapeError:
		return "error"
	case ShapeTupleValue:
		return tupleTerm(elems)
	case ShapeListValue:
		return "[]_"
	}
	return name
}

// tupleTerm is the type term of a tuple of n elements of unknown types.
func tupleTerm(n int) string {
	if n == 1 {
		return "(_,)"
	}
	return "(" + strings.TrimSuffix(strings.Repeat("_, ", n), ", ") + ")"
}

// CaptureLocation is where a capture of a node is: a path of tuple indexes into the node's
// value, or, for a capture whose value is not an element (one in a positive lookahead), its value.
type CaptureLocation struct {
	Name  string
	Path  []int
	Side  bool
	Value any
}

// CaptureLocations returns the locations of the captures of the tuple node n, in name order. A
// capture is located by the identity of its value among the elements of n's tuple and of the
// tuples nested in it, nearest first; a capture that did not match is left out.
func CaptureLocations(n *Node) []CaptureLocation {
	var out []CaptureLocation
	for _, f := range n.d.Fields.sorted() {
		v, ok := f.Value.(*Node)
		if !ok || v == nil {
			if f.Value != nil {
				out = append(out, CaptureLocation{Name: f.Name, Side: true, Value: f.Value})
			}
			continue
		}
		if path, found := findElement(n, v); found {
			out = append(out, CaptureLocation{Name: f.Name, Path: path})
		} else {
			out = append(out, CaptureLocation{Name: f.Name, Side: true, Value: v})
		}
	}
	return out
}

// findElement returns the path of x among the elements of the tuple node n and of the tuple nodes
// nested in it, breadth first.
func findElement(n, x *Node) ([]int, bool) {
	type item struct {
		n    *Node
		path []int
	}
	queue := []item{{n, nil}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		for i, c := range it.n.d.Children {
			if c == nil {
				continue
			}
			path := append(append([]int{}, it.path...), i)
			if c == x {
				return path, true
			}
			if ShapeOf(c) == ShapeTuple && c.Rule() == "" {
				queue = append(queue, item{c, path})
			}
		}
	}
	return nil, false
}

// String renders the value as an S-expression of its shape, without positions, for tests and
// debugging: a string is quoted, a tuple is ( ), a list [ ], nil is nil, a struct is
// Name{Field: value}, and an error is error("message"). A node of a named node type N is written
// with N before its brackets: N("text"), N( ) and N[ ]. With source information, a node is
// followed by @rule, a captured element is preceded by name:, and a capture whose value is not an
// element is written after the elements as &name:value.
func (n *Node) String() string {
	var b strings.Builder
	writeValue(&b, n)
	return b.String()
}

// sexprFrame is a step of writeValue, which keeps its own stack for deep trees.
type sexprFrame struct {
	v     any
	i     int
	enter bool
	// labels maps the elements of the tuples being written to the names that capture them.
	labels map[*Node][]string
	side   []CaptureLocation
	close  string
}

func writeValue(b *strings.Builder, v any) {
	stack := []sexprFrame{{v: v, enter: true}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		n, isNode := f.v.(*Node)
		if !isNode || n == nil {
			writeScalar(b, f.v)
			stack = stack[:len(stack)-1]
			continue
		}
		sh := ShapeOf(n)
		if f.enter {
			f.enter = false
			switch sh {
			case ShapeBox:
				f.v, f.enter = n.d.Box, true
				continue
			case ShapeString:
				if n.d.Kind.IsNodeType() {
					b.WriteString(n.Type() + "(" + strconv.Quote(n.d.Text))
					f.close = ")" + ruleSuffix(n)
				} else {
					b.WriteString(strconv.Quote(n.d.Text))
					f.close = ruleSuffix(n)
				}
				f.i = -1
			case ShapeError:
				msg, _ := n.d.Fields.Get("message")
				b.WriteString("error(" + strconv.Quote(fmt.Sprint(msg)))
				if expected, ok := n.d.Fields.Get("expected"); ok {
					b.WriteString(" expected " + strconv.Quote(fmt.Sprint(expected)))
				}
				b.WriteString(")")
				f.i = -1
			case ShapeTuple, ShapeTupleValue:
				b.WriteString(nodeTypePrefix(n) + "(")
				f.close = ")" + ruleSuffix(n)
				if sh == ShapeTuple && n.d.Labeled {
					f.labels = map[*Node][]string{}
					for _, c := range CaptureLocations(n) {
						if c.Side {
							f.side = append(f.side, c)
							continue
						}
						x := n
						for _, i := range c.Path {
							x = x.d.Children[i]
						}
						f.labels[x] = append(f.labels[x], c.Name)
					}
				}
			case ShapeList, ShapeListValue:
				b.WriteString(nodeTypePrefix(n) + "[")
				f.close = "]" + ruleSuffix(n)
			case ShapeStruct:
				b.WriteString(n.Type() + "{")
				f.close = "}"
			}
		}
		if f.i < 0 {
			b.WriteString(f.close)
			stack = stack[:len(stack)-1]
			continue
		}
		if sh == ShapeStruct {
			fields := n.d.Fields.sorted()
			if f.i == len(fields) {
				b.WriteString(f.close)
				stack = stack[:len(stack)-1]
				continue
			}
			if f.i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fields[f.i].Name + ": ")
			f.i++
			stack = append(stack, sexprFrame{v: fields[f.i-1].Value, enter: true})
			continue
		}
		if f.i == len(n.d.Children) {
			for j, c := range f.side {
				if f.i > 0 || j > 0 {
					b.WriteString(" ")
				}
				b.WriteString("&" + c.Name + ":")
				var side strings.Builder
				writeValue(&side, c.Value)
				b.WriteString(side.String())
			}
			b.WriteString(f.close)
			stack = stack[:len(stack)-1]
			continue
		}
		if f.i > 0 {
			b.WriteString(" ")
		}
		c := n.d.Children[f.i]
		f.i++
		labels := f.labels
		for _, name := range labels[c] {
			b.WriteString(name + ":")
		}
		next := sexprFrame{v: c, enter: true}
		if c != nil && ShapeOf(c) == ShapeTuple && c.Rule() == "" {
			// A tuple nested in the node's own tuple holds the node's captures too.
			next.labels = labels
		}
		stack = append(stack, next)
	}
}

// nodeTypePrefix returns the name that a node of a named node type puts before its opening
// bracket, or "".
func nodeTypePrefix(n *Node) string {
	if n.d.Kind.IsNodeType() {
		return n.Type()
	}
	return ""
}

func ruleSuffix(n *Node) string {
	if r := n.Rule(); r != "" {
		return "@" + r
	}
	return ""
}

func writeScalar(b *strings.Builder, v any) {
	switch v := v.(type) {
	case nil:
		b.WriteString("nil")
	case *Node:
		b.WriteString("nil")
	case string:
		b.WriteString(strconv.Quote(v))
	case int:
		b.WriteString(strconv.Itoa(v))
	case bool:
		b.WriteString(strconv.FormatBool(v))
	default:
		fmt.Fprintf(b, "%v", v)
	}
}

// clone returns a shallow copy of the node.
func (n *Node) clone() *Node {
	c := *n
	c.d.Fresh = true
	if n.d.Fields != nil {
		c.d.Fields = append(Fields(nil), n.d.Fields...)
	}
	return &c
}
