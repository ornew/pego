package runtime

import (
	"slices"
	"testing"
)

// TestTerminalPrepare checks the dispatch tables that Prepare builds: for each ASCII character,
// the alternatives of the leading guarded run whose guards admit it, in order, and the end of
// the run; choices without a table get none, and preparing again builds the same tables.
func TestTerminalPrepare(t *testing.T) {
	class := func(chars string) *Class {
		c := &Class{}
		for _, r := range chars {
			c.ASCII[r>>6] |= 1 << (uint(r) & 63)
		}
		return c
	}
	classes := []*Class{class("aA"), class("abc"), class("d"), class("x")}
	// choice(table 0): guardClass(0) class(3) / guard(1) class(3) / guardClass(2) / class(3)
	// nested in a sequence with a choice without a table.
	code := []uint16{
		TerminalSeq, 0,
		TerminalChoice, 0, 0,
		TerminalGuardClass, 8, 0, 1, TerminalClass, 4, 3, 1,
		TerminalGuard, 8, 1, 2, TerminalClass, 4, 3, 2,
		TerminalGuardClass, 4, 2, 3,
		TerminalClass, 4, 3, 4,
		TerminalChoice, 7, TerminalNoTable, TerminalClass, 4, 3, 5,
	}
	code[1] = uint16(len(code))
	code[3] = uint16(29 - 2)
	tp := &TerminalProgram{Code: code, Classes: classes}
	if got := tp.Prepare(); got != tp {
		t.Fatalf("Prepare returned another program")
	}
	if len(tp.tables) != 1 {
		t.Fatalf("%d tables, want 1", len(tp.tables))
	}
	// Offsets from the choice at 2: the guarded alternatives are at 5, 13 and 21; the run ends
	// at 25, where the unguarded class is.
	want := map[byte][]uint16{'a': {3, 11}, 'A': {3}, 'b': {11}, 'c': {11}, 'd': {19}, 'x': nil, 0: nil, 127: nil}
	table := &tp.tables[0]
	for c, w := range want {
		if got := alts(table, c); !slices.Equal(got, w) {
			t.Errorf("alternatives for %q = %v, want %v", c, got, w)
		}
	}
	if table.end != 23 {
		t.Errorf("end of run = %d, want 23", table.end)
	}
	before := *table
	before.alts = slices.Clone(before.alts)
	tp.Prepare()
	if len(tp.tables) != 1 || tp.tables[0].at != before.at || !slices.Equal(tp.tables[0].alts, before.alts) {
		t.Errorf("preparing again built different tables")
	}
}

// guardChoice returns a program of one choice with a dispatch table whose alternatives are
// class-consuming guards of classes, each followed by the class rest when rest is not nil.
func guardChoice(classes []*Class, rest *Class) *TerminalProgram {
	code := []uint16{TerminalChoice, 0, 0}
	all := classes
	for i := range classes {
		if rest == nil {
			code = append(code, TerminalGuardClass, 4, uint16(i), uint16(i+1))
			continue
		}
		code = append(code, TerminalGuardClass, 8, uint16(i), uint16(i+1), TerminalClass, 4, uint16(len(classes)), 0)
	}
	if rest != nil {
		all = append(slices.Clone(classes), rest)
	}
	code[1] = uint16(len(code))
	return &TerminalProgram{Code: code, Classes: all}
}

// TestTerminalTableLimit checks that a choice whose dispatch table would have more entries
// than its uint16 offsets can index has no table, at the boundary.
func TestTerminalTableLimit(t *testing.T) {
	full := &Class{ASCII: [2]uint64{^uint64(0), ^uint64(0)}}
	allBut := func(r rune) *Class {
		c := *full
		c.ASCII[r>>6] &^= 1 << (uint(r) & 63)
		return &c
	}
	for _, tc := range []struct {
		name    string
		classes []*Class
		ok      bool
	}{
		// 511 * 128 + 127 = 65535 entries.
		{"65535 entries", append(slices.Repeat([]*Class{full}, 511), allBut('q')), true},
		// 512 * 128 = 65536 entries.
		{"65536 entries", slices.Repeat([]*Class{full}, 512), false},
	} {
		tp := guardChoice(tc.classes, nil).Prepare()
		table := &tp.tables[0]
		if table.ok != tc.ok {
			t.Fatalf("%s: table %v, want %v", tc.name, table.ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if n := len(table.alts); n != 65535 {
			t.Fatalf("%s: %d entries", tc.name, n)
		}
		if got := alts(table, 'q'); len(got) != 511 {
			t.Errorf("%s: %d alternatives for 'q', want 511", tc.name, len(got))
		}
		if got := alts(table, 127); len(got) != 512 || got[511] != uint16(3+511*4) {
			t.Errorf("%s: alternatives for 127 end at %v", tc.name, got[len(got)-1])
		}
	}
}

func alts(table *terminalTable, c byte) []uint16 { return table.alts[table.at[c]:table.at[c+1]] }

// TestSkipTerminalAllocs checks that matching a prepared program allocates nothing, where
// expectations are recorded and in a lookahead.
func TestSkipTerminalAllocs(t *testing.T) {
	letter := func(r rune) *Class {
		c := &Class{}
		c.ASCII[r>>6] |= 1 << (uint(r) & 63)
		return c
	}
	digit := &Class{}
	for r := '0'; r <= '9'; r++ {
		digit.ASCII[r>>6] |= 1 << (uint(r) & 63)
	}
	tp := guardChoice([]*Class{letter('a'), letter('b'), letter('c'), letter('d')}, digit).Prepare()
	for _, silent := range []int{0, 1} {
		for _, in := range []string{"c1", "cx", "x", ""} {
			p := &TParser{Parser: &Parser{unit: Bytes, bs: in, n: len(in)}}
			p.Silent = silent
			if n := testing.AllocsPerRun(100, func() {
				p.Pos = 0
				p.SkipTerminal(tp)
			}); n != 0 {
				t.Errorf("silent %d, input %q: %v allocations", silent, in, n)
			}
		}
	}
}

// TestTooDeepPanics pins what the guards of SkipTerminal rely on: a parse never continues
// with p.Depth > p.MaxDepth, because exceeding it panics (generated code and the runtime check
// right after every increment; see TestDepthChecks in the engine).
func TestTooDeepPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TooDeep returned")
		}
	}()
	(&Parser{MaxDepth: 3, Depth: 4}).TooDeep()
}
