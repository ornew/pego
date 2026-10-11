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
		if err := golang.Recognize(src, golang.WithUnit(golang.Bytes)); err == nil {
			t.Errorf("%q: Recognize(Bytes) accepted invalid UTF-8", src)
		}
		if golang.Valid(src) {
			t.Errorf("%q: Valid", src)
		}
	}
	// U+FFFD itself is valid.
	for _, src := range []string{
		"package p // \xef\xbf\xbd",
		"package p; var _ = \"\xef\xbf\xbd\"",
		"package p; var _ = `\xef\xbf\xbd`",
		"package p; var _ = '\xef\xbf\xbd'",
	} {
		if err := golang.Recognize(src, golang.WithUnit(golang.Bytes)); err != nil {
			t.Errorf("%q: Recognize(Bytes): %v", src, err)
		}
		if err := golang.Recognize(src); err != nil {
			t.Errorf("%q: Recognize: %v", src, err)
		}
	}
	// The deviation: code points cannot see invalid UTF-8 in a comment.
	if err := golang.Recognize("package p // \xff"); err != nil {
		t.Logf("Recognize with code points now rejects invalid UTF-8 (update the documentation): %v", err)
	}
}
