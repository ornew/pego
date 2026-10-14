package pego_test

import (
	"github.com/ornew/pego"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPositionMapRoundTrips(t *testing.T) {
	for _, text := range []string{"", "abc", "é😀\r\nx\ry\nz\n", "a\xff\xc0\x80😀", strings.Repeat("é😀abc", 50) + "\r\nlast"} {
		m := pego.NewPositionMap(text)
		boundaries := map[int]bool{len(text): true}
		for i := 0; i < len(text); {
			boundaries[i] = true
			_, n := utf8.DecodeRuneInString(text[i:])
			i += n
		}
		for _, enc := range []pego.PositionEncoding{pego.PositionBytes, pego.PositionCodePoints, pego.PositionUTF16} {
			for off := 0; off <= len(text); off++ {
				unit, err := m.UnitOffset(off, enc)
				if enc != pego.PositionBytes && !boundaries[off] {
					if err == nil {
						t.Fatalf("interior accepted: %q %d/%d", text, enc, off)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				back, err := m.ByteOffsetFromUnit(unit, enc)
				if err != nil || back != off {
					t.Fatalf("flat round trip %d/%d -> %d: %v", enc, off, back, err)
				}
				pos, err := m.Position(off, enc)
				if off > 0 && off < len(text) && text[off-1] == '\r' && text[off] == '\n' {
					if err == nil {
						t.Fatal("CRLF interior accepted")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				back, err = m.ByteOffset(pos, enc)
				if err != nil || back != off {
					t.Fatalf("line round trip %d/%d -> %+v -> %d: %v", enc, off, pos, back, err)
				}
			}
		}
	}
}

func TestPositionMapStrictCoordinates(t *testing.T) {
	m := pego.NewPositionMap("a😀\r\né\xff")
	if off, err := m.ByteOffset(pego.Position{Line: 1, Character: 2}, pego.PositionCodePoints); err != nil || off != len("a😀\r\né\xff") {
		t.Fatalf("malformed byte: %d %v", off, err)
	}
	for _, tc := range []struct {
		p pego.Position
		e pego.PositionEncoding
	}{
		{pego.Position{-1, 0}, pego.PositionBytes}, {pego.Position{2, 0}, pego.PositionBytes},
		{pego.Position{0, -1}, pego.PositionBytes}, {pego.Position{0, 6}, pego.PositionBytes},
		{pego.Position{0, 2}, pego.PositionUTF16}, {pego.Position{0, 0}, 99},
		{pego.Position{0, int(^uint(0) >> 1)}, pego.PositionCodePoints},
	} {
		if _, err := m.ByteOffset(tc.p, tc.e); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	for _, enc := range []pego.PositionEncoding{pego.PositionBytes, pego.PositionCodePoints, pego.PositionUTF16, 99} {
		if _, err := m.UnitOffset(-1, enc); err == nil {
			t.Fatal("negative accepted")
		}
		if _, err := m.UnitOffset(100, enc); err == nil {
			t.Fatal("past end accepted")
		}
		if _, err := m.ByteOffsetFromUnit(100, enc); err == nil {
			t.Fatal("past end unit accepted")
		}
	}
	if _, err := m.ByteOffsetFromUnit(2, pego.PositionUTF16); err == nil {
		t.Fatal("surrogate interior accepted")
	}
}
