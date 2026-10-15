package lsp

import (
	"strings"
	"testing"
)

func TestStructDeclarationCompletionProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		fieldName    bool
	}{
		{"empty", "type Box struct {|}", true},
		{"semicolon", "type Box struct { Old Match; |}", true},
		{"partial-name", "type Box struct { Na|}", true},
		{"partial-name-after-semicolon", "type Box struct { Old Match; Na|}", true},
		{"block-comment", "type Box struct { /* field documentation */ |}", true},
		{"field-type", "type Box struct { Name |}", false},
		{"partial-field-type", "type Box struct { Name Ma|}", false},
		{"grouped-field-type", "type Box struct { Name (\n Ma|) }", false},
		{"alias", "type Box = Ma|", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertStructCompletion(t, tc.source, tc.fieldName, true)
		})
	}
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		t.Run("newline-"+strings.ReplaceAll(newline, "\r", "CR"), func(t *testing.T) {
			assertStructCompletion(t, "type Box struct { Old Match"+newline+"|}", true, true)
			assertStructCompletion(t, "type Box struct { Old Match"+newline+"Na|}", true, true)
			assertStructCompletion(t, "type Box struct {"+newline+"// field documentation"+newline+"|}", true, true)
		})
	}
	t.Run("without-snippets", func(t *testing.T) {
		assertStructCompletion(t, "type Box struct {|}", true, false)
	})
}

func assertStructCompletion(t *testing.T, source string, fieldName, snippets bool) {
	t.Helper()
	c := newServer(t)
	c.request("initialize", map[string]any{"capabilities": map[string]any{"textDocument": map[string]any{
		"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": snippets}},
	}}})
	c.notify("initialized", map[string]any{})
	defer c.exit()
	source = "type Other terminal\n" + source
	off := strings.Index(source, "|")
	source = strings.Replace(source, "|", "", 1)
	const uri = "file:///struct-completion.pego"
	c.open(uri, source)
	position := newTextIndex(source).position(off)
	var got completionList
	c.requestInto("textDocument/completion", at(uri, position.Line, position.Character), &got)
	items := map[string]CompletionItem{}
	for _, item := range got.Items {
		items[item.Label] = item
	}
	for _, name := range []string{"Box", "Other", "Match", "string"} {
		if _, ok := items[name]; !ok {
			t.Errorf("missing type %s: %+v", name, got.Items)
		}
	}
	item, ok := items["FieldName Type"]
	if ok != fieldName {
		t.Fatalf("field snippet present=%v, want %v: %+v", ok, fieldName, got.Items)
	}
	if fieldName {
		if item.Kind != completionKindSnippet {
			t.Errorf("field item kind=%d", item.Kind)
		}
		if snippets {
			if item.InsertText != "${1:FieldName} ${2:Type}" || item.InsertTextFormat != insertSnippet {
				t.Errorf("field snippet: %+v", item)
			}
		} else if item.InsertText != "" || item.InsertTextFormat != 0 {
			t.Errorf("snippet syntax sent to plain client: %+v", item)
		}
		for _, name := range []string{"len", "rule", "type", "Old"} {
			if _, ok := items[name]; ok {
				t.Errorf("unexpected field-name suggestion %s", name)
			}
		}
	}
}

func TestStructDeclarationCompletionPreservesConstructors(t *testing.T) {
	c := newInitialized(t)
	defer c.exit()
	const uri = "file:///constructor-completion.pego"
	source := "type Box struct { Name Match; Count int }\nrule main = \"a\" => Box{Name: $1, |}"
	off := strings.Index(source, "|")
	source = strings.Replace(source, "|", "", 1)
	c.open(uri, source)
	position := newTextIndex(source).position(off)
	var got completionList
	c.requestInto("textDocument/completion", at(uri, position.Line, position.Character), &got)
	if len(got.Items) != 1 || got.Items[0].Label != "Count" || got.Items[0].InsertText != "Count: $1" || got.Items[0].InsertTextFormat != insertSnippet {
		t.Fatalf("constructor completion changed: %+v", got.Items)
	}
}
