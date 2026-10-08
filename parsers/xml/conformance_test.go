package xml_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// suiteCase is a TEST of the W3C XML Conformance Test Suite.
type suiteCase struct {
	collection                      string // the file that lists the test, relative to xmlconf
	id, typ, entities, rec, version string
	edition, namespace              string
	path, output                    string // absolute paths; output is "" without canonical output
	desc                            string
}

// loadSuite reads the index of the suite (xmlconf.xml) and the collections it includes, with this
// package: the collections are external entities, which Decode leaves as references.
func loadSuite(t *testing.T, dir string) []suiteCase {
	t.Helper()
	index := filepath.Join(dir, "xmlconf.xml")
	tree := decodeFile(t, index)
	var cases []suiteCase
	for _, c := range tree.Root.Elements() {
		for _, it := range c.Children {
			ref, ok := it.(*xml.EntityRef)
			if !ok {
				continue
			}
			e := tree.DTD.Entities[ref.Name()]
			file := filepath.Join(dir, e.External.System.Text)
			col := decodeEntity(t, file)
			rel, _ := filepath.Rel(dir, file)
			for _, el := range col.Root.Elements() {
				// The tests are relative to the directory of their collection. (The xml:base of the
				// TESTCASES that includes eduni/misc/ht-bh.xml names another directory.)
				cases = collect(cases, el, rel, filepath.Dir(file))
			}
		}
	}
	return cases
}

func collect(cases []suiteCase, el *xml.Elem, collection, base string) []suiteCase {
	if b, ok := el.Attr("xml:base"); ok {
		base = filepath.Join(base, b)
	}
	if el.Name == "TEST" {
		attr := func(name string) string { v, _ := el.Attr(name); return v }
		c := suiteCase{
			collection: collection,
			id:         attr("ID"), typ: attr("TYPE"), entities: attr("ENTITIES"), rec: attr("RECOMMENDATION"),
			version: attr("VERSION"), edition: attr("EDITION"), namespace: attr("NAMESPACE"),
			path: filepath.Join(base, attr("URI")),
			desc: strings.Join(strings.Fields(el.Text()), " "),
		}
		if c.entities == "" {
			c.entities = "none"
		}
		if o := attr("OUTPUT"); o != "" {
			c.output = filepath.Join(base, o)
		}
		return append(cases, c)
	}
	for _, c := range el.Elements() {
		cases = collect(cases, c, collection, base)
	}
	return cases
}

// decodeEntity decodes an external parsed entity (a text declaration and content) by putting its content
// in an element.
func decodeEntity(t *testing.T, path string) *xml.Tree {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := xml.Transcode(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if strings.HasPrefix(s, "<?xml ") {
		s = s[strings.Index(s, "?>")+2:]
	}
	tree, err := xml.Decode("<entity>" + s + "</entity>")
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return tree
}

func decodeFile(t *testing.T, path string) *xml.Tree {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := xml.DecodeBytes(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return tree
}

// Outcomes of a test.
const (
	pass          = "pass"
	passOutput    = "pass, canonical output equal"
	fail          = "FAIL"
	skipVersion   = "skip: XML 1.1 or namespaces 1.1"
	skipEdition   = "skip: not for the fifth edition"
	skipError     = "skip: TYPE error (optional)"
	skipExternal  = "skip: accepted, the error is in an external entity"
	skipEncoding  = "skip: unsupported encoding"
	outputDiffers = "FAIL: canonical output differs"
)

// run runs a test and returns its outcome and a detail for failures.
func (c suiteCase) run() (string, string) {
	switch {
	case c.rec == "XML1.1" || c.rec == "NS1.1" || c.version != "" && !slices.Contains(strings.Fields(c.version), "1.0"):
		return skipVersion, ""
	case c.edition != "" && !slices.Contains(strings.Fields(c.edition), "5"):
		return skipEdition, ""
	case c.typ == "error":
		return skipError, ""
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return fail, err.Error()
	}
	tree, err := xml.DecodeBytes(data)
	if err == nil && strings.HasPrefix(c.rec, "NS") {
		err = tree.ResolveNamespaces()
	}
	switch c.typ {
	case "valid", "invalid":
		if err != nil {
			var we *xml.WFError
			if errors.As(err, &we) && strings.HasPrefix(we.Msg, "unsupported encoding") {
				return skipEncoding, err.Error()
			}
			return fail, err.Error()
		}
		if c.output != "" && c.entities == "none" {
			want, err := os.ReadFile(c.output)
			if err != nil {
				return fail, err.Error()
			}
			if got := canonical(tree); got != string(want) {
				return outputDiffers, fmt.Sprintf("got %q, want %q", got, want)
			}
			return passOutput, ""
		}
		return pass, ""
	case "not-wf":
		if err == nil {
			if c.entities != "none" {
				return skipExternal, ""
			}
			return fail, "accepted"
		}
		return pass, err.Error()
	}
	return fail, "unknown type " + c.typ
}

// TestConformanceSuite runs the W3C XML Conformance Test Suite (xmlts20130923) found in the directory
// $XMLCONF (the xmlconf directory of the archive), which is not vendored: see the README. Every test
// must have the outcome recorded in knownOutcomes, or pass.
func TestConformanceSuite(t *testing.T) {
	dir := os.Getenv("XMLCONF")
	if dir == "" {
		t.Skip("set XMLCONF to the xmlconf directory of the W3C XML Conformance Test Suite to run it")
	}
	cases := loadSuite(t, dir)
	type key struct{ collection, typ, outcome string }
	counts := map[key]int{}
	for _, c := range cases {
		outcome, detail := c.run()
		counts[key{c.collection, c.typ, outcome}]++
		if want, ok := knownOutcomes[c.id]; ok {
			if outcome != want {
				t.Errorf("%s (%s, %s): %s, recorded as %s: %s", c.id, c.typ, c.path, outcome, want, detail)
			}
			continue
		}
		if strings.HasPrefix(outcome, "FAIL") {
			t.Errorf("%s (%s, %s): %s: %s", c.id, c.typ, c.path, outcome, detail)
		} else if !strings.HasPrefix(outcome, pass) && os.Getenv("XMLCONF_VERBOSE") != "" {
			t.Logf("%s (%s, %s, entities=%s): %s: %s\n\t%s", c.id, c.typ, c.path, c.entities, outcome, detail, c.desc)
		}
	}
	var keys []key
	total := map[string]int{}
	for k, n := range counts {
		keys = append(keys, k)
		total[k.typ+" "+k.outcome] += n
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.collection != b.collection {
			return a.collection < b.collection
		}
		if a.typ != b.typ {
			return a.typ < b.typ
		}
		return a.outcome < b.outcome
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d tests\n", len(cases))
	for _, k := range keys {
		fmt.Fprintf(&b, "%-45s %-8s %-45s %5d\n", k.collection, k.typ, k.outcome, counts[k])
	}
	var tk []string
	for k := range total {
		tk = append(tk, k)
	}
	sort.Strings(tk)
	for _, k := range tk {
		fmt.Fprintf(&b, "total %-60s %5d\n", k, total[k])
	}
	t.Log("\n" + b.String())
}

// knownOutcomes records the tests whose outcome is not pass, with the reason. See the README.
var knownOutcomes = map[string]string{}

// canonical writes a decoded document in the canonical form of the suite's OUTPUT files (James Clark's
// canonical XML with the notations of the DTD, "second canonical form").
func canonical(t *xml.Tree) string {
	var b, dtd strings.Builder
	if t.DTD != nil && len(t.DTD.Notations) > 0 {
		b := &dtd
		b.WriteString("<!DOCTYPE " + t.DTD.Name + " [\n")
		var names []string
		for n := range t.DTD.Notations {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			id := t.DTD.Notations[n].ExternalID
			b.WriteString("<!NOTATION " + n)
			if id.Public != nil {
				b.WriteString(" PUBLIC '" + id.Public.Value() + "'")
				if id.System != nil {
					b.WriteString(" '" + id.System.Value() + "'")
				}
			} else {
				b.WriteString(" SYSTEM '" + id.System.Value() + "'")
			}
			b.WriteString(">\n")
		}
		b.WriteString("]>\n")
	}
	// The processing instructions before the doctype come first.
	var prolog []xml.Item
	for _, it := range t.Prolog {
		if pi, ok := it.(*xml.PI); ok && t.Doc.Doctype != nil && pi.Start < t.Doc.Doctype.Start {
			canonicalItem(&b, it)
		} else {
			prolog = append(prolog, it)
		}
	}
	// Processing instructions of the internal subset are reported, before the notations (as the
	// output of ibm-valid-P29-ibm29v01.xml shows).
	if t.Doc.Doctype != nil {
		for _, d := range t.Doc.Doctype.Subset {
			if pi, ok := d.(*xml.PI); ok {
				canonicalItem(&b, pi)
			}
		}
	}
	b.WriteString(dtd.String())
	for _, it := range prolog {
		canonicalItem(&b, it)
	}
	canonicalItem(&b, t.Root)
	for _, it := range t.Epilog {
		canonicalItem(&b, it)
	}
	return b.String()
}

func canonicalItem(b *strings.Builder, it xml.Item) {
	switch it := it.(type) {
	case *xml.Elem:
		b.WriteString("<" + it.Name)
		attrs := slices.Clone(it.Attrs)
		sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
		for _, a := range attrs {
			b.WriteString(" " + a.Name + `="` + canonicalText(a.Value) + `"`)
		}
		b.WriteString(">")
		for _, c := range it.Children {
			canonicalItem(b, c)
		}
		b.WriteString("</" + it.Name + ">")
	case *xml.Text:
		b.WriteString(canonicalText(it.Data))
	case *xml.PI:
		b.WriteString("<?" + it.Target.Text + " ")
		if it.Data != nil {
			b.WriteString(it.Data.Value())
		}
		b.WriteString("?>")
	case *xml.EntityRef:
		b.WriteString(it.Text)
	}
}

var canonicalReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
	"\t", "&#9;", "\n", "&#10;", "\r", "&#13;")

func canonicalText(s string) string { return canonicalReplacer.Replace(s) }
