package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/ornew/pego/grammar"
)

// The compiled grammar format (.pegoc), specified in the File format section of
// spec/bytecode.md.
//
//	magic    "PEGOC\x00"
//	version  1 byte (compiledVersion)
//	payload  version 3: version 2 prefix, optional length-delimited recognition bundle, optional AST
//	         version 2: instruction set version, string table, start rule, package name, module, AST (optional)
//	crc32    CRC-32 of magic through payload (IEEE, 4 bytes little-endian)
//
// Integers are variable-length (signed ones zigzag-encoded), and strings are referenced by their
// index in the string table. The version 2 string table starts with the module's string table,
// followed by the AST strings and others. Recognition bundles have independent string tables. AST expressions and value expressions are written as
// one byte for the kind followed by their elements in preorder. Rule definitions store the
// results of static analysis (memoization, left-recursion leaders, position dependence). Loading
// skips parsing, static analysis, type checking, and compilation to bytecode.

const (
	compiledMagic   = "PEGOC\x00"
	compiledVersion = 3
	// isaVersion is the instruction set of the value model, the only one supported. Earlier
	// versions built the previous syntax trees, so their files are not loaded and need to be
	// compiled again.
	isaVersion = 25
	maxDepth   = 10000
)

// unsupportedISA is the error for a module of instruction set isa.
func unsupportedISA(isa int) error {
	if isa < isaVersion {
		return fmt.Errorf("unsupported instruction set version %d: the file was compiled by an older pego; compile the grammar again", isa)
	}
	return fmt.Errorf("unsupported instruction set version %d", isa)
}

// ErrNotCompiled reports that the data is not a compiled grammar.
var ErrNotCompiled = errors.New("not a compiled PEGO grammar")

// IsCompiled reports whether the data is in the compiled grammar format.
func IsCompiled(data []byte) bool { return bytes.HasPrefix(data, []byte(compiledMagic)) }

// Expression kinds
const (
	xRef byte = iota + 1
	xLiteral
	xClass
	xAny
	xSeq
	xChoice
	xRepeat
	xOptional
	xAnd
	xNot
	xAtomic
	xDiscard
	xCapture
	xCut
	xTop
	xBottom
	xBeginInput
	xEndInput
	xBeginLine
	xEndLine
	xPredicate
	xAttributed
	xOperatorsRule
	xInlineAction
)

// Value expression kinds
const (
	tInt byte = iota + 1
	tString
	tBool
	tNil
	tCapture
	tIndex
	tVar
	tMember
	tNew
	tCall
	tLambda
	tBinary
	tUnary
	tBind
	tListConstructor
	tRetiredTerminalConstructor // a terminal constructor, before instruction set 14
	tParam                      // a reference to a lambda parameter, a CaptureRef with Param set
	tCast
	tConvert // a node type constructor, T{x}
	tTuple   // a tuple literal, (x, y)
)

// Type and statement kinds
const (
	kTypeRef byte = iota + 1
	kList
	kOptional
	kJoin
	kStruct
	kAlias
	kRetiredTerminal // a terminal type, before instruction set 14
	kTypeDef
	kRuleDef
	kMeet
	kTypeApp
	kNode  // a named node type definition, type T = node[U]
	kTuple // a tuple type (A, B, ...)
	kVarDef
)

type encoder struct {
	body bytes.Buffer
	strs map[string]int
	list []string
	// positions writes the grammar position of a cast, which only a module with source
	// information uses; otherwise compiled bytes would depend on the layout of the source.
	positions bool
}

func (w *encoder) byte(b byte) { w.body.WriteByte(b) }
func (w *encoder) uint(n int) {
	w.body.Write(binary.AppendUvarint(nil, uint64(n)))
}
func (w *encoder) int(n int) {
	w.body.Write(binary.AppendVarint(nil, int64(n)))
}
func (w *encoder) bool(b bool) {
	if b {
		w.byte(1)
	} else {
		w.byte(0)
	}
}
func (w *encoder) str(s string) {
	i, ok := w.strs[s]
	if !ok {
		i = len(w.list)
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.uint(i)
}

// MarshalOptions configures saving.
type MarshalOptions struct {
	// OmitAST omits the grammar's AST. A file without it can only be run by the bytecode backend.
	OmitAST bool
}

// MarshalBinary encodes the compiled grammar as bytes. start is the default start rule (may be
// empty).
func (prog *Program) MarshalBinary(start string) ([]byte, error) {
	return prog.MarshalBinaryWith(start, MarshalOptions{})
}

// MarshalBinaryWith encodes the compiled grammar as bytes with the given options.
func (prog *Program) MarshalBinaryWith(start string, o MarshalOptions) ([]byte, error) {
	if prog.hasMissing {
		return nil, missingUnsupported("compiled grammar serialization")
	}
	if start != "" {
		if err := EntryError(start); err != nil {
			return nil, fmt.Errorf("start rule %s is hidden: a rule whose name begins with an underscore is not an entry point", start)
		}
	}
	if start != "" && !prog.hasRule(start) {
		return nil, fmt.Errorf("start rule %s is not defined", start)
	}
	m := prog.Module()
	withAST := prog.Grammar != nil && !o.OmitAST
	var recognition *Program
	if !withAST && (prog.Grammar != nil || prog.recognitionModule != nil) {
		var err error
		recognition, err = prog.recognizer()
		if err != nil {
			// Preserve the legacy writer's ability to serialize an already-compiled
			// module whose AST has since been mutated into a forbidden stream-recovery
			// shape. Such bytes remain invalid when loaded; don't let constructing an
			// optional recognition bundle change that behavior.
			if validateStreamRecovery(m.Code) == nil {
				return nil, err
			}
		}
	}
	// Legacy AST-less files cannot acquire recognition without recompilation.
	version := compiledVersion
	if recognition == nil {
		version = 2
	}
	w := &encoder{strs: map[string]int{}, positions: prog.sourceInfo}
	for i, s := range m.Strings { // put the module's string table first
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	pkg := prog.pkg
	if prog.Grammar != nil {
		pkg = prog.Grammar.File.Pkg.Name
	}
	w.str(pkg)
	w.module(m)
	if version >= 3 {
		w.bool(recognition != nil)
		if recognition != nil {
			var bundle []byte
			if prog.recognitionModule == nil {
				prog.recognitionBytesOnce.Do(func() {
					prog.recognitionBytes = encodeRecognition(recognition)
				})
				bundle = prog.recognitionBytes
			} else {
				// A loaded module can be changed by its owner between saves.
				bundle = encodeRecognition(recognition)
			}
			w.uint(len(bundle))
			w.body.Write(bundle)
		}
	}
	w.bool(withAST)
	if withAST {
		w.statements(prog)
	}
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(byte(version))
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	return out.Bytes(), nil
}

// finish writes the string table and the body to out and appends the checksum.
func (w *encoder) finish(out *bytes.Buffer) {
	out.Write(binary.AppendUvarint(nil, uint64(len(w.list))))
	for _, s := range w.list {
		out.Write(binary.AppendUvarint(nil, uint64(len(s))))
		out.WriteString(s)
	}
	out.Write(w.body.Bytes())
	out.Write(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(out.Bytes())))
}

// statements writes the statements (type and rule definitions) and the static analysis results.
func (w *encoder) statements(prog *Program) {
	source := prog.SourceGrammar()
	w.uint(len(source.File.Decls))
	for _, st := range source.File.Decls {
		switch st := st.(type) {
		case grammar.TypeDecl:
			w.byte(kTypeDef)
			w.str(st.DeclName())
			w.typeDecl(st)
		case *grammar.VarDef:
			w.byte(kVarDef)
			w.str(st.Name)
			w.optType(st.Type)
			w.optTerm(st.Value)
		case *grammar.RuleDef:
			w.byte(kRuleDef)
			w.str(st.Name)
			w.optType(st.Type)
			w.expr(st.Expr)
			w.optTerm(st.Action)
		}
	}
	// The flags are those of the rules of the program, which include the hidden rules that inline
	// actions are lowered to; the reader lowers the statements in the same way.
	w.uint(len(prog.rules))
	for _, r := range prog.rules {
		w.bool(r.memo)
		w.bool(r.leader)
		w.bool(r.positional)
		w.bool(r.twinOK)
	}
}

// typeDecl writes the kind and the type of a type declaration; its name is written before.
func (w *encoder) typeDecl(s grammar.TypeDecl) {
	switch s := s.(type) {
	case *grammar.TypeDef:
		if s.Struct() == nil {
			w.byte(kNode)
			w.typeExpr(s.Type)
			return
		}
		w.byte(kStruct)
		w.uint(len(defFields(s)))
		for _, f := range defFields(s) {
			w.str(f.Name)
			w.typeExpr(f.Type)
		}
	case *grammar.TypeAlias:
		w.byte(kAlias)
		w.typeExpr(s.Type)
	}
}

func (w *encoder) optType(t grammar.TypeExpr) {
	w.bool(t != nil)
	if t != nil {
		w.typeExpr(t)
	}
}

func (w *encoder) typeExpr(t grammar.TypeExpr) {
	switch t := t.(type) {
	case nil:
		w.byte(kTypeRef)
		w.str("_")
	case *grammar.TypeRef:
		w.byte(kTypeRef)
		w.str(t.Name)
	case *grammar.ListType:
		w.byte(kList)
		w.typeExpr(t.Elem)
	case *grammar.OptionalType:
		w.byte(kOptional)
		w.typeExpr(t.Elem)
	case *grammar.JoinType:
		w.byte(kJoin)
		w.uint(len(t.Types))
		for _, x := range t.Types {
			w.typeExpr(x)
		}
	case *grammar.MeetType:
		w.byte(kMeet)
		w.uint(len(t.Types))
		for _, x := range t.Types {
			w.typeExpr(x)
		}
	case *grammar.TypeApp:
		w.byte(kTypeApp)
		w.str(t.Name)
		w.uint(len(t.Args))
		for _, x := range t.Args {
			w.typeExpr(x)
		}
	case *grammar.TupleType:
		w.byte(kTuple)
		w.uint(len(t.Types))
		for _, x := range t.Types {
			w.typeExpr(x)
		}
	case *grammar.TypeGroup:
		// Parentheses only keep source layout, which compiled grammars do not.
		w.typeExpr(t.Type)
	default:
		// Compilation rejects the other forms (unnamed struct types) for now.
		panic(fmt.Sprintf("compiled grammar: unsupported type expression %T", t))
	}
}

func (w *encoder) exprs(es []grammar.Expr) {
	w.uint(len(es))
	for _, e := range es {
		w.expr(e)
	}
}

func (w *encoder) optExpr(e grammar.Expr) {
	w.bool(e != nil)
	if e != nil {
		w.expr(e)
	}
}

func (w *encoder) expr(e grammar.Expr) {
	switch e := e.(type) {
	case *grammar.Ref:
		w.byte(xRef)
		w.str(e.Name)
		w.str(e.Level)
	case *grammar.Literal:
		w.byte(xLiteral)
		w.str(e.Value)
	case *grammar.CharClass:
		w.byte(xClass)
		w.bool(e.Negated)
		w.uint(len(e.Ranges))
		for _, r := range e.Ranges {
			w.int(int(r.Lo))
			w.int(int(r.Hi))
		}
	case *grammar.Any:
		w.byte(xAny)
	case *grammar.Seq:
		w.byte(xSeq)
		w.exprs(e.Items)
	case *grammar.InlineAction:
		w.byte(xInlineAction)
		w.expr(e.Expr)
		w.bool(e.DefaultAction)
		w.optTerm(e.Action)
	case *grammar.Choice:
		w.byte(xChoice)
		w.exprs(e.Alts)
	case *grammar.Repeat:
		w.byte(xRepeat)
		w.int(e.Min)
		w.int(e.Max)
		w.expr(e.Expr)
	case *grammar.Optional:
		w.byte(xOptional)
		w.expr(e.Expr)
	case *grammar.And:
		w.byte(xAnd)
		w.expr(e.Expr)
	case *grammar.Not:
		w.byte(xNot)
		w.expr(e.Expr)
	case *grammar.Atomic:
		w.byte(xAtomic)
		w.expr(e.Expr)
	case *grammar.Discard:
		w.byte(xDiscard)
		w.expr(e.Expr)
	case *grammar.Capture:
		w.byte(xCapture)
		w.str(e.Name)
		w.expr(e.Expr)
	case *grammar.Cut:
		w.byte(xCut)
	case *grammar.Top:
		w.byte(xTop)
	case *grammar.Bottom:
		w.byte(xBottom)
	case *grammar.BeginInput:
		w.byte(xBeginInput)
	case *grammar.EndInput:
		w.byte(xEndInput)
	case *grammar.BeginLine:
		w.byte(xBeginLine)
	case *grammar.EndLine:
		w.byte(xEndLine)
	case *grammar.Predicate:
		w.byte(xPredicate)
		w.term(e.Term)
	case *grammar.Attributed:
		w.byte(xAttributed)
		w.expr(e.Expr)
		w.uint(len(e.Attrs))
		for _, a := range e.Attrs {
			w.str(a.Name)
			w.uint(len(a.KwArgs))
			keys := make([]string, 0, len(a.KwArgs))
			for key := range a.KwArgs {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, name := range keys {
				w.str(name)
				value, _ := a.Arg(name)
				w.expr(value)
			}
		}
	case *grammar.OperatorsRule:
		w.byte(xOperatorsRule)
		w.optExpr(e.Skip)
		w.uint(len(e.Operands))
		for _, o := range e.Operands {
			w.expr(o.Expr)
			w.optTerm(o.Action)
		}
		w.uint(len(e.Levels))
		for _, l := range e.Levels {
			w.str(l.Name)
			w.uint(len(l.Operators))
			for _, op := range l.Operators {
				w.str(op.Kind)
				w.str(op.Assoc)
				w.expr(op.Expr)
				w.optTerm(op.Action)
			}
		}
	}
}

func (w *encoder) optTerm(t grammar.Term) {
	w.bool(t != nil)
	if t != nil {
		w.term(t)
	}
}

func (w *encoder) terms(ts []grammar.Term) {
	w.uint(len(ts))
	for _, t := range ts {
		w.term(t)
	}
}

func (w *encoder) term(t grammar.Term) {
	switch t := t.(type) {
	case *grammar.IntLit:
		w.byte(tInt)
		w.int(t.Value)
	case *grammar.StringLit:
		w.byte(tString)
		w.str(t.Value)
	case *grammar.BoolLit:
		w.byte(tBool)
		w.bool(t.Value)
	case *grammar.NilLit:
		w.byte(tNil)
	case *grammar.CaptureRef:
		if t.Param {
			w.byte(tParam)
		} else {
			w.byte(tCapture)
		}
		w.str(t.Name)
	case *grammar.IndexRef:
		w.byte(tIndex)
		w.uint(t.Index)
	case *grammar.VarRef:
		w.byte(tVar)
		w.str(t.Name)
	case *grammar.Member:
		w.byte(tMember)
		w.term(t.X)
		w.str(t.Name)
	case *grammar.New:
		if len(t.Args) != 0 {
			w.byte(tConvert)
			w.str(t.Type)
			w.terms(t.Args)
			return
		}
		w.byte(tNew)
		w.str(t.Type)
		w.uint(len(t.Fields))
		for _, f := range t.Fields {
			w.str(f.Name)
			w.term(f.Value)
		}
	case *grammar.Call:
		w.byte(tCall)
		w.str(t.Func)
		w.terms(t.Args)
	case *grammar.TupleLit:
		w.byte(tTuple)
		w.terms(t.Elems)
	case *grammar.ListConstructor:
		w.byte(tListConstructor)
		w.typeExpr(t.Elem)
		w.terms(t.Items)
	case *grammar.Lambda:
		w.byte(tLambda)
		w.uint(len(t.Params))
		for _, p := range t.Params {
			w.str(p)
		}
		w.term(t.Body)
	case *grammar.Binary:
		w.byte(tBinary)
		w.str(t.Op)
		w.term(t.L)
		w.term(t.R)
	case *grammar.Unary:
		w.byte(tUnary)
		w.str(t.Op)
		w.term(t.X)
	case *grammar.Cast:
		w.byte(tCast)
		w.term(t.X)
		w.typeExpr(t.Type)
		var pos grammar.Pos
		if w.positions {
			pos = t.Diagnostic()
		}
		w.int(pos.Line)
		w.int(pos.Col)
	case *grammar.Bind:
		w.byte(tBind)
		w.str(t.Name)
		w.term(t.Value)
	}
}

// decoder reads a compiled grammar. On invalid data it does not panic but records the first
// error.
type decoder struct {
	data  []byte
	pos   int
	strs  []string
	err   error
	depth int
}

func (r *decoder) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("invalid compiled grammar at byte %d: %s", r.pos, fmt.Sprintf(format, args...))
	}
}

func (r *decoder) byte() byte {
	if r.err != nil {
		return 0
	}
	if r.pos >= len(r.data) {
		r.fail("unexpected end of data")
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *decoder) uint() int {
	if r.err != nil {
		return 0
	}
	n, k := binary.Uvarint(r.data[r.pos:])
	if k <= 0 || n > 1<<31 {
		r.fail("invalid integer")
		return 0
	}
	r.pos += k
	return int(n)
}

// count reads an element count. A count larger than the remaining data is invalid (this prevents
// huge allocations).
func (r *decoder) count() int {
	n := r.uint()
	if n > len(r.data)-r.pos {
		r.fail("count %d exceeds data", n)
		return 0
	}
	return n
}

func (r *decoder) int() int {
	if r.err != nil {
		return 0
	}
	n, k := binary.Varint(r.data[r.pos:])
	if k <= 0 {
		r.fail("invalid integer")
		return 0
	}
	r.pos += k
	return int(n)
}

func (r *decoder) bool() bool {
	switch b := r.byte(); b {
	case 0:
		return false
	case 1:
		return true
	default:
		r.fail("invalid boolean %d", b)
		return false
	}
}

func (r *decoder) str() string {
	i := r.uint()
	if r.err != nil {
		return ""
	}
	if i >= len(r.strs) {
		r.fail("string index %d out of range", i)
		return ""
	}
	return r.strs[i]
}

func (r *decoder) enter() bool {
	r.depth++
	if r.depth > maxDepth {
		r.fail("nesting too deep")
	}
	return r.err == nil
}

func (r *decoder) leave() { r.depth-- }

// LoadProgram loads a compiled grammar. It also returns the saved default start rule (empty if
// none). It does not perform parsing, static analysis, type checking, or compilation to bytecode.
// A program loaded from a file without the AST can only be run by the bytecode backend.
func LoadProgram(data []byte, opts Options) (*Program, string, error) {
	if !IsCompiled(data) {
		return nil, "", ErrNotCompiled
	}
	if len(data) < len(compiledMagic)+1+4 {
		return nil, "", errors.New("invalid compiled grammar: too short")
	}
	v := data[len(compiledMagic)]
	if v != 2 && v != compiledVersion {
		return nil, "", fmt.Errorf("unsupported compiled grammar version %d (want %d)", v, compiledVersion)
	}
	body, sum := data[:len(data)-4], binary.LittleEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, "", errors.New("invalid compiled grammar: checksum mismatch")
	}
	r := &decoder{data: body, pos: len(compiledMagic) + 1}
	isa := r.uint()
	if r.err == nil && isa != isaVersion {
		return nil, "", unsupportedISA(isa)
	}
	r.strings()
	start := r.str()
	pkg := r.str()
	m := r.module()
	if r.err != nil {
		return nil, "", r.err
	}
	// Bundle entry checks index the main module's string table. Validate it
	// here so decodeRecognition can safely use it without validating it twice.
	if err := validateModule(m); err != nil {
		return nil, "", fmt.Errorf("invalid compiled grammar: %w", err)
	}
	var recognitionModule *Module
	var entries []recognitionEntry
	if v >= 3 && r.bool() {
		n := r.count()
		if r.err == nil {
			recognitionModule, entries, r.err = decodeRecognition(r.data[r.pos:r.pos+n], m)
			r.pos += n
		}
	}
	var prog *Program
	if r.bool() {
		g, flags := r.statements(pkg)
		if r.err == nil {
			o := opts
			o.SourceInfo = o.SourceInfo || m.SourceInfo
			o.DefaultValue = o.DefaultValue || m.DefaultValue
			p, err := build(g, o, flags)
			if err != nil {
				return nil, "", fmt.Errorf("invalid compiled grammar: %w", err)
			}
			prog = p
		}
	}
	if r.err == nil && r.pos != len(r.data) {
		r.fail("%d trailing bytes", len(r.data)-r.pos)
	}
	if r.err != nil {
		return nil, "", r.err
	}
	if prog == nil {
		prog = moduleProgram(m)
		prog.pkg = pkg
		prog.sourceInfo = prog.sourceInfo || opts.SourceInfo
		prog.defaultValue = prog.defaultValue || opts.DefaultValue
	} else {
		if n := len(prog.rules) + len(prog.twins); n != len(m.Rules) {
			return nil, "", fmt.Errorf("invalid compiled grammar: %d rules in the module for %d rules", len(m.Rules), n)
		}
		if err := sameVars(prog.vars, moduleVarTable(m)); err != nil {
			return nil, "", fmt.Errorf("invalid compiled grammar: the variables of the module differ from the syntax tree: %w", err)
		}
		prog.moduleOnce.Do(func() { prog.module = m })
	}
	prog.recognitionModule, prog.recognitionEntries = recognitionModule, entries
	if start != "" && hiddenRule(start) {
		return nil, "", fmt.Errorf("invalid compiled grammar: start rule %s is hidden", start)
	}
	if start != "" && !prog.hasRule(start) {
		return nil, "", fmt.Errorf("invalid compiled grammar: start rule %s is not defined", start)
	}
	return prog, start, nil
}

func (r *decoder) strings() {
	n := r.count()
	for i := 0; i < n && r.err == nil; i++ {
		l := r.count()
		if r.err != nil {
			return
		}
		r.strs = append(r.strs, string(r.data[r.pos:r.pos+l]))
		r.pos += l
	}
}

func (r *decoder) statements(pkg string) (*grammar.Grammar, []ruleFlags) {
	g := &grammar.Grammar{File: grammar.File{Pkg: grammar.Pkg{Name: pkg}}}
	flags := []ruleFlags{}
	n := r.count()
	for i := 0; i < n && r.err == nil; i++ {
		switch k := r.byte(); k {
		case kTypeDef:
			g.File.Decls = append(g.File.Decls, r.typeDecl(r.str()))
		case kVarDef:
			vd := &grammar.VarDef{Name: r.str()}
			if r.bool() {
				vd.Type = r.typeExpr()
			}
			vd.Value = r.optTerm()
			g.File.Decls = append(g.File.Decls, vd)
		case kRuleDef:
			rd := &grammar.RuleDef{Name: r.str()}
			if r.bool() {
				rd.Type = r.typeExpr()
			}
			rd.Expr = r.expr()
			rd.Action = r.optTerm()
			g.File.Decls = append(g.File.Decls, rd)
		default:
			r.fail("invalid statement kind %d", k)
		}
	}
	nf := r.count()
	for i := 0; i < nf && r.err == nil; i++ {
		f := ruleFlags{memo: r.bool(), leader: r.bool(), positional: r.bool()}
		f.twin = r.bool()
		flags = append(flags, f)
	}
	return g, flags
}

// typeDecl reads the type declaration of the given name.
func (r *decoder) typeDecl(name string) grammar.TypeDecl {
	switch k := r.byte(); k {
	case kStruct:
		st := &grammar.StructType{Fields: []*grammar.Field{}}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			st.Fields = append(st.Fields, &grammar.Field{Name: r.str(), Type: r.typeExpr()})
		}
		return &grammar.TypeDef{Name: name, Type: st}
	case kNode:
		t := r.typeExpr()
		if app, ok := t.(*grammar.TypeApp); !ok || app.Name != "node" || len(app.Args) != 1 {
			r.fail("invalid node type %s", name)
		}
		return &grammar.TypeDef{Name: name, Type: t}
	case kAlias:
		return &grammar.TypeAlias{Name: name, Type: r.typeExpr()}
	default:
		r.fail("invalid type declaration %d", k)
		return &grammar.TypeDef{Name: name, Type: &grammar.StructType{}}
	}
}

func (r *decoder) typeExpr() grammar.TypeExpr {
	if !r.enter() {
		return &grammar.TypeRef{}
	}
	defer r.leave()
	switch k := r.byte(); k {
	case kTypeRef:
		return &grammar.TypeRef{Name: r.str()}
	case kList:
		return &grammar.ListType{Elem: r.typeExpr()}
	case kOptional:
		return &grammar.OptionalType{Elem: r.typeExpr()}
	case kJoin:
		u := &grammar.JoinType{}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			u.Types = append(u.Types, r.typeExpr())
		}
		return u
	case kMeet:
		m := &grammar.MeetType{}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			m.Types = append(m.Types, r.typeExpr())
		}
		return m
	case kTuple:
		tt := &grammar.TupleType{Types: []grammar.TypeExpr{}}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			tt.Types = append(tt.Types, r.typeExpr())
		}
		return tt
	case kTypeApp:
		a := &grammar.TypeApp{Name: r.str()}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			a.Args = append(a.Args, r.typeExpr())
		}
		return a
	default:
		r.fail("invalid type %d", k)
		return &grammar.TypeRef{}
	}
}

func (r *decoder) exprs() []grammar.Expr {
	n := r.count()
	es := make([]grammar.Expr, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		es = append(es, r.expr())
	}
	return es
}

func (r *decoder) expr() grammar.Expr {
	if !r.enter() {
		return &grammar.Bottom{}
	}
	defer r.leave()
	switch k := r.byte(); k {
	case xRef:
		return &grammar.Ref{Name: r.str(), Level: r.str()}
	case xLiteral:
		return &grammar.Literal{Value: r.str()}
	case xClass:
		cc := &grammar.CharClass{Negated: r.bool()}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			cc.Ranges = append(cc.Ranges, grammar.CharRange{Lo: rune(r.int()), Hi: rune(r.int())})
		}
		return cc
	case xAny:
		return &grammar.Any{}
	case xSeq:
		return &grammar.Seq{Items: r.exprs()}
	case xInlineAction:
		a := &grammar.InlineAction{Expr: r.expr(), DefaultAction: r.bool()}
		a.Action = r.optTerm()
		return a
	case xChoice:
		return &grammar.Choice{Alts: r.exprs()}
	case xRepeat:
		rep := &grammar.Repeat{Min: r.int(), Max: r.int()}
		rep.Expr = r.expr()
		return rep
	case xOptional:
		return &grammar.Optional{Expr: r.expr()}
	case xAnd:
		return &grammar.And{Expr: r.expr()}
	case xNot:
		return &grammar.Not{Expr: r.expr()}
	case xAtomic:
		return &grammar.Atomic{Expr: r.expr()}
	case xDiscard:
		return &grammar.Discard{Expr: r.expr()}
	case xCapture:
		return &grammar.Capture{Name: r.str(), Expr: r.expr()}
	case xCut:
		return &grammar.Cut{}
	case xTop:
		return &grammar.Top{}
	case xBottom:
		return &grammar.Bottom{}
	case xBeginInput:
		return &grammar.BeginInput{}
	case xEndInput:
		return &grammar.EndInput{}
	case xBeginLine:
		return &grammar.BeginLine{}
	case xEndLine:
		return &grammar.EndLine{}
	case xPredicate:
		return &grammar.Predicate{Term: r.term()}
	case xAttributed:
		a := &grammar.Attributed{Expr: r.expr()}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			at := &grammar.Attribute{Name: r.str(), KwArgs: map[string]any{}}
			at.Pragma = at.Name == "recover" || at.Name == "stream"
			m := r.count()
			for j := 0; j < m && r.err == nil; j++ {
				name := r.str()
				at.KwArgs[name] = r.expr()
			}
			a.Attrs = append(a.Attrs, at)
		}
		return a
	case xOperatorsRule:
		p := &grammar.OperatorsRule{}
		if r.bool() {
			p.Skip = r.expr()
		}
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			p.Operands = append(p.Operands, &grammar.OperatorsRuleOperand{Expr: r.expr(), Action: r.optTerm()})
		}
		n = r.count()
		for i := 0; i < n && r.err == nil; i++ {
			l := &grammar.OperatorsRuleLevel{Name: r.str()}
			m := r.count()
			for j := 0; j < m && r.err == nil; j++ {
				op := &grammar.OperatorsRuleOperator{Kind: r.str(), Assoc: r.str()}
				op.Expr = r.expr()
				op.Action = r.optTerm()
				l.Operators = append(l.Operators, op)
			}
			p.Levels = append(p.Levels, l)
		}
		return p
	default:
		r.fail("invalid expression kind %d", k)
		return &grammar.Bottom{}
	}
}

func (r *decoder) optTerm() grammar.Term {
	if r.bool() {
		return r.term()
	}
	return nil
}

func (r *decoder) terms() []grammar.Term {
	n := r.count()
	ts := make([]grammar.Term, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		ts = append(ts, r.term())
	}
	return ts
}

func (r *decoder) term() grammar.Term {
	if !r.enter() {
		return &grammar.NilLit{}
	}
	defer r.leave()
	switch k := r.byte(); k {
	case tInt:
		return &grammar.IntLit{Value: r.int()}
	case tString:
		return &grammar.StringLit{Value: r.str()}
	case tBool:
		return &grammar.BoolLit{Value: r.bool()}
	case tNil:
		return &grammar.NilLit{}
	case tCapture:
		return &grammar.CaptureRef{Name: r.str()}
	case tParam:
		return &grammar.CaptureRef{Name: r.str(), Param: true}
	case tIndex:
		return &grammar.IndexRef{Index: r.uint()}
	case tVar:
		return &grammar.VarRef{Name: r.str()}
	case tMember:
		x := r.term()
		return &grammar.Member{X: x, Name: r.str()}
	case tNew:
		n := &grammar.New{Type: r.str()}
		m := r.count()
		for i := 0; i < m && r.err == nil; i++ {
			n.Fields = append(n.Fields, &grammar.FieldInit{Name: r.str(), Value: r.term()})
		}
		return n
	case tConvert:
		return &grammar.New{Type: r.str(), Args: r.terms()}
	case tCall:
		return &grammar.Call{Func: r.str(), Args: r.terms()}
	case tTuple:
		return &grammar.TupleLit{Elems: r.terms()}
	case tListConstructor:
		return &grammar.ListConstructor{Elem: r.typeExpr(), Items: r.terms()}
	case tLambda:
		l := &grammar.Lambda{}
		m := r.count()
		for i := 0; i < m && r.err == nil; i++ {
			l.Params = append(l.Params, r.str())
		}
		l.Body = r.term()
		return l
	case tBinary:
		op := r.str()
		return &grammar.Binary{Op: op, L: r.term(), R: r.term()}
	case tUnary:
		op := r.str()
		return &grammar.Unary{Op: op, X: r.term()}
	case tBind:
		name := r.str()
		return &grammar.Bind{Name: name, Value: r.term()}
	case tCast:
		x := r.term()
		c := &grammar.Cast{X: x, Type: r.typeExpr()}
		line, col := r.int32(), r.int32()
		if r.err == nil && (line < 0 || col < 0) {
			r.fail("invalid cast position %d:%d", line, col)
		}
		c.Pos = grammar.Pos{Line: int(line), Col: int(col)}
		return c
	default:
		r.fail("invalid term kind %d", k)
		return &grammar.NilLit{}
	}
}
