package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGenerateDocumentNode(t *testing.T) {
	g, err := syntax.Parse("rule main = item+ $$\nrule item = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Document: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), "func newDocumentState(") {
		t.Fatal("Document runner missing from requested source")
	}
	if !strings.Contains(string(code), "const documentEnabled = true") {
		t.Fatal("requested generated source does not enable Document branches")
	}
	if !strings.Contains(string(code), "func NewDocument(") || !strings.Contains(string(code), "func (n *Node) Clone()") {
		t.Fatal("Document public API missing from requested source")
	}
	ordinary, err := Generate(g, GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ordinary), "func newDocumentState(") {
		t.Fatal("ordinary generated source includes the Document runner")
	}
	if !strings.Contains(string(ordinary), "const documentEnabled = false") {
		t.Fatal("ordinary generated source does not eliminate Document branches")
	}
	if strings.Contains(string(ordinary), "func NewDocument(") || strings.Contains(string(ordinary), "func (n *Node) Clone()") {
		t.Fatal("ordinary generated source includes the Document API")
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":          "module fixture\n\ngo 1.27.1\n",
		"parser.go":       string(code),
		"batch/parser.go": string(ordinary),
		"parser_test.go": `package fixture
import (
  "context"
  "encoding/json"
  "fmt"
  "testing"
)
func TestGeneratedDocumentAgainstBatch(t *testing.T) {
  for _, unit := range []Unit{CodePoints, Bytes} {
   count := 0
   d, err := NewDocument("aa", func(o *parseOptions) { count++; o.unit = unit })
   if err != nil || count != 1 { t.Fatalf("new document: %v, option count %d", err, count) }
   for _, change := range []struct{ start, end int; text string }{{2, 2, "a"}, {1, 2, "b"}, {1, 2, "a"}} {
    node, err := d.Parse()
    fresh, freshErr := Parse(d.Text(), WithUnit(unit))
    got, _ := json.Marshal(node)
    want, _ := json.Marshal(fresh)
    if string(got) != string(want) || fmt.Sprint(err) != fmt.Sprint(freshErr) {
      t.Fatalf("text %q: document %s / %v; batch %s / %v", d.Text(), got, err, want, freshErr)
    }
    if err := d.Edit(change.start, change.end, change.text); err != nil { t.Fatal(err) }
   }
   if count != 1 { t.Fatalf("constructor option re-evaluated: %d", count) }
  }
}
func TestGeneratedDocumentContract(t *testing.T) {
  d, err := NewDocument("aa")
  if err != nil { t.Fatal(err) }
  first, err := d.Parse()
  if err != nil { t.Fatal(err) }
  clone := first.Clone()
  if clone == first || clone.Children[0] == first.Children[0] { t.Fatal("clone retained document nodes") }
  if err := d.Edit(2, 2, "a"); err != nil { t.Fatal(err) }
  second, err := d.Parse()
  if err != nil { t.Fatal(err) }
  if d.Stats().Reused == 0 || d.Stats().Evaluated == 0 { t.Fatalf("no rule accounting: %+v", d.Stats()) }
  if len(clone.Children[0].Children) != 2 || len(second.Children[0].Children) != 3 { t.Fatal("clone changed after edit") }
  before := d.Text()
  if err := d.Edit(-1, 0, "x"); err == nil || d.Text() != before { t.Fatal("invalid edit changed document") }
  if _, err := d.Parse(WithUnit(Bytes)); err == nil { t.Fatal("unit change accepted") }
  if _, err := d.Parse(WithMaxDepth(-1)); err == nil { t.Fatal("negative depth accepted") }
  canceled, cancel := context.WithCancel(context.Background())
  cancel()
  if _, err := NewDocument("aa", WithContext(canceled)); err == nil { t.Fatal("canceled construction accepted") }
  if _, err := d.Parse(WithContext(canceled)); err == nil { t.Fatal("canceled parse accepted") }
  if _, err := d.Parse(); err != nil { t.Fatalf("retry after cancellation: %v", err) }
}
func TestGeneratedDocumentByteBoundary(t *testing.T) {
  d, err := NewDocument("é", WithUnit(Bytes))
  if err != nil { t.Fatal(err) }
  if err := d.Edit(1, 1, "x"); err == nil || d.Text() != "é" { t.Fatal("UTF-8 interior edit accepted") }
  if err := d.Edit(2, 2, ""); err != nil { t.Fatal(err) }
  if d.Text() != "é" { t.Fatal("no-op edit changed text") }
}
func TestGeneratedDocumentCloneSharing(t *testing.T) {
  n := &Node{Text: "a"}
  parent := &Node{Children: []*Node{n,n}, Fields: Fields{{Name: "value", Value: []any{n}}}}
  copy := parent.Clone()
  if copy.Children[0] != copy.Children[1] || copy.Children[0] == n { t.Fatal("shared children not cloned") }
  value := copy.Fields[0].Value.([]any)[0].(*Node)
  if value != copy.Children[0] { t.Fatal("field alias not preserved") }
}
func TestGeneratedDocumentCloneCyclicAndSharedSlices(t *testing.T) {
  child := &Node{Text: "original"}
  const backingCapacity = 1 << 16
  values := make([]any, 2, backingCapacity)
  values[0], values[1] = values, child
  nodes := make([]*Node, 1, backingCapacity)
  nodes[0] = child
  emptyValues := make([]any, 0, backingCapacity)
  emptyNodes := make([]*Node, 0, backingCapacity)
  root := &Node{Children: nodes, Fields: Fields{
    {Name: "first", Value: values}, {Name: "second", Value: values},
    {Name: "nodes1", Value: nodes}, {Name: "nodes2", Value: nodes},
    {Name: "emptyValues", Value: emptyValues}, {Name: "emptyNodes", Value: emptyNodes},
  }}
  copy := root.Clone()
  first := copy.Fields[0].Value.([]any)
  second := copy.Fields[1].Value.([]any)
  if &first[0] == &values[0] || &first[0] != &second[0] { t.Fatal("shared slice was not copied once") }
  cycle := first[0].([]any)
  if &cycle[0] != &first[0] { t.Fatal("slice cycle was not preserved") }
  if first[1].(*Node) != copy.Children[0] || copy.Children[0] == child { t.Fatal("node identity was not preserved") }
  nodes1 := copy.Fields[2].Value.([]*Node)
  nodes2 := copy.Fields[3].Value.([]*Node)
  if cap(first) != len(first) || cap(nodes1) != len(nodes1) ||
    cap(copy.Fields[4].Value.([]any)) != 0 || cap(copy.Fields[5].Value.([]*Node)) != 0 {
    t.Fatal("clone retained oversized source capacity")
  }
  if &nodes1[0] == &nodes[0] || &nodes1[0] != &nodes2[0] || &nodes1[0] != &copy.Children[0] || nodes1[0] != copy.Children[0] {
    t.Fatal("shared node slice was not copied once")
  }
  values[1] = nil
  nodes[0] = nil
  if first[1] != copy.Children[0] || nodes1[0] != copy.Children[0] { t.Fatal("clone retained source slice storage") }
}
`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Document build/test: %v\n%s", err, out)
	}
}

func TestGenerateDocumentCombinations(t *testing.T) {
	g, err := syntax.Parse("rule main = \"a\"")
	if err != nil {
		t.Fatal(err)
	}
	typed, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Document: true, Types: true})
	if err != nil || !strings.Contains(string(typed), "func (d *Document) ParseAST(") {
		t.Fatalf("typed Document generation: %v", err)
	}
	if _, err := GenerateTS(g, GenOptions{Start: "main", Document: true}); err == nil {
		t.Fatal("TypeScript Document generation was accepted")
	}
}
