package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// The published typed values of a Document are shared with later results, so every test here
// generates a typed Document package and checks the sharing contract: results are read-only and
// unchanged by later edits and parses, aliases follow the memo, and CloneAST detaches a copy.

// Tail is a second list of the same type as Items. A parse allocates its first list of a type
// with make, and later lists from a slab, whose slices must still have no spare capacity.
const sharingSeqGrammar = `type Item = { Text string }
type Pair = { Items []Item; Head Item; Tail []Item }
rule main Pair = h:item ";" xs:chain => Pair{Items: concat($xs, list($h)), Head: $h, Tail: $xs}
rule chain []Item = (c:chain ";" i:item => concat($c, list($i))) / (i:item => list($i))
rule item Item = t:[a-z]+ #atomic => Item{Text: text($t)}`

const sharingListGrammar = `type Item = { Text string }
rule main []Item = xs:chain => $xs
rule chain []Item = (c:chain ";" i:item => concat($c, list($i))) / (i:item => list($i))
rule item Item = t:[a-z]+ #atomic => Item{Text: text($t)}`

// sharingAnyGrammar has a node[_] field, which publishes a node of a statically unknown value.
const sharingAnyGrammar = `type Holder = { Xs node[_] }
@start
rule main Holder = xs:(x:[a-z] ";")+ => Holder{Xs: $xs}`

// sharingRecoverGrammar recovers from bad items, so a published list and a field hold error values
// that share the memo's error and diagnostic slices, and a left-recursive list is rebuilt on edits.
const sharingRecoverGrammar = `type Token = { Text string }
type Box = { Token Token | error }
type Pair = { Boxes []Box; Head Box }
rule main Pair = h:item xs:chain => Pair{Boxes: concat(list($h), $xs), Head: $h}
rule chain []Box = (c:chain i:item => concat($c, list($i))) / (i:item => list($i))
rule item Box = t:(token #recover(skip=[^;]+ ";")) => Box{Token: $t}
rule token Token = t:("a;" @error(message="want token")) #atomic => Token{Text: text($t)}`

const sharingOperatorsGrammar = `type Atom = { Text string }
type Sum = { L Expr; R Expr }
type Expr ~ Atom | Sum
rule main Expr = #operators {
 operand atom
 operand "(" x:main ")" => $x
 level add { infix left "+" => Sum{L: $lhs, R: $rhs} }
}
rule atom Atom = t:([a-z]) #atomic => Atom{Text: text($t)}`

// sharingPackages generates the typed Document packages used by the sharing tests into dir, with
// the given test files. The tests run in the generated module.
func sharingPackages(t *testing.T, tests map[string]string) {
	t.Helper()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("sharedtypeddocument"))
	for _, pkg := range []struct {
		name, grammar string
		legacy        bool
	}{
		{"seq", sharingSeqGrammar, false},
		{"list", sharingListGrammar, false},
		{"slab", sharingListGrammar, true},
		{"ops", sharingOperatorsGrammar, false},
		{"anyf", sharingAnyGrammar, false},
		{"rec", sharingRecoverGrammar, false},
	} {
		g, err := parseTest(pkg.grammar)
		if err != nil {
			t.Fatal(err)
		}
		gentest.GeneratePackageIn(t, dir, "sharedtypeddocument", pkg.name, g, gen.GenOptions{Start: "main", Types: true, Document: true, DisableTypedOutputBootstrap: pkg.legacy})
	}
	for name, text := range tests {
		write(name, text)
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shared typed Document: %v\n%s", err, out)
	}
	if testing.Verbose() {
		t.Logf("generated module output:\n%s", out)
	}
}

// sharingSeqTest checks the struct-valued result: shared immutability, prefix aliasing, the memo
// alias of Head inside one result, cap of published slices, interruption, and random edits.
const sharingSeqTest = `package seq

import (
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	. "sharedtypeddocument/seq/types"
)

// build returns the text of the letters joined with ";".
func build(letters []string) string { return strings.Join(letters, ";") }

// edit applies one random valid edit to the Document and to letters, which keeps at least two items.
func edit(t *testing.T, d *Document, letters *[]string, rng *rand.Rand) {
	t.Helper()
	n := len(*letters)
	l := string(rune('a' + rng.Intn(26)))
	switch op := rng.Intn(3); {
	case op == 0 || n <= 2 && op == 2:
		k := rng.Intn(n + 1)
		if k == 0 {
			if err := d.Edit(0, 0, l+";"); err != nil {
				t.Fatal(err)
			}
		} else if err := d.Edit(2*k-1, 2*k-1, ";"+l); err != nil {
			t.Fatal(err)
		}
		*letters = append((*letters)[:k], append([]string{l}, (*letters)[k:]...)...)
	case op == 1:
		i := rng.Intn(n)
		if err := d.Edit(2*i, 2*i+1, l); err != nil {
			t.Fatal(err)
		}
		(*letters)[i] = l
	default:
		i := rng.Intn(n)
		if i == 0 {
			if err := d.Edit(0, 2, ""); err != nil {
				t.Fatal(err)
			}
		} else if err := d.Edit(2*i-1, 2*i+1, ""); err != nil {
			t.Fatal(err)
		}
		*letters = append((*letters)[:i], (*letters)[i+1:]...)
	}
}

// checkSlices fails when a published slice has spare capacity, which a later append could write.
func checkSlices(t *testing.T, v reflect.Value, name string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			checkSlices(t, v.Elem(), name)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			checkSlices(t, v.Field(i), name)
		}
	case reflect.Slice:
		if v.Cap() != v.Len() {
			t.Fatalf("%s: published slice has spare capacity: len %d cap %d", name, v.Len(), v.Cap())
		}
		for i := 0; i < v.Len(); i++ {
			checkSlices(t, v.Index(i), name)
		}
	}
}

func TestSharedResultsAreImmutableAndFresh(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	letters := []string{"a", "b", "c"}
	d, err := NewDocument(build(letters))
	if err != nil {
		t.Fatal(err)
	}
	type kept struct{ result, snapshot *Pair }
	var all []kept
	for i := 0; i < 60; i++ {
		got, err := d.ParseAST()
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseAST(build(letters))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: edited result differs from a fresh parse of %q", i, build(letters))
		}
		if got.Head.Text != letters[0] || got.Items[len(got.Items)-1] != got.Head {
			t.Fatalf("step %d: head and last item are not one value", i)
		}
		checkSlices(t, reflect.ValueOf(got), "result")
		all = append(all, kept{got, CloneAST(got)})
		edit(t, d, &letters, rng)
	}
	// Every result stays as it was published, whatever edits and parses came after it.
	for i, k := range all {
		if !reflect.DeepEqual(k.result, k.snapshot) {
			t.Fatalf("result %d changed after later edits", i)
		}
	}
}

func TestSharedPrefixValuesAreTheSameValues(t *testing.T) {
	d, err := NewDocument("a;b;c;d")
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(6, 7, "z"); err != nil { // replace the last item
		t.Fatal(err)
	}
	after, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	// Items are the chain b, c, d (then the head a). The unchanged prefix b, c keeps its values.
	if after.Items[0] != before.Items[0] || after.Items[1] != before.Items[1] {
		t.Fatal("unchanged prefix values were not shared between results")
	}
	if after.Items[2] == before.Items[2] || after.Items[2].Text != "z" {
		t.Fatal("edited value was not rebuilt")
	}
	if before.Items[2].Text != "d" {
		t.Fatal("earlier result changed")
	}
	// CloneAST detaches the result and keeps its inner aliases.
	c := CloneAST(after)
	if c.Head == after.Head || c.Items[len(c.Items)-1] != c.Head || c.Items[0] == after.Items[0] {
		t.Fatal("CloneAST shares values with the result or lost its alias")
	}
}

// TestSharedResultMutationIsVisibleLater pins the contract: a caller that mutates a result changes
// the values that later results share, as with Node.
func TestSharedResultMutationIsVisibleLater(t *testing.T) {
	d, err := NewDocument("a;b;c")
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(4, 5, "d"); err != nil {
		t.Fatal(err)
	}
	first.Items[0].Text = "caller"
	second, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if second.Items[0].Text != "caller" {
		t.Fatal("a shared value is not shared; the contract says mutation is visible to later results")
	}
}

// TestSharedLeftRecursionSnapshotSurvivesEdit: chain is left-recursive and grows its list through
// one iteration per item, each one a seed for the next. A result taken before an edit must still
// equal its snapshot after the edit is parsed, so no iteration writes into a seed that was published.
func TestSharedLeftRecursionSnapshotSurvivesEdit(t *testing.T) {
	d, err := NewDocument("a;b;c;d;e")
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CloneAST(before)
	if err := d.Edit(8, 9, "z"); err != nil {
		t.Fatal(err)
	}
	after, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	// Literal expectations, not only a comparison with a fresh parse: a bug in the growth would
	// corrupt both parses the same way.
	if got := pairTexts(before); got != "b;c;d;e;a/a" {
		t.Fatalf("result before the edit: %q", got)
	}
	if got := pairTexts(after); got != "b;c;d;z;a/a" {
		t.Fatalf("edited result: %q", got)
	}
	// Literal expectations, not only a comparison with a fresh parse: a bug in the growth would
	// corrupt both parses the same way.
	if want, _ := ParseAST("a;b;c;d;z"); !reflect.DeepEqual(after, want) {
		t.Fatal("edited result differs from a fresh parse")
	}
	if !reflect.DeepEqual(before, snapshot) {
		t.Fatal("parsing after the edit changed the result taken before it")
	}
}


// pairTexts returns the texts of the items of p, then its head, as "items/head".
func pairTexts(p *Pair) string {
	var texts []string
	for _, it := range p.Items {
		texts = append(texts, it.Text)
	}
	return strings.Join(texts, ";") + "/" + p.Head.Text
}

func TestSharedInterruptionDuringPublication(t *testing.T) {
	d, err := NewDocument("a;b;c;d;e;f;g;h")
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CloneAST(before)
	if err := d.Edit(14, 15, "z"); err != nil {
		t.Fatal(err)
	}
	interrupted := 0
	for limit := uint64(1); ; limit++ {
		got, err := d.ParseAST(WithLimits(Limits{Budget: limit}))
		if err == nil {
			if got == nil {
				t.Fatal("successful parse returned nil")
			}
			break
		}
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("limit %d: %v", limit, err)
		}
		interrupted++
		if got != nil {
			t.Fatalf("limit %d: interrupted parse returned a value", limit)
		}
		if !reflect.DeepEqual(before, snapshot) {
			t.Fatalf("limit %d: an interrupted parse changed an earlier result", limit)
		}
	}
	if interrupted == 0 {
		t.Fatal("no step limit interrupted the parse")
	}
	got, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := ParseAST("a;b;c;d;e;f;g;z"); !reflect.DeepEqual(got, want) {
		t.Fatal("parse after interruptions differs from a fresh parse")
	}
}
`

const sharingListTest = `package list

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	. "sharedtypeddocument/list/types"
)

// edit applies one random valid edit to the Document and to letters, which keeps at least two items.
func edit(t *testing.T, d *Document, letters *[]string, rng *rand.Rand) {
	t.Helper()
	n := len(*letters)
	l := string(rune('a' + rng.Intn(26)))
	switch op := rng.Intn(3); {
	case op == 0 || n <= 2 && op == 2:
		k := rng.Intn(n + 1)
		if k == 0 {
			if err := d.Edit(0, 0, l+";"); err != nil {
				t.Fatal(err)
			}
		} else if err := d.Edit(2*k-1, 2*k-1, ";"+l); err != nil {
			t.Fatal(err)
		}
		*letters = append((*letters)[:k], append([]string{l}, (*letters)[k:]...)...)
	case op == 1:
		i := rng.Intn(n)
		if err := d.Edit(2*i, 2*i+1, l); err != nil {
			t.Fatal(err)
		}
		(*letters)[i] = l
	default:
		i := rng.Intn(n)
		if i == 0 {
			if err := d.Edit(0, 2, ""); err != nil {
				t.Fatal(err)
			}
		} else if err := d.Edit(2*i-1, 2*i+1, ""); err != nil {
			t.Fatal(err)
		}
		*letters = append((*letters)[:i], (*letters)[i+1:]...)
	}
}

// checkSlices fails when a published slice has spare capacity, which a later append could write.
func checkSlices(t *testing.T, v reflect.Value, name string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			checkSlices(t, v.Elem(), name)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			checkSlices(t, v.Field(i), name)
		}
	case reflect.Slice:
		if v.Cap() != v.Len() {
			t.Fatalf("%s: published slice has spare capacity: len %d cap %d", name, v.Len(), v.Cap())
		}
		for i := 0; i < v.Len(); i++ {
			checkSlices(t, v.Index(i), name)
		}
	}
}

// The outer slice of a top-level list result is fresh: appending to one result must not write
// into another, because its capacity equals its length.
func TestSharedOuterSliceIsFresh(t *testing.T) {
	a, err := ParseAST("a;b;c")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseAST("a;b;c")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 || cap(a) != 3 || cap(b) != 3 {
		t.Fatalf("outer slice len %d cap %d, other len %d cap %d", len(a), cap(a), len(b), cap(b))
	}
	a = append(a, &Item{Text: "x"})
	if len(b) != 3 || b[2].Text != "c" {
		t.Fatal("append to one result changed another result")
	}
}

// listTexts returns the texts of the items of a list result, joined with ";".
func listTexts(items []*Item) string {
	var texts []string
	for _, it := range items {
		texts = append(texts, it.Text)
	}
	return strings.Join(texts, ";")
}

// TestSharedLeftRecursionSnapshotSurvivesEdit: chain is left-recursive and grows its list through
// one iteration per item, each one a seed for the next. A result taken before an edit must still
// equal its snapshot after the edit is parsed, so no iteration writes into a seed that was published.
func TestSharedLeftRecursionSnapshotSurvivesEdit(t *testing.T) {
	d, err := NewDocument("a;b;c;d;e")
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CloneAST(before)
	if err := d.Edit(8, 9, "z"); err != nil {
		t.Fatal(err)
	}
	after, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if got := listTexts(before); got != "a;b;c;d;e" {
		t.Fatalf("result before the edit: %q", got)
	}
	if got := listTexts(after); got != "a;b;c;d;z" {
		t.Fatalf("edited result: %q", got)
	}
	if want, _ := ParseAST("a;b;c;d;z"); !reflect.DeepEqual(after, want) {
		t.Fatal("edited result differs from a fresh parse")
	}

	if !reflect.DeepEqual(before, snapshot) {
		t.Fatal("parsing after the edit changed the result taken before it")
	}
}

func TestSharedListEditsKeepEarlierResults(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	letters := []string{"a", "b", "c"}
	d, err := NewDocument(strings.Join(letters, ";"))
	if err != nil {
		t.Fatal(err)
	}
	var kept, snapshots [][]*Item
	for i := 0; i < 60; i++ {
		got, err := d.ParseAST()
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseAST(strings.Join(letters, ";"))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: edited result differs from a fresh parse", i)
		}
		checkSlices(t, reflect.ValueOf(got), "list result")
		kept = append(kept, got)
		snapshots = append(snapshots, CloneAST(got))
		edit(t, d, &letters, rng)
	}
	for i := range kept {
		if !reflect.DeepEqual(kept[i], snapshots[i]) {
			t.Fatalf("result %d changed after later edits", i)
		}
	}
}
`

const sharingOpsTest = `package ops

import (
	"math/rand"
	"reflect"
	"testing"
	. "sharedtypeddocument/ops/types"
)

// The operators rule builds Sum nodes from fresh values. Replacing one letter at a time must keep
// every earlier result as it was published.
func TestSharedOperatorsKeepEarlierResults(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	text := []byte("a+(b+c)+d")
	d, err := NewDocument(string(text))
	if err != nil {
		t.Fatal(err)
	}
	var kept []Expr
	var snapshots []Expr
	for i := 0; i < 80; i++ {
		got, err := d.ParseAST()
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseAST(string(text))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step %d: edited result differs from a fresh parse of %q", i, text)
		}
		kept = append(kept, got)
		snapshots = append(snapshots, CloneAST(got))
		var letters []int
		for j, c := range text {
			if c >= 'a' && c <= 'z' {
				letters = append(letters, j)
			}
		}
		j := letters[rng.Intn(len(letters))]
		c := byte('a' + rng.Intn(26))
		if err := d.Edit(j, j+1, string(c)); err != nil {
			t.Fatal(err)
		}
		text[j] = c
	}
	for i := range kept {
		if !reflect.DeepEqual(kept[i], snapshots[i]) {
			t.Fatalf("result %d changed after later edits", i)
		}
	}
}
`

// TestSharedTypedDocumentContract runs the shared tests of the three generated packages. Each test verifies the
// contract: results are read-only, unchanged by later edits, and the outer slices are fresh.
func TestSharedTypedDocumentContract(t *testing.T) {
	t.Parallel()
	sharingPackages(t, map[string]string{
		"seq/shared_test.go":  sharingSeqTest,
		"list/shared_test.go": sharingListTest,
		"ops/shared_test.go":  sharingOpsTest,
		"slab/shared_test.go": sharingSlabTest,
		"anyf/shared_test.go": sharingAnyTest,
		"rec/shared_test.go":  sharingRecoverTest,
	})
}

// sharingSlabTest uses the list grammar generated without the first-allocation bootstrap, so that
// the top-level list is carved from a slab like the later lists of a parse.
const sharingSlabTest = `package slab

import (
	"testing"

	. "sharedtypeddocument/slab/types"
)

func TestSharedSlabOuterSliceHasNoSpareCapacity(t *testing.T) {
	got, err := ParseAST("a;b;c;d")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || cap(got) != 4 {
		t.Fatalf("outer slice len %d cap %d, want 4 4", len(got), cap(got))
	}
	// Appending to the result must not write into another result of the same parse.
	other, err := ParseAST("e;f;g;h")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, &Item{Text: "x"})
	if other[3].Text != "h" || len(got) != 5 {
		t.Fatal("append to a slab-carved result wrote into another result")
	}
}
`

// sharingAnyTest checks a node[_] field holding a list: the published value must not alias the
// parser's mutable state, so a parse after an edit leaves the earlier result as it was.
const sharingAnyTest = `package anyf

import (
	"reflect"
	"testing"
)

func TestSharedAnyFieldIsDetachedFromParserState(t *testing.T) {
	d, err := NewDocument("a;b;")
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CloneAST(first)
	if err := d.Edit(2, 3, "z"); err != nil {
		t.Fatal(err)
	}
	second, err := d.ParseAST()
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := ParseAST("a;z;"); !reflect.DeepEqual(second, want) {
		t.Fatal("edited result differs from a fresh parse")
	}
	if !reflect.DeepEqual(first, snapshot) {
		t.Fatal("a parse after an edit changed an earlier node[_] value: it aliases parser state")
	}
}
`

// sharingRecoverTest edits a document of valid and recovered items at random, and after every step
// checks the fresh parse, the new result and every earlier result against the snapshot taken when
// it was published.
const sharingRecoverTest = `package rec

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	. "sharedtypeddocument/rec/types"
)

func recText(pieces []string) string { return strings.Join(pieces, "") }

// countErrors counts the generated error values reachable from v.
func countErrors(v reflect.Value) int {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return 0
		}
		if v.Kind() == reflect.Pointer && v.Type().Elem().Name() == "Error" {
			return 1
		}
		return countErrors(v.Elem())
	case reflect.Struct:
		n := 0
		for i := 0; i < v.NumField(); i++ {
			n += countErrors(v.Field(i))
		}
		return n
	case reflect.Slice:
		n := 0
		for i := 0; i < v.Len(); i++ {
			n += countErrors(v.Index(i))
		}
		return n
	}
	return 0
}

func TestSharedRecoverErrorValuesKeepEarlierResults(t *testing.T) {
	pieces := []string{"a;", "b;", "a;x;", "cc;"}
	rng := rand.New(rand.NewSource(37))
	d, err := NewDocument(recText(pieces))
	if err != nil {
		t.Fatal(err)
	}
	var kept, snaps []*Pair
	errorValues := 0
	for step := 0; step < 300; step++ {
		// Recovered items are reported as the error, next to the value; both must match a fresh parse.
		got, gerr := d.ParseAST()
		want, werr := ParseAST(recText(pieces))
		if !reflect.DeepEqual(got, want) || fmt.Sprint(gerr) != fmt.Sprint(werr) {
			t.Fatalf("step %d: result of %q differs from a fresh parse: %v vs %v", step, recText(pieces), gerr, werr)
		}
		errorValues += countErrors(reflect.ValueOf(got))
		kept = append(kept, got)
		snaps = append(snaps, CloneAST(got))
		for k := range kept {
			if !reflect.DeepEqual(CloneAST(kept[k]), snaps[k]) {
				t.Fatalf("step %d: result %d changed after a later parse", step, k)
			}
		}
		// Replace a range of whole items with one or two random items, keeping at least one item.
		i := rng.Intn(len(pieces))
		j := i + rng.Intn(len(pieces)-i+1)
		repl := make([]string, 1+rng.Intn(2))
		for k := range repl {
			repl[k] = []string{"a;", "b;", "a;x;", "cc;"}[rng.Intn(4)]
		}
		start := len(recText(pieces[:i]))
		end := start + len(recText(pieces[i:j]))
		if err := d.Edit(start, end, recText(repl)); err != nil {
			t.Fatal(err)
		}
		pieces = append(append(append([]string{}, pieces[:i]...), repl...), pieces[j:]...)
		if len(pieces) > 12 {
			// Keep the document short, so that the snapshots stay cheap to compare.
			cur := recText(pieces)
			pieces = pieces[len(pieces)-8:]
			if err := d.Edit(0, len(cur), recText(pieces)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if errorValues == 0 {
		t.Fatal("no error values were published; the recovery path was not exercised")
	}
}
`
