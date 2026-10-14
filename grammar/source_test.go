package grammar

import (
	"reflect"
	"strings"
	"testing"
)

func TestSourceInfoJSON(t *testing.T) {
	source := SourceInfo{Pos: Pos{Line: 2, Col: 3}, End: Pos{Line: 4, Col: 5}, DiagnosticPos: Pos{Line: 3, Col: 7}, LeadingComments: " leading\n", TrailingComments: " tail\n", LeadingDetachedComments: []string{"first\n", ""}}
	g := expressionGrammar(&Literal{SourceInfo: source, Value: "x"})
	g.SourceInfo = source
	g.Package = "p"
	g.PackageSource = source
	data, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []func([]byte) (*Grammar, error){UnmarshalJSON, DecodeJSON} {
		copy, err := decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g, copy) {
			t.Fatalf("JSON metadata roundtrip: %s", data)
		}
	}
	// Grammars made before source metadata existed remain valid JSON inputs.
	old := []byte(`{"statements":[{"@type":"RuleDef","name":"main","expr":{"@type":"Literal","value":"x"}}]}`)
	for _, decode := range []func([]byte) (*Grammar, error){UnmarshalJSON, DecodeJSON} {
		copy, err := decode(old)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(copy.SourceInfo, SourceInfo{}) || !reflect.DeepEqual(copy.Rules()[0].SourceInfo, SourceInfo{}) {
			t.Fatal("old JSON must have unknown positions")
		}
		data, err := MarshalJSON(copy)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"source"`) {
			t.Fatalf("zero source metadata not omitted: %s", data)
		}
	}
	if source.Diagnostic() != source.DiagnosticPos {
		t.Fatal("diagnostic token ignored")
	}
	source.DiagnosticPos = Pos{}
	if source.Diagnostic() != source.Pos {
		t.Fatal("diagnostic fallback ignored")
	}
}

func TestGrammarCloneSourcesAndSharing(t *testing.T) {
	comment := &Comment{Text: "// raw", Content: " raw\n"}
	shared := &Optional{SourceInfo: SourceInfo{LeadingDetachedComments: []string{"child"}}, Expr: &Literal{Value: "x"}}
	g := expressionGrammar(&Seq{Items: []Expr{shared, shared}})
	g.LeadingDetachedComments = []string{"root"}
	g.PackageSource.LeadingDetachedComments = []string{"package"}
	g.EndBreak = &LineBreak{Comments: []*Comment{comment}, Trailing: comment}
	copy := g.Clone()
	if !reflect.DeepEqual(g, copy) {
		t.Fatal("clone changed AST")
	}
	seq := copy.Rules()[0].Expr.(*Seq)
	if seq.Items[0] != seq.Items[1] || seq.Items[0] == shared {
		t.Fatal("shared children not preserved independently")
	}
	copy.LeadingDetachedComments[0] = "changed"
	copy.PackageSource.LeadingDetachedComments[0] = "changed"
	seq.Items[0].(*Optional).LeadingDetachedComments[0] = "changed"
	copy.EndBreak.Comments[0].Content = "changed"
	if g.LeadingDetachedComments[0] != "root" || g.PackageSource.LeadingDetachedComments[0] != "package" || shared.LeadingDetachedComments[0] != "child" || comment.Content != " raw\n" {
		t.Fatal("clone aliases original metadata")
	}
	if copy.EndBreak.Comments[0] != copy.EndBreak.Trailing {
		t.Fatal("raw comment sharing lost")
	}
	if (*Grammar)(nil).Clone() != nil {
		t.Fatal("nil clone")
	}
	cycle := &Optional{}
	cycle.Expr = cycle
	cloned := expressionGrammar(cycle).Clone()
	node := cloned.Rules()[0].Expr.(*Optional)
	if node.Expr != node || node == cycle {
		t.Fatal("pointer cycle not copied")
	}
	if err := Validate(cloned); err == nil {
		t.Fatal("cloned cycle must remain invalid")
	}
}

func TestValidationUsesDiagnosticToken(t *testing.T) {
	g := termGrammar(&Member{SourceInfo: SourceInfo{Pos: Pos{Line: 1, Col: 17}, DiagnosticPos: Pos{Line: 1, Col: 20}}, X: &VarRef{Name: "x"}, Name: "_reserved"})
	err, ok := Validate(g).(*ValidationError)
	if !ok || err.Pos != (Pos{Line: 1, Col: 20}) {
		t.Fatalf("diagnostic=%v", err)
	}
}

func TestLayoutOnlyMetadataIsNotSerialized(t *testing.T) {
	g := expressionGrammar(&Literal{SourceInfo: SourceInfo{Layout: &SourceLayout{Before: []*Comment{{Text: "/* raw */"}}}}, Value: "x"})
	data, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"source"`) || strings.Contains(string(data), "Layout") || strings.Contains(string(data), "raw") {
		t.Fatalf("raw-only metadata leaked into JSON: %s", data)
	}
}
