package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Names of the reserved node types.
const (
	TypeMatch    = "Match"    // terminal (literal, character class, atomic, and so on)
	TypeSeq      = "Seq"      // sequence
	TypeList     = "List"     // repetition
	TypeOperator = "Operator" // application of a Pratt operator without an action
	TypeError    = "Error"    // range skipped by error recovery
)

// Node is a value produced by parsing. It is a CST node, a terminal, a list, or a struct built by
// an action.
type Node struct {
	// Type is the node's type name: a reserved type (Match, Seq, List, Operator, Error) for CST
	// nodes, or otherwise the name of a type defined in the grammar.
	Type string `json:"type"`
	// Rule holds the rule name when the node was produced by a rule without an action.
	Rule string `json:"rule,omitempty"`
	// Start and End are the range in the input (in characters; End is exclusive).
	Start int `json:"start"`
	End   int `json:"end"`
	// Text is the text of a terminal.
	Text string `json:"text,omitempty"`
	// Children are the children of Seq, List, and Operator nodes. Omitted elements are nil.
	Children []*Node `json:"children,omitempty"`
	// Fields are the struct fields and captures. Each value is a *Node, int, string, bool, or nil.
	Fields Fields `json:"fields,omitempty"`

	// fresh reports that the node was created in the rule body currently being evaluated and has not
	// yet been returned to any rule. That rule may freely modify a fresh node.
	fresh bool
	// terminal reports that the node is a terminal.
	terminal bool
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
		v, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
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

// String renders the node as an S-expression-like string, for tests and debugging.
//
//	"a"            Match (terminal)
//	Number"12"     user-defined terminal type
//	(Seq "a" "b")  Seq
//	["a" "a"]      List
//	(Op L=1 R=2)   struct
//	x@rule         node produced by the rule rule without an action
func (n *Node) String() string {
	var b strings.Builder
	writeValue(&b, n)
	return b.String()
}

func writeValue(b *strings.Builder, v any) {
	switch v := v.(type) {
	case nil:
		b.WriteString("nil")
	case *Node:
		if v == nil {
			b.WriteString("nil")
			return
		}
		writeNode(b, v)
	case string:
		b.WriteString("`" + v + "`")
	case int:
		b.WriteString(strconv.Itoa(v))
	case bool:
		b.WriteString(strconv.FormatBool(v))
	default:
		fmt.Fprintf(b, "%v", v)
	}
}

func writeNode(b *strings.Builder, n *Node) {
	switch {
	case n.Type == TypeList:
		b.WriteString("[")
		for i, c := range n.Children {
			if i > 0 {
				b.WriteString(" ")
			}
			writeValue(b, c)
		}
		writeFields(b, n, len(n.Children) > 0)
		b.WriteString("]")
	case n.terminal:
		if n.Type != TypeMatch {
			b.WriteString(n.Type)
		}
		b.WriteString(strconv.Quote(n.Text))
		if len(n.Fields) > 0 {
			b.WriteString("{")
			writeFields(b, n, false)
			b.WriteString("}")
		}
	default:
		b.WriteString("(" + n.Type)
		for _, c := range n.Children {
			b.WriteString(" ")
			writeValue(b, c)
		}
		writeFields(b, n, true)
		b.WriteString(")")
	}
	if n.Rule != "" {
		b.WriteString("@" + n.Rule)
	}
}

func writeFields(b *strings.Builder, n *Node, sep bool) {
	for _, f := range n.Fields.sorted() {
		if sep {
			b.WriteString(" ")
		}
		sep = true
		b.WriteString(f.Name + "=")
		writeValue(b, f.Value)
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
