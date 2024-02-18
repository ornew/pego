package token

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileSet(t *testing.T) {
	fs := NewFileSet()
	a := "0\n12\n345\n6789"
	t.Log("a")
	{
		f := fs.AddFile(-1, "a", a)
		t.Logf("file [%d,%d)", f.base, f.end)
		for i := f.Base(); i < f.End(); i++ {
			p := fs.Position(Pos(i))
			rel := i.From(f.Base())
			c := a[rel]
			t.Logf("pos %d (rel %d): %s: %q\n", i, rel, p, c)
		}
		require.Panics(t, func() {
			t.Log(f.Position(0))
		})
		require.Panics(t, func() {
			t.Log(f.Position(15))
		})
	}
	t.Log("b")
	{
		f := fs.AddFile(-1, "b", a)
		t.Logf("file [%d,%d)", f.base, f.end)
		for i := f.Base(); i < f.End(); i++ {
			p := fs.Position(Pos(i))
			rel := i.From(f.Base())
			c := a[rel]
			t.Logf("pos %d (rel %d): %s: %q\n", i, rel, p, c)
		}
		require.Panics(t, func() {
			t.Log(f.Position(14))
		})
		require.Panics(t, func() {
			t.Log(f.Position(29))
		})
	}
	// for i := 0; i < int(f.last); i++ {
	// 	p := f.Position(Pos(i))
	// 	t.Logf("pos %d: %s\n", i, p)
	// }
}
