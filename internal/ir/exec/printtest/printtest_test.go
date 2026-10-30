package printtest

import (
	"strings"
	"testing"
)

// TestWant runs the functions in the interpreter and checks a few results, so that the expected
// output itself is checked against Go's arithmetic.
func TestWant(t *testing.T) {
	got := Want(Build())
	for _, want := range []string{
		"wrap -2147483648 4294967295 18446744073709551614 -2147483648 2147483647 -2147483648 4294967295 -7 -3 -1 -2147483648 195\n",
		"constants -2147483648 4294967295 18446744073709551614 -2147483648 4294967295 -1 false\n",
		"note 1\nnote 3\nnote 6\nnote 10\nnote 11\nexits 6\n",
		"note 121\nscopes 121\n",
		"note 121\nclauses 6 5 8\n",
		"hash 3282035111 -2147483648\n",
		"calls 610\n",
		"flag false\nflag true\nshortcircuit false true\n",
		"handles 42 true true\n",
		"special_x 1\n",
		"note 11\nspecial_x_extra 11\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks\n%s\nin\n%s", want, got)
		}
	}
}
