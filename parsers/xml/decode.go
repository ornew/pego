package xml

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Tree is a document decoded by Decode: its entities expanded, its text and attribute values normalized,
// and the declarations of its internal DTD subset processed.
type Tree struct {
	Version    string // the version of the XML declaration, or "" without one
	Encoding   string // the encoding of the XML declaration, or ""
	Standalone bool   // the XML declaration says standalone="yes"
	DTD        *DTD   // the document type declaration, or nil
	Prolog     []Item // the processing instructions and comments before the root element
	Root       *Elem
	Epilog     []Item // the processing instructions and comments after the root element
	Doc        *Document

	input string
}

// Item is an item of a decoded tree: *Elem, *Text, *PI, *Comment, or *EntityRef for a reference to an
// entity that was not expanded (an external entity, or an undeclared one where XML allows that; see
// Decode).
type Item interface{ treeItem() }

// Elem is a decoded element.
type Elem struct {
	Name     string // the name as written (a qualified name with namespaces)
	Space    string // the namespace name, set by ResolveNamespaces
	Local    string // the local part of the name, set by ResolveNamespaces
	Attrs    []Attr // the specified attributes in document order, then the defaulted ones in declaration order
	Children []Item // the content, adjacent text merged into one *Text
	Src      *Element
	Entity   string // the entity whose replacement text contains the element ("" for the document); Src's Span is relative to that text

	ref int // the position of the outermost entity reference in the document, or -1
}

// Attr is a decoded attribute.
type Attr struct {
	Name      string
	Space     string // the namespace name, set by ResolveNamespaces
	Local     string // the local part of the name, set by ResolveNamespaces
	Value     string // the normalized value (XML 1.0, 3.3.3)
	Defaulted bool   // the value is the default of the attribute-list declaration, not specified in the tag
}

// Text is character data, with references expanded, CDATA sections included and line ends normalized.
type Text struct{ Data string }

func (*Elem) treeItem()      {}
func (*Text) treeItem()      {}
func (*PI) treeItem()        {}
func (*Comment) treeItem()   {}
func (*EntityRef) treeItem() {}

// DTD holds the document type declaration and the declarations of its internal subset that were
// processed. A non-validating processor does not read the external subset or external parameter
// entities; after a reference to a parameter entity it did not read, it does not process the entity and
// attribute-list declarations that follow (unless the document is standalone), because the entity might
// have declared them first. Complete reports whether nothing was left unread.
type DTD struct {
	Name       string
	ExternalID *ExternalID // the external subset, which is not read
	Elements   map[string]*ElementDecl
	Attributes map[string][]*AttributeDecl // by element name, in declaration order
	Entities   map[string]*Entity          // general entities
	Params     map[string]*Entity          // parameter entities
	Notations  map[string]*NotationDecl
	Complete   bool
}

// Entity is a declared entity.
type Entity struct {
	Name      string
	Parameter bool
	Value     string      // the replacement text of an internal entity
	External  *ExternalID // the identifier of an external entity, which is not read
	NData     string      // the notation of an unparsed entity
	Decl      *EntityDecl

	parsed  bool
	content []Content // the replacement text parsed as content
	decls   []Decl    // the replacement text parsed as declarations
	err     error
}

// AttributeDecl is a processed attribute definition of an attribute-list declaration.
type AttributeDecl struct {
	Element, Name string
	Type          string   // CDATA, ID, IDREF, IDREFS, ENTITY, ENTITIES, NMTOKEN, NMTOKENS, NOTATION or ENUMERATION
	Values        []string // the names of an enumeration or a NOTATION type
	Default       string   // #REQUIRED, #IMPLIED, #FIXED, or "" for a default value without #FIXED
	Value         string   // the normalized default value when Default is "" or #FIXED
	Decl          *AttDef
}

// WFError is a violation of a well-formedness constraint found after the parse, or invalid input
// (invalid UTF-8, an unsupported encoding). Pos is in code points; inside the replacement text of an
// entity it is the position of the reference to the entity in the document.
type WFError struct {
	Pos, Line, Col int
	Msg            string
}

func (e *WFError) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// ExpansionLimit bounds the work of expanding entities in one document: each expansion of an internal
// entity costs the length of its replacement text plus one. Decode fails when the total exceeds it,
// which stops exponential expansions ("billion laughs").
var ExpansionLimit = 1 << 24

// Decode parses a document, checks every well-formedness constraint of XML 1.0 that does not need
// external entities, and returns it decoded. The input is the text of the document; DecodeBytes takes
// its bytes in UTF-8 or UTF-16.
//
// References to internal entities are expanded, in text and in attribute values. A reference to an
// external parsed entity, which a non-validating processor need not read, stays in the tree as an
// *EntityRef (in an attribute value it is an error, as XML requires). A reference to an undeclared
// entity is an error where the constraint "Entity Declared" applies: in a document without a DTD, with
// only an internal subset and no parameter-entity references, or with standalone="yes". Elsewhere the
// entity may be declared in what was not read, and the reference stays as an *EntityRef (in an
// attribute value, as its text).
func Decode(input string) (*Tree, error) {
	if !utf8.ValidString(input) {
		i := 0
		for i < len(input) {
			r, n := utf8.DecodeRuneInString(input[i:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			i += n
		}
		pos := utf8.RuneCountInString(input[:i])
		line, col := lineCol(input, pos)
		return nil, &WFError{Pos: pos, Line: line, Col: col, Msg: "invalid UTF-8"}
	}
	doc, err := ParseAST(input)
	if err != nil {
		return nil, err
	}
	return process(doc, input)
}

// DecodeBytes decodes a document from its bytes: it detects the encoding (Transcode) and calls Decode.
func DecodeBytes(data []byte) (*Tree, error) {
	s, err := Transcode(data)
	if err != nil {
		return nil, err
	}
	return Decode(s)
}

// WellFormed reports whether input is a well-formed document, as Decode checks it, returning the first
// error found.
func WellFormed(input string) error {
	_, err := Decode(input)
	return err
}

var predefined = map[string]byte{"lt": '<', "gt": '>', "amp": '&', "apos": '\'', "quot": '"'}

type processor struct {
	input  string
	tree   *Tree
	dtd    *DTD
	strict bool // the constraint Entity Declared applies
	skip   bool // entity and attribute-list declarations are not processed
	open   []*Entity
	budget int
	ref    int    // the position of the outermost entity reference being expanded, or -1
	in     string // the innermost entity being expanded, for messages

	stack []Item // the items of the elements being decoded (see builder)
	buf   []byte // the text being merged (see builder)
	elems []Elem // chunks to allocate from
	texts []Text
	attrs []Attr
	items []Item
}

func process(doc *Document, input string) (*Tree, error) {
	t := &Tree{Doc: doc, input: input}
	p := &processor{input: input, tree: t, strict: true, budget: ExpansionLimit, ref: -1}
	if d := doc.Decl; d != nil {
		t.Version = d.Version.Text
		if d.Encoding != nil {
			t.Encoding = d.Encoding.Text
		}
		t.Standalone = d.Standalone != nil && d.Standalone.Text == "yes"
	}
	if dt := doc.Doctype; dt != nil {
		p.dtd = &DTD{
			Name:       dt.Name.Text,
			ExternalID: dt.ExternalID,
			Elements:   map[string]*ElementDecl{},
			Attributes: map[string][]*AttributeDecl{},
			Entities:   map[string]*Entity{},
			Params:     map[string]*Entity{},
			Notations:  map[string]*NotationDecl{},
			Complete:   dt.ExternalID == nil,
		}
		t.DTD = p.dtd
		hasPE := false
		for _, d := range dt.Subset {
			if _, ok := d.(*PERef); ok {
				hasPE = true
				break
			}
		}
		p.strict = t.Standalone || dt.ExternalID == nil && !hasPE
		if err := p.subset(dt.Subset, true); err != nil {
			return nil, err
		}
	}
	t.Prolog = miscItems(doc.Prolog)
	root, err := p.element(doc.Root, true, "")
	if err != nil {
		return nil, err
	}
	t.Root = root
	t.Epilog = miscItems(doc.Epilog)
	return t, nil
}

func miscItems(ms []Misc) []Item {
	if len(ms) == 0 {
		return nil
	}
	items := make([]Item, len(ms))
	for i, m := range ms {
		items[i] = m.(Item)
	}
	return items
}

// errorf returns a WFError at pos, or at the outermost entity reference being expanded.
func (p *processor) errorf(pos int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if p.ref >= 0 {
		pos = p.ref
		msg = "in the replacement text of entity " + p.in + ": " + msg
	}
	line, col := lineCol(p.input, pos)
	return &WFError{Pos: pos, Line: line, Col: col, Msg: msg}
}

// enter starts the expansion of e, referenced at pos.
func (p *processor) enter(e *Entity, pos int) (ref int, in string, err error) {
	for _, o := range p.open {
		if o == e {
			return 0, "", p.errorf(pos, "recursive reference to entity %s", e.Name)
		}
	}
	p.budget -= len(e.Value) + 1
	if p.budget < 0 {
		return 0, "", p.errorf(pos, "entity expansion limit exceeded")
	}
	ref, in = p.ref, p.in
	if p.ref < 0 {
		p.ref = pos
	}
	p.in = e.Name
	p.open = append(p.open, e)
	return ref, in, nil
}

func (p *processor) leave(ref int, in string) {
	p.open = p.open[:len(p.open)-1]
	p.ref, p.in = ref, in
}

// unread records a parameter entity that was not read.
func (p *processor) unread() {
	p.dtd.Complete = false
	if !p.tree.Standalone {
		p.skip = true
	}
}

// subset processes declarations; doc reports whether they are in the document entity (whose line ends
// are normalized) rather than in the replacement text of a parameter entity.
func (p *processor) subset(decls []Decl, doc bool) error {
	for _, d := range decls {
		var err error
		switch d := d.(type) {
		case *ElementDecl:
			if _, ok := p.dtd.Elements[d.Name.Text]; !ok {
				p.dtd.Elements[d.Name.Text] = d
			}
		case *EntityDecl:
			err = p.entityDecl(d, doc)
		case *AttlistDecl:
			err = p.attlistDecl(d, doc)
		case *NotationDecl:
			if _, ok := p.dtd.Notations[d.Name.Text]; !ok {
				p.dtd.Notations[d.Name.Text] = d
			}
		case *PERef:
			err = p.peRef(d)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *processor) entityDecl(d *EntityDecl, doc bool) error {
	e := &Entity{Name: d.Name.Text, Parameter: d.Parameter, External: d.ExternalID, Decl: d}
	if d.NData != nil {
		e.NData = d.NData.Text
	}
	if d.Value != nil {
		// The replacement text: character references expanded, entity references bypassed.
		raw := d.Value.Text
		if doc {
			raw = newlines(raw)
		}
		var b strings.Builder
		for i := 0; i < len(raw); {
			c := raw[i]
			if c != '&' || i+1 >= len(raw) || raw[i+1] != '#' {
				b.WriteByte(c)
				i++
				continue
			}
			j := i + strings.IndexByte(raw[i:], ';')
			r, ok := charRef(raw[i+2 : j])
			if !ok {
				return p.errorf(d.Value.Start, "character reference %s is not a legal character", raw[i:j+1])
			}
			b.WriteRune(r)
			i = j + 1
		}
		e.Value = b.String()
	}
	if p.skip {
		return nil
	}
	m := p.dtd.Entities
	if d.Parameter {
		m = p.dtd.Params
	}
	if _, ok := m[e.Name]; !ok {
		m[e.Name] = e
	}
	return nil
}

func (p *processor) attlistDecl(d *AttlistDecl, doc bool) error {
	el := d.Name.Text
	for _, def := range d.Defs {
		a := &AttributeDecl{Element: el, Name: def.Name.Text, Decl: def}
		switch t := def.Type.(type) {
		case *Keyword:
			a.Type = t.Text
		case *Enumeration:
			a.Type = "ENUMERATION"
			for _, v := range t.Values {
				a.Values = append(a.Values, v.Text)
			}
		case *NotationType:
			a.Type = "NOTATION"
			for _, n := range t.Names {
				a.Values = append(a.Values, n.Text)
			}
		}
		if def.Default != nil {
			a.Default = def.Default.Text
		}
		if def.Value != nil {
			var b strings.Builder
			if err := p.attValue(&b, def.Value.Text, doc, def.Value.Start); err != nil {
				return err
			}
			a.Value = b.String()
			if a.Type != "CDATA" {
				a.Value = collapse(a.Value)
			}
		}
		if p.skip {
			continue
		}
		dup := false
		for _, o := range p.dtd.Attributes[el] {
			if o.Name == a.Name {
				dup = true
				break
			}
		}
		if !dup {
			p.dtd.Attributes[el] = append(p.dtd.Attributes[el], a)
		}
	}
	return nil
}

func (p *processor) peRef(r *PERef) error {
	name := r.Name()
	e := p.dtd.Params[name]
	switch {
	case e == nil:
		if p.tree.Standalone {
			return p.errorf(r.Start, "parameter entity %%%s; is not declared", name)
		}
		p.unread()
		return nil
	case e.External != nil:
		p.unread()
		return nil
	}
	ref, in, err := p.enter(e, r.Start)
	if err != nil {
		return err
	}
	defer p.leave(ref, in)
	if !e.parsed {
		e.parsed = true
		e.decls, e.err = parseDecls(e.Value)
	}
	if e.err != nil {
		return p.errorf(r.Start, "%v", e.err)
	}
	return p.subset(e.decls, false)
}

// element decodes an element; doc reports whether it is in the document entity (whose line ends are
// normalized), ent names the entity otherwise.
func (p *processor) element(x *Element, doc bool, ent string) (*Elem, error) {
	el := p.newElem()
	*el = Elem{Name: x.Name.Text, Src: x, Entity: ent, ref: p.ref}
	var decls []*AttributeDecl
	if p.dtd != nil {
		decls = p.dtd.Attributes[el.Name]
	}
	if n := len(x.Attrs) + len(decls); n > 0 {
		el.Attrs = p.newAttrs(n)
	}
	for i, a := range x.Attrs {
		name := a.Name.Text
		if i < 16 {
			for _, o := range x.Attrs[:i] {
				if o.Name.Text == name {
					return nil, p.errorf(a.Start, "attribute %s appears twice", name)
				}
			}
		} else {
			for _, o := range el.Attrs[:i] { // a set would be faster; tags with many attributes are rare
				if o.Name == name {
					return nil, p.errorf(a.Start, "attribute %s appears twice", name)
				}
			}
		}
		v := a.Value.Text
		if strings.ContainsAny(v, "&\t\n\r") {
			var b strings.Builder
			if err := p.attValue(&b, v, doc, a.Value.Start); err != nil {
				return nil, err
			}
			v = b.String()
		}
		for _, d := range decls {
			if d.Name == name {
				if d.Type != "CDATA" {
					v = collapse(v)
				}
				break
			}
		}
		el.Attrs = append(el.Attrs, Attr{Name: name, Value: v})
	}
	for _, d := range decls {
		if d.Default != "" && d.Default != "#FIXED" {
			continue
		}
		specified := false
		for _, a := range el.Attrs {
			if a.Name == d.Name {
				specified = true
				break
			}
		}
		if !specified {
			el.Attrs = append(el.Attrs, Attr{Name: d.Name, Value: d.Value, Defaulted: true})
		}
	}
	if len(x.Content) > 0 {
		b := p.builder()
		if err := p.content(&b, x.Content, doc, ent); err != nil {
			return nil, err
		}
		el.Children = b.done()
	}
	return el, nil
}

// builder collects the decoded content of an element, merging adjacent text. The items are collected
// on the processor's stack and the text in its buffer, both shared by all builders: an element flushes
// its pending text before it decodes a child element.
type builder struct {
	p     *processor
	base  int    // the start of the items in p.stack
	text  string // the pending text if it is one piece
	start int    // the start of the pending text in p.buf if multi
	multi bool   // the pending text is in p.buf
}

func (p *processor) builder() builder { return builder{p: p, base: len(p.stack)} }

func (b *builder) addText(s string) {
	switch {
	case s == "":
	case b.multi:
		b.p.buf = append(b.p.buf, s...)
	case b.text == "":
		b.text = s
	default:
		b.start = len(b.p.buf)
		b.p.buf = append(append(b.p.buf, b.text...), s...)
		b.multi = true
	}
}

func (b *builder) addRune(r rune) {
	if !b.multi {
		b.start = len(b.p.buf)
		b.p.buf = append(b.p.buf, b.text...)
		b.multi = true
	}
	b.p.buf = utf8.AppendRune(b.p.buf, r)
}

func (b *builder) flush() {
	p := b.p
	if b.multi {
		p.stack = append(p.stack, p.newText(string(p.buf[b.start:])))
		p.buf = p.buf[:b.start]
		b.multi = false
	} else if b.text != "" {
		p.stack = append(p.stack, p.newText(b.text))
	}
	b.text = ""
}

func (b *builder) add(it Item) {
	b.flush()
	b.p.stack = append(b.p.stack, it)
}

// done returns the items, in a slice of their own.
func (b *builder) done() []Item {
	b.flush()
	p := b.p
	n := len(p.stack) - b.base
	if n == 0 {
		return nil
	}
	if len(p.items) < n {
		p.items = make([]Item, max(n, 1024))
	}
	items := p.items[:n:n]
	p.items = p.items[n:]
	copy(items, p.stack[b.base:])
	clear(p.stack[b.base:])
	p.stack = p.stack[:b.base]
	return items
}

// Elements, texts and attributes are allocated in chunks.

func (p *processor) newElem() *Elem {
	if len(p.elems) == 0 {
		p.elems = make([]Elem, 256)
	}
	e := &p.elems[0]
	p.elems = p.elems[1:]
	return e
}

func (p *processor) newText(s string) *Text {
	if len(p.texts) == 0 {
		p.texts = make([]Text, 256)
	}
	t := &p.texts[0]
	p.texts = p.texts[1:]
	t.Data = s
	return t
}

func (p *processor) newAttrs(n int) []Attr {
	if len(p.attrs) < n {
		p.attrs = make([]Attr, max(n, 256))
	}
	a := p.attrs[:0:n]
	p.attrs = p.attrs[n:]
	return a
}

func (p *processor) content(b *builder, items []Content, doc bool, ent string) error {
	for _, it := range items {
		switch x := it.(type) {
		case *CharData:
			if doc {
				b.addText(newlines(x.Text))
			} else {
				b.addText(x.Text)
			}
		case *CDSect:
			if doc {
				b.addText(newlines(x.Text))
			} else {
				b.addText(x.Text)
			}
		case *CharRef:
			r, ok := charRef(x.Text[2 : len(x.Text)-1])
			if !ok {
				return p.errorf(x.Start, "character reference %s is not a legal character", x.Text)
			}
			b.addRune(r)
		case *EntityRef:
			if err := p.entityRef(b, x); err != nil {
				return err
			}
		case *Element:
			b.flush() // before the element's content uses p.buf and p.stack
			el, err := p.element(x, doc, ent)
			if err != nil {
				return err
			}
			b.add(el)
		case *PI:
			b.add(x)
		case *Comment:
			b.add(x)
		}
	}
	return nil
}

func (p *processor) entityRef(b *builder, x *EntityRef) error {
	name := x.Name()
	if c, ok := predefined[name]; ok {
		b.addRune(rune(c))
		return nil
	}
	var e *Entity
	if p.dtd != nil {
		e = p.dtd.Entities[name]
	}
	switch {
	case e == nil:
		if p.strict {
			return p.errorf(x.Start, "entity &%s; is not declared", name)
		}
		b.add(x)
		return nil
	case e.NData != "":
		return p.errorf(x.Start, "reference to the unparsed entity %s", name)
	case e.External != nil:
		b.add(x)
		return nil
	}
	ref, in, err := p.enter(e, x.Start)
	if err != nil {
		return err
	}
	defer p.leave(ref, in)
	if !e.parsed {
		e.parsed = true
		e.content, e.err = parseContent(e.Value)
	}
	if e.err != nil {
		return p.errorf(x.Start, "%v", e.err)
	}
	return p.content(b, e.content, false, e.Name)
}

// attValue appends the normalized value of the attribute value s to b (XML 1.0, 3.3.3, for CDATA);
// doc reports whether s is in the document entity, whose line ends are normalized.
func (p *processor) attValue(b *strings.Builder, s string, doc bool, pos int) error {
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '&':
			ref, n, ok := scanRef(s[i:])
			if !ok {
				return p.errorf(pos, "malformed reference %q", s[i:min(len(s), i+16)])
			}
			if ref[0] == '#' {
				r, ok := charRef(ref[1:])
				if !ok {
					return p.errorf(pos, "character reference &%s; is not a legal character", ref)
				}
				b.WriteRune(r)
			} else if err := p.attEntity(b, ref, s[i:i+n], pos); err != nil {
				return err
			}
			i += n
		case '\r':
			b.WriteByte(' ')
			i++
			if doc && i < len(s) && s[i] == '\n' {
				i++
			}
		case '\t', '\n':
			b.WriteByte(' ')
			i++
		case '<':
			return p.errorf(pos, "< in an attribute value")
		default:
			b.WriteByte(c)
			i++
		}
	}
	return nil
}

func (p *processor) attEntity(b *strings.Builder, name, text string, pos int) error {
	if c, ok := predefined[name]; ok {
		b.WriteByte(c)
		return nil
	}
	var e *Entity
	if p.dtd != nil {
		e = p.dtd.Entities[name]
	}
	switch {
	case e == nil:
		if p.strict {
			return p.errorf(pos, "entity &%s; is not declared", name)
		}
		b.WriteString(text)
		return nil
	case e.NData != "":
		return p.errorf(pos, "reference to the unparsed entity %s", name)
	case e.External != nil:
		return p.errorf(pos, "reference to the external entity %s in an attribute value", name)
	case strings.IndexByte(e.Value, '<') >= 0:
		return p.errorf(pos, "the replacement text of entity %s, referenced in an attribute value, contains <", name)
	}
	ref, in, err := p.enter(e, pos)
	if err != nil {
		return err
	}
	defer p.leave(ref, in)
	return p.attValue(b, e.Value, false, pos)
}

// scanRef scans the reference at the start of s ("&name;", "&#123;" or "&#x1F;") and returns what is
// between "&" and ";" and the length of the reference.
func scanRef(s string) (ref string, n int, ok bool) {
	j := strings.IndexByte(s, ';')
	if j < 2 {
		return "", 0, false
	}
	ref = s[1:j]
	if ref[0] == '#' {
		digits := ref[1:]
		hex := strings.HasPrefix(digits, "x")
		if hex {
			digits = digits[1:]
		}
		if digits == "" {
			return "", 0, false
		}
		for i := 0; i < len(digits); i++ {
			c := digits[i]
			if !('0' <= c && c <= '9' || hex && ('a' <= c && c <= 'f' || 'A' <= c && c <= 'F')) {
				return "", 0, false
			}
		}
		return ref, j + 1, true
	}
	return ref, j + 1, isName(ref)
}

// charRef decodes the digits of a character reference (after "&#", without ";").
func charRef(digits string) (rune, bool) {
	base := 10
	if strings.HasPrefix(digits, "x") {
		digits, base = digits[1:], 16
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil || n > 0x10FFFF || !isChar(rune(n)) {
		return 0, false
	}
	return rune(n), true
}

// collapse normalizes the value of an attribute whose type is not CDATA: it removes leading and trailing
// spaces and replaces runs of spaces with one.
func collapse(s string) string {
	if !strings.Contains(s, "  ") && !strings.HasPrefix(s, " ") && !strings.HasSuffix(s, " ") {
		return s
	}
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' }), " ")
}

// newlines normalizes line ends (XML 1.0, 2.11): CR LF and CR become LF.
func newlines(s string) string {
	i := strings.IndexByte(s, '\r')
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for ; i >= 0; i = strings.IndexByte(s, '\r') {
		b.WriteString(s[:i])
		b.WriteByte('\n')
		if i+1 < len(s) && s[i+1] == '\n' {
			i++
		}
		s = s[i+1:]
	}
	b.WriteString(s)
	return b.String()
}

// lineCol returns the line and column (1-based, in code points) of the code point position pos.
func lineCol(s string, pos int) (line, col int) {
	line, col = 1, 1
	for _, r := range s {
		if pos == 0 {
			break
		}
		pos--
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

// Prefixes that put the replacement text of an entity in the context that the grammar parses: content
// in an element, declarations in an internal subset.
const (
	contentOpen  = "<x>"
	contentClose = "</x>"
	declsOpen    = "<!DOCTYPE x ["
	declsClose   = "]><x/>"
)

// parseContent parses the replacement text of an internal general entity, which must match the
// production content. Spans in the result are relative to s.
func parseContent(s string) ([]Content, error) {
	doc, err := ParseAST(contentOpen + s + contentClose)
	n := utf8.RuneCountInString(s)
	if err != nil {
		return nil, entitySyntaxError(err, len(contentOpen), n, "an element or markup is not closed in it")
	}
	if doc.Root.End != len(contentOpen)+n+len(contentClose) {
		return nil, errors.New("the replacement text is not content: the elements do not nest in it")
	}
	shift(reflect.ValueOf(doc.Root.Content), -len(contentOpen))
	return doc.Root.Content, nil
}

// parseDecls parses the replacement text of an internal parameter entity referenced between
// declarations, which must match the production extSubsetDecl; as in the internal subset, a
// parameter-entity reference must not occur inside a declaration, and conditional sections are not
// allowed. Spans in the result are relative to s.
func parseDecls(s string) ([]Decl, error) {
	doc, err := ParseAST(declsOpen + s + declsClose)
	n := utf8.RuneCountInString(s)
	if err != nil {
		return nil, entitySyntaxError(err, len(declsOpen), n, "a declaration is not closed in it")
	}
	if doc.Doctype == nil || doc.Doctype.End != len(declsOpen)+n+2 {
		return nil, errors.New("the replacement text is not a sequence of declarations")
	}
	shift(reflect.ValueOf(doc.Doctype.Subset), -len(declsOpen))
	return doc.Doctype.Subset, nil
}

// entitySyntaxError rewrites a syntax error in a replacement text of n code points, wrapped after
// prefix code points, to positions in the text; an error after the text gets the message unclosed.
func entitySyntaxError(err error, prefix, n int, unclosed string) error {
	var se *SyntaxError
	if !errors.As(err, &se) {
		return err
	}
	if se.Pos > prefix+n {
		return errors.New("the replacement text is not well-formed: " + unclosed)
	}
	line, col := se.Line, se.Col
	if line == 1 {
		col -= prefix
	}
	return fmt.Errorf("%d:%d: %s", line, col, se.Message())
}

var spanType = reflect.TypeFor[Span]()

// shift moves the spans of the values reachable from v by d, each value once.
func shift(v reflect.Value, d int) { shiftOnce(v, d, map[uintptr]bool{}) }

func shiftOnce(v reflect.Value, d int, seen map[uintptr]bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			shiftOnce(v.Elem(), d, seen)
		}
	case reflect.Pointer:
		if !v.IsNil() && !seen[v.Pointer()] {
			seen[v.Pointer()] = true
			shiftOnce(v.Elem(), d, seen)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			shiftOnce(v.Index(i), d, seen)
		}
	case reflect.Struct:
		if v.Type() == spanType {
			v.Field(0).SetInt(v.Field(0).Int() + int64(d))
			v.Field(1).SetInt(v.Field(1).Int() + int64(d))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			shiftOnce(v.Field(i), d, seen)
		}
	}
}

// isChar reports whether r matches the production Char.
func isChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD || 0x20 <= r && r <= 0xD7FF || 0xE000 <= r && r <= 0xFFFD ||
		0x10000 <= r && r <= 0x10FFFF
}

func isNameStart(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', r == '_', r == ':':
		return true
	case r < 0xC0:
		return false
	}
	return r <= 0xD6 || 0xD8 <= r && r <= 0xF6 || 0xF8 <= r && r <= 0x2FF || 0x370 <= r && r <= 0x37D ||
		0x37F <= r && r <= 0x1FFF || 0x200C <= r && r <= 0x200D || 0x2070 <= r && r <= 0x218F ||
		0x2C00 <= r && r <= 0x2FEF || 0x3001 <= r && r <= 0xD7FF || 0xF900 <= r && r <= 0xFDCF ||
		0xFDF0 <= r && r <= 0xFFFD || 0x10000 <= r && r <= 0xEFFFF
}

func isNameChar(r rune) bool {
	return isNameStart(r) || '0' <= r && r <= '9' || r == '-' || r == '.' || r == 0xB7 ||
		0x300 <= r && r <= 0x36F || 0x203F <= r && r <= 0x2040
}

// isName reports whether s matches the production Name.
func isName(s string) bool {
	for i, r := range s {
		if i == 0 && !isNameStart(r) || !isNameChar(r) {
			return false
		}
	}
	return s != ""
}
