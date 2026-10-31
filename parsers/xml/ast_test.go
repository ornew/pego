package xml_test

import (
	"testing"

	"github.com/ornew/zeroparse/parsers/xml"
	"github.com/ornew/zeroparse/parsers/xml/types"
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
	if got, err := xml.ExpandAttValue(doc.Root.Attrs[0].Value); got != " <1\t 2& " || err != nil {
		t.Errorf("ExpandAttValue: %q, %v", got, err)
	}
	c := doc.Root.Content
	if got := c[0].(*types.CharData).Value(); got != "t\n" {
		t.Errorf("CharData.Value: %q", got)
	}
	if got := c[1].(*types.EntityRef).Name(); got != "e" {
		t.Errorf("EntityRef.Name: %q", got)
	}
	if r, ok := c[2].(*types.CharRef).Rune(); r != '😀' || !ok {
		t.Errorf("CharRef.Rune: %q, %v", r, ok)
	}
	if got := c[3].(*types.CDSect).Value(); got != "c\n" {
		t.Errorf("CDSect.Value: %q", got)
	}
	if got := c[4].(*types.Comment).Value(); got != "m\n" {
		t.Errorf("Comment.Value: %q", got)
	}
	if got := c[5].(*types.PI).Data.Value(); got != "d\n" {
		t.Errorf("PIData.Value: %q", got)
	}
	doc, err = xml.ParseAST(`<a x="&e;"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xml.ExpandAttValue(doc.Root.Attrs[0].Value); err == nil || err.Error() != "entity &e; is not declared" {
		t.Errorf("ExpandAttValue with an entity: %v", err)
	}
	doc, err = xml.ParseAST(`<a>&#0;</a>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, legal := doc.Root.Content[0].(*types.CharRef).Rune(); legal {
		t.Error("CharRef.Rune: &#0; is legal")
	}
}
