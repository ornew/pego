package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// The Go runtime of generated parsers is the package runtime of this module
// (github.com/ornew/pego/runtime). Generated code reaches it through the exported names that are
// documented there as for generated parsers. A generated package either imports the runtime from
// this module or carries a copy of its files (embed mode), which the generator reads from the
// source registered with SetRuntimeSource.

// RuntimeImportPath is the import path of the runtime package in this module.
const RuntimeImportPath = "github.com/ornew/pego/runtime"

// runtimeAlias is the name generated code gives to the runtime package.
const runtimeAlias = "pegort"

// RuntimeMode selects how a generated Go parser reaches the runtime.
type RuntimeMode int

const (
	// RuntimeEmbed writes a copy of the runtime into the generated package's internal/runtime
	// directory, so that the generated code depends only on the standard library.
	RuntimeEmbed RuntimeMode = iota
	// RuntimeImport imports the runtime from this module.
	RuntimeImport
	// RuntimeOnly generates only the embedded copy of the runtime, for refreshing a package
	// generated with RuntimeEmbed without regenerating its parser.
	RuntimeOnly
)

func (m RuntimeMode) String() string {
	switch m {
	case RuntimeEmbed:
		return "embed"
	case RuntimeImport:
		return "import"
	case RuntimeOnly:
		return "only"
	}
	return fmt.Sprintf("RuntimeMode(%d)", int(m))
}

// ParseRuntimeMode returns the mode named s: embed, import or only.
func ParseRuntimeMode(s string) (RuntimeMode, error) {
	switch s {
	case "embed":
		return RuntimeEmbed, nil
	case "import":
		return RuntimeImport, nil
	case "only":
		return RuntimeOnly, nil
	}
	return 0, fmt.Errorf("unknown runtime mode %q: want embed, import or only", s)
}

// GeneratedFile is a file of generated code. Path is relative to the directory of the generated
// package and uses slashes.
type GeneratedFile struct {
	Path    string
	Content []byte
}

var runtimeSrc struct {
	mu   sync.Mutex
	fsys fs.FS
	info *runtimeInfo
}

// SetRuntimeSource registers the files of the runtime package (the directory runtime of this
// module, in fsys's root) that Generate copies and reads. The pego package registers its embedded
// copy when it is imported.
func SetRuntimeSource(fsys fs.FS) {
	runtimeSrc.mu.Lock()
	defer runtimeSrc.mu.Unlock()
	runtimeSrc.fsys, runtimeSrc.info = fsys, nil
}

type runtimeFile struct {
	name string
	data []byte
}

// runtimeDecl is an exported top-level declaration of the runtime.
type runtimeDecl struct {
	kind token.Token // token.TYPE, CONST, VAR or FUNC
	doc  string
	spec goast.Node
}

type runtimeInfo struct {
	files []runtimeFile
	decls map[string]*runtimeDecl
}

func loadRuntime() (*runtimeInfo, error) {
	runtimeSrc.mu.Lock()
	defer runtimeSrc.mu.Unlock()
	if runtimeSrc.info != nil {
		return runtimeSrc.info, nil
	}
	if runtimeSrc.fsys == nil {
		return nil, fmt.Errorf("the runtime source is not registered: import github.com/ornew/pego, or call engine.SetRuntimeSource")
	}
	entries, err := fs.ReadDir(runtimeSrc.fsys, ".")
	if err != nil {
		return nil, err
	}
	info := &runtimeInfo{decls: map[string]*runtimeDecl{}}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := fs.ReadFile(runtimeSrc.fsys, name)
		if err != nil {
			return nil, err
		}
		info.files = append(info.files, runtimeFile{name, data})
		f, err := goparser.ParseFile(fset, name, data, goparser.ParseComments|goparser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *goast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					info.decls[d.Name.Name] = &runtimeDecl{token.FUNC, d.Doc.Text(), d}
				}
			case *goast.GenDecl:
				for _, s := range d.Specs {
					doc := d.Doc
					switch s := s.(type) {
					case *goast.TypeSpec:
						if s.Doc != nil {
							doc = s.Doc
						}
						if s.Name.IsExported() {
							info.decls[s.Name.Name] = &runtimeDecl{token.TYPE, doc.Text(), s}
						}
					case *goast.ValueSpec:
						if s.Doc != nil {
							doc = s.Doc
						}
						for _, n := range s.Names {
							if n.IsExported() {
								info.decls[n.Name] = &runtimeDecl{d.Tok, doc.Text(), s}
							}
						}
					}
				}
			}
		}
	}
	if len(info.files) == 0 {
		return nil, fmt.Errorf("the registered runtime source has no files")
	}
	sort.Slice(info.files, func(i, j int) bool { return info.files[i].name < info.files[j].name })
	runtimeSrc.info = info
	return info, nil
}

// runtimeFiles returns the copy of the runtime that RuntimeEmbed writes.
func runtimeFiles() ([]GeneratedFile, error) {
	info, err := loadRuntime()
	if err != nil {
		return nil, err
	}
	var out []GeneratedFile
	for _, f := range info.files {
		out = append(out, GeneratedFile{path.Join("internal", "runtime", f.name), append([]byte(generatedHeader+"\n"), f.data...)})
	}
	return out, nil
}

// generatedHeader marks files written by the generator; a generator may remove stale files with it.
const generatedHeader = "// Code generated by pego. DO NOT EDIT."

// runtimeImport returns the import path of the runtime for a generated package.
func (opts GenOptions) runtimeImport() string {
	if opts.Runtime == RuntimeEmbed {
		return path.Join(opts.Module, "internal", "runtime")
	}
	return RuntimeImportPath
}

// stdImports are the standard-library packages generated code may use, by the name it uses them
// with.
var stdImports = map[string]string{
	"atomic": "sync/atomic", "bufio": "bufio", "bytes": "bytes", "context": "context", "errors": "errors",
	"fmt": "fmt", "io": "io", "iter": "iter", "math": "math", "slices": "slices", "sort": "sort",
	"strconv": "strconv", "strings": "strings", "sync": "sync", "unicode": "unicode", "unsafe": "unsafe",
	"utf8": "unicode/utf8",
}

// qualifyRuntime finishes the generated source body of package pkg. Every identifier that the body
// leaves unresolved and that is an exported name of the runtime is the runtime's, and is qualified
// with the runtime package; the standard-library packages the body uses are imported.
func qualifyRuntime(pkg, rtImport, body string, info *runtimeInfo) (string, error) {
	const prefix = "package p\n"
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "body.go", prefix+body, goparser.ParseComments)
	if err != nil {
		return "", err
	}
	var offsets []int
	std := map[string]bool{}
	selectorBase := map[*goast.Ident]bool{}
	goast.Inspect(f, func(n goast.Node) bool {
		switch n := n.(type) {
		case *goast.SelectorExpr:
			if id, ok := n.X.(*goast.Ident); ok {
				selectorBase[id] = true
			}
		}
		return true
	})
	for _, id := range f.Unresolved {
		if _, ok := info.decls[id.Name]; ok {
			offsets = append(offsets, fset.Position(id.Pos()).Offset-len(prefix))
			continue
		}
		if selectorBase[id] {
			if p, ok := stdImports[id.Name]; ok {
				std[p] = true
			}
		}
	}
	// A composite literal of a map, slice or array may use runtime constants as keys, which the
	// parser does not report as unresolved (they may name fields).
	goast.Inspect(f, func(n goast.Node) bool {
		lit, ok := n.(*goast.CompositeLit)
		if !ok {
			return true
		}
		switch lit.Type.(type) {
		case *goast.MapType, *goast.ArrayType:
		default:
			return true
		}
		for _, e := range lit.Elts {
			kv, ok := e.(*goast.KeyValueExpr)
			if !ok {
				continue
			}
			id, ok := kv.Key.(*goast.Ident)
			if !ok || id.Obj != nil {
				continue
			}
			if d, ok := info.decls[id.Name]; ok && (d.kind == token.CONST || d.kind == token.VAR) {
				offsets = append(offsets, fset.Position(id.Pos()).Offset-len(prefix))
			}
		}
		return true
	})
	sort.Sort(sort.Reverse(sort.IntSlice(offsets)))
	b := []byte(body)
	last := -1
	for _, off := range offsets {
		if off == last {
			continue
		}
		last = off
		b = append(b[:off], append([]byte(runtimeAlias+"."), b[off:]...)...)
	}
	var imports strings.Builder
	var paths []string
	for p := range std {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	imports.WriteString("import (\n")
	for _, p := range paths {
		fmt.Fprintf(&imports, "\t%q\n", p)
	}
	fmt.Fprintf(&imports, "\n\t%s %q\n)\n\n", runtimeAlias, rtImport)
	return imports.String() + string(b), nil
}

// declaredRuntimeNames returns the top-level names the generated body declares that are also
// exported names of the runtime. Generated code refers to the runtime by qualified names, so such
// a declaration is only valid where the generator intends it.
func declaredRuntimeNames(body string, info *runtimeInfo) ([]string, error) {
	f, err := goparser.ParseFile(token.NewFileSet(), "body.go", "package p\n"+body, goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	add := func(id *goast.Ident) {
		if _, ok := info.decls[id.Name]; ok {
			out = append(out, id.Name)
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *goast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *goast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *goast.TypeSpec:
					add(s.Name)
				case *goast.ValueSpec:
					for _, n := range s.Names {
						add(n)
					}
				}
			}
		}
	}
	return out, nil
}

// apiNames returns the runtime names that the generated package re-exports for its users, in
// declaration order by group.
func (opts GenOptions) apiNames() []string {
	names := []string{
		"Node", "NodeField", "Fields", "NodeKind", "KindString", "KindTuple", "KindList", "KindError", "KindStruct",
		"KindTupleValue", "KindListValue", "KindValue", "SyntaxError", "SyntaxErrors", "PositionUnit", "CodePoints", "Bytes",
		"ParseOption", "WithUnit", "WithMaxDepth", "WithContext", "WithWorkLimits",
		"WorkLimits", "WorkLimitKind", "WorkLimitError", "ErrWorkLimit", "InputWorkLimit", "StepWorkLimit",
		"Cursor", "CycleError", "WalkAction", "WalkContinue", "WalkSkip", "WalkStop", "WalkVisit", "WalkEdge", "WalkEdgeKind",
		"WalkRoot", "WalkChild", "WalkField", "WalkListElement", "Walk",
	}
	if opts.Observability {
		names = append(names, "TraceKind", "TraceEnter", "TraceExit", "TraceSkipped", "TraceEvent", "WithTrace", "WithTraceFilter", "WithTraceMaxDepth",
			"WithProfile", "Profile", "ProfileSnapshot", "RuleProfile", "ProfileLocation", "SkipReasonCount")
	}
	if opts.Stream {
		names = append(names, "StreamIterator", "ErrStreamClosed", "WithDiagnosticCallback", "WithDiagnosticLimit", "TruncatedSyntaxErrors")
	}
	if opts.Document {
		names = append(names, "ParseStats")
		if !opts.Types {
			names = append(names, "Document")
		}
	}
	if opts.Types {
		names = append(names, "Span")
	}
	return names
}

// apiSource returns the source of the file of the generated package that re-exports the runtime
// names apiNames lists. Types, constants and variables are aliases; functions are wrappers that
// the compiler inlines.
func (opts GenOptions) apiSource(info *runtimeInfo, noSpan bool) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\npackage %s\n\n", generatedHeader, opts.Package)
	b.WriteString("import (\n\t\"context\"\n\n")
	fmt.Fprintf(&b, "\t%s %q\n)\n\n", runtimeAlias, opts.runtimeImport())
	b.WriteString("var _ context.Context\n\n")
	for _, name := range opts.apiNames() {
		if noSpan && name == "Span" {
			// A grammar type has the name.
			continue
		}
		d := info.decls[name]
		if d == nil {
			return "", fmt.Errorf("internal error: the runtime has no %s", name)
		}
		pubName := name
		if name == "Walk" {
			// A typed AST walker of the same package or a hand-written one commonly takes the name Walk.
			pubName = "WalkTree"
		}
		doc := d.doc
		if pubName != name {
			doc = strings.Replace(doc, name+" ", pubName+" ", 1)
		}
		for _, line := range strings.Split(strings.TrimRight(doc, "\n"), "\n") {
			if line == "" && doc == "" {
				break
			}
			if line == "" {
				b.WriteString("//\n")
			} else {
				b.WriteString("// " + line + "\n")
			}
		}
		switch d.kind {
		case token.TYPE:
			fmt.Fprintf(&b, "type %s = %s.%s\n\n", pubName, runtimeAlias, name)
		case token.CONST:
			fmt.Fprintf(&b, "const %s = %s.%s\n\n", pubName, runtimeAlias, name)
		case token.VAR:
			fmt.Fprintf(&b, "var %s = %s.%s\n\n", pubName, runtimeAlias, name)
		case token.FUNC:
			fn := d.spec.(*goast.FuncDecl)
			if fn.Type.TypeParams != nil {
				return "", fmt.Errorf("internal error: the generic function %s cannot be re-exported", name)
			}
			var sig bytes.Buffer
			t := *fn.Type
			if err := printer.Fprint(&sig, token.NewFileSet(), &t); err != nil {
				return "", err
			}
			var args []string
			i := 0
			params := &goast.FieldList{}
			for _, f := range fn.Type.Params.List {
				nf := *f
				if len(f.Names) == 0 {
					nf.Names = []*goast.Ident{goast.NewIdent(fmt.Sprintf("a%d", i))}
				}
				for _, n := range nf.Names {
					a := n.Name
					if _, ok := f.Type.(*goast.Ellipsis); ok {
						a += "..."
					}
					args = append(args, a)
				}
				i++
				params.List = append(params.List, &nf)
			}
			ft := *fn.Type
			ft.Params = params
			var sigb bytes.Buffer
			if err := printer.Fprint(&sigb, token.NewFileSet(), &ft); err != nil {
				return "", err
			}
			call := fmt.Sprintf("%s.%s(%s)", runtimeAlias, name, strings.Join(args, ", "))
			ret := ""
			if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
				ret = "return "
			}
			fmt.Fprintf(&b, "func %s%s { %s%s }\n\n", pubName, strings.TrimPrefix(sigb.String(), "func"), ret, call)
		}
	}
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("generated API does not compile: %w\n%s", err, b.String())
	}
	return string(src), nil
}
