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
func qualifyRuntime(pkg, rtImport, body string, info *runtimeInfo) (string, map[string]bool, error) {
	const prefix = "package p\n"
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "body.go", prefix+body, goparser.ParseComments)
	if err != nil {
		return "", nil, err
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
	used := map[string]bool{}
	for _, id := range f.Unresolved {
		if _, ok := info.decls[id.Name]; ok {
			used[id.Name] = true
			offsets = append(offsets, fset.Position(id.Pos()).Offset-len(prefix))
			continue
		}
		if selectorBase[id] {
			if p, ok := stdImports[id.Name]; ok {
				std[p] = true
			}
		}
	}
	// A key of a composite literal that is not of a struct is an expression, which may name a
	// runtime constant; the parser does not report it as unresolved, since a key may name a field.
	// The kind of the literal comes from its type, resolved through the type declarations of the
	// body and of the runtime, and for a literal with its type elided, from the enclosing one.
	types := map[string]goast.Expr{}
	for _, d := range f.Decls {
		if d, ok := d.(*goast.GenDecl); ok && d.Tok == token.TYPE {
			for _, s := range d.Specs {
				ts := s.(*goast.TypeSpec)
				types[ts.Name.Name] = ts.Type
			}
		}
	}
	var qualifyKeys func(lit *goast.CompositeLit, typ goast.Expr)
	qualifyKeys = func(lit *goast.CompositeLit, typ goast.Expr) {
		if lit.Type != nil {
			typ = lit.Type
		}
		kind, key, elem := literalType(typ, types, info)
		for _, e := range lit.Elts {
			kv, isKV := e.(*goast.KeyValueExpr)
			if !isKV {
				if sub, ok := e.(*goast.CompositeLit); ok && sub.Type == nil {
					qualifyKeys(sub, elem)
				}
				continue
			}
			if id, ok := kv.Key.(*goast.Ident); ok && kind != 's' && id.Obj == nil {
				if d, ok := info.decls[id.Name]; ok && (d.kind == token.CONST || d.kind == token.VAR) {
					used[id.Name] = true
					offsets = append(offsets, fset.Position(id.Pos()).Offset-len(prefix))
				}
			}
			if sub, ok := kv.Key.(*goast.CompositeLit); ok && sub.Type == nil {
				qualifyKeys(sub, key)
			}
			if sub, ok := kv.Value.(*goast.CompositeLit); ok && sub.Type == nil {
				qualifyKeys(sub, elem)
			}
		}
	}
	goast.Inspect(f, func(n goast.Node) bool {
		if lit, ok := n.(*goast.CompositeLit); ok && lit.Type != nil {
			qualifyKeys(lit, nil)
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
	return imports.String() + string(b), used, nil
}

// literalType returns the kind of the composite literal of type typ ('s' for a struct, 'm' for a
// map, 'a' for an array or slice, 0 if unknown) and the types of its keys and elements (nil if
// unknown). Named types are resolved through types, the type declarations of the body, and then
// through the runtime's.
func literalType(typ goast.Expr, types map[string]goast.Expr, info *runtimeInfo) (kind byte, key, elem goast.Expr) {
	for depth := 0; typ != nil && depth < 32; depth++ {
		switch t := typ.(type) {
		case *goast.ParenExpr:
			typ = t.X
		case *goast.StarExpr:
			typ = t.X
		case *goast.IndexExpr:
			typ = t.X
		case *goast.IndexListExpr:
			typ = t.X
		case *goast.StructType:
			return 's', nil, nil
		case *goast.MapType:
			return 'm', t.Key, t.Value
		case *goast.ArrayType:
			return 'a', nil, t.Elt
		case *goast.Ident:
			if next, ok := types[t.Name]; ok {
				typ = next
			} else if d, ok := info.decls[t.Name]; ok && d.kind == token.TYPE {
				typ = d.spec.(*goast.TypeSpec).Type
			} else {
				return 0, nil, nil
			}
		default:
			return 0, nil, nil
		}
	}
	return 0, nil, nil
}

// declaredNames returns the top-level names (other than methods) that the Go source declares.
func declaredNames(src string) ([]string, error) {
	f, err := goparser.ParseFile(token.NewFileSet(), "src.go", src, goparser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *goast.FuncDecl:
			if d.Recv == nil && d.Name.Name != "init" {
				out = append(out, d.Name.Name)
			}
		case *goast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *goast.TypeSpec:
					out = append(out, s.Name.Name)
				case *goast.ValueSpec:
					for _, n := range s.Names {
						out = append(out, n.Name)
					}
				}
			}
		}
	}
	return out, nil
}

// apiPubName returns the name under which api.go declares the runtime name.
func apiPubName(name string) string {
	if name == "Walk" {
		// A typed AST walker of the same package or a hand-written one commonly takes the name Walk.
		return "WalkTree"
	}
	return name
}

// typedEntryNames returns the exported names the typed part of a generated parser declares
// besides the grammar's types and the names of the head of parser.go. checkNoClash catches a
// name missing here.
func (opts GenOptions) typedEntryNames() []string {
	names := []string{"ParseAST", "ParseRuleAST", "Error", "TNode"}
	if opts.Document {
		names = append(names, "Document")
	}
	if opts.Stream {
		names = append(names, "ParseStreamAST", "StreamValue", "IterateAST", "StreamASTIterator")
	}
	return names
}

// reservedNames returns the names that the types of a typed grammar must not take, because the
// generated package declares them: those of api.go, those declared by the part of parser.go that
// is generated before the types (head), and typedEntryNames. The runtime's own names are not
// reserved, since the generated code refers to them by qualified names.
func (opts GenOptions) reservedNames(head string) map[string]bool {
	reserved := map[string]bool{}
	for _, name := range opts.apiNames() {
		if name != "Span" { // a grammar type may take the name; api.go then leaves it out
			reserved[apiPubName(name)] = true
		}
	}
	if names, err := declaredNames("package p\n" + head); err == nil {
		for _, name := range names {
			reserved[name] = true
		}
	}
	for _, name := range opts.typedEntryNames() {
		reserved[name] = true
	}
	return reserved
}

// checkNoClash reports a name that both the body of parser.go (declared) and api.go declare.
func checkNoClash(declared []string, api string) error {
	apiDeclared, err := declaredNames(api)
	if err != nil {
		return err
	}
	inAPI := map[string]bool{}
	for _, name := range apiDeclared {
		inAPI[name] = true
	}
	for _, name := range declared {
		if inAPI[name] {
			return fmt.Errorf("the grammar declares %s, which the generated package also declares for the users of the parser; rename the type", name)
		}
	}
	return nil
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
		pubName := apiPubName(name)
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
