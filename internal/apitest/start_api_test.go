package apitest_test

import (
	"testing"

	"github.com/ornew/zeroparse"
)

// startAPISource has the default start rule main, which matches "aa", and the
// start rule item, which matches only "a".
const startAPISource = "@start(default)\nrule main = item item\n@start\nrule item = \"a\"\nrule other = \"b\"\n"

// An empty start rule name selects the grammar's default start rule on every
// surface of the API, also on a parser that starts at another rule.
func TestEmptyStartSelectsDefault(t *testing.T) {
	p, err := zp.CompileSource(startAPISource, "item")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse("aa"); err == nil {
		t.Fatal("negative control: the item parser accepts aa")
	}
	q, err := p.WithStart("")
	if err != nil {
		t.Fatal(err)
	}
	if q.Start() != "main" {
		t.Errorf("WithStart(\"\").Start() = %q, want main", q.Start())
	}
	if _, err := q.Parse("aa"); err != nil {
		t.Errorf("WithStart(\"\").Parse: %v", err)
	}
	r, err := p.ParsePrefix("", "aab")
	if err != nil || r.ConsumedBytes != 2 {
		t.Errorf("ParsePrefix(\"\"): consumed %d, %v", r.ConsumedBytes, err)
	}
	if r, err := p.ParsePrefix("item", "aab"); err != nil || r.ConsumedBytes != 1 {
		t.Errorf("ParsePrefix(item): consumed %d, %v", r.ConsumedBytes, err)
	}
	d, err := zp.CompileSource(startAPISource, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Start() != "main" {
		t.Errorf("CompileSource(\"\").Start() = %q, want main", d.Start())
	}
	for _, name := range []string{"other", "nope"} {
		if _, err := p.WithStart(name); err == nil {
			t.Errorf("WithStart(%q) succeeds", name)
		}
		if _, err := p.ParsePrefix(name, "a"); err == nil {
			t.Errorf("ParsePrefix(%q) succeeds", name)
		}
	}
}
