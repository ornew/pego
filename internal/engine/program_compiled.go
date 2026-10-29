package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
)

// The compiled grammar format (.pegoc), specified in the File format section of
// spec/bytecode/README.md. A file holds one execution-only compile result: the module of one mode.
//
//	magic    "PEGOC\x00"
//	version  1 byte (compiledVersion)
//	mode     1 byte (CompiledMode)
//	payload  instruction set version, string table, start rule, module
//	crc32    CRC-32 of magic through payload (IEEE, 4 bytes little-endian)
//
// Integers are variable-length (signed ones zigzag-encoded), and strings are referenced by their
// index in the string table, which begins with the module's string table. Loading skips parsing,
// static analysis, type checking, and compilation to bytecode.

const (
	compiledMagic   = "PEGOC\x00"
	compiledVersion = 6
)

// CompiledMode is the mode of a compiled grammar file: the one kind of parse its module serves.
type CompiledMode uint8

const (
	// CompiledParse serves parses that build values without tracing or profiling: Parse,
	// ParsePrefix, ParseStream and Iterate.
	CompiledParse CompiledMode = iota
	// CompiledRecognize serves recognition (RecognizeOnly) without tracing or profiling.
	CompiledRecognize
	// CompiledDocument serves Documents and traced or profiled Parse, ParsePrefix, ParseStream
	// and Iterate.
	CompiledDocument
)

var (
	compiledModeNames = [...]string{CompiledParse: "parse", CompiledRecognize: "recognize", CompiledDocument: "document"}
	// compiledModeAPI names the modes in the Go API, for error messages.
	compiledModeAPI = [...]string{CompiledParse: "pego.CompiledParse", CompiledRecognize: "pego.CompiledRecognize", CompiledDocument: "pego.CompiledDocument"}
)

// String returns the name of the mode: parse, recognize or document.
func (m CompiledMode) String() string {
	if int(m) < len(compiledModeNames) {
		return compiledModeNames[m]
	}
	return fmt.Sprintf("CompiledMode(%d)", uint8(m))
}

// ParseCompiledMode returns the mode named s: "parse", "recognize" or "document".
func ParseCompiledMode(s string) (CompiledMode, error) {
	for m, name := range compiledModeNames {
		if name == s {
			return CompiledMode(m), nil
		}
	}
	return 0, fmt.Errorf("unknown compiled grammar mode %q (want parse, recognize or document)", s)
}

// unsupportedISA is the error for a module of instruction set isa.
func unsupportedISA(isa int) error {
	if isa < bytecode.ISAVersion {
		return fmt.Errorf("unsupported instruction set version %d: the file was compiled by an older pego; compile the grammar again", isa)
	}
	return fmt.Errorf("unsupported instruction set version %d", isa)
}

// errClosureNeedsSource is the error of the closure backend on a program loaded from a compiled
// grammar.
var errClosureNeedsSource = errors.New("the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends")

// errSchemaNeedsSource is the error of a schema export from a program loaded from a compiled
// grammar.
var errSchemaNeedsSource = fmt.Errorf("%w: a compiled grammar holds no schema; export the schema from the grammar source", compile.ErrSchemaUnavailable)

// ErrNotCompiled reports that the data is not a compiled grammar.
var ErrNotCompiled = errors.New("not a compiled PEGO grammar")

// IsCompiled reports whether the data is in the compiled grammar format.
func IsCompiled(data []byte) bool { return bytes.HasPrefix(data, []byte(compiledMagic)) }

// Mode returns the mode of a program loaded from a compiled grammar, and false for a program
// compiled from a grammar.
func (prog *Program) Mode() (CompiledMode, bool) { return prog.mode, prog.loaded }

// checkMode reports an error when prog is loaded from a compiled grammar whose mode does not serve
// a parse with options o; document selects a Document. A compiled grammar runs exactly the parses
// of its mode, without falling back to another.
func (prog *Program) checkMode(o ParseOptions, document bool) error {
	if !prog.loaded {
		return nil
	}
	var need CompiledMode
	var what string
	switch {
	case document:
		need, what = CompiledDocument, "documents need"
	case o.Recognize && o.Trace != nil:
		return fmt.Errorf("the compiled grammar is in %s mode; traced or profiled recognition needs the grammar source: compile the grammar from source", prog.mode)
	case o.Recognize:
		need, what = CompiledRecognize, "recognition needs"
	case o.Trace != nil:
		need, what = CompiledDocument, "tracing and profiling need"
	default:
		need, what = CompiledParse, "parsing needs"
	}
	if need == prog.mode {
		return nil
	}
	return modeError(prog.mode, need, what)
}

// modeError is the error of an operation that needs mode need, on a compiled grammar in mode have;
// what names the operation with its verb, such as "documents need".
func modeError(have, need CompiledMode, what string) error {
	return fmt.Errorf("the compiled grammar is in %s mode; %s %s mode: compile it with -mode %s (%s), or compile the grammar from source", have, what, need, need, compiledModeAPI[need])
}

// MarshalBinary encodes the program in parse mode; see Marshal.
func (prog *Program) MarshalBinary(start string) ([]byte, error) {
	return prog.Marshal(start, CompiledParse)
}

// Marshal encodes the module of mode as a compiled grammar. start is the start rule a loaded parser
// uses: a start rule of the grammar, or empty for its default start rule. A program loaded from a
// compiled grammar is saved only in its own mode, and then reproduces the bytes it was loaded from.
func (prog *Program) Marshal(start string, mode CompiledMode) ([]byte, error) {
	if int(mode) >= len(compiledModeNames) {
		return nil, fmt.Errorf("unknown compiled grammar mode %d", mode)
	}
	if prog.HasMissing {
		return nil, compile.MissingUnsupported("compiled grammar serialization")
	}
	if start != "" {
		if err := prog.CheckStart(start); err != nil {
			return nil, err
		}
	}
	if prog.loaded && mode != prog.mode {
		return nil, fmt.Errorf("the compiled grammar is in %s mode and is saved only in that mode; compile the grammar from source to save it in %s mode", prog.mode, mode)
	}
	var m *bytecode.Module
	switch {
	case prog.loaded || mode != CompiledRecognize:
		m = prog.Module()
	default:
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		m = rec.Module()
	}
	w := &bytecode.Encoder{Strs: map[string]int{}}
	for i, s := range m.Strings { // put the module's string table first
		w.Strs[s] = i
		w.List = append(w.List, s)
	}
	w.Str(start)
	w.WriteModule(m)
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.WriteByte(byte(mode))
	out.Write(binary.AppendUvarint(nil, uint64(m.InstructionSetVersion())))
	w.Finish(&out)
	return out.Bytes(), nil
}

// LoadProgram loads a compiled grammar. It also returns the saved start rule (empty for the
// default start rule). It does not perform parsing, static analysis, type checking, or compilation
// to bytecode. The program runs on the bytecode backends and serves the parses of the file's mode
// only (checkMode).
func LoadProgram(data []byte) (*Program, string, error) {
	if !IsCompiled(data) {
		return nil, "", ErrNotCompiled
	}
	if len(data) < len(compiledMagic)+2+4 {
		return nil, "", errors.New("invalid compiled grammar: too short")
	}
	if v := data[len(compiledMagic)]; v != compiledVersion {
		return nil, "", fmt.Errorf("unsupported compiled grammar version %d (want %d): compile the grammar again", v, compiledVersion)
	}
	body, sum := data[:len(data)-4], binary.LittleEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, "", errors.New("invalid compiled grammar: checksum mismatch")
	}
	mode := CompiledMode(data[len(compiledMagic)+1])
	if int(mode) >= len(compiledModeNames) {
		return nil, "", fmt.Errorf("invalid compiled grammar: unknown mode %d", mode)
	}
	r := &bytecode.Decoder{Data: body, Pos: len(compiledMagic) + 2}
	isa := r.Uint()
	if r.Err == nil && isa != bytecode.ISAVersion {
		return nil, "", unsupportedISA(isa)
	}
	r.Strings()
	start := r.Str()
	m := r.ReadModule()
	if r.Err == nil && r.Pos != len(r.Data) {
		r.Fail("%d trailing bytes", len(r.Data)-r.Pos)
	}
	if r.Err != nil {
		return nil, "", r.Err
	}
	if err := compile.ValidateModule(m); err != nil {
		return nil, "", fmt.Errorf("invalid compiled grammar: %w", err)
	}
	prog := moduleProgram(m)
	prog.loaded, prog.mode = true, mode
	if start != "" {
		if err := prog.CheckStart(start); err != nil {
			return nil, "", fmt.Errorf("invalid compiled grammar: %w", err)
		}
	}
	return prog, start, nil
}

// moduleStarts returns the names of the start rules of a validated module.
func moduleStarts(m *bytecode.Module) []string {
	names := make([]string, len(m.Starts))
	for i, x := range m.Starts {
		names[i] = m.Strings[m.Rules[x].Name]
	}
	return names
}

// moduleDefaultStart returns the name of the default start rule of a validated module.
func moduleDefaultStart(m *bytecode.Module) string { return m.Strings[m.Rules[m.DefaultStart].Name] }

// moduleProgram builds a program from a module alone, without the grammar; only the bytecode
// backends run it.
func moduleProgram(m *bytecode.Module) *Program {
	prog := &compile.Program{ByName: map[string]*compile.Rule{}, Types: map[string]grammar.TypeDecl{}, RecordsSourceInfo: m.SourceInfo, MakesDefaultValue: m.DefaultValue}
	for _, t := range m.Types {
		if t.Node >= 0 {
			// The run time needs only that the type is a node type.
			prog.Types[m.Strings[t.Name]] = &grammar.TypeDef{Name: m.Strings[t.Name], Type: &grammar.TypeApp{Name: "node", Args: []grammar.TypeExpr{&grammar.TypeRef{Name: "_"}}}}
			continue
		}
		st := &grammar.StructType{}
		for _, f := range t.Fields {
			st.Fields = append(st.Fields, &grammar.Field{Name: m.Strings[f]})
		}
		prog.Types[m.Strings[t.Name]] = &grammar.TypeDef{Name: m.Strings[t.Name], Type: st}
	}
	prog.Vars = moduleVarTable(m)
	prog.StartRules, prog.DefaultStartRule = moduleStarts(m), moduleDefaultStart(m)
	prog.BytecodeOnce.Do(func() { prog.Bytecode = m })
	return newProgram(prog, nil)
}
