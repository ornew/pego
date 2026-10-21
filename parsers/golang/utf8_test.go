package golang_test

import (
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// TestInvalidUTF8 checks that the grammar itself rejects invalid UTF-8 with the Bytes position
// unit, where an invalid byte is read as U+FFFD but its text is the byte. With code points (the
// default), the generated functions cannot tell an invalid byte from U+FFFD: that is a deviation,
// which ParseFile and Valid avoid by checking the encoding first.
func TestInvalidUTF8(t *testing.T) {
	for _, src := range []string{
		"package p // \xff",
		"package p /* \xff */",
		"package p\n//line a:1\xff\n",
		"package p; var _ = \"\xff\"",
		"package p; var _ = `\xff`",
		"package p; var _ = '\xff'",
		"package p; var x\xff = 1",
		"package p; var _ = \"\xed\xa0\x80\"", // a surrogate half
	} {
		if _, err := golang.ParseAST(src, golang.WithUnit(golang.Bytes)); err == nil {
			t.Errorf("%q: ParseAST(Bytes) accepted invalid UTF-8", src)
		}
		if golang.Valid(src) {
			t.Errorf("%q: Valid", src)
		}
		// The interpreter running the grammar gives the same result and the same error position.
		agreeWithInterpreter(t, src)
	}
	// U+FFFD itself is valid.
	for _, src := range []string{
		"package p // \xef\xbf\xbd",
		"package p; var _ = \"\xef\xbf\xbd\"",
		"package p; var _ = `\xef\xbf\xbd`",
		"package p; var _ = '\xef\xbf\xbd'",
	} {
		if _, err := golang.ParseAST(src, golang.WithUnit(golang.Bytes)); err != nil {
			t.Errorf("%q: ParseAST(Bytes): %v", src, err)
		}
		if _, err := golang.ParseAST(src); err != nil {
			t.Errorf("%q: ParseAST: %v", src, err)
		}
		agreeWithInterpreter(t, src)
	}
	// The deviation: code points cannot see invalid UTF-8 in a comment.
	if _, err := golang.ParseAST("package p // \xff"); err != nil {
		t.Logf("ParseAST with code points now rejects invalid UTF-8 (update the documentation): %v", err)
	}
}
