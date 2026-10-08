package golang_test

import (
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// FuzzParseFile compares the package with go/parser on arbitrary input (go test -fuzz
// FuzzParseFile): both must accept the same files and build the same trees.
func FuzzParseFile(f *testing.F) {
	for _, s := range []string{
		"package p\n",
		"package p; func f() { if f(T{}) {} }",
		"package p; type T[P *C] int",
		"package p; type T[P *C,] int",
		"package p; func f() { switch x := y.(type) { case int: } }",
		"package p; var _ = <-chan <-chan int(nil)",
		"package p; func f() { go f()(); defer (f)() }",
		"package p; func f() { L: }",
		"package p; func f() { for i := 0\n i < n; i++ {} }",
		"package p; const c = 0x1p-2 + 1_000i + 'a' + \"\\u00e9\" + `raw`",
		"//line x.go:10\npackage p",
		"package p; type I interface { ~int | T[int]; m() }",
		"package p; type S struct { T; *p.U; a, b [N]int `tag` }",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if d := compare("x.go", []byte(src), 0); d != "" {
			t.Fatalf("%q: %s", src, d)
		}
		if d := compare("x.go", []byte(src), golang.ParseComments); d != "" {
			t.Fatalf("%q (ParseComments): %s", src, d)
		}
	})
}
