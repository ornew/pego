package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Names of the reserved node types.
const (
	TypeMatch    = "Match"    // terminal (literal, character class, atomic, and so on)
	TypeSeq      = "Seq"      // sequence
	TypeList     = "List"     // repetition
	TypeOperator = "Operator" // application of a OperatorsRule operator without an action
	TypeMissing  = "Missing"  // literal inserted at an empty source range
	TypeError    = "Error"    // range skipped by error recovery

	// Kinds of the value model that are not nodes. Their names cannot be type names.
	typeBox        = "(box)"   // a value other than a node, carried where a node pointer is expected
	typeTupleValue = "(tuple)" // a tuple value: the Value of a tuple node
	typeListValue  = "(list)"  // a list value: the Value of a list node, or a list made by an action
)

// Node is a value produced by parsing. It is a CST node, a terminal, a list, or a struct built by
// an action. In JSON it is an object with the members type, rule (if any), start, end, text (if
// any), children (if any) and fields (if any).
type Node struct {
	// kind holds the type and rule names (Type, Rule). Nodes share it: names are interned.
	kind *nodeKind
	// Start and End are the range in the input, in the parse's position unit (End is exclusive).
	Start, End int32
	// Text is the text of a terminal.
	Text string
	// Children are the children of Seq, List, and Operator nodes. Omitted elements are nil.
	Children []*Node
	// Fields are the struct fields and captures. Each value is a *Node, int, string, bool, or nil.
	Fields Fields

	// fresh reports that the node was created in the rule body currently being evaluated and has not
	// yet been returned to any rule. That rule may freely modify a fresh node.
	fresh bool
	// terminal reports that the node is a terminal.
	terminal bool
	// labeled reports that the captures in Fields are output (source information was requested).
	labeled bool
	// gen is the number of Document edits that the positions account for (see moveTree).
	gen uint32
	// box is the value carried by a Box.
	box any
}

// Type returns the node's type name: a reserved type (Match, Seq, List, Operator, Error, Missing) for CST
// nodes, or otherwise the name of a type defined in the grammar.
func (n *Node) Type() string { return n.kind.typeName() }

// Rule returns the name of the rule that produced the node, if it was produced by a rule without
// an action, or "".
func (n *Node) Rule() string { return n.kind.ruleName() }

// nodeJSON is the JSON form of a Node.
type nodeJSON struct {
	Type     string  `json:"type"`
	Rule     string  `json:"rule,omitempty"`
	Start    int32   `json:"start"`
	End      int32   `json:"end"`
	Text     string  `json:"text,omitempty"`
	Children []*Node `json:"children,omitempty"`
	Fields   Fields  `json:"fields,omitempty"`
}

// MarshalJSON encodes the node as an object (see Node).
func (n *Node) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	if err := n.WriteJSON(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// nodeKind is the pair of type and rule names of nodes. Kinds are interned (kindOf), so that a node
// holds one pointer for both names.
type nodeKind struct {
	typ, rule string
	reserved  int // the index of a reserved type in reservedTypes with no rule, or -1
}

func (k *nodeKind) typeName() string {
	if k == nil {
		return ""
	}
	return k.typ
}

func (k *nodeKind) ruleName() string {
	if k == nil {
		return ""
	}
	return k.rule
}

var reservedTypes = [...]string{TypeMatch, TypeSeq, TypeList, TypeOperator, TypeError, TypeMissing, typeBox, typeTupleValue, typeListValue}

// Kinds of the reserved types without a rule.
var (
	kindMatch    = kindOf(TypeMatch, "")
	kindSeq      = kindOf(TypeSeq, "")
	kindList     = kindOf(TypeList, "")
	kindOperator = kindOf(TypeOperator, "")
	kindError    = kindOf(TypeError, "")
	kindMissing  = kindOf(TypeMissing, "")

	kindBox        = kindOf(typeBox, "")
	kindTupleValue = kindOf(typeTupleValue, "")
	kindListValue  = kindOf(typeListValue, "")
)

// isNode reports whether n is a node of the value model (a string, tuple, list or error node)
// rather than a struct, a box or a list or tuple value.
func (n *Node) isNode() bool {
	switch n.kind.typeName() {
	case TypeMatch, TypeSeq, TypeList, TypeOperator, TypeError, TypeMissing:
		return true
	}
	return false
}

// unboxNode returns the value that the carrier n stands for: the box's value for a box, nil for
// nil, and n otherwise.
func unboxNode(n *Node) any {
	if n == nil {
		return nil
	}
	if n.kind == kindBox {
		return n.box
	}
	return n
}

// unboxValue is unboxNode for a value that may be a carrier.
func unboxValue(v any) any {
	if n, ok := v.(*Node); ok {
		return unboxNode(n)
	}
	return v
}

// nodeValue returns the Value of the node n: its text for a string node, and a tuple or list
// value sharing its elements otherwise.
func (p *parser) nodeValue(n *Node) any {
	switch {
	case n.terminal:
		return n.Text
	case n.kind.typeName() == TypeList:
		return p.newNode(Node{kind: kindListValue, Start: n.Start, End: n.End, Children: n.Children})
	}
	return p.newNode(Node{kind: kindTupleValue, Start: n.Start, End: n.End, Children: n.Children})
}

var kinds sync.Map // [2]string → *nodeKind

// kindOf returns the interned kind of the type and rule names. It is meant for compilation and
// loading; parsing uses kinds prepared then.
func kindOf(typ, rule string) *nodeKind {
	key := [2]string{typ, rule}
	if k, ok := kinds.Load(key); ok {
		return k.(*nodeKind)
	}
	k := &nodeKind{typ: typ, rule: rule, reserved: -1}
	if rule == "" {
		for i, t := range reservedTypes {
			if t == typ {
				k.reserved = i
			}
		}
	}
	actual, _ := kinds.LoadOrStore(key, k)
	return actual.(*nodeKind)
}

// typeKind returns the kind of nodes of the grammar's type typ without a rule.
func (prog *Program) typeKind(typ string) *nodeKind {
	if k := prog.typeKinds[typ]; k != nil {
		return k
	}
	return kindOf(typ, "")
}

// ruleKinds are the kinds of a rule's nodes: those of the reserved types, which the rule gives its
// name to (finish), and of its terminal type.
type ruleKinds struct {
	reserved [len(reservedTypes)]*nodeKind
	term     *nodeKind
	// termValue is the kind of the terminal type without the rule name (no source information).
	termValue *nodeKind
}

func newRuleKinds(rule, terminalType string) ruleKinds {
	var rk ruleKinds
	for i, t := range reservedTypes {
		rk.reserved[i] = kindOf(t, rule)
	}
	if terminalType != "" {
		rk.term = kindOf(terminalType, rule)
		rk.termValue = kindOf(terminalType, "")
	}
	return rk
}

// named returns the kind k with the rule's name.
func (rk *ruleKinds) named(k *nodeKind, rule string) *nodeKind {
	if k.reserved >= 0 {
		return rk.reserved[k.reserved]
	}
	return kindOf(k.typ, rule)
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

// set sets the field name to v (adding it if absent).
func (fs *Fields) set(name string, v any) {
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
	if n.Children != nil {
		c.Children = make([]*Node, len(n.Children))
		for i, ch := range n.Children {
			c.Children[i] = cloneNode(ch, seen)
		}
	}
	if n.Fields != nil {
		c.Fields = make(Fields, len(n.Fields))
		for i, f := range n.Fields {
			if vn, ok := f.Value.(*Node); ok {
				f.Value = cloneNode(vn, seen)
			}
			c.Fields[i] = f
		}
	}
	return c
}

// IsTerminal reports whether the node is a terminal (Match or a terminal type).
func (n *Node) IsTerminal() bool { return n != nil && n.terminal }

// Field returns the value of the field name.
func (n *Node) Field(name string) any {
	if n == nil {
		return nil
	}
	v, _ := n.Fields.Get(name)
	return v
}

// nodeShape is what a Node stands for in the value model.
type nodeShape uint8

const (
	shapeString     nodeShape = iota // a node whose value is a string (Text)
	shapeTuple                       // a node whose value is a tuple (Children)
	shapeList                        // a node whose value is a list (Children)
	shapeError                       // an error value of recovery (Error or Missing)
	shapeBox                         // a box carrying a value other than a node
	shapeTupleValue                  // a tuple value (Children)
	shapeListValue                   // a list value (Children)
	shapeStruct                      // a struct value (Fields)
)

func (n *Node) shape() nodeShape {
	switch n.kind.typeName() {
	case TypeSeq, TypeOperator:
		return shapeTuple
	case TypeList:
		return shapeList
	case TypeError, TypeMissing:
		return shapeError
	case typeBox:
		return shapeBox
	case typeTupleValue:
		return shapeTupleValue
	case typeListValue:
		return shapeListValue
	}
	if n.terminal {
		return shapeString
	}
	return shapeStruct
}

// captureLocation is where a capture of a node is: a path of tuple indexes into the node's
// value, or, for a capture whose value is not an element (one in a positive lookahead), its value.
type captureLocation struct {
	name  string
	path  []int
	side  bool
	value any
}

// captureLocations returns the locations of the captures of the tuple node n, in name order. A
// capture is located by the identity of its value among the elements of n's tuple and of the
// tuples nested in it, nearest first; a capture that did not match is left out.
func captureLocations(n *Node) []captureLocation {
	var out []captureLocation
	for _, f := range n.Fields.sorted() {
		v, ok := f.Value.(*Node)
		if !ok || v == nil {
			if f.Value != nil {
				out = append(out, captureLocation{name: f.Name, side: true, value: f.Value})
			}
			continue
		}
		if path, found := findElement(n, v); found {
			out = append(out, captureLocation{name: f.Name, path: path})
		} else {
			out = append(out, captureLocation{name: f.Name, side: true, value: v})
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
		for i, c := range it.n.Children {
			if c == nil {
				continue
			}
			path := append(append([]int{}, it.path...), i)
			if c == x {
				return path, true
			}
			if c.shape() == shapeTuple && c.Rule() == "" {
				queue = append(queue, item{c, path})
			}
		}
	}
	return nil, false
}

// String renders the value as an S-expression of its shape, without positions, for tests and
// debugging: a string is quoted, a tuple is ( ), a list [ ], nil is nil, a struct is
// Name{Field: value}, and an error is error("message"). With source information, a node is
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
	side   []captureLocation
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
		sh := n.shape()
		if f.enter {
			f.enter = false
			switch sh {
			case shapeBox:
				f.v, f.enter = n.box, true
				continue
			case shapeString:
				b.WriteString(strconv.Quote(n.Text))
				f.close = ruleSuffix(n)
				f.i = -1
			case shapeError:
				msg, _ := n.Fields.Get("message")
				b.WriteString("error(" + strconv.Quote(fmt.Sprint(msg)))
				if expected, ok := n.Fields.Get("expected"); ok {
					b.WriteString(" expected " + strconv.Quote(fmt.Sprint(expected)))
				}
				b.WriteString(")")
				f.i = -1
			case shapeTuple, shapeTupleValue:
				b.WriteString("(")
				f.close = ")" + ruleSuffix(n)
				if sh == shapeTuple && n.labeled {
					f.labels = map[*Node][]string{}
					for _, c := range captureLocations(n) {
						if c.side {
							f.side = append(f.side, c)
							continue
						}
						x := n
						for _, i := range c.path {
							x = x.Children[i]
						}
						f.labels[x] = append(f.labels[x], c.name)
					}
				}
			case shapeList, shapeListValue:
				b.WriteString("[")
				f.close = "]" + ruleSuffix(n)
			case shapeStruct:
				b.WriteString(n.Type() + "{")
				f.close = "}"
			}
		}
		if f.i < 0 {
			b.WriteString(f.close)
			stack = stack[:len(stack)-1]
			continue
		}
		if sh == shapeStruct {
			fields := n.Fields.sorted()
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
		if f.i == len(n.Children) {
			for j, c := range f.side {
				if f.i > 0 || j > 0 {
					b.WriteString(" ")
				}
				b.WriteString("&" + c.name + ":")
				var side strings.Builder
				writeValue(&side, c.value)
				b.WriteString(side.String())
			}
			b.WriteString(f.close)
			stack = stack[:len(stack)-1]
			continue
		}
		if f.i > 0 {
			b.WriteString(" ")
		}
		c := n.Children[f.i]
		f.i++
		labels := f.labels
		for _, name := range labels[c] {
			b.WriteString(name + ":")
		}
		next := sexprFrame{v: c, enter: true}
		if c != nil && c.shape() == shapeTuple && c.Rule() == "" {
			// A tuple nested in the node's own tuple holds the node's captures too.
			next.labels = labels
		}
		stack = append(stack, next)
	}
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
	c.fresh = true
	if n.Fields != nil {
		c.Fields = append(Fields(nil), n.Fields...)
	}
	return &c
}
