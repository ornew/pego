package lsp

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func commentedNamesSource(typeGap, defGap, newGap, typeName, ruleName string) string {
	return fmt.Sprintf("// Box documentation.\ntype%s%s struct {Text string}\ndef%s%s = \"a\"\ndef%s main:%s = %s -> new%s%s{Text:\"a\"}\n",
		typeGap, typeName, defGap, ruleName, defGap, typeName, ruleName, newGap, typeName)
}

func TestNamesAcrossComments(t *testing.T) {
	for _, tc := range []struct{ name, typeGap, defGap, newGap string }{
		{"whitespace", " \t", "\n ", "\t "},
		{"type", " // Box decoy\n ", " ", " "},
		{"rule", " ", " // helper decoy\n ", " "},
		{"constructor", " ", " ", " // Box decoy\n "},
		{"multiple LF", " // Box 😀\n // second\n ", " // helper 😀\n // second\n ", " // Box 😀\n // second\n "},
		{"CRLF", " // Box\r\n ", " // helper\r\n ", " // Box\r\n "},
		{"CR", " // Box\r ", " // helper\r ", " // Box\r "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newInitialized(t)
			defer c.exit()
			uri := "file:///comment-names.pego"
			src := commentedNamesSource(tc.typeGap, tc.defGap, tc.newGap, "Box", "helper")
			if d := c.open(uri, src); len(d) != 0 {
				t.Fatalf("diagnostics %+v", d)
			}
			idx := newTextIndex(src)
			rangeAt := func(marker, name string) Range {
				i := strings.Index(src, marker)
				if i < 0 {
					t.Fatalf("missing marker %q", marker)
				}
				return idx.rangeOf(i, i+len(name))
			}
			boxDef := rangeAt("Box struct", "Box")
			helperDef := rangeAt("helper =", "helper")
			mainDef := rangeAt("main:", "main")
			boxAnnotation := rangeAt("Box =", "Box")
			boxNew := rangeAt("Box{", "Box")
			helperRef := rangeAt("helper ->", "helper")
			var syms []DocumentSymbol
			c.requestInto("textDocument/documentSymbol", docParams(uri), &syms)
			var names []string
			var selections []Range
			for _, s := range syms {
				names = append(names, s.Name)
				selections = append(selections, s.SelectionRange)
			}
			if !reflect.DeepEqual(names, []string{"Box", "helper", "main"}) || !reflect.DeepEqual(selections, []Range{boxDef, helperDef, mainDef}) {
				t.Errorf("symbols = %v %v; want Box/helper/main at actual names", names, selections)
			}
			for _, point := range []struct {
				r, def Range
				text   string
			}{
				{boxDef, boxDef, "type Box struct"}, {boxNew, boxDef, "type Box struct"},
				{helperDef, helperDef, "def helper:"}, {helperRef, helperDef, "def helper:"},
				{mainDef, mainDef, "def main: Box"},
			} {
				var got []Location
				c.requestInto("textDocument/definition", at(uri, point.r.Start.Line, point.r.Start.Character), &got)
				if !reflect.DeepEqual(got, []Location{{URI: uri, Range: point.def}}) {
					t.Errorf("definition at %+v: %+v", point.r, got)
				}
				text, r := hoverText(c, uri, point.r.Start.Line, point.r.Start.Character)
				if !strings.Contains(text, point.text) || r == nil || *r != point.r {
					t.Errorf("hover at %+v: %q %v", point.r, text, r)
				}
				if point.text == "type Box struct" && !strings.Contains(text, "Box documentation.") {
					t.Errorf("lost documentation: %q", text)
				}
			}
			for _, symbol := range []struct {
				at      Range
				name    string
				refs    []Range
				renamed string
			}{
				{boxNew, "Crate", []Range{boxDef, boxAnnotation, boxNew}, commentedNamesSource(tc.typeGap, tc.defGap, tc.newGap, "Crate", "helper")},
				{helperDef, "word", []Range{helperDef, helperRef}, commentedNamesSource(tc.typeGap, tc.defGap, tc.newGap, "Box", "word")},
			} {
				for _, decl := range []bool{false, true} {
					p := at(uri, symbol.at.Start.Line, symbol.at.Start.Character)
					p["context"] = map[string]any{"includeDeclaration": decl}
					var got []Location
					c.requestInto("textDocument/references", p, &got)
					var want []Location
					for i, r := range symbol.refs {
						if decl || i > 0 {
							want = append(want, Location{URI: uri, Range: r})
						}
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("references at %+v (declaration %v): %v; want %v", symbol.at, decl, got, want)
					}
				}
				p := at(uri, symbol.at.Start.Line, symbol.at.Start.Character)
				p["newName"] = symbol.name
				var we workspaceEdit
				c.requestInto("textDocument/rename", p, &we)
				if got := applyEdits(src, we.Changes[uri]); got != symbol.renamed {
					t.Errorf("rename %s:\n%s\nwant:\n%s", symbol.name, got, symbol.renamed)
				} else if a := analyze(got); len(a.diags) != 0 {
					t.Errorf("renamed source fails analysis: %+v", a.diags)
				}
			}
		})
	}
}

func TestCommentNameAtEOF(t *testing.T) {
	c := newInitialized(t)
	defer c.exit()
	for i, tail := range []string{"def // missing", "type // missing\n// another", `def b = "b" -> new // missing`} {
		uri := fmt.Sprintf("file:///comment-eof-%d.pego", i)
		src := "def good = \"a\"\n" + tail
		if d := c.open(uri, src); len(d) == 0 {
			t.Fatal("incomplete source has no diagnostics")
		}
		var syms []DocumentSymbol
		c.requestInto("textDocument/documentSymbol", docParams(uri), &syms)
		if len(syms) != 1 || syms[0].Name != "good" {
			t.Errorf("incomplete source symbols: %+v", syms)
		}
		comment := strings.Index(src, "//") + 3
		p := newTextIndex(src).position(comment)
		if got := c.request("textDocument/hover", at(uri, p.Line, p.Character)); string(got) != "null" {
			t.Errorf("hover in incomplete comment = %s", got)
		}
		if got := c.request("textDocument/prepareRename", at(uri, p.Line, p.Character)); string(got) != "null" {
			t.Errorf("rename in incomplete comment = %s", got)
		}
	}
}

// Analyze identical source before/after. Commented grammars intentionally gain
// missing symbols/references; the plain control performs equivalent work.
func BenchmarkAnalyzeCommentedNames(b *testing.B) {
	for _, gap := range []struct{ name, text string }{{"plain", " "}, {"comments", " // name\n // second\n "}} {
		var src strings.Builder
		src.WriteString("type" + gap.text + "Box struct {Text string}\n")
		for i := 0; i < 128; i++ {
			fmt.Fprintf(&src, "def%sr%d:Box = \"a\" -> new%sBox{Text:\"a\"}\n", gap.text, i, gap.text)
		}
		text := src.String()
		b.Run(gap.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				analyze(text)
			}
		})
	}
}
