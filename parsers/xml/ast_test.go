package xml_test

import (
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// TestASTHelpers checks the methods of the typed values.
func TestASTHelpers(t *testing.T) {
	doc, err := xml.ParseAST("<!DOCTYPE a PUBLIC ' -//X//\r\n Y// ' 's\r\n'><a x=' &lt;1&#x9;\r\n2&amp; '>t\r\n&e;&#x1F600;<![CDATA[c\r]]><!--m\r\n--><?p d\r?></a>")
	if err != nil {
		t.Fatal(err)
	}
	id := doc.Doctype.ExternalID
	if got := id.Public.Value(); got != "-//X// Y//" {
		t.Errorf("PubidLiteral.Value: %q", got)
	}
	if got := id.System.Value(); got != "s\n" {
		t.Errorf("SystemLiteral.Value: %q", got)
	}
	if got, err := doc.Root.Attrs[0].Value.Value(); got != " <1\t 2& " || err != nil {
		t.Errorf("AttValue.Value: %q, %v", got, err)
	}
	c := doc.Root.Content
	if got := c[0].(*xml.CharData).Value(); got != "t\n" {
		t.Errorf("CharData.Value: %q", got)
	}
	if got := c[1].(*xml.EntityRef).Name(); got != "e" {
		t.Errorf("EntityRef.Name: %q", got)
	}
	if r, ok := c[2].(*xml.CharRef).Rune(); r != '😀' || !ok {
		t.Errorf("CharRef.Rune: %q, %v", r, ok)
	}
	if got := c[3].(*xml.CDSect).Value(); got != "c\n" {
		t.Errorf("CDSect.Value: %q", got)
	}
	if got := c[4].(*xml.Comment).Value(); got != "m\n" {
		t.Errorf("Comment.Value: %q", got)
	}
	if got := c[5].(*xml.PI).Data.Value(); got != "d\n" {
		t.Errorf("PIData.Value: %q", got)
	}
	doc, err = xml.ParseAST(`<a x="&e;"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Root.Attrs[0].Value.Value(); err == nil || err.Error() != "entity &e; is not declared" {
		t.Errorf("AttValue.Value with an entity: %v", err)
	}
	doc, err = xml.ParseAST(`<a>&#0;</a>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, legal := doc.Root.Content[0].(*xml.CharRef).Rune(); legal {
		t.Error("CharRef.Rune: &#0; is legal")
	}
}
