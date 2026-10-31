package gentest

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compiletest"
	"github.com/ornew/zeroparse/internal/gen"
)

// AnonJoinGrammar is the grammar whose anonymous joins the generated types name.
const AnonJoinGrammar = `type Leaf = { Text string }
type Pair = { L Leaf; R Leaf }
type Box = { Else *(Leaf | Pair); Items [](Pair | Leaf); Same *(Leaf | Pair) }
rule main Box = e:alt? xs:item* => Box{Else: $e, Items: $xs.Value, Same: $e}
rule alt = leaf / pair
rule item = pair / leaf
rule leaf Leaf = t:[a-z]+ ";" => Leaf{Text: text($t)}
rule pair Pair = "(" l:leaf r:leaf ")" => Pair{L: $l, R: $r}`

// GoTypesOf generates the Go types of src as package p and returns the text of the generated file.
func GoTypesOf(t *testing.T, src string) (string, error) {
	t.Helper()
	g, err := compiletest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(t, g, gen.GenOptions{Start: "main", Types: true, Module: "example.com/m", Package: "p"})
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

// AnonErrorJoinGrammar is the grammar of the anonymous joins that have an error member.
const AnonErrorJoinGrammar = `type Block = { Text string }
type R = { Else *(Block | error); Rest []Block; Last *(Block | error) }
rule main R = e:alt? xs:alt* => R{Else: $e, Rest: $xs.Value, Last: $e}
rule alt Block = t:[a-z]+ ";" #recover(skip=[^;]+ ";") => Block{Text: text($t)}`
