package golang_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/golang"
)

// TestNesting checks the one known deviation in the language: go/parser accepts nesting up to
// 100,000 levels, and the generated parser stops at 100,000 rule calls, which are about 16,600
// levels of parentheses (twice that for array types and indexes, three times for blocks).
func TestNesting(t *testing.T) {
	deep := func(n int) string {
		return "package p; var x = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n)
	}
	for _, n := range []int{1, 100, 5000, 16000} {
		if d := compare("x.go", []byte(deep(n)), 0); d != "" {
			t.Errorf("%d levels: %s", n, d)
		}
	}
	// go/parser accepts 30,000 levels; the parser reports an error, and does not run out of stack.
	err := golang.Recognize(deep(30000), golang.Bytes)
	if err == nil {
		t.Error("30,000 levels: accepted; the documentation of the deviation is out of date")
	} else if !strings.Contains(err.Error(), "nesting too deep") {
		t.Errorf("30,000 levels: %v", err)
	}
	// Beyond go/parser's own limit, both reject.
	if d := compare("x.go", []byte(deep(100000)), 0); d != "" {
		t.Errorf("100,000 levels: %s", d)
	}
}
