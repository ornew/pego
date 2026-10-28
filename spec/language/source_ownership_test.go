package language_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/spec/language/internal/sourceast"
)

// The canonical grammar and the hand-written frontend must assign the same
// source metadata to every statement and list element. The comparison keys each
// owner by its kind and start position, so a missing or extra owner is a failure
// as well as a different span or comment.

// manualOwners reads the owners of the hand-written parse from the AST values.
// The JSON form does not tag concrete list elements, so it cannot key them.
func manualOwners(t *testing.T, g *grammar.Grammar) map[string]grammar.SourceInfo {
	t.Helper()
	kinds := map[string]string{
		"Pkg": "Pkg", "TypeDef": "TypeDef", "TypeAlias": "TypeAlias", "VarDef": "VarDef", "RuleDef": "RuleDef", "Field": "Field",
		"OperatorsRuleOperand": "OperatorsOperand", "OperatorsRuleLevel": "OperatorsLevel",
		"OperatorsRuleOperator": "OperatorsOperator", "FieldInit": "InitField",
	}
	owners := map[string]grammar.SourceInfo{}
	add := func(kind string, s grammar.SourceInfo) {
		s.Layout = nil
		if kind == "Pkg" && !s.Pos.IsValid() {
			return // no package clause
		}
		owners[fmt.Sprintf("%s@%d:%d", kind, s.Pos.Line, s.Pos.Col)] = s
	}
	sourceType := reflect.TypeOf(grammar.SourceInfo{})
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
		case reflect.Struct:
			if v.Type() == sourceType {
				return
			}
			if kind, ok := kinds[v.Type().Name()]; ok {
				add(kind, v.FieldByName("SourceInfo").Interface().(grammar.SourceInfo))
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

func canonicalLead(t *testing.T, n *pego.Node) sourceast.Lead {
	t.Helper()
	if n == nil {
		return sourceast.Lead{}
	}
	var l sourceast.Lead
	l.Leading, _ = n.Field("Leading").(string)
	l.HasNext, _ = n.Field("HasNext").(bool)
	if d, ok := n.Field("Detached").(*pego.Node); ok && d != nil {
		for _, c := range elems(d) {
			l.Detached = append(l.Detached, c.Field("Text").(string))
		}
	}
	return l
}

// canonicalOwners converts the ownership records of the canonical tree.
func canonicalOwners(t *testing.T, src string, root *pego.Node) map[string]grammar.SourceInfo {
	t.Helper()
	positions := sourceast.NewPositions(src)
	owners := map[string]grammar.SourceInfo{}
	kinds := map[string]string{
		"TypeDef": "TypeDef", "TypeAlias": "TypeAlias", "VarDef": "VarDef", "RuleDef": "RuleDef", "Field": "Field",
		"OperatorsOperand": "OperatorsOperand", "OperatorsLevel": "OperatorsLevel",
		"Prefix": "OperatorsOperator", "Postfix": "OperatorsOperator", "Infix": "OperatorsOperator",
		"InitField": "InitField", "OperatorsSkip": "OperatorsSkip",
	}
	// list converts the statements of one list; an error node owns nothing.
	list := func(head sourceast.Lead, statements *pego.Node) {
		if statements == nil {
			return
		}
		var items []*sourceast.Item
		var kind []string
		for _, s := range elems(statements) {
			k, ok := kinds[s.Type()]
			if !ok {
				items, kind = append(items, nil), append(kind, "")
				continue
			}
			span := sourceField(t, s, "SourceSpan")
			source := sourceField(t, s, "Source")
			items = append(items, &sourceast.Item{
				Start: int(span.Start()), End: int(span.End()),
				Trailing: stringList(source, "Trailing"), Next: canonicalLead(t, sourceField(t, source, "Next")),
			})
			kind = append(kind, k)
		}
		infos, err := positions.Items(head, items)
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range elems(statements) {
			// A rule with attributes has the position of "rule" for diagnostics.
			if attrs := s.Field("Attrs"); kind[i] == "RuleDef" && attrs != nil && len(elems(attrs.(*pego.Node))) > 0 {
				attrs := elems(attrs.(*pego.Node))
				off := nextToken([]rune(src), int(attrs[len(attrs)-1].End()))
				keyword, err := positions.Source(sourceast.Metadata{Start: off, End: off})
				if err != nil {
					t.Fatal(err)
				}
				infos[i].DiagnosticPos = keyword.Pos
			}
		}
		for i, info := range infos {
			if items[i] != nil {
				owners[fmt.Sprintf("%s@%d:%d", kind[i], info.Pos.Line, info.Pos.Col)] = info
			}
		}
	}
	var walk func(*pego.Node)
	walk = func(n *pego.Node) {
		if n == nil {
			return
		}
		head := func() sourceast.Lead {
			h, _ := n.Field("Head").(*pego.Node)
			return canonicalLead(t, h)
		}
		switch n.Type() {
		case "File":
			lead := head()
			if pkg, ok := n.Field("Pkg").(*pego.Node); ok && pkg != nil && pkg.Type() == "Pkg" {
				meta := sourceField(t, pkg, "Source")
				span := sourceField(t, meta, "Span")
				m := sourceast.Metadata{Start: int(span.Start()), End: int(span.End()), Leading: meta.Field("Leading").(string), Trailing: stringList(meta, "Trailing")}
				for _, c := range elems(sourceField(t, meta, "Detached")) {
					m.Detached = append(m.Detached, c.Field("Text").(string))
				}
				s, err := positions.Source(m)
				if err != nil {
					t.Fatal(err)
				}
				owners[fmt.Sprintf("Pkg@%d:%d", s.Pos.Line, s.Pos.Col)] = s
				lead = canonicalLead(t, sourceField(t, meta, "Next"))
			} else if pkg != nil {
				lead = sourceast.Lead{}
			}
			list(lead, sourceField(t, n, "Decls"))
		case "StructType":
			list(head(), sourceField(t, n, "Fields"))
		case "OperatorsRule":
			list(head(), sourceField(t, n, "Items"))
		case "OperatorsLevel":
			list(head(), sourceField(t, n, "Operators"))
		case "Constructor":
			list(head(), sourceField(t, n, "Inits"))
		}
		for name, v := range n.Fields() {
			if name == "SourceSpan" || name == "Source" {
				continue
			}
			if c, ok := v.(*pego.Node); ok {
				walk(c)
			}
		}
		for _, e := range n.Elems() {
			walk(e)
		}
	}
	walk(root)
	return owners
}

func sortedKeys(m map[string]grammar.SourceInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// assertOwnerParity requires identical owners from both frontends for src.
func assertOwnerParity(t *testing.T, p *pego.Parser, name, src string) {
	t.Helper()
	manual, err := pego.ParseGrammar(src)
	if err != nil {
		return // acceptance parity is checked by TestSyntaxSummary
	}
	want := manualOwners(t, manual)
	n, err := p.Parse(src)
	if err != nil {
		t.Fatalf("%s: canonical parser rejects accepted source: %v", name, err)
	}
	got := canonicalOwners(t, src, n)
	if reflect.DeepEqual(got, want) {
		return
	}
	for _, k := range sortedKeys(want) {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: canonical owner %s missing; manual %+v", name, k, want[k])
		} else if !reflect.DeepEqual(g, want[k]) {
			t.Errorf("%s: owner %s differs:\n canonical %+v\n manual    %+v", name, k, g, want[k])
		}
	}
	for _, k := range sortedKeys(got) {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: canonical owner %s is extra; %+v", name, k, got[k])
		}
	}
	t.FailNow()
}

var ownershipCorpus = []string{
	"package p",
	"// leading\n// package\npackage p // trailing\n// detached\n\n// rule\nrule main = \"x\"",
	"rule a = \"x\"\nrule b = \"y\"",
	"// a\nrule a = \"x\" // a trail\n// b\nrule b = \"y\" // b trail\n",
	"rule a = \"x\" // t\n\n// detached\n\n// lead\nrule b = \"y\"\n",
	"rule a = \"x\" /* t */\nrule b = \"y\" /* t2 */\n",
	"rule a = \"x\" /* amb */ rule b = \"y\"",
	"rule a = \"x\"; // after semicolon\n// next\nrule b = \"y\"",
	"rule a = \"x\" /* before */ ; /* after */\nrule b = \"y\"",
	"type T = {\n A int /* x */ /* y */\n B string /*y1*//*y2*/ // y3\n}\nrule r = \"a\" /* a1 */ /* a2 */ \"b\" /* b1 */ /* b2 */\n",
	"rule s = \"s\" /* s1 */ /* s2 */ ; // s3\nrule t = \"t\" /* t1 */ ; /* t2 */\n",
	"type T = { A int /* x1 */ /* x2 */ ; B string /* y1 */ /* y2 */\n}\nrule z = \"z\" /* z1 */ /* z2 */\n",
	"rule a = \"x\" // inline\n// standalone\n\nrule b = \"y\"",
	"rule a = \"x\"\n// standalone\n\n// lead\nrule b = \"y\"",
	"rule a = \"x\"\n/* one */\n/* two */\nrule b = \"y\"",
	"rule a = \"x\"\n\n\n// eof\n",
	"rule a = \"x\" // eof trailing",
	"// only a comment\n",
	"/**/\n\n// lead\nrule a = \"x\"",
	"type T = { A int }\n// t\ntype U ~ T // u\n// r\nrule x = \"1\" => T{A: 1}",
	"type T = { // open\n  // a\n  A int // a trail\n  // b\n\n  B []{ C int; D string } // b trail\n  // end\n}\n",
	"type T = { A int; // after\n // b\n B int; /* c */ C int }",
	"type T = {\n // c\n}",
	"type T = {}",
	"type T = {/*a*/ /*b*/ // c\n A int\n}\n",
	"type W = { // w1\n B int\n}\n",
	"type W = { /*a*/ // w1\n // w2\n B int\n}\n",
	"type R = { /*a*/ // b\n A int }",
	"rule e = #operators {/*a*/ /*b*/ // c\n operand \"1\"\n}\n",
	"rule e = #operators { // o\n operand \"1\"\n level {/*a*/ // c\n infix left \"+\"\n }\n}\n",
	"type T = { /* x */ }\n",
	"type T = { // x\n}\n",
	"type T = { /* x */ A int /* y */ }",
	"type T = node[string] // n\n// next\ntype U ~ T",
	"rule x = #operators { // open\n // skip\n skip \" \"* // skip trail\n // operand\n operand \"1\" => N{} // operand trail\n level l { // level open\n // p\n prefix \"-\" => N{} // p trail\n infix left \"+\" // i trail\n } // level trail\n}\n",
	"rule x = #operators {\n operand \"1\"; // a\n // b\n operand \"2\" // c\n // d\n}\n",
	"rule x = #operators { skip \" \"; operand \"a\"; level { infix left \"+\" ; postfix \"!\" } }",
	"rule x = #operators {\n level { // c\n }\n}",
	"rule x = #operators {\n operand \"1\" // a\n} #ignorecase // after\n// next\nrule y = \"a\"i // b\n",
	"rule x = #operators { operand \"1\" } /* a */ #ignorecase /* b */ => ...\n",
	"rule x = \"a\" => N{A: 1, // a1\n // lead b\n B: 2 /* b */, /* c */ C: 3} // after",
	"rule x = \"a\" => N{ // open\n A: 1 // a\n , // comma\n B: 2 // b\n // end\n}",
	"rule x = \"a\" => N{A: 1,}\n",
	"rule x = \"a\" => N{/* a */ A: M{ /* b */ B: 1 // b1\n }}",
	"rule x = \"a\" => map(list($x), (i) => N{A: i, // c\n B: i})",
	"// lead\r\npackage p // t\r\n\r\n// r\r\nrule a = \"x\" // t2\r\n",
	"rule a = \"x\"\rrule b = \"y\" // t\r// c\rrule c = \"z\"",
	"rule a = \"😀\" // 😀\n// ß\nrule b = \"y\"",
	"package p; rule a = \"x\"",
	"rule a = \"x\" => ... // d\nrule b = \"y\"",
	"rule a = x:\"x\" { true } // p\nrule b = \"y\"",
	"rule a = (\"x\" /* in */) // g\nrule b = \"y\"",
	"rule a = \"x\" #atomic // pragma\nrule b = \"y\"",
	"rule a = \"x\" @name(1) // attr\nrule b = \"y\"",
	"rule a T = \"x\" // typed\nrule b []T = \"y\"",
	// Declaration attributes.
	"// lead\n@start @foo // t\n// between\n@bar /* b */ @baz(1, k=2)\n// kw\nrule a = \"x\" // trail\n// next\nrule b = \"y\"",
	"rule a = \"x\" // t\n\n// detached\n\n// lead\n@start(default)\nrule b = \"y\"\n",
	"@start(\n // in\n default, // d\n)\nrule a = \"x\"",
	// Expression and type operands.
	"rule a = /* l */ b /* t */ c // x\n",
	"rule a = b // t\n  / c // u\n  / d\n",
	"rule a = (\n // l\n b // t\n c\n // end\n) // g\n",
	"rule a = b /* x */ #atomic /* y */ #repeat(2) /* z */ @n /* w */\n",
	"rule a = x:/* l */ b* /* t */\n",
	"rule a = b => /* l */ f(/* a */ 1 /* b */, /* c */ x /* d */) /* t */\n",
	"rule a = b => x /* m */ . /* n */ Foo /* o */ + /* p */ y.$z\n",
	"rule a = b => N{ /* l */ A: /* v */ 1 /* t */ , B: [] /* e */ int{ /* i */ 1 } }\n",
	"rule a = b => (/* l */ x /* t */) + (a /* a */, /* b */ b) + ((i) /* p */ => /* q */ i)\n",
	"type T = { A /* a */ []int /* b */ | /* c */ string; B (/* d */ int /* e */, string /* f */) }\n",
	"type T /* a */ = /* b */ { /* c */ A int }\ntype U /* a */ ~ /* b */ T /* c */\n",
	"rule a = {\n // l\n x := 1; // t\n y // u\n} // v\n",
	"rule a = b => f(\n // l\n 1 /* a */, // b\n 2 // c\n // end\n)\n",
	"rule a = b => N{\n // l\n A: 1 /* a */, // b\n B: 2 // c\n // end\n}\n",
	"rule a = b => []N{ // open\n}\nrule c = b => []N{\n // l\n 1 /* a */, 2\n}[0]\n",
	"rule a = b => (1 /* a */, 2 /* b */,)\n",
	"rule a = #operators {\n operand /* a */ b /* b */ => /* c */ N{} /* d */\n level { infix /* e */ left /* f */ \"+\" /* g */ }\n}\n",
}

func TestCanonicalOwnerParity(t *testing.T) {
	p, _ := sourceGrammar(t)
	for i, src := range ownershipCorpus {
		assertOwnerParity(t, p, fmt.Sprintf("case %d", i), src)
		assertNodeParity(t, p, fmt.Sprintf("case %d", i), src)
	}
}

// TestCanonicalOwnerParityNegativeControl shows the comparison sees owners:
// a corpus case yields comment-bearing owners on both sides, and a changed
// comment is reported as a difference.
func TestCanonicalOwnerParityNegativeControl(t *testing.T) {
	p, _ := sourceGrammar(t)
	src := "// lead\nrule a = \"x\" // trail\n// next\nrule b = \"y\"\n"
	n, err := p.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got := canonicalOwners(t, src, n)
	a := got["RuleDef@2:1"]
	if a.LeadingComments != " lead\n" || !reflect.DeepEqual(a.TrailingComments, []string{" trail\n"}) {
		t.Fatalf("unexpected owner %+v", a)
	}
	if b := got["RuleDef@4:1"]; b.LeadingComments != " next\n" {
		t.Fatalf("unexpected owner %+v", b)
	}
	manual, err := pego.ParseGrammar(strings.Replace(src, "// trail", "// other", 1))
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got, manualOwners(t, manual)) {
		t.Fatal("changed comment was not detected")
	}
}

// TestCanonicalOwnerParityRepository compares every grammar of the repository.
func TestCanonicalOwnerParityRepository(t *testing.T) {
	p, _ := sourceGrammar(t)
	checked := 0
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != "../.." && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".pego":
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			assertOwnerParity(t, p, path, string(src))
			assertNodeParity(t, p, path, string(src))
		case ".md":
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, block := range pegoCodeBlocks(string(src)) {
				checked++
				assertOwnerParity(t, p, fmt.Sprintf("%s block %d", path, i+1), block)
				assertNodeParity(t, p, fmt.Sprintf("%s block %d", path, i+1), block)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no grammar checked")
	}
}

// emptyStatements hold a statement that is empty: a semicolon that follows a
// newline or another semicolon. Both frontends reject them, at the same place.
var emptyStatements = []string{
	"rule a = \"x\" // before\n; // after\n// next\nrule b = \"y\"",
	"rule a = \"x\";; // after\nrule b = \"y\"",
	"rule a = \"x\" ;/* x */; // after\nrule b = \"y\"",
	"rule a = \"x\" ;/* x */;\n// lead\nrule b = \"y\"",
	"package p // t\n; // t2\nrule a = \"x\"",
	"type T = { A int\n; B int }",
	"rule a = {x\n; y}",
	"rule x = #operators { operand \"1\";; operand \"2\" }",
	"rule x = #operators { level { infix left \"+\" // c\n; postfix \"!\" } }",
}

func TestEmptyStatementsAreRejected(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, src := range emptyStatements {
		_, merr := pego.ParseGrammar(src)
		_, cerr := p.Parse(src)
		if merr == nil || cerr == nil {
			t.Errorf("%q: hand-written error %v, canonical error %v; both must reject", src, merr, cerr)
			continue
		}
		var mline, mcol, cline, ccol int
		if _, err := fmt.Sscanf(merr.Error(), "%d:%d:", &mline, &mcol); err != nil {
			t.Errorf("%q: hand-written error %q has no position", src, merr)
		}
		if _, err := fmt.Sscanf(cerr.Error(), "%d:%d:", &cline, &ccol); err != nil {
			t.Errorf("%q: canonical error %q has no position", src, cerr)
		}
		if mline != cline || mcol != ccol {
			t.Errorf("%q: hand-written error at %d:%d (%v), canonical at %d:%d (%v)", src, mline, mcol, merr, cline, ccol, cerr)
		}
	}
}
