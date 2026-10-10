package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_CHUNK_DIR retains both generated paths for paired allocation
// measurements. Normal test runs remove the fixtures automatically.
func TestGeneratedTypedChunkOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`
type Token terminal
type Item struct { Token Token }
type Group struct { Items []Item }
type Root struct { Groups []Group, Missing *Group }
def main = gs:group* $$ -> new Root{Groups: $gs}
def group = "[" xs:item* "]" -> new Group{Items: $xs}
def item = x:token -> new Item{Token: $x}
def token: Token = "é"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, conv := range []bool{false, true} {
		t.Run(strconv.FormatBool(conv), func(t *testing.T) {
			code, err := Generate(g, GenOptions{Package: "chunks", Start: "main", Types: true, convertTypes: conv})
			if err != nil {
				t.Fatal(err)
			}
			dir := os.Getenv("PEGO_TYPED_CHUNK_DIR")
			if dir == "" {
				dir = t.TempDir()
			} else {
				dir = filepath.Join(dir, strconv.FormatBool(conv))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, data := range map[string][]byte{
				"parser.go": code, "chunks_test.go": []byte(typedChunksFixture), "go.mod": []byte("module chunks\n\ngo 1.27.1\n"),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "-run", "^TestChunk")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated chunk ownership: %v\n%s", err, out)
			}
		})
	}
}

const typedChunksFixture = `package chunks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestChunkValues(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		empty, err := ParseAST("", unit)
		if err != nil || empty == nil || empty.Groups == nil || len(empty.Groups) != 0 { t.Fatal("empty root list") }
		for _, count := range []int{0, 1, 7, 8, 9, 23, 24, 25, 55, 56, 57, 63, 64, 65, 119, 120, 121, 255, 256, 257, 511, 512, 513, 1025} {
			// Several lists exhaust a partial chunk; empty and exact-size large
			// lists exercise the other list-allocation paths in the same parse.
			input := strings.Repeat("["+strings.Repeat("é", count)+"]", 3)+"[]"
			v, err := ParseAST(input, unit)
			if err != nil { t.Fatal(err) }
			if v.Missing != nil || len(v.Groups) != 4 { t.Fatal("root fields") }
			seen := map[*Token]bool{}
			items := map[*Item]bool{}
			width := 1
			if unit == Bytes { width = 2 }
			if v.Start != 0 || v.End != 3*(count*width+2)+2 { t.Fatal("root span") }
			for j, group := range v.Groups {
				n := count
				if j == 3 { n = 0 }
				if group.Start != j*(count*width+2) || group.End != group.Start+n*width+2 { t.Fatal("group span") }
				if group.Items == nil || len(group.Items) != n || cap(group.Items) != n { t.Fatal("list extent") }
				for i, item := range group.Items {
					if item == nil || items[item] { t.Fatal("item pointer reused") }
					items[item] = true
					x := item.Token
					start := j*(count*width+2)+1+i*width
					if x == nil || seen[x] || x.Text != "é" || x.Start != start || x.End != start+width || item.Start != start || item.End != start+width { t.Fatalf("count%d group%d item%d: %+v", count, j, i, item) }
					seen[x] = true
				}
			}
			before, err := json.Marshal(v)
			if err != nil { t.Fatal(err) }
			// A caller's append must not overwrite the following returned list.
			original := v.Groups[0].Items
			_ = append(original, &Item{})
			if _, err := ParseAST("[", unit); err == nil { t.Fatal("invalid input accepted") }
			if _, err := ParseAST("[é]", unit); err != nil { t.Fatal(err) }
			after, err := json.Marshal(v)
			if err != nil || !bytes.Equal(before, after) { t.Fatal("retained result changed after append or later parse") }
		}
	}
}

func BenchmarkChunkParseAST(b *testing.B) {
	for _, count := range []int{1, 24, 257, 1025} {
		input := strings.Repeat("["+strings.Repeat("é", count)+"]", 3)+"[]"
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseAST(input); err != nil { b.Fatal(err) }
			}
		})
	}
}
`
