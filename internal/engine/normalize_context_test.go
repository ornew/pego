package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

func TestNormalizationContextParity(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = ("a" #repeat(1, 3) #atomic @error("as")) "b" $$`,
		`rule main = ("a" #atomic ";") #recover(skip=([^;]+ ";")) $$`,
		`var x int
rule main = {x := (1); (x == 1)} "a"`,
		`type T = { Text (string) }
type Alias ~ (T)
rule main (Alias) = n:([0-9]+) => (T{Text: (text($n))})`,
		`rule main = #operators { skip " "*; operand [0-9]+; operand "(" main ")"; level { infix left "+" / "-" } }`,
		`rule main = "a" @error(nil, "bad")`,
		`rule main = "a" @error("a", message="b")`,
		`rule main = "a" @atomic`,
		`rule main = "a" #repeat(3, 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			g, err := parseTest(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			for _, eager := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				got, gotErr := compile.NormalizeAttributesControlled(g, eager, &compile.Control{Ctx: ctx})
				want, wantErr := compile.NormalizeAttributesWithCopies(g, eager)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Fatalf("context changed normalized result: %v / %v", gotErr, wantErr)
				}
				after, err := grammar.MarshalJSON(g)
				if err != nil || string(after) != string(before) {
					t.Fatalf("normalization changed source: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gotErr := CompileContext(ctx, g, compile.Options{})
			want, wantErr := Compile(g, compile.Options{})
			if !reflect.DeepEqual(gotErr, wantErr) {
				t.Fatalf("compile diagnostics differ: %v / %v", gotErr, wantErr)
			}
			if gotErr == nil && gentest.ResultJSON(got.Parse("main", "a")) != gentest.ResultJSON(want.Parse("main", "a")) {
				t.Fatal("context changed compiled behavior")
			}
		})
	}
}

