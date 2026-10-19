package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedGoWorkLayoutReference(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generation-time work-layout references")
	}
	g, err := syntax.Parse(`type Word = { Text string }

type WordOrError ~ Word | error

type Bag = {
    Items []WordOrError
    Count int
}

type WithContext = { Text string }

rule collision WithContext = t:("z") #atomic => WithContext{Text: text($t)}

rule main Bag = {n := 1} x:word* #cut => Bag{Items: $x.Value, Count: len($x) + n}

rule word Word = t:("a" / "é") #atomic => Word{Text: text($t)}

rule recovered Bag = x:word #recover(skip="!") #cut => Bag{Items: list($x)}

rule bad Bag = v1:"a" #cut => Bag{Count: 1 / (len($v1) - 1)}

rule anynil any = t:word? => $t

rule seq = "a" "é"?

rule ptrnil *node[_] = t:seq? => $t

rule converted = "a" => nil`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module layoutfixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var imports, cases strings.Builder
	for i, opts := range []GenOptions{{Start: "main"}, {Start: "main", Types: true}, {Start: "main", Types: true, convertTypes: true}, {Start: "anynil", Types: true}} {
		for _, reference := range []bool{false, true} {
			pkg := fmt.Sprintf("layout_%d_%v", i, reference)
			opts.Package, opts.Recognize, opts.referenceWorkControlLayout = pkg, true, reference
			code, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			if reference {
				checkWorkReferenceSource(t, code)
				both := opts
				both.referenceWorkControls = true
				combined, err := Generate(g, both)
				if err != nil || !bytes.Equal(combined, code) {
					t.Fatal("reference flags changed output", err)
				}
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			typed := ""
			if opts.Types {
				typed = layoutTypedState
				// The direct typed runtime needs no converter, and a converting parser no typed
				// runtime.
				if opts.convertTypes {
					typed = strings.ReplaceAll(typed, ",reflect.TypeOf(tslabs{})", "")
				} else {
					typed = strings.ReplaceAll(typed, "reflect.TypeOf(astConv{}),", "")
				}
			}
			fixture := strings.NewReplacer("PACKAGE", pkg, "REFERENCE", fmt.Sprint(reference), "TYPED_STATE", typed).Replace(layoutStateConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "layout_test.go": []byte(fixture)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			fmt.Fprintf(&imports, "p%d_%v \"layoutfixture/%s\"\n", i, reference, pkg)
		}
		typed := "nil"
		if opts.Types {
			typed = fmt.Sprintf("func(s string,b bool)(any,error){u:=p%d_false.CodePoints;if b{u=p%d_false.Bytes};v,e:=p%d_false.ParseAST(s,p%d_false.WithUnit(u));return v,e}", i, i, i, i)
		}
		typedRef := strings.ReplaceAll(typed, fmt.Sprintf("p%d_false", i), fmt.Sprintf("p%d_true", i))
		named := "nil"
		if opts.Types {
			named = fmt.Sprintf("func(name,s string,b bool)(any,error){u:=p%d_false.CodePoints;if b{u=p%d_false.Bytes};return p%d_false.ParseRuleAST(name,s,p%d_false.WithUnit(u))}", i, i, i, i)
		}
		namedRef := strings.ReplaceAll(named, fmt.Sprintf("p%d_false", i), fmt.Sprintf("p%d_true", i))
		fmt.Fprintf(&cases, `{"mode%d",func(s string,b bool)(any,error){u:=p%d_false.CodePoints;if b{u=p%d_false.Bytes};return p%d_false.Parse(s,p%d_false.WithUnit(u))},func(s string,b bool)(any,error){u:=p%d_true.CodePoints;if b{u=p%d_true.Bytes};return p%d_true.Parse(s,p%d_true.WithUnit(u))},%s,%s,%s,%s,func(s string,b bool)error{u:=p%d_false.CodePoints;if b{u=p%d_false.Bytes};return p%d_false.Recognize(s,p%d_false.WithUnit(u))},func(s string,b bool)error{u:=p%d_true.CodePoints;if b{u=p%d_true.Bytes};return p%d_true.Recognize(s,p%d_true.WithUnit(u))}},
`, i, i, i, i, i, i, i, i, i, typed, typedRef, named, namedRef, i, i, i, i, i, i, i, i)
	}
	driver := strings.NewReplacer("IMPORTS", imports.String(), "CASES", cases.String()).Replace(layoutParityDriver)
	if err := os.WriteFile(filepath.Join(dir, "parity_test.go"), []byte(driver), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated work layout reference: %v\n%s", err, out)
	}
}

func checkWorkReferenceSource(t *testing.T, src []byte) {
	t.Helper()
	f, err := goparser.ParseFile(token.NewFileSet(), "reference.go", src, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*goast.FuncDecl); ok && fn.Recv == nil && (fn.Name.Name == "WithContext" || fn.Name.Name == "WithWorkLimits") {
			t.Fatal("ordinary-call layout reference retains control options")
		}
	}
	goast.Inspect(f, func(n goast.Node) bool {
		if s, ok := n.(*goast.SelectorExpr); ok && (s.Sel.Name == "control" || s.Sel.Name == "context" || s.Sel.Name == "prepared") {
			t.Fatalf("reference retains invocation state selector %s", s.Sel.Name)
		}
		return true
	})
}

const layoutStateConsumer = `package PACKAGE
import("reflect";"testing";"unsafe")
const reference=REFERENCE
func TestInvocationStateLayout(t *testing.T){
 optionType:=reflect.TypeOf(parseOptions{});_,control:=optionType.FieldByName("control")
 if control==reference{t.Fatal("option control field presence",control)}
 if reference {
  if optionType.NumField()!=2 || unsafe.Sizeof(parseOptions{})!=unsafe.Sizeof(struct{unit PositionUnit;maxDepth int}{}) || unsafe.Offsetof(parseOptions{}.unit)!=0 || unsafe.Offsetof(parseOptions{}.maxDepth)!=unsafe.Sizeof(PositionUnit(0)){t.Fatal("ordinary option layout changed")}
 }
 for _,typ:=range []reflect.Type{reflect.TypeOf(parser{})}{_,has:=typ.FieldByName("control");if has==reference{t.Fatal("parser control state presence",typ)}}
 TYPED_STATE
 if reference {for _,depth:=range []int{1,32}{if _,err:=ParseRule("word","a",WithMaxDepth(depth));err!=nil{t.Fatal("positive depth changed",err)}};calls:=0;opt:=func(o *parseOptions){calls++;o.unit=Bytes};if _,err:=Parse("a",opt,WithMaxDepth(0));err!=nil || calls!=1{t.Fatal("ordinary options changed",err,calls)};if _,err:=ParseRule("missing","a",WithMaxDepth(-1));err==nil || err.Error()!="max depth must be non-negative: -1"{t.Fatal("validation order changed",err)}}
}
`
const layoutTypedState = `for _,typ:=range []reflect.Type{reflect.TypeOf(astConv{}),reflect.TypeOf(tslabs{})}{_,has:=typ.FieldByName("control");if has==reference{t.Fatal("converter control state presence",typ)}}`
const layoutParityDriver = `package layoutfixture
import("encoding/json";"fmt";"reflect";"sync";"testing";
IMPORTS
)
func normalized(v any,e error)string{data,err:=json.Marshal(v);if err!=nil{panic(err)};shape:="nil";if v!=nil{r:=reflect.ValueOf(v);typ:=r.Type();shape=typ.Kind().String();if typ.Kind()==reflect.Pointer{shape+="/"+typ.Elem().Name()+fmt.Sprint(r.IsNil())}};return shape+string(data)+fmt.Sprint(e)}
func TestOrdinaryReferenceParity(t *testing.T){
 rows:=[]struct{name string;parse,ref,typed,typedRef func(string,bool)(any,error);named,namedRef func(string,string,bool)(any,error);recognize,recognizeRef func(string,bool)error}{CASES}
 for _,row:=range rows{for _,bytes:=range []bool{false,true}{for _,input:=range []string{"","a","aa","é","aé","!","a!","ax","z","\xff"}{
  a,e:=row.parse(input,bytes);b,f:=row.ref(input,bytes);if normalized(a,e)!=normalized(b,f){t.Fatal("Node parity",row.name,bytes,input)}
  if row.typed!=nil{a,e=row.typed(input,bytes);b,f=row.typedRef(input,bytes);if normalized(a,e)!=normalized(b,f){t.Fatal("typed parity",row.name,bytes,input)}}
  if fmt.Sprint(row.recognize(input,bytes))!=fmt.Sprint(row.recognizeRef(input,bytes)){t.Fatal("recognition parity",row.name,bytes,input)}
  if row.named!=nil{for _,name:=range []string{"main","recovered","bad","anynil","ptrnil","collision","missing"}{a,e=row.named(name,input,bytes);b,f=row.namedRef(name,input,bytes);if normalized(a,e)!=normalized(b,f){t.Fatal("named parity",row.name,name,bytes,input)}}}
 }}}
 for _,row:=range rows{
  retained,err:=row.parse("a",false);if err!=nil{t.Fatal(err)};before:=normalized(retained,err)
  var wg sync.WaitGroup;for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();for j:=0;j<12;j++{row.ref("aa",false);row.parse("ax",true)}}()};wg.Wait()
  if normalized(retained,err)!=before{t.Fatal("retained Node changed",row.name)}
  if row.typed!=nil{retained,err:=row.typed("a",false);if err!=nil{t.Fatal(err)};before:=normalized(retained,err);for j:=0;j<12;j++{row.typedRef("aa",false);row.typed("ax",true)};if normalized(retained,err)!=before{t.Fatal("retained typed result changed",row.name)}}
 }
}
`

func TestGeneratedGoWorkLayoutReferenceDrift(t *testing.T) {
	g, err := syntax.Parse(`type Word = { Text string }

rule main Word = t:("a") #atomic => Word{Text: text($t)}

rule other any = "a" => nil

rule converted = "a" => nil`)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Generate(g, GenOptions{Package: "reference", Start: "main", Types: true, referenceWorkControls: true})
	if err != nil {
		t.Fatal(err)
	}
	// The direct typed runtime (tslabs) has no converter; a converting parser has astConv.
	converting, err := Generate(g, GenOptions{Package: "reference", Start: "main", Types: true, convertTypes: true, referenceWorkControls: true})
	if err != nil {
		t.Fatal(err)
	}
	sourceOf := func(name string) []byte {
		if name == "astConv" {
			return converting
		}
		return source
	}
	mutations := map[string]func(*testing.T, *goast.File){
		"missing constant": func(t *testing.T, f *goast.File) {
			workReferenceValue(t, f, "workControlsEnabled").Names[0].Name = "changed"
		},
		"enabled constant": func(t *testing.T, f *goast.File) {
			workReferenceValue(t, f, "workControlsEnabled").Values[0] = goast.NewIdent("true")
		},
		"extra option": func(t *testing.T, f *goast.File) {
			s := workReferenceStruct(t, f, "parseOptions")
			s.Fields.List = append(s.Fields.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent("extra")}, Type: goast.NewIdent("bool")})
		},
		"embedded option": func(t *testing.T, f *goast.File) {
			s := workReferenceStruct(t, f, "parseOptions")
			s.Fields.List = append(s.Fields.List, &goast.Field{Type: goast.NewIdent("workControl")})
		},
		"changed preparation": func(t *testing.T, f *goast.File) {
			fn := workReferenceFunction(t, f, "prepareOptions")
			fn.Body.List = append(fn.Body.List, &goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("changed")}})
		},
		"changed rejection": func(t *testing.T, f *goast.File) {
			fn := workReferenceFunction(t, f, "resolveOptions")
			for _, s := range fn.Body.List {
				if c, ok := s.(*goast.IfStmt); ok && referencesField(c.Cond, "context", "limits") {
					c.Body.List = append(c.Body.List, &goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("changed")}})
				}
			}
		},
	}
	for _, name := range []string{"WithContext", "WithWorkLimits", "prepareOptions", "resolveOptions"} {
		mutations["missing function "+name] = func(t *testing.T, f *goast.File) { workReferenceFunction(t, f, name).Name.Name = "changed" }
	}
	sources := map[string][]byte{}
	for _, name := range []string{"parser", "parseOptions", "astConv", "tslabs"} {
		sources["missing control "+name] = sourceOf(name)
		mutations["missing control "+name] = func(t *testing.T, f *goast.File) {
			s := workReferenceStruct(t, f, name)
			for _, field := range s.Fields.List {
				for _, n := range field.Names {
					if n.Name == "control" {
						n.Name = "changed"
					}
				}
			}
		}
	}
	for _, name := range []string{"unit", "maxDepth", "context", "limits", "prepared"} {
		mutations["changed option "+name] = func(t *testing.T, f *goast.File) {
			s := workReferenceStruct(t, f, "parseOptions")
			for _, field := range s.Fields.List {
				for _, n := range field.Names {
					if n.Name == name {
						field.Type = goast.NewIdent("changed")
					}
				}
			}
		}
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			src, ok := sources[name]
			if !ok {
				src = source
			}
			f, err := goparser.ParseFile(token.NewFileSet(), "reference.go", src, goparser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			mutate(t, f)
			if result, err := removeGeneratedWorkState([]byte(workAnchorText(f))); err == nil || result != nil {
				t.Fatalf("accepted drift: %v", err)
			}
		})
	}
	for _, c := range []struct {
		src   []byte
		names []string
	}{{source, []string{"parser", "tslabs"}}, {converting, []string{"parser", "astConv"}}} {
		t.Run("grouped fields preserve other names", func(t *testing.T) {
			groupedFieldsPreserveOtherNames(t, c.src, c.names)
		})
	}
}

// groupedFieldsPreserveOtherNames checks that removing the control field of the structs names
// keeps a field declared in the same group.
func groupedFieldsPreserveOtherNames(t *testing.T, source []byte, names []string) {
	{
		f, err := goparser.ParseFile(token.NewFileSet(), "reference.go", source, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			for _, field := range workReferenceStruct(t, f, name).Fields.List {
				for _, n := range field.Names {
					if n.Name == "control" {
						field.Names = append(field.Names, goast.NewIdent("other"))
						break
					}
				}
			}
		}
		result, err := removeGeneratedWorkState([]byte(workAnchorText(f)))
		if err != nil {
			t.Fatal(err)
		}
		f, err = goparser.ParseFile(token.NewFileSet(), "reference.go", result, goparser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			found := false
			for _, field := range workReferenceStruct(t, f, name).Fields.List {
				for _, n := range field.Names {
					if n.Name == "other" {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("removed unrelated field", name)
			}
		}
	}
}

func workReferenceStruct(t *testing.T, f *goast.File, name string) *goast.StructType {
	t.Helper()
	for _, d := range f.Decls {
		if d, ok := d.(*goast.GenDecl); ok {
			for _, s := range d.Specs {
				if s, ok := s.(*goast.TypeSpec); ok && s.Name.Name == name {
					return s.Type.(*goast.StructType)
				}
			}
		}
	}
	t.Fatal("missing struct", name)
	return nil
}
func workReferenceValue(t *testing.T, f *goast.File, name string) *goast.ValueSpec {
	t.Helper()
	for _, d := range f.Decls {
		if d, ok := d.(*goast.GenDecl); ok {
			for _, s := range d.Specs {
				if s, ok := s.(*goast.ValueSpec); ok {
					for _, n := range s.Names {
						if n.Name == name {
							return s
						}
					}
				}
			}
		}
	}
	t.Fatal("missing value", name)
	return nil
}
func workReferenceFunction(t *testing.T, f *goast.File, name string) *goast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		if d, ok := d.(*goast.FuncDecl); ok && d.Recv == nil && d.Name.Name == name {
			return d
		}
	}
	t.Fatal("missing function", name)
	return nil
}
