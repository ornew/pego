package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// The compiled grammar format (.pegoc), specified in the File format section of
// spec/bytecode.md. A file holds one execution-only compile result: the module of one mode.
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
	// isaVersion is the instruction set of the value model, the only one supported. Earlier
	// versions built the previous syntax trees, so their files are not loaded and need to be
	// compiled again.
	isaVersion = 26
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
	if isa < isaVersion {
		return fmt.Errorf("unsupported instruction set version %d: the file was compiled by an older pego; compile the grammar again", isa)
	}
	return fmt.Errorf("unsupported instruction set version %d", isa)
}

// errClosureNeedsSource is the error of the closure backend on a program loaded from a compiled
// grammar.
var errClosureNeedsSource = errors.New("the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends")

// errSchemaNeedsSource is the error of a schema export from a program loaded from a compiled
// grammar.
var errSchemaNeedsSource = fmt.Errorf("%w: a compiled grammar holds no schema; export the schema from the grammar source", ErrSchemaUnavailable)

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

type encoder struct {
	body bytes.Buffer
	strs map[string]int
	list []string
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
	if prog.hasMissing {
		return nil, missingUnsupported("compiled grammar serialization")
	}
	if start != "" {
		if err := prog.CheckStart(start); err != nil {
			return nil, err
		}
	}
	if prog.loaded && mode != prog.mode {
		return nil, fmt.Errorf("the compiled grammar is in %s mode and is saved only in that mode; compile the grammar from source to save it in %s mode", prog.mode, mode)
	}
	var m *Module
	switch {
	case prog.loaded || mode != CompiledRecognize:
		m = prog.Module()
	default:
		rec, err := prog.recognizer()
		if err != nil {
			return nil, err
		}
		m = rec.Module()
	}
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings { // put the module's string table first
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	w.module(m)
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.WriteByte(byte(mode))
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

// decoder reads a compiled grammar. On invalid data it does not panic but records the first
// error.
type decoder struct {
	data []byte
	pos  int
	strs []string
	err  error
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
	r := &decoder{data: body, pos: len(compiledMagic) + 2}
	isa := r.uint()
	if r.err == nil && isa != isaVersion {
		return nil, "", unsupportedISA(isa)
	}
	r.strings()
	start := r.str()
	m := r.module()
	if r.err == nil && r.pos != len(r.data) {
		r.fail("%d trailing bytes", len(r.data)-r.pos)
	}
	if r.err != nil {
		return nil, "", r.err
	}
	if err := validateModule(m); err != nil {
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
func moduleStarts(m *Module) []string {
	names := make([]string, len(m.Starts))
	for i, x := range m.Starts {
		names[i] = m.Strings[m.Rules[x].Name]
	}
	return names
}

// moduleDefaultStart returns the name of the default start rule of a validated module.
func moduleDefaultStart(m *Module) string { return m.Strings[m.Rules[m.DefaultStart].Name] }
