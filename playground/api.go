// Command playground is the WebAssembly half of the PEGO web playground.
//
// Built with GOOS=js GOARCH=wasm, it installs a global object pego whose methods take a request
// encoded as JSON and return a response encoded as JSON:
//
//	pego.compile(req)  -> grammar info and diagnostics          (req: {grammar})
//	pego.parse(req)    -> tree as JSON and S-expression, errors (req: parseRequest)
//	pego.format(req)   -> formatted grammar                     (req: {grammar})
//	pego.generate(req) -> generated Go or TypeScript code       (req: generateRequest)
//	pego.version(req)  -> versions of Go and of this module
//
// Requests and responses are plain JSON so that the API is easy to call from a Web Worker and from
// Node, and so that it can be tested natively. Everything is implemented with the public pego and
// grammar packages only; see site/ for the page that uses it.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// diagnostic is an error in a grammar. Line and Col are 1-based (Col counts code points) and are 0
// when the error has no position.
type diagnostic struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Message string `json:"message"`
}

type ruleInfo struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"` // the declared type, if any
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type typeInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // struct, alias or terminal
	Spec string `json:"spec"` // the right-hand side of the definition
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type compileResult struct {
	OK          bool         `json:"ok"`
	Package     string       `json:"package,omitempty"`
	Rules       []ruleInfo   `json:"rules"`
	Types       []typeInfo   `json:"types"`
	Start       string       `json:"start,omitempty"` // the start rule used when none is requested
	Diagnostics []diagnostic `json:"diagnostics"`
}

type grammarRequest struct {
	Grammar string `json:"grammar"`
}

type parseRequest struct {
	Grammar   string `json:"grammar"`
	Input     string `json:"input"`
	Start     string `json:"start"`     // start rule; empty selects the default (main, or else the first rule)
	Unit      string `json:"unit"`      // codepoints (default) or bytes
	Backend   string `json:"backend"`   // closure, bytecode, bytecode-iterative, or empty for the default
	Recognize bool   `json:"recognize"` // check the input without building a tree
	// SourceInfo adds the rule of each node and the names of captures to the tree, as
	// pego parse -source-info does.
	SourceInfo bool `json:"sourceInfo,omitempty"`
	// DefaultValue drops value actions and declared rule types, so the tree is the default node
	// tree, as pego parse -default-value does. Predicates still run.
	DefaultValue bool `json:"defaultValue,omitempty"`
	// MaxDepth limits the nesting of rule calls (pego.WithMaxDepth); 0 selects the playground's
	// limit, maxDepth, for the backends that nest rule calls on the stack.
	MaxDepth int `json:"maxDepth,omitempty"`
	// Runs is the number of times the parse is timed (Micros is then the median); 0 or 1 parses once.
	// It is limited to maxParseRuns.
	Runs int `json:"runs,omitempty"`
}

// syntaxError mirrors pego.SyntaxError. Pos is in the unit of the parse; Message is the description
// without the position, as printed by the pego command.
type syntaxError struct {
	Pos      int      `json:"pos"`
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Expected []string `json:"expected,omitempty"`
	Messages []string `json:"messages,omitempty"`
	Message  string   `json:"message"`
}

type parseResult struct {
	Compile compileResult `json:"compile"`
	Start   string        `json:"start,omitempty"` // the start rule used; empty if the requested one is not defined
	// Matched reports that the input matched, possibly after recovering from errors.
	Matched bool `json:"matched"`
	// JSON is the tree exactly as pego parse prints it, and SExpr as pego parse -f sexpr prints it
	// (without the final newline). Both are empty in recognition mode and when the input does not match.
	JSON  string `json:"json,omitempty"`
	SExpr string `json:"sexpr,omitempty"`
	Nodes int    `json:"nodes,omitempty"` // number of nodes in the tree
	Depth int    `json:"depth,omitempty"` // how deeply the nodes are nested
	// Errors are the syntax errors: the one that stopped the parse, or those recovered from with
	// #recover (Recovered is then true and the tree is still returned).
	Errors    []syntaxError `json:"errors,omitempty"`
	Recovered bool          `json:"recovered,omitempty"`
	// Error is any other error, such as a runtime error in an action or an invalid option.
	Error string `json:"error,omitempty"`
	// Micros is the time the parse took, in microseconds: the median of the Runs timed parses.
	Micros int64 `json:"micros"`
	// Runs is the number of parses that Micros is measured over.
	Runs int `json:"runs,omitempty"`
}

// maxParseRuns bounds the parses that one request times, so that a comparison stays responsive.
const maxParseRuns = 100

type formatResult struct {
	Formatted   string       `json:"formatted,omitempty"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type generateRequest struct {
	Language  string `json:"language,omitempty"` // go (default) or typescript
	Grammar   string `json:"grammar"`
	Package   string `json:"package"`
	Start     string `json:"start"`
	Types     bool   `json:"types"`
	Recognize bool   `json:"recognize"`
}

type generateResult struct {
	// Code is the generated files one after another, each under a banner when there are several,
	// for display. Files holds them separately, to be saved under their names.
	Code        string          `json:"code,omitempty"`
	Files       []generatedFile `json:"files,omitempty"`
	Diagnostics []diagnostic    `json:"diagnostics"`
}

type generatedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type versionResult struct {
	Go       string `json:"go"`
	Module   string `json:"module"`
	Revision string `json:"revision,omitempty"`
	Modified bool   `json:"modified,omitempty"`
}

// handle runs the method with the JSON request req and returns the JSON response. A panic is turned
// into an error response so that a bug in a parse does not stop the WebAssembly program.
func handle(method string, req []byte) (resp []byte) {
	defer func() {
		if r := recover(); r != nil {
			resp = errorResponse(fmt.Errorf("internal error: %v", r))
		}
	}()
	var v any
	var err error
	switch method {
	case "compile":
		var r grammarRequest
		if err = decode(req, &r); err == nil {
			v = compileInfo(r.Grammar)
		}
	case "parse":
		var r parseRequest
		if err = decode(req, &r); err == nil {
			v = parse(r)
		}
	case "format":
		var r grammarRequest
		if err = decode(req, &r); err == nil {
			v = format(r.Grammar)
		}
	case "generate":
		var r generateRequest
		if err = decode(req, &r); err == nil {
			v = generate(r)
		}
	case "version":
		v = version()
	default:
		err = fmt.Errorf("unknown method %q", method)
	}
	if err != nil {
		return errorResponse(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return errorResponse(err)
	}
	return b
}

func decode(req []byte, v any) error {
	if len(bytes.TrimSpace(req)) == 0 {
		return nil
	}
	if err := json.Unmarshal(req, v); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

func errorResponse(err error) []byte {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return b
}

// Limits that keep a parse and the display of its result within the stack. Natively, the defaults
// of the engine apply, and trees are limited by encoding/json, which refuses to nest more than 10,000
// levels (two per node level). main_js.go lowers both for WebAssembly.
var (
	maxDepth     = 0    // nesting of rule calls (pego.WithMaxDepth); 0 is the engine's default
	maxTreeDepth = 4000 // nesting of nodes in a tree that is encoded and displayed
)

// compiled is the result of compiling one grammar source. The last one is kept, so that typing in the
// input does not compile the grammar again.
type compiled struct {
	src    string
	g      *grammar.Grammar
	info   compileResult
	parser *pego.Parser // nil if the grammar has errors
	// variants are the parsers compiled with options, compiled when first requested.
	variants map[compileVariant]*pego.Parser
}

// compileVariant is the set of compile options a parse request can ask for.
type compileVariant struct{ sourceInfo, defaultValue bool }

// variantParser returns the parser of c compiled with the options of v.
func (c *compiled) variantParser(v compileVariant) (*pego.Parser, error) {
	if p := c.variants[v]; p != nil {
		return p, nil
	}
	var opts []pego.CompileOption
	if v.sourceInfo {
		opts = append(opts, pego.WithSourceInfo())
	}
	if v.defaultValue {
		opts = append(opts, pego.WithDefaultValue())
	}
	p, err := pego.Compile(c.g, c.info.Start, opts...)
	if err != nil {
		return nil, err
	}
	if c.variants == nil {
		c.variants = map[compileVariant]*pego.Parser{}
	}
	c.variants[v] = p
	return p, nil
}

var last *compiled

func compileSource(src string) *compiled {
	if last != nil && last.src == src {
		return last
	}
	c := &compiled{src: src}
	c.info.Rules, c.info.Types, c.info.Diagnostics = []ruleInfo{}, []typeInfo{}, []diagnostic{}
	g, err := pego.ParseGrammar(src)
	if err != nil {
		c.info.Diagnostics = diagnostics(err)
		last = c
		return c
	}
	c.g = g
	c.info.Package = g.File.Pkg.Name
	for _, r := range g.Rules() {
		ri := ruleInfo{Name: r.Name, Line: r.Pos.Line, Col: r.Pos.Col}
		if r.Type != nil {
			ri.Type = grammar.FormatType(r.Type)
		}
		c.info.Rules = append(c.info.Rules, ri)
	}
	for _, t := range g.Types() {
		c.info.Types = append(c.info.Types, describeType(t))
	}
	c.info.Start = defaultStart(c.info.Rules)
	if c.info.Start == "" {
		c.info.Diagnostics = []diagnostic{{Message: "the grammar defines no rules"}}
		last = c
		return c
	}
	p, err := pego.Compile(g, c.info.Start)
	if err != nil {
		c.info.Diagnostics = diagnostics(err)
	} else {
		c.parser = p
		c.info.OK = true
	}
	last = c
	return c
}

func compileInfo(src string) compileResult { return compileSource(src).info }

// defaultStart returns main if the grammar defines it, and otherwise its first rule, as the start rule.
func defaultStart(rules []ruleInfo) string {
	if len(rules) == 0 {
		return ""
	}
	for _, r := range rules {
		if r.Name == "main" {
			return "main"
		}
	}
	return rules[0].Name
}

func describeType(t grammar.TypeDecl) typeInfo {
	pos := t.Diagnostic()
	ti := typeInfo{Name: t.DeclName(), Line: pos.Line, Col: pos.Col}
	switch s := t.(type) {
	case *grammar.TypeDef:
		if s.Struct() == nil {
			ti.Kind = "node"
			ti.Spec = grammar.FormatType(s.Type)
			break
		}
		ti.Kind = "struct"
		fields := make([]string, len(s.Fields()))
		for i, f := range s.Fields() {
			fields[i] = f.Name + " " + grammar.FormatType(f.Type)
		}
		ti.Spec = "{ " + strings.Join(fields, "; ") + " }"
		if len(fields) == 0 {
			ti.Spec = "{}"
		}
	case *grammar.TypeAlias:
		ti.Kind = "alias"
		ti.Spec = grammar.FormatType(s.Type)
	}
	return ti
}

// posPrefix matches the position that grammar and compile errors start with.
var posPrefix = regexp.MustCompile(`^(\d+):(\d+): `)

// diagnostics splits a grammar or compile error into one diagnostic per line. The pego package reports
// several errors as lines of the form "line:col: message" (or "message" without a position).
func diagnostics(err error) []diagnostic {
	var ds []diagnostic
	for l := range strings.SplitSeq(err.Error(), "\n") {
		if l == "" {
			continue
		}
		d := diagnostic{Message: l}
		if m := posPrefix.FindStringSubmatch(l); m != nil {
			d.Line, _ = strconv.Atoi(m[1])
			d.Col, _ = strconv.Atoi(m[2])
			d.Message = l[len(m[0]):]
		}
		ds = append(ds, d)
	}
	return ds
}

func parse(req parseRequest) parseResult {
	c := compileSource(req.Grammar)
	res := parseResult{Compile: c.info}
	if c.parser == nil {
		return res
	}
	var opts []pego.ParseOption
	switch req.Unit {
	case "", "codepoints":
	case "bytes":
		opts = append(opts, pego.WithUnit(pego.Bytes))
	default:
		res.Error = fmt.Sprintf("unknown unit %q", req.Unit)
		return res
	}
	switch req.Backend {
	case "":
	case "closure":
		opts = append(opts, pego.WithBackend(pego.Closure))
	case "bytecode":
		opts = append(opts, pego.WithBackend(pego.Bytecode))
	case "bytecode-iterative":
		opts = append(opts, pego.WithBackend(pego.BytecodeIterative))
	default:
		res.Error = fmt.Sprintf("unknown backend %q", req.Backend)
		return res
	}
	if req.Recognize {
		opts = append(opts, pego.RecognizeOnly())
	}
	// The iterative backend keeps rule calls on its own stack, so only the others need maxDepth.
	if d := req.MaxDepth; d > 0 {
		opts = append(opts, pego.WithMaxDepth(d))
	} else if maxDepth > 0 && req.Backend != "bytecode-iterative" {
		opts = append(opts, pego.WithMaxDepth(maxDepth))
	}
	p := c.parser
	if req.SourceInfo || req.DefaultValue {
		var err error
		if p, err = c.variantParser(compileVariant{req.SourceInfo, req.DefaultValue}); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	if req.Start != "" && req.Start != p.Start() {
		var err error
		if p, err = p.WithStart(req.Start); err != nil {
			res.Error = err.Error() // start rule X is not defined
			return res
		}
	}
	res.Start = p.Start()

	// The first run provides the result; the others only add timings.
	runs := min(max(req.Runs, 1), maxParseRuns)
	t0 := time.Now()
	node, err := p.Parse(req.Input, opts...)
	times := []int64{time.Since(t0).Microseconds()}
	for range runs - 1 {
		t0 := time.Now()
		_, _ = p.Parse(req.Input, opts...)
		times = append(times, time.Since(t0).Microseconds())
	}
	res.Micros = medianMicros(times)
	res.Runs = len(times)

	// The input matched if the parse succeeded, possibly after recovering from errors. A start rule
	// that produces no value (a discarded match) returns a nil node and no error.
	res.Matched = !isFailure(err)
	if node != nil {
		res.Nodes, res.Depth = measureTree(node)
	}
	if node != nil && res.Depth > maxTreeDepth {
		// Encoding the tree recurses once per level, and so does the page that shows it.
		res.Error = fmt.Sprintf("the tree is nested %d levels deep, more than the %d levels the playground can show", res.Depth, maxTreeDepth)
		node = nil
	}
	if node != nil {
		// Encode exactly like pego parse: an indented json.Encoder, without the final newline.
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(node); err != nil {
			res.Error = err.Error()
			return res
		}
		res.JSON = strings.TrimSuffix(b.String(), "\n")
		res.SExpr = node.String()
	}
	if err != nil {
		var list pego.SyntaxErrors
		var one *pego.SyntaxError
		switch {
		case errors.As(err, &list):
			for _, e := range list {
				res.Errors = append(res.Errors, convertError(e))
			}
			res.Recovered = res.Matched
		case errors.As(err, &one):
			res.Errors = []syntaxError{convertError(one)}
		default:
			res.Error = err.Error()
		}
	}
	return res
}

// medianMicros returns the median of the non-empty list of durations, the lower one of the two middle
// values when the count is even.
func medianMicros(times []int64) int64 {
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	return sorted[(len(sorted)-1)/2]
}

// isFailure reports whether err means that the input did not match. A parse that recovered from
// errors returns SyntaxErrors and still matches.
func isFailure(err error) bool {
	if err == nil {
		return false
	}
	var list pego.SyntaxErrors
	return !errors.As(err, &list)
}

func convertError(e *pego.SyntaxError) syntaxError {
	return syntaxError{
		Pos: e.Pos, Line: e.Line, Col: e.Col,
		Expected: e.Expected, Messages: e.Messages, Message: e.Message(),
	}
}

// measureTree returns the number of nodes of the tree n and how deeply they are nested (1 for a
// single node). It does not recurse, so that it works on trees of any depth.
func measureTree(n *pego.Node) (count, depth int) {
	type item struct {
		n     *pego.Node
		depth int
	}
	stack := []item{{n, 1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		depth = max(depth, it.depth)
		for _, c := range it.n.Elems() {
			if c != nil {
				stack = append(stack, item{c, it.depth + 1})
			}
		}
		for _, v := range it.n.Fields() {
			if c, ok := v.(*pego.Node); ok && c != nil {
				stack = append(stack, item{c, it.depth + 1})
			}
		}
	}
	return count, depth
}

func format(src string) formatResult {
	res := formatResult{Diagnostics: []diagnostic{}}
	g, err := pego.ParseGrammar(src)
	if err != nil {
		res.Diagnostics = diagnostics(err)
		return res
	}
	res.Formatted = grammar.Format(g)
	return res
}

func generate(req generateRequest) generateResult {
	res := generateResult{Diagnostics: []diagnostic{}}
	c := compileSource(req.Grammar)
	if c.g == nil {
		res.Diagnostics = c.info.Diagnostics
		return res
	}
	pkg := req.Package
	if pkg == "" {
		pkg = "parser"
	}
	start := req.Start
	if start == "" {
		start = c.info.Start
	} else if !slices.ContainsFunc(c.info.Rules, func(r ruleInfo) bool { return r.Name == start }) {
		res.Diagnostics = []diagnostic{{Message: fmt.Sprintf("start rule %s is not defined", start)}}
		return res
	}
	var opts []pego.GenOption
	if req.Types {
		opts = append(opts, pego.WithTypes())
	}
	if req.Recognize {
		opts = append(opts, pego.WithRecognize())
	}
	var files []pego.GeneratedFile
	var err error
	switch req.Language {
	case "", "go":
		// The runtime is imported, so the files shown are the generated package without a copy of it.
		opts = append(opts, pego.WithRuntime(pego.RuntimeImport), pego.WithPackageName(pkg))
		files, err = pego.GenerateGo(c.g, "example.com/"+pkg, start, opts...)
	case "typescript":
		files, err = pego.GenerateTypeScript(c.g, start, opts...)
	default:
		res.Diagnostics = []diagnostic{{Message: fmt.Sprintf("unknown generation language %q", req.Language)}}
		return res
	}
	if err != nil {
		res.Diagnostics = diagnostics(err)
		return res
	}
	var code strings.Builder
	for _, f := range files {
		res.Files = append(res.Files, generatedFile{f.Path, string(f.Content)})
	}
	// The runtime is long and the same for every grammar: display it after the files of the grammar.
	shown := slices.Clone(files)
	slices.SortStableFunc(shown, func(a, b pego.GeneratedFile) int {
		return cmp.Compare(btoi(a.Path == "runtime.ts"), btoi(b.Path == "runtime.ts"))
	})
	for i, f := range shown {
		if len(shown) > 1 {
			if i > 0 {
				code.WriteString("\n")
			}
			fmt.Fprintf(&code, "// ===== %s =====\n\n", f.Path)
		}
		code.Write(f.Content)
	}
	res.Code = code.String()
	return res
}

func version() versionResult {
	v := versionResult{Go: runtime.Version(), Module: "(devel)"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	if info.Main.Version != "" {
		v.Module = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			v.Revision = s.Value
		case "vcs.modified":
			v.Modified = s.Value == "true"
		}
	}
	return v
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
