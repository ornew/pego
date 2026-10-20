package spec_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/sourceast"
)

// The canonical grammar keeps every comment of the source with the gaps between
// tokens; it does not decide which node owns a comment at an expression or type
// operand. The adapter does: sourceast.Claim gives a comment at a token to the
// outermost node that starts or ends there. This file builds the public AST's
// nodes from the canonical tree, in the order the hand-written parser completes
// them, and requires the same source information for every one of them.

func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// nextToken returns the offset of the first token at or after off.
func nextToken(src []rune, off int) int {
	for off < len(src) {
		switch {
		case isSpaceRune(src[off]):
			off++
		case src[off] == '/' && off+1 < len(src) && src[off+1] == '/':
			for off < len(src) && src[off] != '\n' && src[off] != '\r' {
				off++
			}
		case src[off] == '/' && off+1 < len(src) && src[off+1] == '*':
			depth := 0
			for off < len(src) {
				if src[off] == '/' && off+1 < len(src) && src[off+1] == '*' {
					depth++
					off += 2
				} else if src[off] == '*' && off+1 < len(src) && src[off+1] == '/' {
					depth--
					off += 2
					if depth == 0 {
						break
					}
				} else {
					off++
				}
			}
		default:
			return off
		}
	}
	return off
}

// separators returns the end of the last of the separator tokens sep that
// follow off, or nothing when the next token is not one. One is the most a
// single separator takes.
func separators(src []rune, off int, sep rune, most int) []int {
	var last []int
	for n := 0; n < most; n++ {
		at := nextToken(src, off)
		if at >= len(src) || src[at] != sep {
			break
		}
		off = at + 1
		last = []int{off}
	}
	return last
}

func fieldNode(n *pego.Node, name string) *pego.Node {
	c, _ := n.Field(name).(*pego.Node)
	return c
}

func listNodes(n *pego.Node, name string) []*pego.Node {
	l := fieldNode(n, name)
	if l == nil {
		return nil
	}
	return elems(l)
}

func canonicalGaps(t *testing.T, root *pego.Node) []sourceast.Gap {
	t.Helper()
	var gaps []sourceast.Gap
	for _, g := range listNodes(root, "Gaps") {
		gap := sourceast.Gap{Start: g.Start(), End: g.End()}
		gap.Trailing, _ = g.Field("Trailing").(string)
		gap.HasTrailing, _ = g.Field("HasTrailing").(bool)
		gap.Leading, _ = g.Field("Leading").(string)
		gap.HasNext, _ = g.Field("HasNext").(bool)
		for _, c := range listNodes(g, "Detached") {
			gap.Detached = append(gap.Detached, c.Field("Text").(string))
		}
		gaps = append(gaps, gap)
	}
	return gaps
}

// shaper builds the nodes of the public AST from the canonical tree.
type shaper struct {
	t      *testing.T
	src    []rune
	nodes  []sourceast.Node
	labels []string
	hidden []bool // completed by the hand-written parser but kept out of the AST
}

func (s *shaper) add(label string, events ...sourceast.Event) int {
	s.nodes = append(s.nodes, sourceast.Node{Events: events, Created: sourceast.Unset})
	s.labels = append(s.labels, label)
	s.hidden = append(s.hidden, false)
	return len(s.nodes) - 1
}

func (s *shaper) span(label string, n *pego.Node) int {
	return s.add(label, sourceast.Event{Start: n.Start(), End: n.End()})
}

// created records the position the hand-written parser gave node i before it
// completed it.
func (s *shaper) created(i, off int) int {
	s.nodes[i].Created = off
	return i
}

func (s *shaper) later(i int, at []int) { s.nodes[i].Later = append(s.nodes[i].Later, at...) }

func (s *shaper) hide(i int) { s.hidden[i] = true }

// flatten returns the operands of a chain of binary nodes of one type, which
// the public AST holds in one node.
func flatten(n *pego.Node, typ string) []*pego.Node {
	if n.Type() != typ {
		return []*pego.Node{n}
	}
	return append(flatten(fieldNode(n, "L"), typ), fieldNode(n, "R"))
}

// statement adds the node of a statement or list element with its span.
func (s *shaper) statement(label string, n *pego.Node, sep []int) {
	span := sourceField(s.t, n, "SourceSpan")
	i := s.span(label, span)
	s.later(i, sep)
}

func (s *shaper) file(root *pego.Node) {
	if pkg, ok := root.Field("Pkg").(*pego.Node); ok && pkg != nil && pkg.Type() == "Pkg" {
		span := sourceField(s.t, sourceField(s.t, pkg, "Source"), "Span")
		i := s.span("Pkg", span)
		s.later(i, separators(s.src, span.End(), ';', 1<<30))
	}
	for _, d := range listNodes(root, "Decls") {
		s.decl(d)
	}
}

func (s *shaper) decl(d *pego.Node) {
	span := sourceField(s.t, d, "SourceSpan")
	sep := separators(s.src, span.End(), ';', 1<<30)
	switch d.Type() {
	case "TypeDef":
		ty := fieldNode(d, "Type")
		if ty.Type() == "StructType" {
			// The hand-written parser completes the struct from the "=".
			name := fieldNode(d, "Name")
			eq := nextToken(s.src, name.End())
			s.structType(ty, eq)
		} else {
			s.typ(ty)
		}
		s.statement("TypeDef", d, sep)
	case "TypeAlias":
		s.typ(fieldNode(d, "Type"))
		s.statement("TypeAlias", d, sep)
	case "VarDef":
		if t := fieldNode(d, "Type"); t != nil {
			s.typ(t)
		}
		if v := fieldNode(d, "Value"); v != nil {
			s.action(v)
		}
		s.statement("VarDef", d, sep)
	case "RuleDef":
		if t := fieldNode(d, "Type"); t != nil {
			s.typ(t)
		}
		s.rule(fieldNode(d, "Expr"))
		if a := fieldNode(d, "Action"); a != nil {
			s.action(a)
		}
		s.statement("RuleDef", d, sep)
	default:
		s.t.Fatalf("unexpected declaration %s", d.Type())
	}
}

// Types.

func (s *shaper) structType(n *pego.Node, start int) {
	for _, f := range listNodes(n, "Fields") {
		span := sourceField(s.t, f, "SourceSpan")
		s.typ(fieldNode(f, "Type"))
		s.statement("Field", f, separators(s.src, span.End(), ';', 1))
	}
	s.add("StructType", sourceast.Event{Start: start, End: n.End()})
}

func (s *shaper) typ(n *pego.Node) {
	switch n.Type() {
	case "TypeRef", "Bottom":
		s.span("TypeRef", n)
	case "TypeApp":
		for _, a := range listNodes(n, "Args") {
			s.typ(a)
		}
		s.span("TypeApp", n)
	case "TypeGroup":
		s.typ(fieldNode(n, "Type"))
		s.span("TypeGroup", n)
	case "TupleType":
		for _, a := range listNodes(n, "Types") {
			s.typ(a)
		}
		s.span("TupleType", n)
	case "StructType":
		s.structType(n, n.Start())
	case "OptionalType", "ListType":
		s.typ(fieldNode(n, "Type"))
		s.span(n.Type(), n)
	case "UnionType", "IntersectionType":
		for _, o := range flatten(n, n.Type()) {
			s.typ(o)
		}
		s.span(n.Type(), n)
	default:
		s.t.Fatalf("unexpected type node %s", n.Type())
	}
}

// Parser expressions.

// pragma adds a node that a pragma completes twice: at the "#" and then, once
// the suffix loop has it, from the start of its operand.
func (s *shaper) pragma(label string, n, operand *pego.Node) int {
	hash := nextToken(s.src, operand.End())
	i := s.add(label,
		sourceast.Event{Start: hash, End: n.End()},
		sourceast.Event{Start: operand.Start(), End: n.End()})
	if label == "Repeat" {
		return s.created(i, operand.Start()) // #repeat starts at its operand
	}
	return s.created(i, hash)
}

func (s *shaper) rule(n *pego.Node) {
	switch n.Type() {
	case "RuleRef":
		s.span("Ref", n)
	case "LevelCall":
		s.created(s.span("Ref", n), fieldNode(n, "Rule").Start())
	case "RuleGroup":
		s.rule(fieldNode(n, "Expr"))
		s.span("Group", n)
	case "InlineAction":
		s.rule(fieldNode(n, "Expr"))
		if a := fieldNode(n, "Action"); a != nil {
			s.action(a)
		}
		s.created(s.span("InlineAction", n), nextToken(s.src, fieldNode(n, "Expr").End()))
	case "Sequence":
		for _, o := range flatten(n, "Sequence") {
			s.rule(o)
		}
		s.span("Seq", n)
	case "Choice":
		for _, o := range flatten(n, "Choice") {
			s.rule(o)
		}
		s.span("Choice", n)
	case "Discard":
		s.rule(fieldNode(n, "X"))
		s.span("Discard", n)
	case "Lookahead":
		s.rule(fieldNode(n, "X"))
		if n.Field("Op") == "&" {
			s.span("And", n)
		} else {
			s.span("Not", n)
		}
	case "Capture":
		s.rule(fieldNode(n, "Expr"))
		s.span("Capture", n)
	case "Top", "Bottom", "BeginLine", "BeginInput", "EndLine", "EndInput", "Any", "Cut", "CharClass":
		s.span(n.Type(), n)
	case "String":
		s.span("Literal", n)
	case "Optional":
		s.rule(fieldNode(n, "X"))
		s.span("Optional", n)
	case "ZeroOrMore", "OneOrMore":
		s.rule(fieldNode(n, "X"))
		s.span("Repeat", n)
	case "Repeat":
		x := fieldNode(n, "X")
		s.rule(x)
		s.pragma("Repeat", n, x)
	case "Atomic", "Stream":
		x := fieldNode(n, "X")
		s.rule(x)
		s.pragma(n.Type(), n, x)
	case "Recover":
		x := fieldNode(n, "X")
		s.rule(x)
		s.rule(fieldNode(n, "Skip"))
		s.pragma("Recovery", n, x)
	case "Attributed", "MissingPragma":
		s.attributed(n)
	case "Predicate":
		s.block(n, "Predicate")
	case "OperatorsRule":
		s.operators(n)
	default:
		s.t.Fatalf("unexpected rule expression %s", n.Type())
	}
}

// attributed adds the one node that a run of attributes and #missing pragmas
// becomes. It is completed again by every suffix after the first.
func (s *shaper) attributed(n *pego.Node) {
	var layers []*pego.Node // outermost first
	base := n
	for base.Type() == "Attributed" || base.Type() == "MissingPragma" {
		layers = append(layers, base)
		base = fieldNode(base, "X")
	}
	s.rule(base)
	var events []sourceast.Event
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if layer.Type() == "MissingPragma" {
			hash := nextToken(s.src, fieldNode(layer, "X").End())
			events = append(events,
				sourceast.Event{Start: hash, End: layer.End()},
				sourceast.Event{Start: base.Start(), End: layer.End()})
			continue
		}
		// The suffix loop reads the whole run of attributes before it completes the node.
		run := layer
		for ; i > 0 && layers[i-1].Type() == "Attributed"; i-- {
			s.attribute(run)
			run = layers[i-1]
		}
		s.attribute(run)
		events = append(events, sourceast.Event{Start: base.Start(), End: run.End()})
	}
	s.add("Attributed", events...)
}

func (s *shaper) attribute(layer *pego.Node) {
	attr := fieldNode(layer, "Attr")
	for _, v := range listNodes(attr, "Args") {
		s.attrValue(v)
	}
	for _, kw := range listNodes(attr, "KwArgs") {
		s.attrValue(fieldNode(kw, "Value"))
	}
	s.span("Attribute", attr)
}

// attrValue completes the nodes of an argument. Numbers and keywords are read
// without a node, and a string is completed as a literal that the attribute
// then holds as its value.
func (s *shaper) attrValue(v *pego.Node) {
	if v == nil {
		return
	}
	switch v.Type() {
	case "Int", "True", "False", "Nil":
	case "String":
		s.hide(s.span("Literal", v))
	default:
		s.rule(v)
	}
}

func (s *shaper) operators(n *pego.Node) {
	for _, it := range listNodes(n, "Items") {
		span := sourceField(s.t, it, "SourceSpan")
		sep := separators(s.src, span.End(), ';', 1<<30)
		switch it.Type() {
		case "OperatorsSkip":
			s.rule(fieldNode(it, "Expr"))
			s.statement("OperatorsSkip", it, sep)
		case "OperatorsOperand":
			s.rule(fieldNode(it, "Expr"))
			if a := fieldNode(it, "Action"); a != nil {
				s.action(a)
			}
			s.statement("OperatorsRuleOperand", it, sep)
		case "OperatorsLevel":
			for _, op := range listNodes(it, "Operators") {
				ospan := sourceField(s.t, op, "SourceSpan")
				s.rule(fieldNode(op, "Expr"))
				if a := fieldNode(op, "Action"); a != nil {
					s.action(a)
				}
				s.statement("OperatorsRuleOperator", op, separators(s.src, ospan.End(), ';', 1<<30))
			}
			s.statement("OperatorsRuleLevel", it, sep)
		default:
			s.t.Fatalf("unexpected operators item %s", it.Type())
		}
	}
	s.created(s.span("OperatorsRule", n), nextToken(s.src, n.Start()+len("operators")))
}

// Value expressions.

// block adds the statements of an action block. The statements of a predicate
// are a block of their own unless there is exactly one.
func (s *shaper) block(n *pego.Node, label string) {
	stmts := listNodes(n, "Actions")
	for _, st := range stmts {
		s.action(st)
		// A statement takes the trailing group after its separator too.
		s.later(len(s.nodes)-1, separators(s.src, st.End(), ';', 1))
	}
	if label == "Block" {
		s.span("Block", n)
		return
	}
	if len(stmts) != 1 {
		s.span("Block", n)
	}
	s.span("Predicate", n)
}

func (s *shaper) action(n *pego.Node) {
	switch n.Type() {
	case "Int":
		s.span("IntLit", n)
	case "String":
		s.span("StringLit", n)
	case "True", "False":
		s.span("BoolLit", n)
	case "Nil":
		s.span("NilLit", n)
	case "CaptureRef":
		s.span("CaptureRef", n)
	case "VarRef":
		s.span("VarRef", n)
	case "DefaultAction":
	case "BinaryOp":
		s.action(fieldNode(n, "L"))
		s.action(fieldNode(n, "R"))
		s.created(s.span("Binary", n), nextToken(s.src, fieldNode(n, "L").End()))
	case "UnaryOp":
		s.action(fieldNode(n, "X"))
		s.span("Unary", n)
	case "FieldAccess":
		s.action(fieldNode(n, "X"))
		s.created(s.span("Member", n), fieldNode(n, "Name").Start())
	case "CaptureAccess":
		s.action(fieldNode(n, "X"))
		s.created(s.span("Member", n), fieldNode(n, "Capture").Start())
	case "Cast":
		s.action(fieldNode(n, "X"))
		s.typ(fieldNode(n, "Type"))
		dot := nextToken(s.src, fieldNode(n, "X").End())
		s.created(s.span("Cast", n), nextToken(s.src, dot+1))
	case "Call":
		for _, a := range listNodes(n, "Args") {
			s.action(a)
		}
		s.span("Call", n)
	case "Constructor":
		for _, f := range listNodes(n, "Inits") {
			span := sourceField(s.t, f, "SourceSpan")
			s.action(fieldNode(f, "Val"))
			s.statement("FieldInit", f, separators(s.src, span.End(), ',', 1))
		}
		for _, a := range listNodes(n, "Args") {
			s.action(a)
		}
		s.span("New", n)
	case "ListConstructor":
		s.typ(fieldNode(n, "Elem"))
		for _, a := range listNodes(n, "Items") {
			s.action(a)
		}
		s.span("ListConstructor", n)
	case "ActionTuple":
		for _, a := range listNodes(n, "Elems") {
			s.action(a)
		}
		s.span("TupleLit", n)
	case "ActionGroup":
		s.action(fieldNode(n, "Expr"))
		s.span("TermGroup", n)
	case "Lambda":
		s.action(fieldNode(n, "Body"))
		s.span("Lambda", n)
	case "ActionBlock":
		s.block(n, "Block")
	case "ActionAssign":
		s.action(fieldNode(n, "Expr"))
		s.span("Assign", n)
	default:
		s.t.Fatalf("unexpected action node %s", n.Type())
	}
}

// nodeKey names a node by its kind and span; the span keeps nested nodes of one
// kind apart.
func nodeKey(kind string, s grammar.SourceInfo) string {
	return fmt.Sprintf("%s@%d:%d-%d:%d", kind, s.Pos.Line, s.Pos.Col, s.End.Line, s.End.Col)
}

// canonicalNodes assigns source information to every node of the canonical tree.
func canonicalNodes(t *testing.T, src string, root *pego.Node) map[string]grammar.SourceInfo {
	t.Helper()
	s := &shaper{t: t, src: []rune(src)}
	s.file(root)
	infos, err := sourceast.NewPositions(src).Claim(canonicalGaps(t, root), s.nodes)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]grammar.SourceInfo{}
	for i, info := range infos {
		if s.hidden[i] {
			continue
		}
		key := nodeKey(s.labels[i], info)
		if _, dup := owners[key]; dup {
			t.Fatalf("two canonical nodes %s", key)
		}
		owners[key] = info
	}
	return owners
}

// manualNodes reads the source information of every node of the hand-written
// parse. A node the parser never completed has no end and is skipped.
func manualNodes(t *testing.T, g *grammar.Grammar) map[string]grammar.SourceInfo {
	t.Helper()
	owners := map[string]grammar.SourceInfo{}
	sourceType := reflect.TypeOf(grammar.SourceInfo{})
	add := func(kind string, s grammar.SourceInfo) {
		s.Layout = nil
		if len(s.LeadingDetachedComments) == 0 {
			s.LeadingDetachedComments = nil
		}
		if !s.End.IsValid() || kind == "File" {
			return // the file's span holds no comments
		}
		key := nodeKey(kind, s)
		if _, dup := owners[key]; dup {
			t.Fatalf("two manual nodes %s", key)
		}
		owners[key] = s
	}
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		case reflect.Struct:
			if v.Type() == sourceType {
				return
			}
			if f := v.FieldByName("SourceInfo"); f.IsValid() && f.Type() == sourceType {
				kind := v.Type().Name()
				if c, ok := v.Interface().(grammar.CaptureRef); ok && c.Param {
					kind = "VarRef"
				}
				add(kind, f.Interface().(grammar.SourceInfo))
			}
			if v.Type().Name() == "OperatorsRule" {
				if s := v.FieldByName("SkipSource").Interface().(grammar.SourceInfo); s.Pos.IsValid() {
					add("OperatorsSkip", s)
				}
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(g))
	return owners
}

// assertNodeParity requires identical source information for every node of src.
func assertNodeParity(t *testing.T, p *pego.Parser, name, src string) {
	t.Helper()
	if !compareNodes(t, p, name, src) {
		t.FailNow()
	}
}

// compareNodes reports whether the frontends agree on src. They must agree on
// acceptance: a source that only one of them rejects fails. A source both
// reject is skipped. compared counts the sources compareNodes compared.
var compared int

func compareNodes(t *testing.T, p *pego.Parser, name, src string) bool {
	t.Helper()
	manual, merr := pego.ParseGrammar(src)
	n, err := p.Parse(src)
	switch {
	case merr != nil && err != nil:
		return true
	case merr != nil:
		t.Errorf("%s: the hand-written parser rejects a source the canonical parser accepts: %v", name, merr)
		return false
	case err != nil:
		t.Errorf("%s: the canonical parser rejects a source the hand-written parser accepts: %v", name, err)
		return false
	}
	compared++
	want, got := manualNodes(t, manual), canonicalNodes(t, src, n)
	if reflect.DeepEqual(got, want) {
		return true
	}
	keys := func(m map[string]grammar.SourceInfo) []string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	for _, k := range keys(want) {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: canonical node %s missing; manual %+v", name, k, want[k])
		} else if !reflect.DeepEqual(g, want[k]) {
			t.Errorf("%s: node %s differs:\n canonical %+v\n manual    %+v", name, k, g, want[k])
		}
	}
	for _, k := range keys(got) {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: canonical node %s is extra; %+v", name, k, got[k])
		}
	}
	return false
}
