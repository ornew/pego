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
