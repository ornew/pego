package value

// The functions in this file build and change nodes for the tests of packages outside the engine,
// which cannot reach the fields of a Node. They are functions rather than methods so that they do
// not become part of the public Node API.

// NewNode returns a node of kind k with the range [start, end). text is the string of a
// KindString node, elems the elements of a tuple or list, typ the type name of a KindStruct node,
// fields its fields (or captures), and value the value of a KindValue node.
func NewNode(k Kind, start, end int, text string, elems []*Node, typ string, fields Fields, value any) *Node {
	n := &Node{d: Data{Start: int32(start), End: int32(end), Text: text, Children: elems, Fields: fields, Box: value}}
	switch k {
	case KindString:
		n.d.Kind, n.d.Terminal = MatchKind, true
	case KindTuple:
		n.d.Kind = SeqKind
	case KindList:
		n.d.Kind = ListKind
	case KindError:
		n.d.Kind = ErrorKind
	case KindStruct:
		n.d.Kind = KindOf(typ, "")
	case KindTupleValue:
		n.d.Kind = TupleValueKind
	case KindListValue:
		n.d.Kind = ListValueKind
	case KindValue:
		n.d.Kind = BoxKind
	}
	return n
}

// SetNodeElems replaces the elements of the tuple or list n, which can make a cycle.
func SetNodeElems(n *Node, elems []*Node) { n.d.Children = elems }

// SetNodeText replaces the string of the KindString node n.
func SetNodeText(n *Node, text string) {
	n.d.Text = text
}

// SetNodeStart replaces the start of the range of n.
func SetNodeStart(n *Node, start int) { n.d.Start = int32(start) }

// SetNodeField sets the field name of n to v, adding it if absent.
func SetNodeField(n *Node, name string, v any) {
	SetField(&n.d.Fields, name, v)
}
