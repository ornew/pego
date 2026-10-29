package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// TestGeneratedDocumentInvalidUTF8EditEquivalence is the generated Document
// counterpart of TestDocumentInvalidUTF8EditEquivalence: random edits at
// decoded boundaries insert, join and split valid and invalid UTF-8
// sequences, and each Node Parse and typed ParseAST must match a batch parse
// of the current text, in both position units.
func TestGeneratedDocumentInvalidUTF8EditEquivalence(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedDocumentInvalidUTF8EditEquivalence)
}

func testGeneratedDocumentInvalidUTF8EditEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated Documents")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	grammars := []string{
		"rule main = item* $$\nrule item = .",
		"rule main = item* $$\nrule item = \"é\" / \"日\" / \"😀\" / \"�\"",
		"rule main = item* $$\nrule item = [é日😀�]",
		"rule main = x $$\nrule x = .",
		"rule main = (euro / other)* $$\nrule euro = [€😀]\nrule other = .",
		`type Char = { Text string }
type Result = { Wide []Char; Other []Char }
rule main Result = w:wide* o:other* $$ => Result{Wide: $w.Value, Other: $o.Value}
rule wide Char = t:([€😀é]) #atomic => Char{Text: text($t)}
rule other Char = t:(.) #atomic => Char{Text: text($t)}`,
	}
	files := map[string]string{"go.mod": testGoMod("fixture")}
	for i, src := range grammars {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		pkg := fmt.Sprintf("g%d", i)
		code, err := generateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Document: true})
		if err != nil {
			t.Fatal(err)
		}
		files[pkg+"/parser.go"] = string(code)
		files[pkg+"/parser_test.go"] = strings.Replace(generatedDocumentUTF8Consumer, "package fixture", "package "+pkg, 1)
	}
	dir := t.TempDir()
	for name, source := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const generatedDocumentUTF8Consumer = `package fixture

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"unicode/utf8"
)

// Invalid bytes each decode as a one-byte character.
func decodedBoundaries(s string) []int {
	positions := []int{0}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		positions = append(positions, i)
	}
	return positions
}

func TestInvalidUTF8EditEquivalence(t *testing.T) {
	pieces := []string{"", "a", "\n", "é", "日", "😀", "€", "\x80", "\xa9", "\xc3", "\xe6\xbc", "\xe2\x82", "\xac", "\xf0\x9f\x98", "\xff", "\xed\xa0\x80"}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		d, err := NewDocument("\x80\xc3", WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(15))
		for i := 0; i < 500; i++ {
			// Alternate the parse entry points so each reuses results the
			// other recorded.
			if i%2 == 0 {
				got, err := d.Parse()
				want, wantErr := Parse(d.Text(), WithUnit(unit))
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				if string(g) != string(w) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
					t.Fatalf("unit %d, edit %d, text %x: document %s / %v; batch %s / %v", unit, i, d.Text(), g, err, w, wantErr)
				}
			} else {
				got, err := d.ParseAST()
				want, wantErr := ParseAST(d.Text(), WithUnit(unit))
				if !reflect.DeepEqual(got, want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
					t.Fatalf("unit %d, edit %d, text %x: document %#v / %v; batch %#v / %v", unit, i, d.Text(), got, err, want, wantErr)
				}
			}
			bounds := decodedBoundaries(d.Text())
			if unit == CodePoints {
				for j := range bounds {
					bounds[j] = j
				}
			}
			start := rng.Intn(len(bounds))
			end := start + rng.Intn(len(bounds)-start)
			if err := d.Edit(bounds[start], bounds[end], pieces[rng.Intn(len(pieces))]); err != nil {
				t.Fatalf("unit %d, edit %d [%d,%d): %v", unit, i, bounds[start], bounds[end], err)
			}
		}
	}
}
`
