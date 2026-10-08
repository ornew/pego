// Command playground is the WebAssembly half of the PEGO web playground.
//
// Built with GOOS=js GOARCH=wasm, it installs a global object pego whose methods take a request
// encoded as JSON and return a response encoded as JSON:
//
//	pego.compile(req)  -> grammar info and diagnostics          (req: {grammar})
//	pego.parse(req)    -> tree as JSON and S-expression, errors (req: parseRequest)
//	pego.format(req)   -> formatted grammar                     (req: {grammar})
//	pego.generate(req) -> generated Go code                     (req: generateRequest)
//	pego.version(req)  -> versions of Go and of this module
//
// Requests and responses are plain JSON so that the API is easy to call from a Web Worker and from
// Node, and so that it can be tested natively. Everything is implemented with the public pego and
// grammar packages only; see site/ for the page that uses it.
package main

import (
	"bytes"
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
	Start     string `json:"start"`     // start rule; empty or unknown selects the default
	Unit      string `json:"unit"`      // codepoints (default) or bytes
	Backend   string `json:"backend"`   // closure, bytecode, bytecode-iterative, or empty for the default
	Recognize bool   `json:"recognize"` // check the input without building a tree
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
	Start   string        `json:"start,omitempty"` // the start rule used
	// Matched reports that the input matched, possibly after recovering from errors.
	Matched bool `json:"matched"`
	// JSON is the tree exactly as pego parse prints it, and SExpr as pego parse -f sexpr prints it
	// (without the final newline). Both are empty in recognition mode and when the input does not match.
	JSON  string `json:"json,omitempty"`
	SExpr string `json:"sexpr,omitempty"`
	Nodes int    `json:"nodes,omitempty"` // number of nodes in the tree
	// Errors are the syntax errors: the one that stopped the parse, or those recovered from with
	// #recover (Recovered is then true and the tree is still returned).
	Errors    []syntaxError `json:"errors,omitempty"`
	Recovered bool          `json:"recovered,omitempty"`
	// Error is any other error, such as a runtime error in an action or an invalid option.
	Error string `json:"error,omitempty"`
	// Micros is the time the parse took, in microseconds.
	Micros int64 `json:"micros"`
}

type formatResult struct {
	Formatted   string       `json:"formatted,omitempty"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type generateRequest struct {
	Grammar   string `json:"grammar"`
	Package   string `json:"package"`
	Start     string `json:"start"`
	Types     bool   `json:"types"`
	Recognize bool   `json:"recognize"`
}

type generateResult struct {
	Code        string       `json:"code,omitempty"`
	Diagnostics []diagnostic `json:"diagnostics"`
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

// compiled is the result of compiling one grammar source. The last one is kept, so that typing in the
// input does not compile the grammar again.
type compiled struct {
	src    string
	g      *grammar.Grammar
	info   compileResult
	parser *pego.Parser // nil if the grammar has errors
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
	c.info.Package = g.Package
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

func describeType(t *grammar.TypeDef) typeInfo {
	ti := typeInfo{Name: t.Name, Line: t.Pos.Line, Col: t.Pos.Col}
	switch s := t.Spec.(type) {
	case *grammar.StructSpec:
		ti.Kind = "struct"
		fields := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			fields[i] = f.Name + " " + grammar.FormatType(f.Type)
		}
		ti.Spec = "struct { " + strings.Join(fields, ", ") + " }"
		if len(fields) == 0 {
			ti.Spec = "struct {}"
		}
	case *grammar.AliasSpec:
		ti.Kind = "alias"
		ti.Spec = grammar.FormatType(s.Type)
	case *grammar.TerminalSpec:
		ti.Kind = "terminal"
		ti.Spec = "terminal"
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
	p := c.parser
	if req.Start != "" && req.Start != p.Start() && slices.ContainsFunc(c.info.Rules, func(r ruleInfo) bool { return r.Name == req.Start }) {
		var err error
		if p, err = p.WithStart(req.Start); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	res.Start = p.Start()

	t0 := time.Now()
	node, err := p.Parse(req.Input, opts...)
	res.Micros = time.Since(t0).Microseconds()

	// The input matched if the parse succeeded, possibly after recovering from errors. A start rule
	// that produces no value (a discarded match) returns a nil node and no error.
	res.Matched = !isFailure(err)
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
		res.Nodes = countNodes(node)
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

func countNodes(n *pego.Node) int {
	if n == nil {
		return 0
	}
	count := 1
	for _, c := range n.Children {
		count += countNodes(c)
	}
	for _, f := range n.Fields {
		if c, ok := f.Value.(*pego.Node); ok {
			count += countNodes(c)
		}
	}
	return count
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
	if !slices.ContainsFunc(c.info.Rules, func(r ruleInfo) bool { return r.Name == start }) {
		start = c.info.Start
	}
	var opts []pego.GenOption
	if req.Types {
		opts = append(opts, pego.WithTypes())
	}
	if req.Recognize {
		opts = append(opts, pego.WithRecognize())
	}
	code, err := pego.GenerateGo(c.g, pkg, start, opts...)
	if err != nil {
		res.Diagnostics = diagnostics(err)
		return res
	}
	res.Code = string(code)
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
