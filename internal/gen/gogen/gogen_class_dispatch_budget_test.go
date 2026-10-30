package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedGoClassDispatchBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated character-class control consumers")
	}
	var chars strings.Builder
	for i := 0; i < 320; i++ {
		chars.WriteRune(rune(0x1000 + i*2))
	}
	source := `type Text = { Text string }
rule main Text = x:wide #cut => $x
rule wide Text = t:[` + chars.String() + `] #atomic => Text{Text: text($t)}
rule scan Text = t:([` + chars.String() + `]+) #atomic => Text{Text: text($t)}
rule negative Text = t:[^` + chars.String() + `] #atomic => Text{Text: text($t)}
rule negative_scan Text = t:([^` + chars.String() + `]+) #atomic => Text{Text: text($t)}
rule optional Text = t:([` + chars.String() + `]? "z") #atomic => Text{Text: text($t)}
rule fallback Text = t:([` + chars.String() + `] / "z") #atomic => Text{Text: text($t)}`
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("classfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("class_%v", convert)
		code, e := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, ConvertTypes: convert})
		if e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(dir, pkg)
		if e = os.Mkdir(path, 0755); e != nil {
			t.Fatal(e)
		}
		consumer := strings.ReplaceAll(goClassDispatchConsumer, "PACKAGE", pkg)
		for name, data := range map[string][]byte{"parser.go": code, "class_test.go": []byte(consumer)} {
			if e = os.WriteFile(filepath.Join(path, name), data, 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("class consumers: %v\n%s", e, out)
	}
}

const goClassDispatchConsumer = `package PACKAGE
import("context";"encoding/json";"errors";"reflect";"runtime";"strings";"testing")
func jsonValue(t *testing.T,v any)string{t.Helper();b,e:=json.Marshal(v);if e!=nil{t.Fatal(e)};return string(b)}
func zero(v any)bool{return v==nil||reflect.ValueOf(v).IsZero()}
type classContext struct{context.Context;done chan struct{};inside bool}
func(c *classContext)Done()<-chan struct{}{return c.done}
func(c *classContext)Err()error{
 if c.inside{return context.Canceled}
 var pcs [32]uintptr;n:=runtime.Callers(2,pcs[:]);f:=runtime.CallersFrames(pcs[:n])
 for{frame,more:=f.Next();if strings.HasSuffix(frame.Function,".(*Parser).RejectBudgetClass"){c.inside=true;close(c.done);return context.Canceled};if !more{break}}
 return nil
}
func TestClassDispatch(t *testing.T){
 first,middle,last:=string(rune(0x1000)),string(rune(0x1000+160*2)),string(rune(0x1000+319*2))
 cases:=[]struct{name,input string;valid bool}{
 {"main",first,true},{"main",middle,true},{"main",last,true},{"main","z",false},{"main",string(rune(0x1001)),false},{"main","",false},
 {"scan",first+middle+last,true},{"scan","z",false},{"scan","",false},
 {"negative","z",true},{"negative",string(rune(0x1001)),true},{"negative",first,false},{"negative","",false},
 {"negative_scan","zz",true},{"negative_scan",last,false},{"negative_scan","",false},
 {"optional","z",true},{"optional",last+"z",true},{"optional","",false},
 {"fallback","z",true},{"fallback",last,true},{"fallback","",false},
 }
 for _,u:=range []PositionUnit{CodePoints,Bytes}{for _,tc:=range cases{
  routes:=[]struct{name string;call func(...ParseOption)(any,error)}{
   {"Node",func(o ...ParseOption)(any,error){return ParseRule(tc.name,tc.input,o...)}},
   {"typed",func(o ...ParseOption)(any,error){return ParseRuleAST(tc.name,tc.input,o...)}},
  }
  for _,r:=range routes {t.Run(tc.name+"/"+r.name+"/"+string(rune('0'+u))+"/"+tc.input,func(t *testing.T){
   v,e:=r.call(WithUnit(u));if (e==nil)!=tc.valid{t.Fatalf("baseline err=%v",e)}
   if tc.valid&&zero(v){t.Fatal("valid baseline has zero output")}
   want,diag:=jsonValue(t,v),jsonValue(t,e)
   a,ae:=r.call(WithUnit(u),WithLimits(Limits{Budget: ^uint64(0)}))
   if tc.valid&&zero(a){t.Fatal("valid controlled output is zero")};if jsonValue(t,a)!=want||jsonValue(t,ae)!=diag{t.Fatal("active outcome differs")}
   // Find the exact boundary for both completed success and syntax failure.
   lo,hi:=uint64(1),uint64(1<<20)
   for lo<hi {m:=lo+(hi-lo)/2;_,err:=r.call(WithUnit(u),WithLimits(Limits{Budget:m}));if errors.Is(err,ErrLimit){lo=m+1}else{hi=m}}
   a,ae=r.call(WithUnit(u),WithLimits(Limits{Budget:lo}));if tc.valid&&zero(a){t.Fatal("valid controlled output is zero")};if jsonValue(t,a)!=want||jsonValue(t,ae)!=diag{t.Fatal("exact boundary changed result")}
   a,ae=r.call(WithUnit(u),WithLimits(Limits{Budget:lo-1}));if lo<=1||!zero(a)||!errors.Is(ae,ErrLimit){t.Fatalf("one short result=%v err=%v",a,ae)}
   if tc.input==last&&tc.name=="main"&&lo<320{t.Fatal("wide membership is not charged")}
   retry,re:=r.call(WithUnit(u));if tc.valid&&zero(retry){t.Fatal("valid retry output is zero")};if jsonValue(t,retry)!=want||jsonValue(t,re)!=diag{t.Fatal("retry differs")}
  })}
 }}
 for _,u:=range []PositionUnit{CodePoints,Bytes}{
  c:=&classContext{Context:context.Background(),done:make(chan struct{})}
  n,e:=Parse(last,WithUnit(u),WithContext(c));if n!=nil||!errors.Is(e,context.Canceled)||!c.inside{t.Fatalf("membership cancellation: %v inside=%v",e,c.inside)}
  if n,e:=Parse(last,WithUnit(u));e!=nil||n==nil{t.Fatal("cancellation retry")}
 }
}
`
