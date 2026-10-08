package xml_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/xml"
)

func ExampleDecode() {
	tree, err := xml.Decode(`<?xml version="1.0"?>
<!DOCTYPE catalog [
  <!ENTITY pego "PEGO">
  <!ATTLIST book lang CDATA "en">
]>
<catalog>
  <book id="b1"><title>&pego; &amp; XML</title></book>
  <book id="b2" lang="ja"><title>Parsing&#x20;Expression Grammars</title></book>
</catalog>`)
	if err != nil {
		panic(err)
	}
	for _, b := range tree.Root.Elements() {
		id, _ := b.Attr("id")
		lang, _ := b.Attr("lang")
		fmt.Printf("%s %s %q\n", id, lang, b.Text())
	}
	// Output:
	// b1 en "PEGO & XML"
	// b2 ja "Parsing Expression Grammars"
}

func ExampleDecode_error() {
	_, err := xml.Decode(`<a x="1" x="2"/>`)
	var we *xml.WFError
	if errors.As(err, &we) {
		fmt.Println(we.Line, we.Col, we.Msg)
	}
	_, err = xml.Decode(`<a><b></a>`)
	var se *xml.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Message())
	}
	// Output:
	// 1 10 attribute x appears twice
	// 1 10 mismatched end tag
}

func ExampleParseAST() {
	doc, err := xml.ParseAST(`<a x="1"><b>text</b><!-- note --></a>`)
	if err != nil {
		panic(err)
	}
	for _, c := range doc.Root.Content {
		switch c := c.(type) {
		case *xml.Element:
			fmt.Printf("element %s at %d-%d\n", c.Name.Text, c.Start, c.End)
		case *xml.Comment:
			fmt.Printf("comment %q at %d-%d\n", c.Text, c.Start, c.End)
		}
	}
	// Output:
	// element b at 9-20
	// comment " note " at 24-30
}

func ExampleTree_ResolveNamespaces() {
	tree, err := xml.Decode(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:x="urn:x"><x:id>1</x:id></feed>`)
	if err != nil {
		panic(err)
	}
	if err := tree.ResolveNamespaces(); err != nil {
		panic(err)
	}
	fmt.Println(tree.Root.Space, tree.Root.Local)
	id := tree.Root.Elements()[0]
	fmt.Println(id.Space, id.Local)
	// Output:
	// http://www.w3.org/2005/Atom feed
	// urn:x id
}

func ExampleWellFormed() {
	fmt.Println(xml.WellFormed(`<a><b/></a>`))
	fmt.Println(xml.WellFormed(`<a>&nbsp;</a>`))
	// Output:
	// <nil>
	// 1:4: entity &nbsp; is not declared
}
