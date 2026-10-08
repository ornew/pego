package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const pairs = `
def main = ws pair+ $$
def pair = key "=" value ";"? ws
def key = @(?a-z)+
def value = @(?0-9)+
def ws = (? \n)*`

func TestTrace(t *testing.T) {
	g := writeFile(t, "pairs.pego", pairs)
	out, err := runCLI(t, "", "trace", "-g", g, "-i", "a=1;\nb=x", "-failures")
	if err == nil || err.Error() != "2:3: syntax error: expected (?0-9)" {
		t.Errorf("error %v", err)
	}
	want := `main 1:1
  ws 1:1 -> matched 1:1-1:1 ""
  pair 1:1
    key 1:1 -> matched 1:1-1:2 "a"
    value 1:3 -> matched 1:3-1:4 "1"
    ws 1:5 -> matched 1:5-2:1 "\n"
  pair -> matched 1:1-2:1 "a=1;\n"
  pair 2:1
    key 2:1 -> matched 2:1-2:2 "b"
    value 2:3 -> failed (at 2:3: expected (?0-9))
  pair -> failed (at 2:3: expected (?0-9))
main -> failed (at 2:3: expected (?0-9))
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// -rule shows the calls of the rule (and those nested in them) at the top level; -max-depth
	// hides deeper calls.
	out, err = runCLI(t, "a=1;b=22", "trace", "-g", g, "-rule", "value,ws", "-backend", "bytecode")
	want = `ws 1:1 -> matched 1:1-1:1 ""
value 1:3 -> matched 1:3-1:4 "1"
ws 1:5 -> matched 1:5-1:5 ""
value 1:7 -> matched 1:7-1:9 "22"
ws 1:9 -> matched 1:9-1:9 ""
`
	if err != nil || out != want {
		t.Errorf("-rule: got %v\n%s\nwant\n%s", err, out, want)
	}
	out, err = runCLI(t, "a=1;b=22", "trace", "-g", g, "-max-depth", "2", "-unit", "bytes")
	want = `main 1:1
  ws 1:1 -> matched 1:1-1:1 ""
  pair 1:1 -> matched 1:1-1:5 "a=1;"
  pair 1:5 -> matched 1:5-1:9 "b=22"
  pair 1:9 -> failed
main -> matched 1:1-1:9 "a=1;b=22"
`
	if err != nil || out != want {
		t.Errorf("-max-depth: got %v\n%s\nwant\n%s", err, out, want)
	}

	// JSON: one event per line
	out, err = runCLI(t, "", "trace", "-g", g, "-i", "a=x", "-f", "json", "-failures", "-rule", "value")
	if err == nil {
		t.Error("expected a syntax error")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines:\n%s", len(lines), out)
	}
	var enter, exit map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &enter); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &exit); err != nil {
		t.Fatal(err)
	}
	if enter["event"] != "enter" || enter["rule"] != "value" || enter["depth"] != 3.0 || enter["pos"] != 2.0 || enter["col"] != 3.0 {
		t.Errorf("enter %v", enter)
	}
	failure, _ := exit["failure"].(map[string]any)
	if exit["event"] != "exit" || exit["matched"] != false || exit["evals"] != 1.0 || failure == nil || failure["col"] != 3.0 ||
		strings.Join(toStrings(failure["expected"]), ",") != "(?0-9)" {
		t.Errorf("exit %v", exit)
	}

	if _, err := runCLI(t, "", "trace", "-g", g, "-f", "xml"); err == nil {
		t.Error("expected an error for an unknown format")
	}
	if _, err := runCLI(t, "", "trace", "-i", "a=1"); err == nil {
		t.Error("expected an error without -g")
	}
}

func toStrings(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, x := range l {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// TestTraceLeftRecursion shows the growth of a left-recursive rule and memo hits.
func TestTraceLeftRecursion(t *testing.T) {
	g := writeFile(t, "lr.pego", `
def main = sum $$
def sum = sum "+" n / n
def n = @(?0-9)+`)
	out, err := runCLI(t, "", "trace", "-g", g, "-i", "1+2")
	want := `main 1:1
  sum 1:1
    sum 1:1 -> failed [memo]
    n 1:1 -> matched 1:1-1:2 "1"
    sum 1:1 -> matched 1:1-1:2 "1" [memo]
    n 1:3 -> matched 1:3-1:4 "2"
    sum 1:1 -> matched 1:1-1:4 "1+2" [memo]
    n 1:1 -> matched 1:1-1:2 "1"
  sum -> matched 1:1-1:4 "1+2" [3 evaluations]
main -> matched 1:1-1:4 "1+2"
`
	if err != nil || out != want {
		t.Errorf("got %v\n%s\nwant\n%s", err, out, want)
	}
}

func TestProfileCmd(t *testing.T) {
	g := writeFile(t, "pairs.pego", pairs)
	out, err := runCLI(t, "a=1;b=22;c=333", "profile", "-g", g, "-sort", "name")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"result: ok\n",
		"examined 15 positions in ",
		"16 calls, 16 body evaluations, 0 memo hits (0% of calls)\n",
		"rule  calls  evals  memo  matched  failed  repeats  consumed  wasted",
		"What to look at:\n- Most time is spent in ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Rows sorted by name; the counts do not depend on time.
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 12 && f[0] != "rule" {
			rows = append(rows, strings.Join(f[:9], " "))
		}
	}
	want := []string{
		"key 4 4 0 3 1 0 3 1",
		"main 1 1 0 1 0 0 14 0",
		"pair 4 4 0 3 1 0 14 1",
		"value 3 3 0 3 0 0 6 0",
		"ws 4 4 0 4 0 0 0 0",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}

	out, err = runCLI(t, "", "profile", "-g", g, "-i", "a=1;b=", "-f", "json", "-n", "2", "-sort", "calls")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Result        string
		Parses, Calls int
		Rules         []struct {
			Rule  string
			Calls int
		}
		Hints []string
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Result != "1:7: syntax error: expected (?0-9)" || res.Parses != 1 || len(res.Rules) != 2 || res.Rules[0].Calls < res.Rules[1].Calls || len(res.Hints) == 0 {
		t.Errorf("got %+v", res)
	}
	if _, err := runCLI(t, "", "profile", "-g", g, "-sort", "size"); err == nil {
		t.Error("expected an error for an unknown sort column")
	}
}

func TestExplain(t *testing.T) {
	g := writeFile(t, "pairs.pego", pairs)
	out, err := runCLI(t, "", "explain", "-g", g, "-i", "a=1;\nb=x")
	want := `2:3: syntax error: expected (?0-9)

Calls that recorded what was expected at 2:3 (innermost first):

  value 2:3: expected (?0-9)
    in pair 2:1
    in main 1:1
`
	if err != nil || out != want {
		t.Errorf("got %v\n%s\nwant\n%s", err, out, want)
	}
	// Calls that matched can record expectations after their match; each call shows only the
	// expectations it recorded itself. The start rule's $$ expects the end of input.
	out, err = runCLI(t, "", "explain", "-g", g, "-i", "a=1 !", "-backend", "bytecode-iterative")
	want = `1:5: syntax error: expected (? \n), (?a-z), end of input

Calls that recorded what was expected at 1:5 (innermost first):

  ws 1:4: matched " ", then expected (? \n)
    in pair 1:1
    in main 1:1

  key 1:5: expected (?a-z)
    in pair 1:5
    in main 1:1

  main 1:1: expected end of input
`
	if err != nil || out != want {
		t.Errorf("got %v\n%s\nwant\n%s", err, out, want)
	}
	// Without $$, the parser expects the end of input after the start rule returns.
	unanchored := writeFile(t, "unanchored.pego", `def main = pair+
def pair = @(?a-z) "=" @(?0-9)`)
	out, err = runCLI(t, "", "explain", "-g", unanchored, "-i", "a=1!")
	want = `1:4: syntax error: expected (?a-z), end of input

Calls that recorded what was expected at 1:4 (innermost first):

  pair 1:4: expected (?a-z)
    in main 1:1

  The start rule matched up to 1:4, but the input does not end there.
`
	if err != nil || out != want {
		t.Errorf("got %v\n%s\nwant\n%s", err, out, want)
	}
	if out, err := runCLI(t, "", "explain", "-g", g, "-i", "a=1"); err != nil || out != "ok: the input matches\n" {
		t.Errorf("got %v %q", err, out)
	}
	// Errors recovered by #recover are explained one by one.
	r := writeFile(t, "recover.pego", `
def main = stmt* $$
def stmt = (word ";") #recover(skip=(?^;)+ ";")
def word = @(?a-z)+`)
	out, err = runCLI(t, "", "explain", "-g", r, "-i", "a;1;b;2;")
	for _, want := range []string{
		"1:3: syntax error: expected (?a-z) (recovered)\n",
		"  word 1:3: expected (?a-z)\n    in stmt 1:3\n    in main 1:1\n",
		"1:7: syntax error: expected (?a-z) (recovered)\n",
	} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("got %v, output lacks %q:\n%s", err, want, out)
		}
	}
}

// TestExplainRecoveredBody checks that the expectations of the expression a #recover recovered
// from, which the record of the enclosing call no longer holds, are attributed to that call.
func TestExplainRecoveredBody(t *testing.T) {
	r := writeFile(t, "recover.pego", `
def main = stmt* $$
def stmt = (word ";") #recover(skip=(?^;)+ ";")
def word = @(?a-z)+`)
	out, err := runCLI(t, "", "explain", "-g", r, "-i", "ab1;cd;", "-backend", "bytecode")
	want := `1:3: syntax error: expected ";", (?a-z)

Calls that recorded what was expected at 1:3 (innermost first):

  word 1:1: matched "ab", then expected (?a-z)
    in stmt 1:1
    in main 1:1

  stmt 1:1: expected ";" (recovered by #recover)
    in main 1:1
`
	if err != nil || out != want {
		t.Errorf("got %v\n%s\nwant\n%s", err, out, want)
	}
}
