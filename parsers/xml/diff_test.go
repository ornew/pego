package xml_test

import (
	stdxml "encoding/xml"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// The differential tests compare Decode (with ResolveNamespaces) with encoding/xml's Decoder.Token on
// documents without a document type declaration, which encoding/xml does not process. Both are reduced
// to a list of strings: start and end tags with their namespace names, attributes in order, text
// (adjacent character data merged), comments and processing instructions; encoding/xml's character data
// outside the root element and its token for the XML declaration are dropped, and its names for
// namespace declarations are those of encoding/xml.

func stdTokens(src string) ([]string, error) {
	d := stdxml.NewDecoder(strings.NewReader(src))
	// The input is text already; Decode does not convert it either.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var toks []string
	depth := 0
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			toks = append(toks, "T"+text.String())
			text.Reset()
		}
	}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			flush()
			return toks, nil
		}
		if err != nil {
			return nil, err
		}
		switch tok := tok.(type) {
		case stdxml.StartElement:
			flush()
			var b strings.Builder
			// Namespace names are attribute values, which encoding/xml does not normalize.
			fmt.Fprintf(&b, "<{%s}%s", spaces(tok.Name.Space), tok.Name.Local)
			for _, a := range tok.Attr {
				fmt.Fprintf(&b, " {%s}%s=%q", spaces(a.Name.Space), a.Name.Local, spaces(a.Value))
			}
			toks = append(toks, b.String()+">")
			depth++
		case stdxml.EndElement:
			flush()
			toks = append(toks, fmt.Sprintf("</{%s}%s>", spaces(tok.Name.Space), tok.Name.Local))
			depth--
		case stdxml.CharData:
			if depth > 0 {
				text.Write(tok)
			}
		case stdxml.Comment:
			flush()
			toks = append(toks, "C"+newlines(string(tok)))
		case stdxml.ProcInst:
			if tok.Target != "xml" {
				flush()
				toks = append(toks, "P"+tok.Target+" "+newlines(string(tok.Inst)))
			}
		case stdxml.Directive:
			return nil, fmt.Errorf("directive")
		}
	}
}

// spaces replaces white space with spaces, as attribute-value normalization does; encoding/xml does not
// normalize attribute values.
func spaces(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}

func ourTokens(t *xml.Tree) []string {
	var toks []string
	var item func(it xml.Item)
	item = func(it xml.Item) {
		switch it := it.(type) {
		case *xml.Elem:
			var b strings.Builder
			fmt.Fprintf(&b, "<{%s}%s", it.Space, it.Local)
			for _, a := range it.Attrs {
				space, local := a.Space, a.Local
				if space == xml.XMLNSNamespace { // encoding/xml's names
					if local == "xmlns" {
						space = ""
					} else {
						space = "xmlns"
					}
				}
				fmt.Fprintf(&b, " {%s}%s=%q", space, local, spaces(a.Value))
			}
			toks = append(toks, b.String()+">")
			for _, c := range it.Children {
				item(c)
			}
			toks = append(toks, fmt.Sprintf("</{%s}%s>", it.Space, it.Local))
		case *xml.Text:
			toks = append(toks, "T"+it.Data)
		case *xml.Comment:
			toks = append(toks, "C"+it.Value())
		case *xml.PI:
			data := ""
			if it.Data != nil {
				data = it.Data.Value()
			}
			toks = append(toks, "P"+it.Target.Text+" "+data)
		}
	}
	for _, it := range t.Prolog {
		item(it)
	}
	item(t.Root)
	for _, it := range t.Epilog {
		item(it)
	}
	return toks
}

// compare decodes src with both and reports differences; src must be well-formed and namespace-well-
// formed for this package.
func compare(t *testing.T, src string) {
	t.Helper()
	tree, err := xml.Decode(src)
	if err == nil {
		err = tree.ResolveNamespaces()
	}
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	want, err := stdTokens(src)
	if err != nil {
		t.Fatalf("%q: encoding/xml: %v", src, err)
	}
	got := ourTokens(tree)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%q:\n got  %q\n want %q", src, got, want)
	}
}

// randomDoc generates a well-formed, namespace-well-formed document without a DTD.
func randomDoc(r *rand.Rand) string {
	var b strings.Builder
	pick := func(xs ...string) string { return xs[r.IntN(len(xs))] }
	if r.IntN(2) == 0 {
		b.WriteString(pick(`<?xml version="1.0"?>`, `<?xml version='1.0' encoding="UTF-8" standalone='no' ?>`) + pick("", "\n"))
	}
	misc := func() {
		switch r.IntN(4) {
		case 0:
			b.WriteString("<!--" + pick("", " c ", "a-b", "<&>", "é😀") + "-->")
		case 1:
			b.WriteString("<?" + pick("pi", "x-y", "xmlfoo") + pick("", " ", " data", " a?b>c") + "?>")
		case 2:
			b.WriteString(pick(" ", "\n", "\t"))
		}
	}
	misc()
	text := func() string {
		return pick("", "x", " ", "\n", "é", "日本", "😀", "&lt;", "&amp;", "&gt;", "&quot;", "&apos;", "&#65;", "&#x10000;",
			"]", "]]", "x>", "<![CDATA[]]>", "<![CDATA[<&]]]>", "a\nb")
	}
	attrValue := func() string {
		var v strings.Builder
		for range r.IntN(4) {
			v.WriteString(pick("v", " ", "é", "&lt;", "&amp;", "&#38;", "&#x20;", ">", "'", "]]>"))
		}
		return v.String()
	}
	var elem func(depth int, prefixes []string)
	elem = func(depth int, prefixes []string) {
		var decls []string
		if r.IntN(4) == 0 {
			p := pick("p", "q", "ns")
			decls = append(decls, fmt.Sprintf(` xmlns:%s="urn:%s%d"`, p, p, r.IntN(3)))
			prefixes = append(prefixes, p)
		}
		if r.IntN(6) == 0 {
			decls = append(decls, pick(` xmlns="urn:d"`, ` xmlns=""`))
		}
		name := pick("a", "b", "c", "long-name", "x.y", "é", "_u")
		if len(prefixes) > 0 && r.IntN(2) == 0 {
			name = prefixes[r.IntN(len(prefixes))] + ":" + name
		}
		b.WriteString("<" + name)
		used := map[string]bool{}
		for _, d := range decls {
			b.WriteString(d)
		}
		for range r.IntN(3) {
			an := pick("id", "k", "v", "xml:lang")
			if len(prefixes) > 0 && an != "xml:lang" && r.IntN(3) == 0 {
				an = prefixes[r.IntN(len(prefixes))] + ":" + an
			}
			if used[an] || strings.Contains(an, ":") && used["*"+an[strings.IndexByte(an, ':'):]] {
				continue
			}
			used[an] = true
			if strings.Contains(an, ":") {
				used["*"+an[strings.IndexByte(an, ':'):]] = true // two prefixes may share a namespace name
			}
			q := pick(`"`, `'`)
			v := strings.ReplaceAll(attrValue(), q, map[string]string{`"`: "&quot;", `'`: "&apos;"}[q])
			b.WriteString(pick(" ", "\n ", "  ") + an + pick("=", " = ") + q + v + q)
		}
		if depth == 0 || r.IntN(4) == 0 {
			b.WriteString(pick("/>", " />"))
			return
		}
		b.WriteString(">")
		for range r.IntN(5) {
			switch r.IntN(4) {
			case 0, 1:
				b.WriteString(text())
			case 2:
				elem(depth-1, prefixes)
			case 3:
				misc()
			}
		}
		b.WriteString("</" + name + pick(">", " >"))
	}
	elem(4, nil)
	misc()
	misc()
	return b.String()
}

func TestDifferential(t *testing.T) {
	for _, src := range []string{
		`<a/>`, `<?xml version="1.0"?><!--c--><?p d?><a x="1" y='&lt;&#65;'>t&amp;<![CDATA[<x>]]><b/><!--x--><?q?></a><!--e-->`,
		`<p:a xmlns:p="urn:p" xmlns="urn:d" p:x="1" y="2"><b xml:lang="en"/><c xmlns=""/></p:a>`,
		"<a>\r\n\r</a>", "<a x='\r\n\t'/>", "<é ü=''>😀</é>",
	} {
		compare(t, src)
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 3000 {
		compare(t, randomDoc(r))
	}
}

// FuzzDecode compares Decode with encoding/xml on arbitrary input: every document without a DTD that
// this package accepts as namespace-well-formed must be accepted by encoding/xml with the same tokens.
// (encoding/xml accepts more: see the README.) It also checks that Recognize agrees with ParseAST.
func FuzzDecode(f *testing.F) {
	r := rand.New(rand.NewPCG(5, 6))
	for range 20 {
		f.Add(randomDoc(r))
	}
	f.Add(`<!DOCTYPE a [<!ENTITY e "<b>&#38;amp;</b>">]><a x="&e;">&e;</a>`)
	f.Fuzz(func(t *testing.T, src string) {
		_, perr := xml.ParseAST(src)
		rerr := xml.Recognize(src)
		if (perr == nil) != (rerr == nil) {
			t.Fatalf("%q: ParseAST: %v, Recognize: %v", src, perr, rerr)
		}
		tree, err := xml.Decode(src)
		// encoding/xml rejects versions other than 1.0, which XML 1.0 processes as 1.0 (2.8).
		if err != nil || tree.DTD != nil || tree.ResolveNamespaces() != nil || tree.Version != "" && tree.Version != "1.0" {
			return
		}
		want, err := stdTokens(src)
		if err != nil {
			// encoding/xml checks names against the character classes of the editions before the
			// fifth, which allows many more characters in names.
			if strings.Contains(err.Error(), "invalid XML name") && !isASCII(src) {
				return
			}
			t.Fatalf("%q: accepted, but encoding/xml: %v", src, err)
		}
		if got := ourTokens(tree); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%q:\n got  %q\n want %q", src, got, want)
		}
	})
}

// newlines normalizes line ends, which encoding/xml does not do in comments and processing
// instructions.
func newlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
