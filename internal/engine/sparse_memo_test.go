package engine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestSparseMemoRuleAndPageBoundaries(t *testing.T) {
	for _, stride := range []int{63, 64, 65, 128, 175} {
		memo := memoTable{stride: stride}
		positions := []int{0, 1, 63, 64, 1023, 1024, 1025, 65535}
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) || sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: first/repeated mismatch", stride, pos, rule)
				}
			}
		}
		for i := len(positions) - 1; i >= 0; i-- {
			if sparseMemoRecord(&memo, positions[i], stride-1) {
				t.Fatal("returning to an earlier page lost its bit")
			}
		}
		if !memo.reset() {
			t.Fatal("bounded memo table rejected reuse")
		}
		for _, dir := range memo.pages {
			if dir.last != nil {
				t.Fatal("cached page pointer survived reset")
			}
		}
		memo.stride = stride
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: stale bit survived reset", stride, pos, rule)
				}
			}
		}
		if sparseMemo && stride > 64 && len(memo.seen) != 0 {
			t.Fatal("sparse path allocated the dense bitset")
		}
	}
}

func sparseMemoRecord(memo *memoTable, pos, rule int) bool {
	if sparseMemo && memo.stride > 64 {
		return memo.markSparseSeen(pos, rule)
	}
	return memo.markSeen(pos, rule)
}

func TestSparseMemoStrideChanges(t *testing.T) {
	memo := memoTable{}
	for _, stride := range []int{175, 65, 64, 128, 175} {
		memo.stride = stride
		if !sparseMemoRecord(&memo, 1024, stride-1) || sparseMemoRecord(&memo, 1024, stride-1) {
			t.Fatal("stride change reused a stale bit")
		}
		if !memo.reset() {
			t.Fatal("bounded table rejected reuse")
		}
	}
}

func TestSparseMemoResetBounds(t *testing.T) {
	for _, memo := range []memoTable{
		{pages: make([]seenRulePages, 0, maxScratch/5+1)},
		{pages: make([]seenRulePages, 0, maxScratch/5), bitPages: arena[[16]uint64]{chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{pages: []seenRulePages{{pages: make([]*[16]uint64, 0, maxScratch+1)}}},
	} {
		if memo.reset() {
			t.Fatal("oversized directory retained")
		}
	}
	memo := memoTable{stride: 65}
	for i := 0; i <= maxScratch/16; i++ {
		memo.markSparseSeen(i*1024, 0)
	}
	if memo.reset() {
		t.Fatal("oversized pages retained")
	}
}

func TestSparseMemoParseControls(t *testing.T) {
	src, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prog.nseen <= 64 {
		t.Fatal("control no longer exercises sparse rule count")
	}
	for i, input := range []string{"const 日本語 = \"😀\";\nconst v = [1, 2].map(x => x + 1);\n", "function f( { return 1;", strings.Repeat("const 日本語 = 1 + 2;\n", 100), strings.Repeat("const a = 1;\n", 100) + "function f( { return 1;"} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, recognize := range []bool{false, true} {
				checkTrace(t, prog, "main", input, ParseOptions{Unit: unit, Recognize: recognize})
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					work, result := parseWork(prog, "main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					t.Logf("semantic=%d/%s/%s/%t result=%x work=%x", i, backend, unit, recognize, sha256.Sum256([]byte(result)), sha256.Sum256([]byte(fmt.Sprintf("%+v", work))))
				}
			}
		}
	}
}

func TestSparseMemoInputSelection(t *testing.T) {
	data, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"/*" + strings.Repeat("a", 1020) + "*/",
		"/*" + strings.Repeat("a", 1021) + "*/",
		"/*" + strings.Repeat("é", 512) + "*/",
		"/*" + strings.Repeat("a", 1020) + "*/",
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				p := newParser(prog, text, unit, true)
				p.deferMemo = true
				if _, err := prog.run(p, backend, "main"); err != nil {
					t.Fatal(err)
				}
				want := len([]rune(text)) > 1024
				if unit == Bytes {
					want = len(text) > 1024
				}
				want = want && sparseMemo
				if p.memo.sparse != want {
					t.Fatalf("%s/%s: sparse=%t want=%t", backend, unit, p.memo.sparse, want)
				}
				if !want && len(p.memo.pages) != 0 {
					t.Fatal("short input allocated sparse directory")
				}
			}
		}
	}
}
