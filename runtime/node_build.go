package runtime

// The functions in this file build and change nodes for the tests of packages outside runtime, which
// cannot reach the fields of a Node. They are for generated parsers and tests; not a stable API.
// They are functions rather than methods so that they are not part of the Node API. Generated
// parsers re-export none of them.

// NewNode returns a node of kind k with the range [start, end). text is the string of a
// KindString node, elems the elements of a tuple or list, typ the type name of a KindStruct node,
// fields its fields (or captures), and value the value of a KindValue node.
//
// NewNode is for generated parsers and tests; not a stable API.
func NewNode(k NodeKind, start, end int, text string, elems []*Node, typ string, fields Fields, value any) *Node {
	n := &Node{RawStart: int32(start), RawEnd: int32(end), RawText: text, RawChildren: elems, RawFields: fields, box: value}
	switch k {
	case KindString:
		n.RawKind, n.RawTerminal = RawKindMatch, true
	case KindTuple:
		n.RawKind = RawKindSeq
	case KindList:
		n.RawKind = RawKindList
	case KindError:
		n.RawKind = kindError
	case KindStruct:
		n.RawKind = KindOf(typ, "")
	case KindTupleValue:
		n.RawKind = kindTupleValue
	case KindListValue:
		n.RawKind = kindListValue
	case KindValue:
		n.RawKind = kindBox
	}
	return n
}

// SetNodeElems replaces the elements of the tuple or list n, which can make a cycle.
//
// SetNodeElems is for generated parsers and tests; not a stable API.
func SetNodeElems(n *Node, elems []*Node) { n.RawChildren = elems }

// SetNodeText replaces the string of the KindString node n.
//
// SetNodeText is for generated parsers and tests; not a stable API.
func SetNodeText(n *Node, text string) {
	n.RawText = text
}

// SetNodeStart replaces the start of the range of n.
//
// SetNodeStart is for generated parsers and tests; not a stable API.
func SetNodeStart(n *Node, start int) { n.RawStart = int32(start) }

// SetNodeField sets the field name of n to v, adding it if absent.
//
// SetNodeField is for generated parsers and tests; not a stable API.
func SetNodeField(n *Node, name string, v any) {
	for i := range n.RawFields {
		if n.RawFields[i].Name == name {
			n.RawFields[i].Value = v
			return
		}
	}
	n.RawFields = append(n.RawFields, NodeField{Name: name, Value: v})
}
