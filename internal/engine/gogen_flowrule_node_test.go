package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedNodeDirectFrameRoutes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNodeDirectFrameRoutes)
}

func testGeneratedNodeDirectFrameRoutes(t *testing.T) {
	for _, src := range []string{
		`type N = { Items []node[string] }
rule main N = items:(x:[a-z] #atomic ",")+ $$ => N{Items: map($items, (e) => e.$x)}`,
		`var size int
rule main = n:[0-9] #atomic {size := len($n)} {size == 1} "é" $$ => $n`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "node", Start: "main"})
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
	t.Parallel()
	gentest.EachMode(t, testGeneratedNodeDirectRetention)
}

func testGeneratedNodeDirectRetention(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	c := gentest.NodeDirectCases[0]
	g, err := parseTest(c.Src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "node", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	p := compileBare(t, c.Src) // generated parsers keep no source information by default
	var rows strings.Builder
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, rule := range []string{"main", "pair"} {
			for _, input := range []string{"a-123456?", "b-7!", "c-1", "d-x?", ""} {
				n, err := p.ParseWith(rule, input, ParseOptions{Unit: unit})
				fmt.Fprintf(&rows, "{%q,%q,%s,%q},\n", rule, input,
					map[PositionUnit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], resultJSON(n, err))
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
 cases:=[]struct{rule,input string;unit PositionUnit;want string}{` + rows.String() + `}
 var saved []*Node;var snapshots []string
 for repeat:=0;repeat<3;repeat++ {for _,c:=range cases {
  n,err:=ParseRule(c.rule,c.input,WithUnit(c.unit))
  if got:=encoded(n,err);got!=c.want {t.Fatalf("%s %q: got %s want %s",c.rule,c.input,got,c.want)}
  if err==nil {saved=append(saved,n);snapshots=append(snapshots,encoded(n,nil))}
  for i,n:=range saved {if encoded(n,nil)!=snapshots[i] {t.Fatalf("retained tree %d changed",i)}}
 }}
}
`
	dir := t.TempDir()
	for name, data := range map[string][]byte{"parser.go": code, "node_test.go": []byte(test), "go.mod": []byte(gentest.GoMod("node"))} {
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
	t.Parallel()
	gentest.EachMode(t, testGeneratedNodeDirectRoutes)
}

func testGeneratedNodeDirectRoutes(t *testing.T) {
	for _, c := range gentest.NodeDirectCases {
		t.Run(c.Name, func(t *testing.T) {
			g, err := parseTest(c.Src)
			if err != nil {
				t.Fatal(err)
			}
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "node", Start: "main", Recognize: true})
			if err != nil {
				t.Fatal(err)
			}
			name := "item"
			if strings.Contains(c.Name, "memo_action") {
				name = "pair"
			}
			body := regexp.MustCompile(`// ` + name + ` \(Node body inlined\)\s+func (\w+)\(p \*rt\.Parser, _ int\)`).FindSubmatch(code)
			if len(body) != 2 {
				t.Fatalf("no direct Node body for %s", name)
			}
			if !strings.Contains(string(code), "].Body = "+string(body[1])+"\n") {
				t.Fatalf("%s is not used by the rule-table body", body[1])
			}
			if name == "pair" {
				// The rule table is the list of specs; the id of a rule is its position.
				specs := regexp.MustCompile(`(?s)var rulesSpec = \[\.\.\.\]ruleSpec\{\n(.*?)\n\}\n`).FindSubmatch(code)
				if len(specs) != 2 {
					t.Fatal("no rule table")
				}
				id, memo := -1, false
				for i, line := range strings.Split(string(specs[1]), "\n") {
					if strings.HasPrefix(line, `	{"pair",`) {
						id, memo = i, strings.Contains(line, "RuleMemo")
					}
				}
				if !memo {
					t.Fatal("pair must retain memoization")
				}
				if !strings.Contains(string(code), "p.Call(rules["+strconv.Itoa(id)+"],") {
					t.Fatal("pair must use the memo-aware call route")
				}
			}
		})
	}
}
