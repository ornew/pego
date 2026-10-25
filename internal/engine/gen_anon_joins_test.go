package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const anonJoinGrammar = `type Leaf = { Text string }
type Pair = { L Leaf; R Leaf }
type Box = { Else *(Leaf | Pair); Items [](Pair | Leaf); Same *(Leaf | Pair) }
rule main Box = e:alt? xs:item* => Box{Else: $e, Items: $xs.Value, Same: $e}
rule alt = leaf / pair
rule item = pair / leaf
rule leaf Leaf = t:[a-z]+ ";" => Leaf{Text: text($t)}
rule pair Pair = "(" l:leaf r:leaf ")" => Pair{L: $l, R: $r}`

func goTypesOf(t *testing.T, src string) (string, error) {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(g, GenOptions{Start: "main", Types: true, Module: "example.com/m", Package: "p"})
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.HasSuffix(f.Path, "types.go") {
			return strings.Join(strings.Fields(string(f.Content)), " "), nil
		}
	}
	t.Fatal("no types.go")
	return "", nil
}

// An anonymous join of declared node types is an interface named after its members; the same
// members in another order, in a field, a list element or the value of a rule, are one interface.
func TestGeneratedTypedAnonymousJoinInterface(t *testing.T) {
	types, err := goTypesOf(t, anonJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type LeafOrPair interface{ isLeafOrPair() }",
		"Else LeafOrPair",
		"Items []LeafOrPair",
		"Same LeafOrPair",
		"func (*Leaf) isLeafOrPair()",
		"func (*Pair) isLeafOrPair()",
		"func (*Error) isLeafOrPair()",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("types.go lacks %q:\n%s", want, types)
		}
	}
	if strings.Contains(types, "PairOrLeaf") {
		t.Error("the same members in another order have an interface of their own")
	}
	if n := strings.Count(types, "interface{ is"); n != 1 {
		t.Errorf("%d interfaces, want 1", n)
	}
}

// A join with the members of an alias is that alias, not an interface of its own, and a join with a
// member that is no declared node type keeps its Go type.
func TestGeneratedTypedAnonymousJoinReusesAlias(t *testing.T) {
	types, err := goTypesOf(t, `type A = { X string }
type B = { Y string }
type AB ~ A | B
type R = { F *(B | A); G *(A | node[string]) }
rule main R = a:a $$ => R{F: $a, G: $a}
rule a A = t:[a-z] => A{X: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(types, "F AB") {
		t.Errorf("F is not the alias AB:\n%s", types)
	}
	if strings.Contains(types, "BOrA") || strings.Contains(types, "AOrB") || strings.Contains(types, "AOrNode") {
		t.Errorf("an interface was made for a join that has one already or none:\n%s", types)
	}
	if !strings.Contains(types, "G any") {
		t.Errorf("G keeps its Go type:\n%s", types)
	}
}

// The interface name is a generation error if a type of the grammar has it.
func TestGeneratedTypedAnonymousJoinNameCollision(t *testing.T) {
	_, err := goTypesOf(t, `type A = { X string }
type B = { Y string }
type AOrB = { Z string }
type R = { F *(A | B) }
rule main R = a:a $$ => R{F: $a}
rule a A = t:[a-z] => A{X: text($t)}`)
	if err == nil || !strings.Contains(err.Error(), "AOrB") || !strings.Contains(err.Error(), "rename the type") {
		t.Fatalf("got %v, want an error naming AOrB", err)
	}
	// The error names a join of one declared node type as written, with error.
	_, err = goTypesOf(t, `type A = { X string }
type AOrError = { Z string }
type R = { F *(A | error) }
rule main R = a:a $$ => R{F: $a}
rule a A = t:[a-z] => A{X: text($t)}`)
	if err == nil || !strings.Contains(err.Error(), "the join A | error needs a generated interface named AOrError") {
		t.Fatalf("got %v, want an error naming the join A | error", err)
	}
}

// The TypeScript type of an anonymous join is the union of its members, in a field, a list
// element and the start value.
func TestGeneratedTypedAnonymousJoinTypeScript(t *testing.T) {
	g, err := parseTest(anonJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, GenOptions{Types: true, Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path != "types.ts" {
			continue
		}
		c := string(f.Content)
		for _, want := range []string{
			`readonly "Else": RecoveredError | null | Leaf | Pair;`,
			`readonly "Items": readonly (RecoveredError | null | Pair | Leaf)[] | null;`,
		} {
			if !strings.Contains(c, want) {
				t.Errorf("types.ts lacks %q:\n%s", want, c)
			}
		}
		if strings.Contains(c, `"Else": TypedValue`) {
			t.Error("Else is TypedValue")
		}
	}
}

// The typed parsers of anonymous joins compile and convert like the Node route.
func TestGeneratedTypedAnonymousJoinValues(t *testing.T) {
	eachMode(t, testGeneratedTypedAnonymousJoinValues)
}

func testGeneratedTypedAnonymousJoinValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := []genCase{{"anonymous joins", anonJoinGrammar, []string{"", "a;", "(a;b;)", "a;(a;b;)c;", "(a;"}}}
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true}, len(cases))
}

// The name of an interface follows the order of the members where the set first appears in the
// file, not the names of the types that hold it: renaming a type does not rename the interface.
func TestGeneratedTypedAnonymousJoinNameFollowsSourceOrder(t *testing.T) {
	grammar := func(first, second string) string {
		return `type A = { X string }
type B = { Y string }
type ` + first + ` = { F *(A | B) }
type ` + second + ` = { G *(B | A) }
rule main ` + first + ` = a:a $$ => ` + first + `{F: $a}
rule a A = t:[a-z] => A{X: text($t)}`
	}
	// Zed is declared before Alpha, and sorts after it.
	for _, names := range [][2]string{{"Zed", "Alpha"}, {"Alpha", "Zed"}, {"Mid", "Beta"}} {
		types, err := goTypesOf(t, grammar(names[0], names[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(types, "type AOrB interface") || strings.Contains(types, "BOrA") {
			t.Errorf("%v: the interface is not named after the first spelling A | B:\n%s", names, types)
		}
	}
}

const anonErrorJoinGrammar = `type Block = { Text string }
type R = { Else *(Block | error); Rest []Block; Last *(Block | error) }
rule main R = e:alt? xs:alt* => R{Else: $e, Rest: $xs.Value, Last: $e}
rule alt Block = t:[a-z]+ ";" #recover(skip=[^;]+ ";") => Block{Text: text($t)}`

// One declared node type and error, the value of a rule that recovers, is a join with an interface
// named after the type and Error, implemented by *Block and *Error, and a union in TypeScript.
func TestGeneratedTypedAnonymousJoinOneNodeAndError(t *testing.T) {
	types, err := goTypesOf(t, anonErrorJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type BlockOrError interface{ isBlockOrError() }",
		"Else BlockOrError",
		"Last BlockOrError",
		"Rest []*Block",
		"func (*Block) isBlockOrError()",
		"func (*Error) isBlockOrError()",
		"is the join type Block | error.",
	} {
		if !strings.Contains(types, want) {
			t.Errorf("types.go lacks %q:\n%s", want, types)
		}
	}
	if n := strings.Count(types, "interface{ is"); n != 1 {
		t.Errorf("%d interfaces, want 1", n)
	}
	// An alias of the same members is the type of the field.
	types, err = goTypesOf(t, `type Block = { Text string }
type S ~ Block | error
type R = { Else *(Block | error) }
rule main R = e:alt $$ => R{Else: $e}
rule alt Block = t:[a-z]+ ";" => Block{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(types, "Else S") || strings.Contains(types, "BlockOrError") {
		t.Errorf("the field is not the alias S:\n%s", types)
	}
	g, err := parseTest(anonErrorJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateTS(g, GenOptions{Types: true, Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "types.ts" && !strings.Contains(string(f.Content), `readonly "Else": RecoveredError | null | Block;`) {
			t.Errorf("types.ts lacks the union for Else:\n%s", f.Content)
		}
	}
}

// Two different sets of members that get the same interface name are a generation error.
func TestGeneratedTypedAnonymousJoinSameNameDifferentSets(t *testing.T) {
	_, err := goTypesOf(t, `type A = { X string }
type B = { Y string }
type C = { Z string }
type AOrB = { W string }
type R = { F *(A | B | C); G *(AOrB | C) }
rule main R = a:a $$ => R{F: $a}
rule a A = t:[a-z] => A{X: text($t)}`)
	if err == nil || !strings.Contains(err.Error(), "AOrBOrC") {
		t.Fatalf("got %v, want an error naming AOrBOrC", err)
	}
}

// The compiled parsers of joins of one node and error convert like the Node route.
func TestGeneratedTypedAnonymousJoinOneNodeAndErrorValues(t *testing.T) {
	eachMode(t, testGeneratedTypedAnonymousJoinOneNodeAndErrorValues)
}

func testGeneratedTypedAnonymousJoinOneNodeAndErrorValues(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := []genCase{{"one node and error", anonErrorJoinGrammar, []string{"", "a;", "a;b;", "1;a;", "a;2;b;"}}}
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true}, len(cases))
}
