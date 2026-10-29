package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
	"github.com/ornew/pego/schema"
)

func missingNodes(t *testing.T, root *value.Node) []*value.Node {
	t.Helper()
	var found []*value.Node
	seen := make(map[*value.Node]bool)
	if err := value.Walk(root, func(v value.WalkVisit) (value.WalkAction, error) {
		if v.Node != nil && v.Node.Type() == value.TypeMissing && !seen[v.Node] {
			seen[v.Node] = true
			found = append(found, v.Node)
		}
		return value.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	return found
}

func requireInsertions(t *testing.T, prog *Program, input string, o ParseOptions, count int) (*value.Node, SyntaxErrors) {
	t.Helper()
	n, err := prog.ParseWith("main", input, o)
	var recovered SyntaxErrors
	if !errors.As(err, &recovered) || len(recovered) != count {
		t.Fatalf("input %q: node=%v, recoveries=%#v, error=%v; want %d insertions", input, n, recovered, err, count)
	}
	if o.Recognize {
		if n != nil {
			t.Fatal("recognition returned a tree")
		}
	} else if n == nil {
		t.Fatal("recovered parse returned no tree")
	}
	return n, recovered
}

func TestMissingTokensLiteralAndUnits(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, controlled := range []bool{false, true} {
			if controlled && !core.WorkControlsEnabled {
				continue
			}
			o := ParseOptions{Unit: unit, MissingTokens: 8}
			if controlled {
				o.Context = context.Background()
				o.WorkLimits.MaxSteps = 1 << 20
			}
			prog := mustCompile(t, `rule main = "é" ";" #missing $$`)
			n, errs := requireInsertions(t, prog, "é", o, 1)
			insertions := missingNodes(t, n)
			pos := int32(1)
			if unit == Bytes {
				pos = 2
			}
			if len(insertions) != 1 {
				t.Fatalf("missing nodes: %v", insertions)
			}
			m := insertions[0]
			if value.D(m).Start != pos || value.D(m).End != pos || value.D(m).Text != "" || errs[0].Pos != int(pos) || errs[0].Col != int(pos)+1 {
				t.Fatal("fabricated source range/text or wrong unit", m, errs)
			}
			if expected, _ := value.D(m).Fields.Get("expected"); expected != ";" {
				t.Fatal(expected)
			}
			if message, _ := value.D(m).Fields.Get("message"); message != errs[0].Error() {
				t.Fatal(message, errs[0])
			}
			if _, err := prog.ParseWith("main", "é;", o); err != nil {
				t.Fatal("ordinary literal changed", err)
			}
			o.Recognize = true
			_, recognized := requireInsertions(t, prog, "é", o, 1)
			if !reflect.DeepEqual(errs, recognized) {
				t.Fatal("recognition diagnostic differs", errs, recognized)
			}
			// Strict mode and explicit zero retain the original acceptance set.
			o.Recognize, o.MissingTokens = false, 0
			if n, err := prog.ParseWith("main", "é", o); n != nil || err == nil {
				t.Fatal("strict mode inserted", n, err)
			}
		}
	}
	partial := mustCompile(t, `rule main = "ab" #missing "a" $$`)
	n, _ := requireInsertions(t, partial, "a", ParseOptions{MissingTokens: 2}, 1)
	if m := missingNodes(t, n)[0]; value.D(m).Start != 0 || value.D(m).End != 0 {
		t.Fatal("partial prefix was consumed", m)
	}
	invalid := mustCompile(t, `rule main = . ";" #missing $$`)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		n, errs := requireInsertions(t, invalid, "\xff", ParseOptions{Unit: unit, MissingTokens: 2}, 1)
		if value.D(missingNodes(t, n)[0]).Start != 1 || errs[0].Pos != 1 {
			t.Fatal("invalid UTF-8 position")
		}
	}
}

func TestMissingTokensRollbackBudgetAndCut(t *testing.T) {
	prog := mustCompile(t, `rule main = (token "x" / token "y") $$
rule token = ";" #missing`)
	for _, controlled := range []bool{false, true} {
		if controlled && !core.WorkControlsEnabled {
			continue
		}
		o := ParseOptions{MissingTokens: 2}
		if controlled {
			o.Context = context.Background()
		}
		n, _ := requireInsertions(t, prog, "y", o, 1)
		if len(missingNodes(t, n)) != 1 {
			t.Fatal("abandoned insertion leaked")
		}
		o.MissingTokens = 1
		n, err := prog.ParseWith("main", "y", o)
		var limit *MissingTokenLimitError
		if n != nil || !errors.As(err, &limit) || limit.Limit != 1 || limit.Position != 0 {
			t.Fatal("rollback refunded budget or reused a completed insertion", n, err)
		}
		if _, err := prog.ParseWith("main", ";y", ParseOptions{}); err != nil {
			t.Fatal("retry inherited insertion state", err)
		}
	}
	captures := mustCompile(t, `rule main = (x:(";" #missing) "a" / ";" #missing "b") $$`)
	n, _ := requireInsertions(t, captures, "b", ParseOptions{MissingTokens: 2}, 1)
	if _, ok := value.D(n).Fields.Get("x"); ok {
		t.Fatal("abandoned capture survived")
	}
	cut := mustCompile(t, `rule main = (";" #missing #cut "x" / "y") $$`)
	if n, err := cut.ParseWith("main", "y", ParseOptions{MissingTokens: 2}); n != nil || err == nil {
		t.Fatal("insertion changed cut scope", n, err)
	}
	chain := mustCompile(t, `rule main = ";" #missing ")" #missing $$`)
	_, errs := requireInsertions(t, chain, "", ParseOptions{MissingTokens: 2}, 2)
	if errs[0].Expected[0] != `";"` || errs[1].Expected[0] != `")"` {
		t.Fatal("diagnostic encounter order", errs)
	}
	// A whole-input failure exposes only its final ordinary syntax error.
	if n, err := chain.ParseWith("main", "x", ParseOptions{MissingTokens: 2}); n != nil {
		t.Fatal(n, err)
	} else {
		var ordinary *SyntaxError
		if !errors.As(err, &ordinary) {
			t.Fatal("recovery batch escaped a failed parse", err)
		}
	}
}

func TestMissingTokensLookaheadRepetitionsAndTwins(t *testing.T) {
	for _, source := range []string{
		`rule main = !token token $$
rule token = ";" #missing`,
		`rule main = (&token "x" / token) $$
rule token = ";" #missing`,
		`rule main = (token &token / token) $$
rule token = ";" #missing`,
	} {
		prog := mustCompile(t, source)
		requireInsertions(t, prog, "", ParseOptions{MissingTokens: 8}, 1)
	}
	for _, expression := range []string{`(";" #missing)*`, `(";" #missing)+`, `((";" #missing)*)*`} {
		prog := mustCompile(t, `rule main = `+expression+` $$`)
		n, _ := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 4}, 1)
		if len(missingNodes(t, n)) != 1 {
			t.Fatal("zero-progress repetition did not stop once")
		}
	}
	minimum := mustCompile(t, `rule main = (";" #missing) #repeat(2) $$`)
	if n, err := minimum.ParseWith("main", "", ParseOptions{MissingTokens: 4}); n != nil || err == nil {
		t.Fatal("nullable insertion met a two-iteration minimum", n, err)
	}
	// Atomic use invokes token's value-free twin. It must share the same site
	// identity as the later value-producing call at the same position.
	twins := mustCompile(t, `rule main = token #atomic token $$
rule token = ";" #missing`)
	if n, err := twins.ParseWith("main", "", ParseOptions{MissingTokens: 4}); n != nil || err == nil {
		t.Fatal("value-free twin supplied a second insertion site", n, err)
	}
}

func TestMissingTokensLeftRecursion(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		if controlled && !core.WorkControlsEnabled {
			continue
		}
		o := ParseOptions{MissingTokens: 64}
		if controlled {
			o.Context = context.Background()
			o.WorkLimits.MaxSteps = 1 << 20
		}
		for _, recognize := range []bool{false, true} {
			o.Recognize = recognize
			for _, source := range []string{
				`rule main = e $$
rule e = e "x" / ";" #missing`,
				`rule main = e $$
rule e = e !e "x" / ";" #missing`,
				`rule main = e $$
rule e = other "x" / ";" #missing
rule other = e`,
			} {
				prog := mustCompile(t, source)
				for _, input := range []string{"", "x", "xx"} {
					n, _ := requireInsertions(t, prog, input, o, 1)
					if !recognize && len(missingNodes(t, n)) != 1 {
						t.Fatal("LR seed diagnostics/value mismatch")
					}
				}
			}
			// A successful seed can only grow by consuming input. An extra
			// Missing value at the same end position does not win a growth tie.
			stable := mustCompile(t, `rule main = e $$
rule e = e ";" #missing / "x"`)
			n, err := stable.ParseWith("main", "x", o)
			if err != nil || len(missingNodes(t, n)) != 0 {
				t.Fatal("zero-width LR growth won", n, err)
			}
		}
	}
}

func TestMissingTokensOperatorsCandidates(t *testing.T) {
	infix := mustCompile(t, `rule main = e $$
rule e = #operators {
    operand "a"
    level { infix left "+" #missing }
}`)
	n, _ := requireInsertions(t, infix, "aa", ParseOptions{MissingTokens: 64}, 1)
	if len(missingNodes(t, n)) != 1 {
		t.Fatal("discarded operator candidate leaked")
	}
	postfix := mustCompile(t, `rule main = e $$
rule e = #operators {
    operand "a"
    level { postfix "!" #missing }
}`)
	if n, err := postfix.ParseWith("main", "a", ParseOptions{MissingTokens: 8}); err != nil || len(missingNodes(t, n)) != 0 {
		t.Fatal("empty postfix was applied", n, err)
	}
	nullable := mustCompile(t, `rule main = e $$
rule e = #operators {
    operand ""
    level { infix left "+" #missing }
}`)
	requireInsertions(t, nullable, "", ParseOptions{MissingTokens: 8}, 1)
}

func TestMissingTokensLabelsAndConsumingRecovery(t *testing.T) {
	for _, expression := range []string{
		`";" @error("semicolon") #missing`,
		`";" #missing @error("semicolon")`,
		`(";" #missing @error("inner")) @error("semicolon")`,
		`(";" #missing) #recover(skip="!") @error("semicolon")`,
	} {
		prog := mustCompile(t, `rule main = `+expression+` $$`)
		n, errs := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 4}, 1)
		if errs[0].Message() != "semicolon" || len(missingNodes(t, n)) != 1 {
			t.Fatal("outer label or recovery composition", n, errs)
		}
	}
	ordinary := mustCompile(t, `rule main = (";" #recover(skip="!")) @error("outer") $$`)
	_, err := ordinary.ParseWith("main", "!", ParseOptions{MissingTokens: 4})
	var errs SyntaxErrors
	if !errors.As(err, &errs) || errs[0].Message() == "outer" {
		t.Fatal("ordinary consuming recovery label changed", err)
	}
}

func TestMissingTokensValidationAndRepresentations(t *testing.T) {
	for _, expression := range []string{`"" #missing`, `[a-z] #missing`, `("a" "b") #missing`, `"a" @missing`, `"a" #atomic #missing`} {
		if message := compileError(t, `rule main = `+expression); !strings.Contains(message, "missing") {
			t.Fatal(expression, message)
		}
	}
	if message := compileError(t, `type Missing = { Text string }
rule main = "a"`); !strings.Contains(message, "type Missing is reserved") {
		t.Fatal(message)
	}
	const source = `type Item ~ node[string] | missing
type Root = { Token Item }
rule main Root = x:(";" #missing) => Root{Token: $x}`
	prog := mustCompile(t, source)
	n, _ := requireInsertions(t, prog, "", ParseOptions{MissingTokens: 1}, 1)
	if len(missingNodes(t, n)) != 1 {
		t.Fatal("declared join lost Missing identity")
	}
	data, err := grammar.MarshalJSON(prog.Grammar)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	parsed, err := parseTest(formatted)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Compile(parsed, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	requireInsertions(t, reloaded, "", ParseOptions{MissingTokens: 1}, 1)
	sch, err := outputSchemaPrototype(prog, "main", false)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip schema.Output
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{"name":"Token","shape":{"kind":"join","alternatives":[{"kind":"missing"},`) {
		t.Fatalf("schema lost Missing: %s", data)
	}
}

func TestMissingTokensLedgerSeedBudget(t *testing.T) {
	p := &core.Parser{Missing: &core.MissingTokenState{Limit: 2}}
	event := core.MissingTokenEvent{Site: core.MissingTokenSite{ID: 1, Pos: 3}, Err: &SyntaxError{Pos: 3}}
	if !p.InsertMissing(event) || p.InsertMissing(event) {
		t.Fatal("site guard")
	}
	seed := p.MissingEvents(0)
	p.ResetMissing(0)
	if p.Missing.Attempts != 1 || seed[0].Site != event.Site {
		t.Fatal("rollback refunded budget or cleared owned seed")
	}
	p.ReplayMissing(seed, []*SyntaxError{event.Err})
	p.ReplayMissing(seed, []*SyntaxError{event.Err})
	if p.Missing.Attempts != 2 || len(p.Recovered) != 1 {
		t.Fatal("installed event replay was charged or duplicated")
	}
	p.ResetMissing(0)
	defer func() {
		f, ok := recover().(core.Fatal)
		var limit *MissingTokenLimitError
		if !ok || !errors.As(f.Err, &limit) || limit.Position != 3 {
			t.Fatal("seed replay did not exhaust budget")
		}
	}()
	p.ReplayMissing(seed, []*SyntaxError{event.Err})
}

func TestMissingTokensModuleGuard(t *testing.T) {
	prog := mustCompile(t, `rule main = ";" #missing`)
	defer func() {
		x := recover()
		err, ok := x.(error)
		if !ok || !strings.Contains(err.Error(), "bytecode module construction") {
			t.Fatal("silent bytecode lowering", x)
		}
	}()
	prog.Module()
}

func TestMissingTokensPredicateRecognitionAndMemoDependencies(t *testing.T) {
	prog := mustCompile(t, `rule main = x:(token #atomic) {len($x) == 0} $$
rule token = ";" #missing`)
	for _, recognize := range []bool{false, true} {
		requireInsertions(t, prog, "", ParseOptions{MissingTokens: 4, Recognize: recognize}, 1)
	}
	dependent := mustCompile(t, `rule main = (outer "x" / outer "y") $$
rule outer = inner
rule inner = ";" #missing`)
	for _, name := range []string{"main", "outer", "inner"} {
		if !dependent.ByName[name].Missing {
			t.Fatal("missing dependency not transitive", name)
		}
	}
	// The budget must still be spent when completed nested rule results would
	// otherwise be reused after an alternative restores the same source position.
	if n, err := dependent.ParseWith("main", "y", ParseOptions{MissingTokens: 1}); n != nil {
		t.Fatal(n, err)
	} else {
		var limit *MissingTokenLimitError
		if !errors.As(err, &limit) {
			t.Fatal(err)
		}
	}
	// Explicitly inserting a value does not change how a successful plain
	// parse runs its unrelated action, while recognition may skip that action.
	actions := mustCompile(t, `type R = { N int }
rule main = ";" #missing => R{N: 1/0}`)
	if n, err := actions.ParseWith("main", "", ParseOptions{MissingTokens: 2}); n != nil || err == nil {
		t.Fatal("ordinary action was skipped", n, err)
	}
	requireInsertions(t, actions, "", ParseOptions{MissingTokens: 2, Recognize: true}, 1)
}

func TestMissingTokensRetainedResultsAndLiteralRecursion(t *testing.T) {
	prog := mustCompile(t, `rule main = e $$
rule e = ";" #missing e / "x"`)
	n, errs := requireInsertions(t, prog, "x", ParseOptions{MissingTokens: 8}, 1)
	before, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	retained := n.Clone()
	for i := 0; i < 4; i++ {
		requireInsertions(t, prog, "x", ParseOptions{MissingTokens: 8}, 1)
	}
	after, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := json.Marshal(retained)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(before) != string(cloned) || len(errs) != 1 {
		t.Fatal("later batch parses changed retained Missing results")
	}
}

func TestMissingTokensMemoSeedContextAndRecycledPointers(t *testing.T) {
	p := &core.Parser{Missing: &core.MissingTokenState{Limit: 8}, Memo: core.NewMemoTable()}
	err := &SyntaxError{Pos: 0}
	p.InsertMissing(core.MissingTokenEvent{Site: core.MissingTokenSite{ID: 1, Pos: 0}, Err: err})
	e := &core.MemoEntry{}
	p.RecordMissingSeed(e, 0)
	if len(p.MissingSeedEvents(e)) != 1 {
		t.Fatal("seed did not retain insertion")
	}
	p.ResetMissing(0)
	// A pointer retired by the memo slab can become a seed with no insertions.
	p.RecordMissingSeed(e, 0)
	if len(p.MissingSeedEvents(e)) != 0 {
		t.Fatal("recycled pointer inherited an insertion")
	}
	for _, controlled := range []bool{false, true} {
		p := &core.Parser{Memo: core.NewMemoTable()}
		if controlled && core.WorkControlsEnabled {
			p.Control = core.NewWorkControl(context.Background(), limits.WorkLimits{})
		}
		ordinary := core.MemoKey{Rule: 1}
		lookahead := core.MemoKey{Rule: 1, MissingSilent: true}
		p.MemoPut(ordinary, &core.MemoEntry{Ok: true, End: 7})
		p.MemoPut(lookahead, &core.MemoEntry{Ok: false})
		if e, ok := p.MemoGet(ordinary); !ok || !e.Ok || e.End != 7 {
			t.Fatal("lookahead replaced insertion-enabled seed")
		}
		if e, ok := p.MemoGet(lookahead); !ok || e.Ok {
			t.Fatal("lookahead imported an insertion-enabled seed")
		}
	}
}

func TestMissingTokensTypeOnlyGrammarCannotBeLowered(t *testing.T) {
	eachMode(t, testMissingTokensTypeOnlyGrammarCannotBeLowered)
}

func testMissingTokensTypeOnlyGrammarCannotBeLowered(t *testing.T) {
	prog := mustCompile(t, `type Item ~ missing
rule main = "a"`)
	if n, err := prog.Parse("main", "a"); n == nil || err != nil {
		t.Fatal(n, err)
	}
	if !prog.HasMissing {
		t.Fatal("reserved Missing reference was not recorded")
	}
	if _, err := prog.MarshalBinary("main"); err == nil {
		t.Fatal("saved a reserved type without a supported binary representation")
	}
	if _, err := generateCode(t, prog.Grammar, gen.GenOptions{Package: "example", Start: "main"}); err == nil {
		t.Fatal("generated unsupported Missing type")
	}
}

// TestMissingTypeChecked checks that a missing at run time takes only a missing token: an error
// value of recovery is an error but not a missing.
func TestMissingTypeChecked(t *testing.T) {
	o := ParseOptions{MissingTokens: 8}
	inserted := mustCompile(t, "type P = { M missing }\nrule main = \"a\" m:(\";\" #missing) $$ => P{M: $m.(_).(missing)}")
	n, err := inserted.ParseWith("main", "a", o)
	if n == nil || err == nil || strings.Contains(err.Error(), "cannot") {
		t.Fatalf("inserted: %v %v", n, err) // the tree, with the insertion's syntax error
	}
	recovered := mustCompile(t, "rule main = \"a\" m:inner \"b\" $$ => $m.(_).(missing)\nrule inner = \"c\" #recover(skip=[x-y]*)")
	if _, err := recovered.ParseWith("main", "axyb", o); err == nil || !strings.Contains(err.Error(), "cannot cast error to missing") {
		t.Fatalf("recovered: %v", err)
	}
}
