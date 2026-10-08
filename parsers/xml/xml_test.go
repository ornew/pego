package xml_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ornew/pego/parsers/xml"
)

// TestDecode checks decoded documents in the canonical form of the conformance suite (see canonical).
func TestDecode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`<a/>`, `<a></a>`},
		{"<?xml version='1.0' encoding=\"UTF-8\" standalone='yes'?>\n<!--c--><?p x?> <a/> <?q?>", `<?p x?><a></a><?q ?>`},
		{"\ufeff<a/>", `<a></a>`},
		// Text: references, CDATA sections and line ends.
		{"<a>x&lt;&gt;&amp;&apos;&quot;&#65;&#x42;&#x10000;<![CDATA[<&>]]>]</a>", `<a>x&lt;&gt;&amp;'&quot;AB` + "\U00010000" + `&lt;&amp;&gt;]</a>`},
		{"<a>1\r\n2\r3\n<![CDATA[\r\n]]>&#13;</a>", `<a>1&#10;2&#10;3&#10;&#10;&#13;</a>`},
		{`<a>]]&gt;] ]></a>`, `<a>]]&gt;] ]&gt;</a>`},
		// Attribute values (3.3.3).
		{"<a x=' 1\t2\n3\r\n4\r5 ' y=\"&#9;&#10;&#13;&#32;\" z='&lt;&#60;'/>", `<a x=" 1 2 3 4 5 " y="&#9;&#10;&#13; " z="&lt;&lt;"></a>`},
		// Internal entities, nested, in content and in attribute values.
		{`<!DOCTYPE a [<!ENTITY e "x&f;y"><!ENTITY f "<b>&#38;amp;</b>">]><a>&e;</a>`, `<a>x<b>&amp;</b>y</a>`},
		{`<!DOCTYPE a [<!ENTITY e "1 &f; 2"><!ENTITY f "&#38;#60;&#9;">]><a v="&e;"/>`, `<a v="1 &lt;  2"></a>`},
		{`<!DOCTYPE a [<!ENTITY e "&#38;#38;"><!ENTITY f "&#38;#60;">]><a>&e;&f;</a>`, `<a>&amp;&lt;</a>`},
		// Markup made of character references in an entity value is markup in the replacement text.
		{`<!DOCTYPE a [<!ENTITY e "&#60;b>x&#60;/b>&#38;#60;">]><a>&e;</a>`, `<a><b>x</b>&lt;</a>`},
		// White space characters of a replacement text become spaces in attribute values; line ends of
		// replacement texts are not normalized.
		{`<!DOCTYPE a [<!ENTITY e "1&#9;2&#13;&#10;3">]><a x="&e;">&e;</a>`, `<a x="1 2  3">1&#9;2&#13;&#10;3</a>`},
		// Attribute defaults: references expanded where they are declared.
		{`<!DOCTYPE a [<!ENTITY e "v"><!ATTLIST a x CDATA "&e;&#60;" y CDATA "&#38;e;">]><a/>`, `<a x="v&lt;" y="&amp;e;"></a>`},
		// An undeclared entity in a default, where the DTD was not read entirely.
		{`<!DOCTYPE a SYSTEM "a.dtd" [<!ATTLIST a x CDATA "&u;">]><a/>`, `<a x="&amp;u;"></a>`},
		// The first declaration of an entity binds.
		{`<!DOCTYPE a [<!ENTITY e "1"><!ENTITY e "2">]><a>&e;</a>`, `<a>1</a>`},
		// Attribute defaults and normalization of tokenized types.
		{`<!DOCTYPE a [<!ATTLIST a x CDATA " 1  2 " y NMTOKENS " 1  2 " z (p|q) #FIXED "q" r ID #IMPLIED s CDATA #REQUIRED>
		  <!ATTLIST a x CDATA "ignored" t CDATA "t">]><a y=" a  b " s="s"/>`,
			`<a s="s" t="t" x=" 1  2 " y="a b" z="q"></a>`},
		// Parameter entities between declarations.
		{`<!DOCTYPE a [<!ENTITY % p "<!ENTITY e 'from p'><!ATTLIST a x CDATA 'd'>">%p;]><a>&e;</a>`, `<a x="d">from p</a>`},
		// After a parameter entity that is not read, entity and attribute-list declarations are not
		// processed: the reference to e stays.
		{`<!DOCTYPE a [<!ENTITY % p SYSTEM "p.ent">%p;<!ENTITY e "x"><!ATTLIST a x CDATA 'd'>]><a>&e;</a>`, `<a>&e;</a>`},
		// ... unless the document is standalone.
		{`<?xml version="1.0" standalone="yes"?><!DOCTYPE a [<!ENTITY % p SYSTEM "p.ent">%p;<!ENTITY e "x">]><a>&e;</a>`, `<a>x</a>`},
		// External entities are not read; an undeclared entity is allowed with an external subset.
		{`<!DOCTYPE a [<!ENTITY e SYSTEM "e.ent">]><a>&e;</a>`, `<a>&e;</a>`},
		{`<!DOCTYPE a SYSTEM "a.dtd"><a v="&u;">&u;</a>`, `<a v="&amp;u;">&u;</a>`},
		// Notations, with the processing instructions of the subset (second canonical form).
		{`<!DOCTYPE a [<!NOTATION n SYSTEM "s"><!NOTATION m PUBLIC "p"><!NOTATION l PUBLIC "p" "s"><?pi?>]><a/>`,
			"<?pi ?><!DOCTYPE a [\n<!NOTATION l PUBLIC 'p' 's'>\n<!NOTATION m PUBLIC 'p'>\n<!NOTATION n SYSTEM 's'>\n]>\n<a></a>"},
		// Names of the fifth edition.
		{"<\u0E5C\u309a:\U00010000 a\u00B7\u0300=''/>", "<\u0E5C\u309a:\U00010000 a\u00B7\u0300=\"\"></\u0E5C\u309a:\U00010000>"},
		{`<?xml version="1.7"?><a/>`, `<a></a>`},
	} {
		tree, err := xml.Decode(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got := canonical(tree); got != tc.want {
			t.Errorf("%q:\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
}

// TestNotWellFormed checks that documents that break a rule of the syntax or a well-formedness
// constraint are rejected, with the expected message.
func TestNotWellFormed(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{``, `1:1: syntax error`},
		{`<a>`, `1:4: syntax error`},
		{`<a></b>`, `1:7: mismatched end tag`},
		{`<a/><b/>`, `1:5: syntax error: expected "<!--", "<?", (? \t\r\n), end of input`},
		{`text<a/>`, `1:1: syntax error`},
		{` <?xml version="1.0"?><a/>`, `1:7: the processing instruction target xml is reserved`},
		{`<?xml version="2.0"?><a/>`, `1:16: syntax error`},
		{`<?XML version="1.0"?><a/>`, `1:6: the processing instruction target xml is reserved`},
		{`<a>]]></a>`, `1:4: syntax error`},
		{`<a><!-- a -- b --></a>`, `1:12: syntax error`},
		{`<a><!-- a ---></a>`, `1:12: syntax error`},
		{"<a>\x01</a>", `1:4: syntax error`},
		{"<a>\uFFFE</a>", `1:4: syntax error`},
		{"<a>\xff</a>", `1:4: invalid UTF-8`},
		{`<a b="<"/>`, `1:7: syntax error`},
		{`<a b="&"/>`, `1:8: syntax error`},
		{`<a b=1/>`, `1:6: syntax error`},
		{`<a b="1"c="2"/>`, `1:9: syntax error`},
		{`<a>&#0;</a>`, `1:4: character reference &#0; is not a legal character`},
		{`<a>&#xD800;</a>`, `1:4: character reference &#xD800; is not a legal character`},
		{`<a x="&#xFFFF;"/>`, `1:7: character reference &#xFFFF; is not a legal character`},
		{`<a x="1" x="2"/>`, `1:10: attribute x appears twice`},
		{`<a>&e;</a>`, `1:4: entity &e; is not declared`},
		{`<a x="&e;"/>`, `1:7: entity &e; is not declared`},
		{`<!DOCTYPE a [<!ENTITY e "x">]><a>&f;</a>`, `1:34: entity &f; is not declared`},
		{`<?xml version="1.0" standalone="yes"?><!DOCTYPE a SYSTEM "a.dtd"><a>&f;</a>`, `entity &f; is not declared`},
		{`<?xml version="1.0" standalone="yes"?><!DOCTYPE a [%p;]><a/>`, `parameter entity %p; is not declared`},
		{`<!DOCTYPE a [<!ENTITY e "&e;">]><a>&e;</a>`, `1:36: in the replacement text of entity e: recursive reference to entity e`},
		{`<!DOCTYPE a [<!ENTITY e "&f;"><!ENTITY f "&e;">]><a x="&e;"/>`, `recursive reference to entity e`},
		{`<!DOCTYPE a [<!ENTITY % p "&#37;p;">%p;]><a/>`, `recursive reference to entity p`},
		{`<!DOCTYPE a [<!ENTITY e "<b>">]><a>&e;</a>`, `1:36: in the replacement text of entity e: the replacement text is not well-formed: an element or markup is not closed in it`},
		{`<!DOCTYPE a [<!ENTITY e "<b>">]><a>&e;</b></a>`, `1:42: mismatched end tag`},
		{`<!DOCTYPE a [<!ENTITY e "</a><a>">]><a>&e;</a>`, `1:40: in the replacement text of entity e: 1:4: mismatched end tag`},
		// The replacement text is parsed inside an element x: an end tag x does not end it early.
		{`<!DOCTYPE x [<!ENTITY e "</x><x>">]><x>&e;</x>`, `1:40: in the replacement text of entity e: 1:5: syntax error`},
		{`<!DOCTYPE x [<!ENTITY e "</x><!--c--><x>">]><x>&e;</x>`, `1:48: in the replacement text of entity e: 1:13: syntax error`},
		{`<!DOCTYPE a [<!ENTITY e "&#38;">]><a>&e;</a>`, `in the replacement text of entity e: 1:2: syntax error`},
		{`<!DOCTYPE a [<!ENTITY e "&#60;">]><a x="&e;"/>`, `the replacement text of entity e, referenced in an attribute value, contains <`},
		{`<!DOCTYPE a [<!ENTITY e "&#38;">]><a x="&e;"/>`, `malformed reference`},
		{`<!DOCTYPE a [<!ENTITY e SYSTEM "e.ent">]><a x="&e;"/>`, `reference to the external entity e in an attribute value`},
		{`<!DOCTYPE a [<!NOTATION n SYSTEM "n"><!ENTITY e SYSTEM "e.gif" NDATA n>]><a>&e;</a>`, `reference to the unparsed entity e`},
		{`<!DOCTYPE a [<!ATTLIST a x CDATA "&e;">]><a/>`, `entity &e; is not declared`},
		{`<!DOCTYPE a [<!ATTLIST a x CDATA "&e;"><!ENTITY e "v">]><a/>`, `entity &e; is not declared`},
		{`<!DOCTYPE a [<!ENTITY e "a&#0;">]><a/>`, `character reference &#0; is not a legal character`},
		{`<!DOCTYPE a [<!ENTITY e "%p;">]><a/>`, `1:26: syntax error`},
		{`<!DOCTYPE a [<!ELEMENT a %p;>]><a/>`, `1:26: syntax error`},
		{`<!DOCTYPE a [<![INCLUDE[<!ELEMENT a ANY>]]>]><a/>`, `1:14: syntax error`},
		{`<!DOCTYPE a [<!ENTITY % p "<!ELEMENT a"><!ENTITY % q "ANY>">%p;%q;]><a/>`, `1:61: in the replacement text of entity p: 1:12: syntax error`},
		{`<!DOCTYPE a [<!ENTITY % p "<![INCLUDE[]]>">%p;]><a/>`, `in the replacement text of entity p: 1:1: syntax error`},
		{`<!DOCTYPE a [<!ENTITY % p "]><a/><!--">%p;]><a/>`, `1:40: in the replacement text of entity p: the replacement text is not well-formed: a declaration is not closed in it`},
		{`<!DOCTYPE a [<!ENTITY % p "%q;"><!ENTITY % q "%p;">]><a/>`, `1:28: syntax error`},
		{`<!DOCTYPE a [<!ELEMENT a (#PCDATA|b)>]><a/>`, `1:36: syntax error`},
		{`<!DOCTYPE a [<!ELEMENT a (b|c,d)>]><a/>`, `1:30: syntax error`},
		{`<!DOCTYPE a [<!ATTLIST a x CDATA>]><a/>`, `1:33: syntax error`},
		{`<!DOCTYPE a PUBLIC "{" "s"><a/>`, `1:21: syntax error`},
		{`<!DOCTYPE a [<!NOTATION n>]><a/>`, `1:26: syntax error`},
		{`<a><?xml-stylesheet?><?xml ?></a>`, `1:27: the processing instruction target xml is reserved`},
		{`<a/><!DOCTYPE a>`, `1:5: syntax error`},
		{`<a>%</a>&#65;`, `1:9: syntax error`},
	} {
		_, err := xml.Decode(tc.in)
		if err == nil {
			t.Errorf("%q: accepted", tc.in)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q:\n got  %v\n want %s", tc.in, err, tc.want)
		}
	}
}

func TestExpansionLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE a [<!ENTITY e0 "lol">`)
	for i := 1; i < 12; i++ {
		b.WriteString(`<!ENTITY e` + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ` "`)
		for range 10 {
			b.WriteString(`&e` + string(rune('0'+(i-1)/10)) + string(rune('0'+(i-1)%10)) + `;`)
		}
		b.WriteString(`">`)
	}
	src := strings.Replace(b.String(), "e0 ", "e00 ", 1) + `]><a>&e11;</a>`
	_, err := xml.Decode(src)
	if err == nil || !strings.Contains(err.Error(), "entity expansion limit exceeded") {
		t.Errorf("billion laughs: %v", err)
	}
	_, err = xml.Decode(strings.Replace(src, "<a>&e11;</a>", `<a x="&e11;"/>`, 1))
	if err == nil || !strings.Contains(err.Error(), "entity expansion limit exceeded") {
		t.Errorf("billion laughs in an attribute: %v", err)
	}
}

func TestNamespaces(t *testing.T) {
	type name struct{ space, local string }
	tree, err := xml.Decode(`<!DOCTYPE p:a [<!ATTLIST p:a xmlns:d CDATA "urn:d" d:x CDATA "dx">]>
		<p:a xmlns:p="urn:p" xmlns="urn:default" y="1" p:y="2"><b xml:lang="en"/><d:c xmlns=""><e/></d:c></p:a>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.ResolveNamespaces(); err != nil {
		t.Fatal(err)
	}
	var got []name
	var walk func(*xml.Elem)
	walk = func(e *xml.Elem) {
		got = append(got, name{e.Space, e.Local})
		for _, a := range e.Attrs {
			got = append(got, name{a.Space, a.Local})
		}
		for _, c := range e.Elements() {
			walk(c)
		}
	}
	walk(tree.Root)
	want := []name{
		{"urn:p", "a"}, {xml.XMLNSNamespace, "p"}, {xml.XMLNSNamespace, "xmlns"}, {"", "y"}, {"urn:p", "y"},
		{xml.XMLNSNamespace, "d"}, {"urn:d", "x"},
		{"urn:default", "b"}, {xml.XMLNamespace, "lang"},
		{"urn:d", "c"}, {xml.XMLNSNamespace, "xmlns"},
		{"", "e"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%d: got %v, want %v", i, got[i], want[i])
		}
	}

	for _, tc := range []struct{ in, want string }{
		{`<p:a/>`, `the prefix p is not declared`},
		{`<a p:x="1"/>`, `the prefix p is not declared`},
		{`<a:b:c xmlns:a="u"/>`, `not a qualified name`},
		{`<a xmlns:p="u" :x="1"/>`, `not a qualified name`},
		{`<a xmlns:p="u" p:-x="1"/>`, `not a qualified name`},
		{`<a xmlns:p="u" xmlns:q="u" p:x="1" q:x="2"/>`, `the same namespace name and local part`},
		{`<a xmlns:p=""/>`, `cannot be undeclared`},
		{`<a xmlns:xmlns="u"/>`, `xmlns must not be declared`},
		{`<a xmlns:xml="u"/>`, `the prefix xml cannot be bound`},
		{`<a xmlns:x="http://www.w3.org/XML/1998/namespace"/>`, `can be bound only to the prefix xml`},
		{`<a xmlns="http://www.w3.org/2000/xmlns/"/>`, `cannot be the default namespace`},
		{`<xmlns:a/>`, `has the prefix xmlns`},
		{`<?a:b?><a/>`, `the processing instruction target a:b contains a colon`},
		{`<!DOCTYPE a [<!ENTITY a:b "x">]><a/>`, `the name a:b contains a colon`},
		{`<!DOCTYPE a [<!ENTITY % p "<!NOTATION a:b SYSTEM 's'>">%p;]><a/>`, `the name a:b contains a colon`},
	} {
		tree, err := xml.Decode(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		err = tree.ResolveNamespaces()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %s", tc.in, err, tc.want)
		}
	}
	// xml:prefix may be declared with its namespace name.
	tree, err = xml.Decode(`<a xmlns:xml="http://www.w3.org/XML/1998/namespace" xml:lang="en"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := tree.ResolveNamespaces(); err != nil {
		t.Error(err)
	}
}

func TestTranscode(t *testing.T) {
	utf16be := func(s string, bom bool) []byte {
		var b []byte
		if bom {
			b = append(b, 0xFE, 0xFF)
		}
		for _, u := range utf16.Encode([]rune(s)) {
			b = append(b, byte(u>>8), byte(u))
		}
		return b
	}
	utf16le := func(s string, bom bool) []byte {
		var b []byte
		if bom {
			b = append(b, 0xFF, 0xFE)
		}
		for _, u := range utf16.Encode([]rune(s)) {
			b = append(b, byte(u), byte(u>>8))
		}
		return b
	}
	doc := "<a>é😀</a>"
	for _, tc := range []struct {
		name string
		in   []byte
		want string // "" for an error
		err  string
	}{
		{"UTF-8", []byte(doc), doc, ""},
		{"UTF-8 BOM", []byte("\uFEFF" + doc), doc, ""},
		{"UTF-16BE BOM", utf16be(doc, true), doc, ""},
		{"UTF-16LE BOM", utf16le(doc, true), doc, ""},
		{"UTF-16BE", utf16be(`<?xml version="1.0" encoding="UTF-16BE"?>`+doc, false), `<?xml version="1.0" encoding="UTF-16BE"?>` + doc, ""},
		{"UTF-16LE declared", utf16le(`<?xml version='1.0' encoding='utf-16'?>`+doc, true), `<?xml version='1.0' encoding='utf-16'?>` + doc, ""},
		{"Latin-1", []byte("<?xml version='1.0' encoding='ISO-8859-1'?><a>\xe9</a>"), "<?xml version='1.0' encoding='ISO-8859-1'?><a>é</a>", ""},
		{"ASCII", []byte(`<?xml version="1.0" encoding="us-ascii"?><a/>`), `<?xml version="1.0" encoding="us-ascii"?><a/>`, ""},
		{"not ASCII", []byte("<?xml version='1.0' encoding='US-ASCII'?><a>\xe9</a>"), "", "not US-ASCII"},
		{"UTF-16 declared in UTF-8", []byte(`<?xml version="1.0" encoding="UTF-16"?><a/>`), "", "not in UTF-16"},
		{"UTF-8 declared in UTF-16", utf16be(`<?xml version="1.0" encoding="UTF-8"?><a/>`, true), "", "but the document is in UTF-16"},
		{"Latin-1 with a UTF-8 BOM", []byte("\uFEFF<?xml version='1.0' encoding='ISO-8859-1'?><a/>"), "", "byte order mark"},
		{"unsupported", []byte(`<?xml version="1.0" encoding="Shift_JIS"?><a/>`), "", "unsupported encoding: SHIFT_JIS"},
		{"UCS-4", []byte("\x00\x00\x00<\x00\x00\x00a"), "", "unsupported encoding: UCS-4"},
		{"odd UTF-16", append(utf16be(doc, true), 0), "", "odd number of bytes"},
		{"unpaired surrogate", append(utf16be(doc, true), 0xD8, 0), "", "unpaired surrogate"},
	} {
		got, err := xml.Transcode(tc.in)
		switch {
		case tc.err != "":
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: got %q, %v, want error %s", tc.name, got, err, tc.err)
			}
		case err != nil || got != tc.want:
			t.Errorf("%s: got %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := xml.DecodeBytes(utf16le(doc, true)); err != nil {
		t.Errorf("DecodeBytes: %v", err)
	}
}

// TestEntityPositions checks positions reported inside entities and the spans of what they contain.
func TestEntityPositions(t *testing.T) {
	src := "<!DOCTYPE a [<!ENTITY e '<b x=\"1\"/>'>]>\n<a>é&e;</a>"
	tree, err := xml.Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	b := tree.Root.Elements()[0]
	if b.Entity != "e" || b.Src.Start != 0 || b.Src.End != 10 || b.Src.Attrs[0].Value.Start != 6 {
		t.Errorf("element from an entity: %q %+v", b.Entity, b.Src.Span)
	}
	_, err = xml.Decode("<!DOCTYPE a [<!ENTITY e '<b x=\"1\" x=\"2\"/>'>]>\n<a>é&e;</a>")
	var we *xml.WFError
	if !errors.As(err, &we) || we.Line != 2 || we.Col != 5 || we.Pos != 50 {
		t.Errorf("error in an entity: %#v", err)
	}
}

// TestRecognize checks that Recognize and ParseAST agree on every input of the tests.
func TestRecognize(t *testing.T) {
	inputs := []string{`<a/>`, `<a></b>`, `<!DOCTYPE a [<!ENTITY e "x">]><a>&e;</a>`, `<a>]]></a>`, ``}
	files, _ := filepath.Glob("testdata/*.txt")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, string(data))
	}
	for _, in := range inputs {
		_, perr := xml.ParseAST(in)
		rerr := xml.Recognize(in)
		if (perr == nil) != (rerr == nil) || perr != nil && perr.Error() != rerr.Error() {
			t.Errorf("%q: ParseAST: %v, Recognize: %v", in, perr, rerr)
		}
	}
}

// TestDeepNesting checks that nesting up to the depth limit of the generated parser parses and that
// deeper nesting is an error, not a stack overflow.
func TestDeepNesting(t *testing.T) {
	for _, tc := range []struct {
		depth int
		ok    bool
	}{{24000, true}, {26000, false}, {1000000, false}} {
		src := strings.Repeat("<a>", tc.depth) + strings.Repeat("</a>", tc.depth)
		if _, err := xml.Decode(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: Decode: %v", tc.depth, err)
		}
		if err := xml.Recognize(src); (err == nil) != tc.ok {
			t.Errorf("depth %d: Recognize: %v", tc.depth, err)
		}
	}
}

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := xml.Parse(string(data))
		if n != nil {
			got = n.String()
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}
