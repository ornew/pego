package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// An edit can complete an incomplete UTF-8 sequence that ends before it. The
// invalid bytes were separate characters, so results that examined them must
// not be reused.
func TestGeneratedDocumentCompletesUTF8Prefix(t *testing.T) {
	eachMode(t, testGeneratedDocumentCompletesUTF8Prefix)
}

func testGeneratedDocumentCompletesUTF8Prefix(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	const nodeGrammar = "rule main = (euro / other)* $$\nrule euro = [€😀]\nrule other = .\n"
	const typedGrammar = `type Char = { Text string }
type Result = { Wide []Char; Other []Char }
rule main Result = w:wide* o:other* $$ => Result{Wide: $w.Value, Other: $o.Value}
rule wide Char = t:([€😀]) #atomic => Char{Text: text($t)}
rule other Char = t:(.) #atomic => Char{Text: text($t)}
`
	const cases = `
type documentEdit struct{ start, end int; text string }
// Each case starts from text and applies edits in the selected unit; the
// positions are the same in both units because every byte before an edit is
// an ASCII character or an invalid byte.
var utf8Cases = []struct{ text string; edits []documentEdit }{
 {"\xe2", []documentEdit{{1, 1, "\x82\xac"}}},
 {"\xe2\x82", []documentEdit{{2, 2, "\xac"}}},
 {"a\xf0\x9f\x98x", []documentEdit{{4, 5, "\x80"}}},
 {"\xe2x", []documentEdit{{1, 2, "\x82"}, {2, 2, "\xac"}}},
}
`
	for _, tc := range []struct {
		name, grammar, parse, test string
		opts                       gen.GenOptions
	}{
		{"node", nodeGrammar, "Parse", `
func TestCompletedPrefix(t *testing.T) {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  for _, c := range utf8Cases {
   d, err := NewDocument(c.text, WithUnit(unit))
   if err != nil { t.Fatal(err) }
   if _, err := d.Parse(); err != nil { t.Fatal(err) }
   for _, e := range c.edits {
    if err := d.Edit(e.start, e.end, e.text); err != nil { t.Fatal(err) }
    got, err := d.Parse()
    want, wantErr := Parse(d.Text(), WithUnit(unit))
    g, _ := json.Marshal(got)
    w, _ := json.Marshal(want)
    if string(g) != string(w) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
     t.Fatalf("unit %d, text %q: document %s / %v; batch %s / %v", unit, d.Text(), g, err, w, wantErr)
    }
   }
  }
  // Results before the incomplete sequence and the position after them are
  // still reused.
  d, err := NewDocument("ab\xe2", WithUnit(unit))
  if err != nil { t.Fatal(err) }
  if _, err := d.Parse(); err != nil { t.Fatal(err) }
  if err := d.Edit(3, 3, "\x82\xac"); err != nil { t.Fatal(err) }
  if _, err := d.Parse(); err != nil || d.Stats().Reused == 0 { t.Fatalf("unit %d: %v, stats %+v", unit, err, d.Stats()) }
 }
}
`, gen.GenOptions{Package: "fixture", Start: "main", Document: true}},
		{"typed", typedGrammar, "ParseAST", `
func TestCompletedPrefix(t *testing.T) {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  for _, c := range utf8Cases {
   d, err := NewDocument(c.text, WithUnit(unit))
   if err != nil { t.Fatal(err) }
   if _, err := d.ParseAST(); err != nil { t.Fatal(err) }
   for _, e := range c.edits {
    if err := d.Edit(e.start, e.end, e.text); err != nil { t.Fatal(err) }
    got, err := d.ParseAST()
    want, wantErr := ParseAST(d.Text(), WithUnit(unit))
    if !reflect.DeepEqual(got, want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
     t.Fatalf("unit %d, text %q: document %+v / %v; batch %+v / %v", unit, d.Text(), got, err, want, wantErr)
    }
   }
  }
 }
}
`, gen.GenOptions{Package: "fixture", Start: "main", Types: true, Document: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseTest(tc.grammar)
			if err != nil {
				t.Fatal(err)
			}
			code, err := generateCode(g, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			imports := `import (
 "encoding/json"
 "fmt"
 "testing"
)
var _ = json.Marshal
`
			if tc.name == "typed" {
				imports = `import (
 "fmt"
 "reflect"
 "testing"
)
`
			}
			dir := t.TempDir()
			for name, source := range map[string]string{
				"go.mod":         testGoMod("fixture"),
				"parser.go":      string(code),
				"parser_test.go": "package fixture\n" + imports + cases + tc.test,
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "./...")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
