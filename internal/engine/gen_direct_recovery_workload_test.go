package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_RECOVERY_DIR retains generated modules for separately built,
// alternating benchmark binaries; ordinary tests use a temporary directory.
func TestGeneratedTypedRecoveryWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_RECOVERY_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	for _, c := range typedRecoveryWorkloads {
		t.Run(c.name, func(t *testing.T) {
			fixture := strings.NewReplacer("ACCEPTED", c.accepted, "RECOVERED", c.recovered,
				"ERRORS_PER_ITEM", c.errors).Replace(typedRecoveryWorkloadFixture)
			testGeneratedTypedBodyWorkload(t, typedRecoveryWorkloadTypes+c.src, fixture,
				filepath.Join(dir, c.name), "// Recovery inlined under the runtime-owned frame.",
				GenOptions{disableTypedRecoveryBodies: true})
		})
	}
}

var typedRecoveryWorkloads = []struct{ name, src, accepted, recovered, errors string }{
	{"ordinary", `
def expr: Expr = atom #recover(skip=(?^;)+)
def atom: Atom = "é" "b" "c"
`, "ébc;", "?;", "1"},
	{"pratt", `
def expr: Expr = pratt {
    operand atom #recover(skip="?")
    level { postfix ("[" atom "]") #recover(skip="[?]") -> new Post{X:$lhs} }
}
def atom: Atom = "é"
`, "é[é];", "?[?];", "2"},
	{"lr", `
def expr: Post = (previous:expr "+" x:(atom #recover(skip="?"))
               / x:(atom #recover(skip="?"))) -> new Post{X:$x}
def atom: Atom = "é"
`, "é+é;", "?+?;", "2"},
}

const typedRecoveryWorkloadTypes = `
type Atom terminal
type Post struct { X Expr }
type Expr = Atom | Post
type Doc struct { Items []Expr, N int }
def main: Doc = items:(e:expr -";")* $$ -> new Doc{Items:map($items,(x)=>$x.e), N:len($items)}
`

// Generation includes compilation, runtime embedding and formatting, but not
// parsing the source grammar or building/running the resulting Go code.
func BenchmarkTypedRecoveryGeneration(b *testing.B) {
	calculator, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		b.Fatal(err)
	}
	typescript, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		b.Fatal(err)
	}
	cases := []struct{ name, src string }{{"calculator", string(calculator)}, {"typescript", string(typescript)}}
	for _, c := range typedRecoveryWorkloads {
		cases = append(cases, struct{ name, src string }{c.name, typedRecoveryWorkloadTypes + c.src})
	}
	opts := GenOptions{Package: "recoveryfixture", Start: "main", Types: true, Recognize: true,
		disableTypedRecoveryBodies: os.Getenv("PEGO_REFERENCE_RECOVERY") == "1"}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			g, err := syntax.Parse(c.src)
			if err != nil {
				b.Fatal(err)
			}
			code, err := Generate(g, opts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := Generate(g, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(code)), "source-bytes")
		})
	}
}

const typedRecoveryWorkloadFixture = `package lrfixture

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"
)

func recoveryInput(mode string, n int) string {
    switch mode {
    case "accepted": return strings.Repeat("ACCEPTED", n)
    case "recovered": return strings.Repeat("RECOVERED", n)
    default: return strings.Repeat("ACCEPTED", n)+"?"
    }
}

func errorNodes(v Expr) int {
    switch v := v.(type) {
    case *Error:
        if v.Text != "?" || v.End <= v.Start || v.Message == "" { panic("invalid error node") }
        return 1
    case *Post: return errorNodes(v.X)
    default: return 0
    }
}

func checkRecovery(v *Doc, err error, mode string, n int) bool {
    if mode == "rejected" { return v == nil && err != nil }
    if v == nil || v.N != n || len(v.Items) != n { return false }
    if mode == "accepted" { return err == nil }
    es, ok := err.(SyntaxErrors)
    if !ok || len(es) != ERRORS_PER_ITEM*n { return false }
    for _, e := range es { if e.Pos < 0 || e.Error() == "" { return false } }
    for _, item := range v.Items { if errorNodes(item) != 1 { return false } }
    return true
}

func TestRecoveryWorkload(t *testing.T) {
    for _, unit := range []Unit{CodePoints, Bytes} {
        saved, savedErr := ParseAST(recoveryInput("recovered", 2), unit)
        if !checkRecovery(saved, savedErr, "recovered", 2) { t.Fatalf("saved value %v %v", saved, savedErr) }
        before, _ := json.Marshal(struct { Value any; Error any }{saved, savedErr})
        p := &tparser{parser:&parser{}}
        for _, n := range []int{1,128,1} {
            for _, mode := range []string{"accepted","recovered","rejected"} {
                input := recoveryInput(mode,n)
                value, err := p.run(trules[0],input,[]Unit{unit},&tslabs{})
                v := tAs[*Doc](value)
                if !checkRecovery(v,err,mode,n) { t.Fatalf("%s/%d: %v %v",mode,n,v,err) }
                data, _ := json.Marshal(struct { Value any; Error any }{v,err})
                pooled, pooledErr := ParseAST(input,unit)
                pooledData, _ := json.Marshal(struct { Value any; Error any }{pooled,pooledErr})
                if string(data) != string(pooledData) { t.Fatal("manual and pooled parsers differ") }
                fmt.Printf("OBS %v %s/%d %s\n",unit,mode,n,data)
                if p.depth != 0 || p.frame != nil || len(p.trail) != 0 { t.Fatal("live invocation state") }
                for _, u := range p.trail[:cap(p.trail)] {
                    if u.f != nil || u.old != nil || u.slot != 0 { t.Fatal("retained undo state") }
                }
                p.recycle()
                recycled, _ := json.Marshal(struct { Value any; Error any }{v,err})
                if string(data) != string(recycled) { t.Fatal("recycle changed returned values or diagnostics") }
            }
        }
        after, _ := json.Marshal(struct { Value any; Error any }{saved,savedErr})
        if string(before) != string(after) { t.Fatal("pooled reuse changed saved values or diagnostics") }
    }
}

var recoveryResult *Doc
var recoveryErr error

func BenchmarkTypedRecovery(b *testing.B) {
    for _, n := range []int{1,128} {
        for _, mode := range []string{"accepted","recovered","rejected"} {
            input := recoveryInput(mode,n)
            for _, unit := range []Unit{CodePoints,Bytes} {
                b.Run(fmt.Sprintf("%d/%s/%v",n,mode,unit),func(b *testing.B) {
                    v, err := ParseAST(input,unit)
                    if !checkRecovery(v,err,mode,n) { b.Fatalf("%v %v",v,err) }
                    b.ReportAllocs()
                    for b.Loop() { recoveryResult,recoveryErr = ParseAST(input,unit) }
                })
            }
        }
    }
}
`
