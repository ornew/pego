package syntax

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestFormatInteriorCommentOwners(t *testing.T) {
	for _, src := range []string{
		`rule main = "x" => int(/* argument */ "1")`,
		`type A = { X int }
rule main = "x" => A{X: /* value */ 1}`,
		`type A = { X int }
rule main = "x" => A{/* field */ X: 1}`,
		`rule main = "x" => 1 + /* right */ 2`,
		`rule main = {x := /* assignment */ 1} "x"`,
		`type Int terminal
rule main = "x" => Int{/* terminal */ "0"}`,
		`rule main = "x" => list(/* same */ 1, /* same */ 2)`,
		`rule main = "x" => map(list(), /* lambda */ (x) => $x)`,
		`rule main = "x" => int(/* unary */ -1)`,
		`rule main = "x" => int(- /* operand */ 1)`,
		`rule main = "x" => int(
            // argument
            "1"
        )`,
		`rule main = "x" => (1 +
            /* right */ 2)`,
		`rule main = "x" => (1 +
            // right
            2)`,
		`rule main = "x" => int(
            /* argument */ "1"
        )`,
		`rule main = {x := (
            /* assignment */ 1
        )} "x"`,
	} {
		t.Run(src, func(t *testing.T) {
			before, err := Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := grammar.Format(before)
			after, err := Parse(formatted)
			if err != nil {
				t.Fatalf("formatted parse: %v\n%s", err, formatted)
			}
			if len(before.AllComments()) != len(after.AllComments()) {
				t.Fatalf("comment count changed\n%s", formatted)
			}
			if second := grammar.Format(after); second != formatted {
				t.Fatalf("formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, second)
			}
			clone := before.Clone()
			if cloned := grammar.Format(clone); cloned != formatted {
				t.Fatal("clone changed formatting")
			}
			want, got := sourceCommentOwnerPaths(t, before), sourceCommentOwnerPaths(t, after)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("owner paths changed: before=%v after=%v\n%s", want, got, formatted)
			}
		})
	}
}

func sourceCommentOwnerPaths(t *testing.T, g *grammar.Grammar) map[string]any {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	owners := map[string]any{}
	var visit func(any, string)
	visit = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "source" || key == "packageSource" || key == "skipSource" {
					fields := child.(map[string]any)
					comments := map[string]any{}
					for _, name := range []string{"leadingComments", "trailingComments", "leadingDetachedComments"} {
						if value, ok := fields[name]; ok {
							comments[name] = value
						}
					}
					if len(comments) != 0 {
						owners[path+"/"+key] = comments
					}
				} else {
					visit(child, path+"/"+key)
				}
			}
		case []any:
			for i, child := range v {
				visit(child, path+"/"+strconv.Itoa(i))
			}
		}
	}
	visit(value, "")
	return owners
}

func TestInteriorLayoutJSONAndClone(t *testing.T) {
	before, err := Parse(`rule main = "x" => int(/* argument */ "1")`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(before)
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []func([]byte) (*grammar.Grammar, error){grammar.UnmarshalJSON, grammar.DecodeJSON} {
		decoded, err := decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sourceCommentOwnerPaths(t, before), sourceCommentOwnerPaths(t, decoded)) {
			t.Fatal("JSON lost normalized owners")
		}
		arg := decoded.Rules()[0].Action.(*grammar.Call).Args[0]
		if arg.(grammar.SourceNode).Source().Layout != nil {
			t.Fatal("JSON serialized raw layout")
		}
	}
	clone := before.Clone()
	original := before.Rules()[0].Action.(*grammar.Call).Args[0].(grammar.SourceNode).Source().Layout
	copied := clone.Rules()[0].Action.(*grammar.Call).Args[0].(grammar.SourceNode).Source().Layout
	if original == copied || original.Before[0] == copied.Before[0] {
		t.Fatal("layout alias in clone")
	}
	copied.Before[0].Text = "/* changed */"
	if original.Before[0].Text != "/* argument */" {
		t.Fatal("clone changed original layout")
	}
	programmatic := &grammar.Call{Func: "int", Args: []grammar.Term{&grammar.StringLit{Value: "1"}}}
	if got := grammar.FormatTerm(programmatic); got != `int("1")` {
		t.Fatal("nil-layout canonical fallback", got)
	}
}
