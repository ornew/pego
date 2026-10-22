package engine

import (
	"strings"
	"testing"
)

const pruneFixture = `package p

var tables = []int{1}
var used = []int{2}
var lonely = []int{3}

func Parse() { _ = trules[1]; a1() }
func a1()    { _ = used; b1() }
func b1()    {}
func u2()    { _ = lonely }
func u3()    {}
func s4()    {}

func init() {
	trules[1].Entry = a1
	trules[2].Entry = u2
	trules[3].Entry = u3
	trules[3].Body = s4
}
`

func TestPruneTypedEntries(t *testing.T) {
	got, err := pruneTypedEntries([]byte(pruneFixture))
	if err != nil {
		t.Fatal(err)
	}
	src := string(got)
	for _, want := range []string{"func Parse()", "func a1()", "func b1()", "var used", "trules[1].Entry = a1", "trules[3].Body = s4", "func s4()"} {
		if !strings.Contains(src, want) {
			t.Errorf("pruned source lost %q:\n%s", want, src)
		}
	}
	for _, gone := range []string{"u2", "u3", "lonely", "tables", "trules[2].Entry", "trules[3].Entry"} {
		if strings.Contains(src, gone) {
			t.Errorf("pruned source kept %q:\n%s", gone, src)
		}
	}
	// The pruned source is stable.
	again, err := pruneTypedEntries(got)
	if err != nil || string(again) != src {
		t.Errorf("pruning is not idempotent: %v", err)
	}
}

func TestPruneTypedEntriesKeepsAllWhenTableIsRead(t *testing.T) {
	const src = `package p

func Parse() { for _, r := range trules { _ = r }; _ = trules[1] }
func u2()    {}
func init()  { trules[2].Entry = u2 }
`
	got, err := pruneTypedEntries([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "trules[2].Entry = u2") || !strings.Contains(string(got), "func u2()") {
		t.Errorf("a read of the whole table must keep every entry:\n%s", got)
	}
}

func prune(t *testing.T, src string) string {
	t.Helper()
	got, err := pruneTypedEntries([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestPruneTypedEntriesKeepsAllForVariableIndex(t *testing.T) {
	got := prune(t, `package p

func Parse(i int) { _ = trules[i] }
func u2()         {}
func init()       { trules[2].Entry = u2 }
`)
	if !strings.Contains(got, "trules[2].Entry = u2") || !strings.Contains(got, "func u2()") {
		t.Errorf("a variable index must keep every entry:\n%s", got)
	}
}

func TestPruneTypedEntriesIgnoresStringsAndComments(t *testing.T) {
	got := prune(t, "package p\n\n"+
		"func Parse() { _ = trules[1]; _ = \"u2 lonely\"; _ = `u2` } // u2 lonely\n\n"+
		"var lonely = 1\nfunc u2() {}\nfunc init() { trules[2].Entry = u2 }\n")
	for _, gone := range []string{"var lonely", "func u2()", "trules[2].Entry"} {
		if strings.Contains(got, gone) {
			t.Errorf("a name in a string or a comment kept %q:\n%s", gone, got)
		}
	}
}

func TestPruneTypedEntriesKeepsWhatEveryKindOfReferenceNames(t *testing.T) {
	got := prune(t, `package p

type T struct{}

func (T) M()                { viaMethod() }
func viaMethod()            {}
func viaLiteral()           {}
func viaGeneric[X any](x X) { viaGenericCallee() }
func viaGenericCallee()     {}
func viaVar() int           { return 1 }
func unused()               {}

var v = viaVar()

func Parse() { _ = trules[1]; viaGeneric(1); _ = v }

func init() {
	trules[1].Entry = func(p int) { viaLiteral() }
	trules[2].Entry = unused
}
`)
	for _, want := range []string{"func viaMethod()", "func viaLiteral()", "func viaGenericCallee()", "func viaVar()", "var v ", "func viaGeneric["} {
		if !strings.Contains(got, want) {
			t.Errorf("pruned source lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "unused") {
		t.Errorf("pruned source kept an unreferenced function:\n%s", got)
	}
}

// TestPruneTypedEntriesIgnoresLocalNames checks that a local variable or parameter that shares
// the name of a package-level function or variable does not keep it.
func TestPruneTypedEntriesIgnoresLocalNames(t *testing.T) {
	got := prune(t, `package p

func shadowed() {}
func param()    {}
func outer()    {}

var global = 1

func Parse() {
	_ = trules[1]
	var shadowed int
	shadowed++
	global := shadowed
	_ = global
	_ = func(param int) int { return param }
	outer()
}

func init() { trules[2].Entry = shadowed }
`)
	if !strings.Contains(got, "func outer()") {
		t.Errorf("pruned source lost a called function:\n%s", got)
	}
	for _, gone := range []string{"func shadowed()", "func param()", "var global"} {
		if strings.Contains(got, gone) {
			t.Errorf("a local name kept %q:\n%s", gone, got)
		}
	}
}

func TestPruneTypedEntriesRemovesDocComments(t *testing.T) {
	got := prune(t, `package p

func Parse() { _ = trules[1] }

// u2 is only entered by its entry.
// A second line.
func u2() {}

// lonely holds a table.
var lonely = 1

func init() { trules[2].Entry = u2 }
`)
	for _, gone := range []string{"u2", "lonely", "second line", "holds a table"} {
		if strings.Contains(got, gone) {
			t.Errorf("a doc comment or declaration stayed behind (%q):\n%s", gone, got)
		}
	}
}
