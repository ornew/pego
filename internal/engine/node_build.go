package engine

// The functions in this file build and change nodes for the tests of packages outside the engine,
// which cannot reach the fields of a Node. They are functions rather than methods so that they do
// not become part of the public Node API.

// NewNode returns a node of kind k with the range [start, end). text is the string of a
// KindString node, elems the elements of a tuple or list, typ the type name of a KindStruct node,
// fields its fields (or captures), and value the value of a KindValue node.
func NewNode(k Kind, start, end int, text string, elems []*Node, typ string, fields Fields, value any) *Node {
	n := &Node{start: int32(start), end: int32(end), text: text, children: elems, fields: fields, box: value}
	switch k {
	case KindString:
		n.kind, n.terminal = kindMatch, true
	case KindTuple:
		n.kind = kindSeq
	case KindList:
		n.kind = kindList
	case KindError:
		n.kind = kindError
	case KindStruct:
		n.kind = kindOf(typ, "")
	case KindTupleValue:
		n.kind = kindTupleValue
	case KindListValue:
		n.kind = kindListValue
	case KindValue:
		n.kind = kindBox
	}
	return n
}

// SetNodeElems replaces the elements of the tuple or list n, which can make a cycle.
func SetNodeElems(n *Node, elems []*Node) { n.children = elems }

// SetNodeText replaces the string of the KindString node n.
func SetNodeText(n *Node, text string) {
	n.text = text
}

// SetNodeStart replaces the start of the range of n.
func SetNodeStart(n *Node, start int) { n.start = int32(start) }

// SetNodeField sets the field name of n to v, adding it if absent.
func SetNodeField(n *Node, name string, v any) {
	n.fields.set(name, v)
}
