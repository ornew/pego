package engine

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// updateGoldens rewrites the entries of the key/value goldens (goldenFile.check) that a run checks, instead
// of comparing them; entries of the checked scope that the run no longer produces are removed:
//
//	go test ./internal/engine -run <test> -update
//
// Review the diff of a rewritten golden: a regenerated golden makes a regression pass.
var updateGoldens = flag.Bool("update", false, "rewrite the entries of the golden files that the run checks")

// readGolden reads the file of "key<TAB>value" lines.
func readGolden(t testing.TB, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// writeGolden writes the golden with its header comment and its entries in key order.
func writeGolden(t testing.TB, path, header string, entries map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(header)
	for _, k := range keys {
		b.WriteString(k + "\t" + entries[k] + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goldenRules is the part of the header of every key/value golden that says how it is kept.
const goldenRules = "# A run checks every entry of its scope: an entry that is missing, stale or different fails. -update replaces the\n" +
	"# entries of the scope, drops stale ones and logs a summary per case. Review the diff: it is a regression freeze.\n"

// goldenFile describes a key/value golden: its path, its header, how a key maps to the case it belongs to
// (for the update summary; nil means the key is the case) and, for goldens of measured totals, the two
// totals of a value that the summary sums (nil for none).
type goldenFile struct {
	path, header string
	caseOf       func(key string) string
	totals       func(value string) (a, b uint64)
	totalNames   [2]string
}

// goldenCaseBefore returns a caseOf for keys "case | entry": the part before the last " | ".
func goldenCaseBefore(key string) string {
	if i := strings.LastIndex(key, " | "); i >= 0 {
		return key[:i]
	}
	return key
}

// check compares the entries of got, which are all the keys with the prefix scope that a complete run
// produces, with the golden. A key of got that the golden lacks, a value that differs and a key of the
// golden with the prefix scope that the run did not produce (a case that was removed or renamed, or a
// report that is missing) are errors. A run that filters its entries must not call check. With -update
// the entries with the prefix scope are replaced by got, the others stay, and the summary of what changed
// is logged for each case.
func (g goldenFile) check(t *testing.T, scope string, got map[string]string) {
	t.Helper()
	if len(got) == 0 {
		t.Errorf("%s: nothing to compare with %s", t.Name(), g.path)
		return
	}
	for k := range got {
		if !strings.HasPrefix(k, scope) {
			t.Fatalf("%s: key %q is outside the scope %q", t.Name(), k, scope)
		}
	}
	want := readGolden(t, g.path)
	if *updateGoldens {
		g.logSummary(t, scope, want, got)
		for k := range want {
			if strings.HasPrefix(k, scope) {
				delete(want, k)
			}
		}
		for k, v := range got {
			want[k] = v
		}
		writeGolden(t, g.path, g.header, want)
		return
	}
	var problems []string
	for _, k := range sortedKeys(got) {
		w, ok := want[k]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("no entry for %q", k))
		case w != got[k]:
			problems = append(problems, fmt.Sprintf("%q differs:\n got  %s\n want %s", k, got[k], w))
		}
	}
	for _, k := range sortedKeys(want) {
		if _, ok := got[k]; !ok && strings.HasPrefix(k, scope) {
			problems = append(problems, fmt.Sprintf("the run produced no entry for %q", k))
		}
	}
	for i, p := range problems {
		if i == 20 {
			t.Errorf("%s: and %d more differences from %s", t.Name(), len(problems)-i, g.path)
			break
		}
		t.Errorf("%s: %s in %s (run with -update after review)", t.Name(), p, g.path)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// logSummary logs, for each case with a change under scope, the number of changed, added and removed
// entries and, for a golden with totals, the sums of the totals before and after.
func (g goldenFile) logSummary(t *testing.T, scope string, old, got map[string]string) {
	t.Helper()
	type delta struct {
		changed, added, removed int
		before, after           [2]uint64
	}
	deltas := map[string]*delta{}
	of := func(k string) *delta {
		c := k
		if g.caseOf != nil {
			c = g.caseOf(k)
		}
		d := deltas[c]
		if d == nil {
			d = &delta{}
			deltas[c] = d
		}
		return d
	}
	sum := func(arr *[2]uint64, v string) {
		if g.totals != nil {
			a, b := g.totals(v)
			arr[0], arr[1] = arr[0]+a, arr[1]+b
		}
	}
	for k, v := range old {
		if !strings.HasPrefix(k, scope) {
			continue
		}
		d := of(k)
		sum(&d.before, v)
		if nv, ok := got[k]; !ok {
			d.removed++
		} else if nv != v {
			d.changed++
		}
	}
	for k, v := range got {
		d := of(k)
		sum(&d.after, v)
		if _, ok := old[k]; !ok {
			d.added++
		}
	}
	var names []string
	for c, d := range deltas {
		if d.changed+d.added+d.removed != 0 {
			names = append(names, c)
		}
	}
	sort.Strings(names)
	var tc, ta, tr int
	for _, c := range names {
		d := deltas[c]
		tc, ta, tr = tc+d.changed, ta+d.added, tr+d.removed
		line := fmt.Sprintf("update %s: %s: %d changed, %d added, %d removed", g.path, c, d.changed, d.added, d.removed)
		if g.totals != nil {
			line += fmt.Sprintf("; %s %d -> %d, %s %d -> %d", g.totalNames[0], d.before[0], d.after[0], g.totalNames[1], d.before[1], d.after[1])
		}
		t.Log(line)
	}
	t.Logf("update %s scope %q: %d cases changed (%d entries changed, %d added, %d removed) of %d entries", g.path, scope, len(names), tc, ta, tr, len(got))
}
