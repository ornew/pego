package engine

import (
	"strings"
	"testing"
)

// dropUnreadCase is a source that compiles before and after dropUnread; keep lists what the
// result must contain and gone what it must not.
type dropUnreadCase struct {
	name, src  string
	keep, gone []string
}

func TestDropUnreadSoundness(t *testing.T) {
	cases := []dropUnreadCase{{
		// A read in the initial value of a var statement.
		name: "read in var initializer",
		src: `package p

func f(in []int) int {
	var x int
	x = len(in)
	var y = x + 1
	return y
}
`,
		keep: []string{"x = len(in)", "var y = x + 1"},
	}, {
		// A variable that only a for statement assigns and increments stays.
		name: "for post statement",
		src: `package p

func f(n int) int {
	var x int
	for x = 0; n > 0; x++ {
		n--
	}
	return n
}
`,
		keep: []string{"x++", "var x int"},
	}, {
		// A silencer of a variable that is not dropped stays.
		name: "silencer of a live variable",
		src: `package p

func f(n int) int {
	var x int
	x = n
	k := n + 1
	_, _ = x, k
	return n
}
`,
		keep: []string{"k := n + 1", "_ = k"},
		gone: []string{"x = n", "var x int"},
	}, {
		// A variable that only a statement in an if assigns is silenced when its reader is cut.
		name: "variable assigned in an if statement",
		src: `package p

func f(n int) int {
	var a, x int
	if a = n; n > 0 {
		n--
	}
	x = a
	_ = x
	return n
}
`,
		keep: []string{"if a = n; n > 0", "_ = a"},
		gone: []string{"x = a", "var a, x", "_ = x"},
	}, {
		// A local of the name of a named result hides it; the function is left alone.
		name: "named result",
		src: `package p

func f(n int) (x int) {
	if n > 0 {
		var x int
		x = n
		_ = x
	}
	x = 7
	return
}
`,
		keep: []string{"x = 7", "x = n"},
	}, {
		// A local of the name of a package-level variable hides it; the function is left alone.
		name: "package variable",
		src: `package p

var count int

func f(n int) int {
	if n > 0 {
		var count int
		count = n
		_ = count
	}
	count = n
	return n
}

func Count() int { return count }
`,
		keep: []string{"count = n\n\treturn n", "var count int\n\t\tcount = n"},
	}, {
		// A local of the name of a package-level function that is only written stays, silenced.
		name: "package function name written only",
		src: `package p

func v1() int { return 1 }

func f(n int) int {
	var v1 int
	v1 = n
	_ = v1
	return v1x(n)
}

func v1x(n int) int { return n + v1() }
`,
		keep: []string{"var v1 int\n\tv1 = n\n\t_ = v1"},
	}, {
		// A variable written by an if statement and by a plain assignment, whose reader is cut,
		// is silenced.
		name: "variable written by an if and a plain assignment",
		src: `package p

func f(n int) int {
	var a, x int
	if a = n; n > 0 {
		n--
	}
	a = 2
	x = a
	_ = x
	return n
}
`,
		keep: []string{"if a = n; n > 0", "_ = a"},
		gone: []string{"x = a", "_ = x"},
	}, {
		// A variable written opposite an untyped nil cannot take the blank identifier, so it stays
		// and is silenced.
		name: "untyped nil opposite a dead variable",
		src: `package p

func g() error { return nil }

func f() error {
	var x, y error
	x, y = nil, g()
	_ = x
	return y
}
`,
		keep: []string{"x, y = nil, g()", "_ = x"},
	}, {
		// Comparing interfaces and shifting can panic, so the operation stays.
		name: "assignments that can panic",
		src: `package p

func f(a, b any, n int) int {
	var x bool
	var y int
	x = a == b
	y = 1 << n
	_, _ = x, y
	return 0
}
`,
		keep: []string{"_ = a == b", "_ = 1 << n"},
		gone: []string{"var x", "var y"},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typeCheck(t, c.src)
			got, err := dropUnread([]byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			typeCheck(t, string(got))
			for _, want := range c.keep {
				if !strings.Contains(string(got), want) {
					t.Errorf("source lost %q:\n%s", want, got)
				}
			}
			for _, bad := range c.gone {
				if strings.Contains(string(got), bad) {
					t.Errorf("source keeps %q:\n%s", bad, got)
				}
			}
		})
	}
}
