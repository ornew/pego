package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_LOCAL_LAYOUT_DIR preserves generated controls for benchmarks of
// prebuilt binaries, independently of source generation and compilation.
func TestGeneratedTypedLocalLayoutWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_LOCAL_LAYOUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	for _, c := range typedLocalLayoutWorkloads {
		t.Run(c.name, func(t *testing.T) {
			testGeneratedTypedBodyWorkload(t, typedLocalLayoutWorkloadTypes+c.src,
				typedLocalLayoutWorkloadFixture, filepath.Join(dir, c.name),
				"// body, invoked as by invoke (body inlined)\nfunc (p *tparser)",
				GenOptions{disableTypedLocalLayouts: true})
		})
	}
}

const typedLocalLayoutWorkloadTypes = `
def main: Doc = [n=0] e:body $$ -> $e
`

var typedLocalLayoutWorkloads = []struct{ name, src string }{
	{"ordinary", `
type Doc struct { Items []Match, N int, Missing *Match }
def body: Doc = ((_|_ ignored:@"z") / items:(x:@"é" -",")+)
    -> new Doc{Items:map($items,(e)=>$e.x), N:len($items), Missing:$ignored}
`},
	{"element", `
type Doc struct { Items []*Match, N int, Missing *Match }
def body: Doc = ((_|_ ignored:@"z") / items:(((_|_ unused:@"z") / x:@"é") [$unused==nil && len($x)>0] -",")+)
    -> new Doc{Items:map($items,(e)=>$e.x), N:len($items), Missing:$ignored}
`},
	{"cuts", `
type Doc struct { Items []Match, N int, Missing *Match }
def body: Doc = ((_|_ ignored:@"z") / items:(x:@"é" -- [n=n+1] -",")+)
    -> new Doc{Items:map($items,(e)=>$e.x), N:n, Missing:$ignored}
`},
}

func BenchmarkTypedLocalLayoutGeneration(b *testing.B) {
	calculator, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		b.Fatal(err)
	}
	typescript, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		b.Fatal(err)
	}
	cases := []struct{ name, src string }{{"calculator", string(calculator)}, {"typescript", string(typescript)}}
	for _, c := range typedLocalLayoutWorkloads {
		cases = append(cases, struct{ name, src string }{c.name, typedLocalLayoutWorkloadTypes + c.src})
	}
	opts := GenOptions{Package: "localfixture", Start: "main", Types: true, Recognize: true,
		disableTypedLocalLayouts: os.Getenv("PEGO_REFERENCE_LOCAL_LAYOUTS") == "1"}
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

const typedLocalLayoutWorkloadFixture = `package lrfixture

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"
)

func localInput(n int, rejected bool) string {
    input := strings.Repeat("é,", n)
    if rejected { input += "é?" }
    return input
}

func checkLocal(v *Doc, err error, n int, rejected bool, unit Unit) bool {
    if rejected { return v == nil && err != nil }
    if err != nil || v == nil || v.N != n || len(v.Items) != n || v.Missing != nil { return false }
    width := 1
    if unit == Bytes { width = 2 }
    for i, item := range v.Items {
        if item.Text != "é" || item.Start != i*(width+1) || item.End != item.Start+width { return false }
    }
    return true
}

func TestLocalLayoutWorkload(t *testing.T) {
    for _, unit := range []Unit{CodePoints,Bytes} {
        saved, err := ParseAST(localInput(3,false),WithUnit(unit))
        if !checkLocal(saved,err,3,false,unit) { t.Fatalf("saved value %v %v",saved,err) }
        before, _ := json.Marshal(saved)
        p := &tparser{parser:&parser{}}
        for _, n := range []int{1,128,1} {
            for _, rejected := range []bool{false,true} {
                input := localInput(n,rejected)
                value, err := p.run(trules[0],input,parseOptions{unit: unit, maxDepth: defaultMaxDepth},&tslabs{})
                v := tAs[*Doc](value)
                if !checkLocal(v,err,n,rejected,unit) { t.Fatalf("%d/%v: %v %v",n,rejected,v,err) }
                data, _ := json.Marshal(struct { Value any; Error string }{v,fmt.Sprint(err)})
                pooled, pooledErr := ParseAST(input,WithUnit(unit))
                pooledData, _ := json.Marshal(struct { Value any; Error string }{pooled,fmt.Sprint(pooledErr)})
                if string(data) != string(pooledData) { t.Fatal("manual and pooled parsers differ") }
                fmt.Printf("OBS %v %d/%v %s\n",unit,n,rejected,data)
                if p.depth != 0 || p.frame != nil || len(p.trail) != 0 { t.Fatal("live invocation state") }
                for _, u := range p.trail[:cap(p.trail)] {
                    if u.f != nil || u.old != nil || u.slot != 0 { t.Fatal("retained undo state") }
                }
                p.recycle()
                after, _ := json.Marshal(struct { Value any; Error string }{v,fmt.Sprint(err)})
                if string(after) != string(data) { t.Fatal("recycle changed returned result") }
            }
        }
        after, _ := json.Marshal(saved)
        if string(after) != string(before) { t.Fatal("pooled reuse changed saved AST") }
    }
}

var localResult *Doc
var localErr error

func BenchmarkTypedLocalLayout(b *testing.B) {
    for _, n := range []int{1,128} {
        for _, rejected := range []bool{false,true} {
            input := localInput(n,rejected)
            for _, unit := range []Unit{CodePoints,Bytes} {
                b.Run(fmt.Sprintf("%d/rejected=%v/%v",n,rejected,unit),func(b *testing.B) {
                    v, err := ParseAST(input,WithUnit(unit))
                    if !checkLocal(v,err,n,rejected,unit) { b.Fatalf("%v %v",v,err) }
                    b.ReportAllocs()
                    for b.Loop() { localResult,localErr = ParseAST(input,WithUnit(unit)) }
                })
            }
        }
    }
}
`
