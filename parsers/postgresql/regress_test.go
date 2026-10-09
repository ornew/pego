package postgresql_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/postgresql"
)

var indexRE = regexp.MustCompile(`\[\d+\]`)

// corpus is one statement of the regression scripts of PostgreSQL 18 with the answer of the reference
// implementation (libpg_query 18, through pglast): internal/refgen/gen.py writes testdata/regress.jsonl.gz.
type corpusStmt struct {
	File string `json:"f"`
	Line int    `json:"l"`
	SQL  string `json:"sql"`
	OK   bool   `json:"ok"`
	Err  string `json:"err"`
	Tree []any  `json:"tree"`
}

func loadCorpus(t testing.TB) []corpusStmt {
	f, err := os.Open("testdata/regress.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	var out []corpusStmt
	for sc.Scan() {
		var s corpusStmt
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// stripLocations removes what the comparison ignores: the locations of the reference's nodes.
func stripLocations(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = stripLocations(x[i])
		}
		return x
	case map[string]any:
		for k, e := range x {
			switch k {
			case "location", "rexpr_list_start", "rexpr_list_end", "list_start", "list_end", "stmt_len", "stmt_location",
				"conninfo_location", "name_location", "arg_location", "payload_location":
				delete(x, k)
			default:
				x[k] = stripLocations(e)
			}
		}
		return x
	}
	return v
}

// firstDiff describes the first difference of two decoded JSON trees.
func firstDiff(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return path + ": " + fmt.Sprint(a) + " / " + fmt.Sprint(b)
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			xv, xok := x[k]
			yv, yok := y[k]
			if !xok {
				return path + "/" + k + ": absent / " + trunc(fmt.Sprint(yv))
			}
			if !yok {
				return path + "/" + k + ": " + trunc(fmt.Sprint(xv)) + " / absent"
			}
			if d := firstDiff(xv, yv, path+"/"+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path + ": list " + trunc(fmt.Sprint(a)) + " / " + trunc(fmt.Sprint(b))
		}
		for i := range x {
			if d := firstDiff(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	}
	if reflect.DeepEqual(a, b) {
		return ""
	}
	// numbers: int64 against float64
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok && af == bf {
			return ""
		}
	}
	return path + ": " + trunc(fmt.Sprint(a)) + " / " + trunc(fmt.Sprint(b))
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}

func trunc(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// TestRegress parses every statement of the regression scripts of PostgreSQL and compares with the
// reference: whether it is accepted, and for the statements that both accept, the raw parse tree.
func TestRegress(t *testing.T) {
	corpus := loadCorpus(t)
	var accepted, rejected, agree, bothAccept, treeEqual int
	var accDiffs, treeDiffs []string
	cats := map[string]int{}
	catEx := map[string]string{}
	for _, s := range corpus {
		script, err := postgresql.ParseAST(s.SQL)
		ok := err == nil
		if ok {
			accepted++
		} else {
			rejected++
		}
		if ok != s.OK {
			if len(accDiffs) < 200 {
				accDiffs = append(accDiffs, fmt.Sprintf("%s:%d accepted=%v reference=%v: %.100q", s.File, s.Line, ok, s.OK, s.SQL))
			}
			continue
		}
		agree++
		if !ok {
			continue
		}
		bothAccept++
		tree, err := postgresql.RawTree(script)
		if err != nil {
			if len(treeDiffs) < 200 {
				treeDiffs = append(treeDiffs, fmt.Sprintf("%s:%d %v: %.100q", s.File, s.Line, err, s.SQL))
			}
			continue
		}
		// normalize through JSON so that numbers compare equal
		b, _ := json.Marshal(tree)
		var mine []any
		json.Unmarshal(b, &mine)
		ref := stripLocations(any(s.Tree)).([]any)
		if d := firstDiff(mine, ref, ""); d == "" {
			treeEqual++
		} else {
			cat := indexRE.ReplaceAllString(strings.SplitN(d, ": ", 2)[0], "[]")
			cats[cat]++
			if _, ok := catEx[cat]; !ok {
				catEx[cat] = fmt.Sprintf("%s:%d %s: %.100q", s.File, s.Line, d, s.SQL)
			}
			if len(treeDiffs) < 200 {
				treeDiffs = append(treeDiffs, fmt.Sprintf("%s:%d %s: %.100q", s.File, s.Line, d, s.SQL))
			}
		}
	}
	t.Logf("%d statements: accepted %d, rejected %d; the grammar and the reference agree on %d; of the %d accepted by both, %d have equal trees",
		len(corpus), accepted, rejected, agree, bothAccept, treeEqual)
	if os.Getenv("PG_CATS") != "" {
		var ks []string
		for k := range cats {
			ks = append(ks, k)
		}
		sort.Slice(ks, func(i, j int) bool { return cats[ks[i]] > cats[ks[j]] })
		for i, k := range ks {
			if i < 40 {
				t.Logf("%5d %s\n      e.g. %s", cats[k], k, trunc(catEx[k]))
			}
		}
	}
	if os.Getenv("PG_SHOW") != "" {
		t.Log("acceptance differences:\n" + strings.Join(accDiffs, "\n"))
	}
	if os.Getenv("PG_SHOW") != "" || os.Getenv("PG_TREES") != "" {
		t.Log("tree differences:\n" + strings.Join(treeDiffs, "\n"))
	}
}
