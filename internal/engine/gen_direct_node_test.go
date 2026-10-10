package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var nodeDirectCases = []genCase{
	{"node/direct_memo_action", `
type Pair struct { Left Match, Right Match }
def main = pair "!" $$ / pair "?" $$
def pair: Pair = left:@(?a-z) "-" right:number -> new Pair{Left: $left, Right: $right}
def number = @(?0-9)+
`, []string{"a-12!", "a-12?", "a-x?", "a-12", "é-12?", ""}},
	{"node/direct_nested_frames", `
type Item struct { Name Match, Value Match }
type Doc struct { Items []Item }
def main: Doc = items:item{1,3} $$ -> new Doc{Items: $items}
def item: Item = name:@(?a-z)+ ":" value:@(?0-9)+ ";" -> new Item{Name: $name, Value: $value}
`, []string{"a:1;b:22;", "a:1;", "a:1;b:x;", "a:1;b:2;c:3;d:4;", ""}},
	{"node/direct_expectations", `
def main = item ";" $$
def item = ("let" " " name #error(message="expected a name")) / ("let" " " "_" name)
def name = @(?a-z)+
`, []string{"let abc;", "let ;", "let _abc;", "let 1;", "let abc", ""}},
}

func TestGeneratedNodeDirectFrameRoutes(t *testing.T) {
	for _, src := range []string{
		`type N struct { Items []Match }
def main: N = items:(x:@(?a-z) ",")+ $$ -> new N{Items: map($items, (e) => $e.x)}`,
		`def main = n:@(?0-9) [size = len($n)] [size == 1] "é" $$ -> $n`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := Generate(g, GenOptions{Package: "node", Start: "main"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(code), "// main (Node body inlined)") {
			t.Fatal("frame-dependent main rule must use an inlined Node body")
		}
	}
}

// Returned trees must outlive the pooled parser's frames and construction
// stacks, including when a later parse fails or uses external rule entry.
func TestGeneratedNodeDirectRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	c := nodeDirectCases[0]
	g, err := syntax.Parse(c.src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "node", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	p := compile(t, c.src)
	var rows strings.Builder
	for _, unit := range []Unit{CodePoints, Bytes} {
		for _, rule := range []string{"main", "pair"} {
			for _, input := range []string{"a-123456?", "b-7!", "c-1", "d-x?", ""} {
				n, err := p.ParseWith(rule, input, ParseOptions{Unit: unit})
				fmt.Fprintf(&rows, "{%q,%q,%s,%q},\n", rule, input,
					map[Unit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], resultJSON(n, err))
			}
		}
	}
	test := `package node
import("encoding/json";"testing")
func encoded(n *Node, err error) string {
 out:=map[string]any{"node":n};if err!=nil{out["err"]=err.Error()}
 b,_:=json.Marshal(out);return string(b)
}
func TestRetainedTrees(t *testing.T) {
 cases:=[]struct{rule,input string;unit Unit;want string}{` + rows.String() + `}
 var saved []*Node;var snapshots []string
 for repeat:=0;repeat<3;repeat++ {for _,c:=range cases {
  n,err:=ParseRule(c.rule,c.input,c.unit)
  if got:=encoded(n,err);got!=c.want {t.Fatalf("%s %q: got %s want %s",c.rule,c.input,got,c.want)}
  if err==nil {saved=append(saved,n);snapshots=append(snapshots,encoded(n,nil))}
  for i,n:=range saved {if encoded(n,nil)!=snapshots[i] {t.Fatalf("retained tree %d changed",i)}}
 }}
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": code, "node_test.go": []byte(test), "go.mod": []byte("module node\n\ngo 1.27.1\n")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Require the optimized Node route so parity tests cannot silently test only
// the old expression-method emitter. A memoized action rule must still enter
// through call and leave finalization to the ordinary runtime wrapper.
func TestGeneratedNodeDirectRoutes(t *testing.T) {
	for _, c := range nodeDirectCases {
		t.Run(c.name, func(t *testing.T) {
			g, err := syntax.Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			code, err := Generate(g, GenOptions{Package: "node", Start: "main", Recognize: true})
			if err != nil {
				t.Fatal(err)
			}
			name := "item"
			if strings.Contains(c.name, "memo_action") {
				name = "pair"
			}
			body := regexp.MustCompile(`// ` + name + ` \(Node body inlined\)\s+func \(p \*parser\) (\w+)\(`).FindSubmatch(code)
			if len(body) != 2 {
				t.Fatalf("no direct Node body for %s", name)
			}
			if !strings.Contains(string(code), "return p."+string(body[1])+"() }") {
				t.Fatalf("%s is not used by the rule-table body", body[1])
			}
			if name == "pair" {
				metadata := regexp.MustCompile(`\{id: (\d+), name: "pair",[^\n]+memo: true,`).FindSubmatch(code)
				if len(metadata) != 2 {
					t.Fatal("pair must retain memoization")
				}
				if !strings.Contains(string(code), "p.call(rules["+string(metadata[1])+"],") {
					t.Fatal("pair must use the memo-aware call route")
				}
			}
		})
	}
}
