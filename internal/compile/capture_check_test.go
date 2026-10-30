package compile

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// Compare the capture merger with the join, including values whose top-level optional/union
// normalization must not be skipped.
func TestCaptureJoinEquivalence(t *testing.T) {
	t.Parallel()
	str := TyStringNode
	tuple := NodeTy{Value: TupleTy{Elems: []Ty{str, str}}}
	ts := []Ty{TyAny, TyBottom, TyNil, str, TyInt, TyString, NamedTy{Name: "A", Kind: 's'}, ListTy{Elem: str},
		OptTy{Elem: str}, OptTy{Elem: OptTy{Elem: str}}, JoinTy{Alts: []Ty{str, tuple}}, JoinTy{Alts: []Ty{tuple, str}},
		JoinTy{Alts: []Ty{str, str, TyBottom}}, JoinTy{Alts: []Ty{TyAny, str}}, ListTy{Elem: OptTy{Elem: str}},
		NodeTy{Value: ListTy{Elem: JoinTy{Alts: []Ty{str, tuple}}}}}
	for i, a := range ts {
		for j, b := range ts {
			got := mergeCap([]CapIndex{{Name: "x", Paths: [][]int{{0}}, T: a}}, CapIndex{Name: "x", Paths: [][]int{{1}}, T: b})
			if want := Vjoin(a, b); len(got) != 1 || got[0].T.String() != want.String() || len(got[0].Paths) != 2 {
				t.Fatalf("pair %d/%d: got %v, want %s", i, j, got, want)
			}
		}
	}
}

func TestDuplicateCaptureDiagnosticPositions(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 64, 1024} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			line := `rule main = ` + strings.Repeat(`"😀" x:item `, n) + `=> R{X: $x}`
			src := "type R = { X node[string] }\nrule item = k:\"a\" v:(y:\"b\")*\n" + line
			col := utf8.RuneCountInString(line[:strings.Index(line, "X:")]) + 1
			want := fmt.Sprintf("3:%d: cannot use node[(node[string], node[[]node[(node[string],), {\"y\": 0}]]), {\"k\": 0, \"v\": 1}] as node[string] in field X of R", col)
			if got := compileError(t, src); got != want {
				t.Errorf("got %q; want %q", got, want)
			}
		})
	}
}
