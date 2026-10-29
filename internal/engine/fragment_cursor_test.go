package engine

import (
	"testing"

	"github.com/ornew/pego/internal/engine/core"
)

func TestFragmentCursorOriginalBytes(t *testing.T) {
	cases := []struct {
		name, source string
		offsets      []int
	}{
		{"empty", "", []int{0}},
		{"multibyte", "aé😀tail", []int{0, 1, 3, 7, 8, 9, 10, 11}},
		{"malformed", "a\xff\xc0\x80😀tail", []int{0, 1, 2, 3, 4, 8, 9, 10, 11, 12}},
		{"replacement scalar", "�\xfftail", []int{0, 3, 4, 5, 6, 7, 8}},
		{"truncated scalar", "é\xe2\x82", []int{0, 2, 3, 4}},
		{"line terminators", "a\r\né\rb\n", []int{0, 1, 2, 3, 5, 6, 7, 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, indexed := range []bool{false, true} {
				in := core.NewTextInput(tc.source, CodePoints, indexed)
				for cursor, want := range tc.offsets {
					if got := in.PrefixByteOffset(tc.source, cursor); got != want {
						t.Fatalf("indexed=%v cursor=%d: byte endpoint=%d, want %d", indexed, cursor, got, want)
					}
				}
				if in.Src != tc.source {
					t.Fatal("endpoint conversion changed original source")
				}
				if !indexed && in.Offs != nil {
					t.Fatal("fallback retained a lazy index")
				}
			}
			// A byte-mode cursor can address a valid UTF-8 scalar's interior.
			in := core.NewTextInput(tc.source, Bytes, false)
			for cursor := 0; cursor <= len(tc.source); cursor++ {
				if got := in.PrefixByteOffset(tc.source, cursor); got != cursor {
					t.Fatalf("byte cursor=%d: endpoint=%d", cursor, got)
				}
			}
		})
	}
}

func TestFragmentCursorMalformedPrefixParity(t *testing.T) {
	prog := mustCompile(t, `rule main = . .`)
	source := "😀\xffunconsumed"
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, recognize := range []bool{false, true} {
				options := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
				prefix, err := prog.ParsePrefixWith("main", source, options)
				want := 2
				if unit == Bytes {
					want = 5
				}
				if err != nil || prefix.Consumed != want || prefix.ConsumedBytes != 5 {
					t.Fatalf("%v/%v/recognize=%v: %+v, %v", backend, unit, recognize, prefix, err)
				}
				reference, err := prog.ParseWith("main", source[:prefix.ConsumedBytes], options)
				if err != nil {
					t.Fatalf("consumed-slice reference: %v", err)
				}
				if recognize {
					if prefix.Node != nil || reference != nil {
						t.Fatal("recognition built a node")
					}
				} else if prefix.Node == nil || reference == nil || prefix.Node.String() != reference.String() {
					t.Fatalf("prefix/reference nodes differ: %v / %v", prefix.Node, reference)
				}
				if _, err = prog.ParseWith("main", source, options); err == nil {
					t.Fatal("whole-input parse accepted unconsumed suffix")
				}
			}
		}
	}
}
