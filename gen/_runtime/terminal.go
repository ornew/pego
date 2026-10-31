package runtime

// Operations of a TerminalProgram. Every operation starts with its code and its length in
// words, the operation included, so that a reader can step over it.
const (
	// TerminalClass, class, id: consumes one character of Classes[class], or records the
	// expectation id where it is expected.
	TerminalClass uint16 = iota
	// TerminalLiteral, literal, id: matches Literals[literal], or records id at its start.
	TerminalLiteral
	// TerminalSeq, operations...: matches the operations in turn.
	TerminalSeq
	// TerminalChoice, table, operations...: matches the first of the operations that matches.
	// Unless table is TerminalNoTable, the choice begins with a run of guarded alternatives
	// and Prepare builds the dispatch table number table for it if the table fits.
	TerminalChoice
	// TerminalOptional, operation: matches the operation or nothing.
	TerminalOptional
	// TerminalNot, operation: succeeds, consuming nothing, where the operation does not match.
	TerminalNot
	// TerminalGuard, class, id, operation: an alternative of a choice that is skipped where the
	// next character is not a member of Classes[class], recording id at the position of the
	// choice, as the generated code of a choice skips such an alternative; otherwise it is the
	// operation.
	TerminalGuard
	// TerminalGuardClass, class, id, operations...: a TerminalGuard of an alternative that
	// begins with the class Classes[class], whose operations are the rest of the alternative:
	// where the guard admits the next character, the class matches it, so the character is
	// consumed and the operations are matched in turn.
	TerminalGuardClass
)

// TerminalNoTable marks a TerminalChoice without a dispatch table.
const TerminalNoTable uint16 = 0xffff

// TerminalProgram is a value-free expression made only of literals, character classes,
// sequences, choices, options and negative lookaheads, which generated parsers match through
// this table instead of straight-line code; not a stable API.
type TerminalProgram struct {
	Code     []uint16
	Classes  []*Class
	Literals []TerminalText
	tables   []terminalTable
}

// terminalTable is the dispatch table of a choice that begins with a run of guarded
// alternatives: alts[at[c]:at[c+1]] are the offsets, from the choice, of the alternatives of
// the run whose guards admit the ASCII character c, in order, and end is the offset of the
// first operation after the run. A choice whose table would have more than
// maxTerminalTableEntries entries, which at cannot index, has none (ok is false) and tests
// its guards in turn.
type terminalTable struct {
	at   [129]uint16
	alts []uint16
	end  uint16
	ok   bool
}

// maxTerminalTableEntries is the largest number of entries of a dispatch table.
const maxTerminalTableEntries = 0xffff

// TerminalText is a literal of a TerminalProgram, as the generated code passes it to
// MatchLiteral.
type TerminalText struct {
	Runes []rune
	Text  string
}

// Prepare builds the dispatch tables of the choices of t from the classes of their guards and
// returns t. Generated parsers declare a program as static data and prepare it once.
func (t *TerminalProgram) Prepare() *TerminalProgram {
	t.tables = nil
	t.build(0)
	return t
}

// build builds the dispatch tables of the operation at pc and of those inside it.
func (t *TerminalProgram) build(pc int) {
	code := t.Code
	end := pc + int(code[pc+1])
	switch code[pc] {
	case TerminalSeq, TerminalOptional, TerminalNot:
		pc += 2
	case TerminalGuard, TerminalGuardClass:
		pc += 4
	case TerminalChoice:
		if slot := int(code[pc+2]); slot != int(TerminalNoTable) {
			for slot >= len(t.tables) {
				t.tables = append(t.tables, terminalTable{})
			}
			t.tables[slot] = t.table(pc)
		}
		pc += 3
	default:
		return
	}
	for ; pc < end; pc += int(code[pc+1]) {
		t.build(pc)
	}
}

// table returns the dispatch table of the choice at pc.
func (t *TerminalProgram) table(pc int) terminalTable {
	code := t.Code
	end := pc + int(code[pc+1])
	run := pc + 3
	for run < end && (code[run] == TerminalGuard || code[run] == TerminalGuardClass) {
		run += int(code[run+1])
	}
	table := terminalTable{end: uint16(run - pc), ok: true}
	for c := range rune(128) {
		table.at[c] = uint16(len(table.alts))
		for k := pc + 3; k < run; k += int(code[k+1]) {
			if t.Classes[code[k+2]].Has(c) {
				if len(table.alts) == maxTerminalTableEntries {
					return terminalTable{}
				}
				table.alts = append(table.alts, uint16(k-pc))
			}
		}
	}
	table.at[128] = uint16(len(table.alts))
	return table
}

// SkipTerminal matches t in an ordinary parse and reports whether it matched. It consumes the
// same input as the generated code of the expression and records the same expectations at the
// same positions. On failure the position is unspecified, as after the generated code; the
// caller restores it.
func (p *TParser) SkipTerminal(t *TerminalProgram) bool {
	return p.skipTerminal(t, 0)
}

func (p *TParser) skipTerminal(t *TerminalProgram, pc int) bool {
	code := t.Code
	switch code[pc] {
	case TerminalClass:
		c := t.Classes[code[pc+2]]
		return p.ClassFast(c) || p.classSkip(c, ExpID(code[pc+3]))
	case TerminalLiteral:
		lit := &t.Literals[code[pc+2]]
		_, ok := p.Parser.MatchLiteralOrdinary(lit.Runes, lit.Text, ExpID(code[pc+3]), false)
		return ok
	case TerminalSeq:
		return p.skipTerminalSeq(t, pc+2, pc+int(code[pc+1]))
	case TerminalChoice:
		return p.skipTerminalChoice(t, pc)
	case TerminalOptional:
		start := p.Pos
		if !p.skipTerminal(t, pc+2) {
			p.Pos = start
		}
		return true
	case TerminalNot:
		start := p.Pos
		p.Silent++
		ok := p.skipTerminal(t, pc+2)
		p.Silent--
		p.Pos = start
		return !ok
	}
	panic("zp: unknown terminal operation")
}

// skipTerminalSeq matches the operations from pc to end in turn.
func (p *TParser) skipTerminalSeq(t *TerminalProgram, pc, end int) bool {
	code := t.Code
	for ; pc < end; pc += int(code[pc+1]) {
		if code[pc] == TerminalClass {
			c := t.Classes[code[pc+2]]
			if !p.ClassFast(c) && !p.classSkip(c, ExpID(code[pc+3])) {
				return false
			}
			continue
		}
		if !p.skipTerminal(t, pc) {
			return false
		}
	}
	return true
}

// skipTerminalChoice matches the choice at pc. The character is read at the first guarded
// alternative, as by the generated code, and each guard that does not admit it records its
// expectation at the position of the choice. Where ExpectOrdinary records nothing there (in
// a lookahead, or behind the farthest position, which only grows), skipping an alternative
// has no effect, and a choice with a dispatch table runs only the alternatives of its run
// whose guards admit the character.
func (p *TParser) skipTerminalChoice(t *TerminalProgram, pc int) bool {
	code := t.Code
	start, end := p.Pos, pc+int(code[pc+1])
	var ch rune
	size, peeked, more := 0, false, false
	k := pc + 3
	if slot := code[pc+2]; slot != TerminalNoTable && (p.Silent > 0 || start < p.farthest) {
		ch, size, more = p.NextOrdinary()
		peeked = true
		if table := &t.tables[slot]; table.ok && more && uint32(ch) < 128 {
			for _, at := range table.alts[table.at[ch]:table.at[ch+1]] {
				if p.skipTerminalAlt(t, pc+int(at), size) {
					return true
				}
				p.Pos = start
			}
			k = pc + int(table.end)
		}
	}
	for ; k < end; k += int(code[k+1]) {
		if op := code[k]; op == TerminalGuard || op == TerminalGuardClass {
			if !peeked {
				ch, size, more = p.NextOrdinary()
				peeked = true
			}
			if !more || !t.Classes[code[k+2]].Has(ch) {
				p.ExpectOrdinary(start, ExpID(code[k+3]))
				continue
			}
			if p.skipTerminalAlt(t, k, size) {
				return true
			}
		} else if p.skipTerminal(t, k) {
			return true
		}
		p.Pos = start
	}
	return false
}

// skipTerminalAlt matches the guarded alternative at pc, whose guard has admitted the next
// character, of size bytes or code points.
func (p *TParser) skipTerminalAlt(t *TerminalProgram, pc, size int) bool {
	code := t.Code
	if code[pc] == TerminalGuardClass {
		p.Pos += size
		return p.skipTerminalSeq(t, pc+4, pc+int(code[pc+1]))
	}
	return p.skipTerminal(t, pc+4)
}
