package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/ir/flow"
)

// stmt returns a removable assignment of the targets, with the values reading uses.
func testVarStmt(fv *gogen.FlowVars, text string, pure bool, targets []*flow.Var, uses ...*flow.Var) string {
	st := &gogen.VarStmt{Targets: targets, Pure: pure, Uses: map[*flow.Var]bool{}}
	for _, u := range uses {
		st.Uses[u] = true
	}
	st.Text = func(dead func(*flow.Var) bool) string {
		out := text
		for _, t := range targets {
			if t != nil && dead(t) {
				out = strings.ReplaceAll(out, t.Name, "_")
			}
		}
		return out + "\n"
	}
	return fv.Add(st)
}

func testVars(names ...string) []*flow.Var {
	var vs []*flow.Var
	for _, n := range names {
		vs = append(vs, &flow.Var{Name: n, Type: flow.TypeInt})
	}
	return vs
}

func TestFlowVarsCascade(t *testing.T) {
	t.Parallel()
	// b = a reads a; nothing reads b, so the statement goes and a becomes unread, and then a = p.Pos goes.
	fv := gogen.NewFlowVars()
	v := testVars("a", "b", "c")
	body := testVarStmt(&fv, "a = p.Pos", true, v[:1]) + testVarStmt(&fv, "b = a", true, v[1:2], v[0]) +
		testVarStmt(&fv, "c = p.Pos", true, v[2:3])
	fv.Hard[v[2]] = true
	res := fv.Resolve(v, body+"tail\n")
	if res.Body != "c = p.Pos\ntail\n" {
		t.Errorf("body %q", res.Body)
	}
	if !res.Dead(v[0]) || !res.Dead(v[1]) || res.Dead(v[2]) || !res.Read(v[2]) {
		t.Errorf("dead a %v b %v c %v, read c %v", res.Dead(v[0]), res.Dead(v[1]), res.Dead(v[2]), res.Read(v[2]))
	}
}

func TestFlowVarsEffectsStay(t *testing.T) {
	t.Parallel()
	// A call has an effect: the statement stays with the blank identifier for the unread result, and the
	// variable its arguments read stays read.
	fv := gogen.NewFlowVars()
	v := testVars("d", "ok", "y", "x")
	body := testVarStmt(&fv, "d, ok = call()", false, v[:2]) + testVarStmt(&fv, "x = f(y)", false, v[3:4], v[2])
	fv.Hard[v[1]] = true
	res := fv.Resolve(v, body)
	if want := "_, ok = call()\n_ = f(y)\n"; res.Body != want {
		t.Errorf("body %q, want %q", res.Body, want)
	}
	if !res.Dead(v[0]) || !res.Dead(v[3]) || res.Dead(v[1]) || res.Dead(v[2]) || !res.Read(v[2]) {
		t.Errorf("dead d %v ok %v y %v x %v", res.Dead(v[0]), res.Dead(v[1]), res.Dead(v[2]), res.Dead(v[3]))
	}
}

func TestFlowVarsStaySilenced(t *testing.T) {
	t.Parallel()
	// A variable assigned where no statement can be left out, or opposite an untyped nil, stays declared
	// when nothing reads it. So does the target of a statement that is not a variable of the function.
	fv := gogen.NewFlowVars()
	v := testVars("s", "n", "w")
	other := &flow.Var{Name: "v"}
	fv.Stuck[v[0]] = true
	fv.Pinned[v[1]] = true
	body := testVarStmt(&fv, "w = p.Pos", true, v[2:3]) + testVarStmt(&fv, "v = p.Pos", true, []*flow.Var{other}) +
		testVarStmt(&fv, "p.Pos = 1", true, []*flow.Var{nil})
	res := fv.Resolve(v, body)
	if res.Body != "v = p.Pos\np.Pos = 1\n" {
		t.Errorf("body %q", res.Body)
	}
	for i, want := range []bool{false, false, true} {
		if res.Dead(v[i]) != want || res.Read(v[i]) {
			t.Errorf("%s: dead %v read %v, want dead %v and unread", v[i].Name, res.Dead(v[i]), res.Read(v[i]), want)
		}
	}
}

func TestFlowVarsCycleStays(t *testing.T) {
	t.Parallel()
	// Two variables that only read each other stay, as the removal of one statement at a time leaves
	// them: each statement reads the variable the other writes.
	fv := gogen.NewFlowVars()
	v := testVars("x", "y")
	body := testVarStmt(&fv, "x = y", true, v[:1], v[1]) + testVarStmt(&fv, "y = x", true, v[1:], v[0])
	res := fv.Resolve(v, body)
	if res.Body != "x = y\ny = x\n" || res.Dead(v[0]) || res.Dead(v[1]) {
		t.Errorf("body %q", res.Body)
	}
}

func TestFlowVarsUnreachableReadsDoNotCount(t *testing.T) {
	t.Parallel()
	// The printer records no read at an unreachable point, so a variable read only there is dead.
	fv := gogen.NewFlowVars()
	v := testVars("a")
	body := testVarStmt(&fv, "a = p.Pos", true, v)
	res := fv.Resolve(v, body)
	if res.Body != "" || !res.Dead(v[0]) {
		t.Errorf("body %q", res.Body)
	}
}

// The tests of generated code catch a wrong decision of which variables are unread: one that treats
// every variable as unread, and one that ignores the reads of the assignments that stay.
func TestFlowVarsMutantFailsToCompile(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]*gogen.Hooks{"every variable unread": {FlowVarsMutant: true}, "assignment reads ignored": {FlowVarsMutantUses: true}} {
		t.Run(name, func(t *testing.T) {
			setGenHooks(t, h)
			testFlowVarsMutantFailsToCompile(t)
		})
	}
}

func testFlowVarsMutantFailsToCompile(t *testing.T) {
	g, err := parseTest("rule main = (kw / id)+ !.\nrule kw = \"select\"i / \"from\"i / \"x\"\nrule id = [a-z]+")
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(t, g, gen.GenOptions{Package: "p", Start: "main", Runtime: gen.RuntimeImport, Module: "example.com/p", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(localErrors(t, files)) == 0 {
		t.Error("no compile error with every variable treated as unread")
	}
}

// The check of the printed body fires when it names a variable that is not declared.
func TestFlowVarsNamedDeadVariableIsAnError(t *testing.T) {
	t.Parallel()
	fv := gogen.NewFlowVars()
	v := testVars("v3", "v4")
	body := testVarStmt(&fv, "v3 = p.Pos", true, v[:1]) + "use(v3)\n"
	res := fv.Resolve(v, body)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "internal error") || !strings.Contains(res.Err.Error(), "v3") {
		t.Errorf("err = %v, want an internal error that names v3", res.Err)
	}
	// A body that does not name it, or a function with nothing dead, has no error.
	fv = gogen.NewFlowVars()
	v = testVars("v3")
	if res := fv.Resolve(v, testVarStmt(&fv, "v3 = p.Pos", true, v)+"use(w)\n"); res.Err != nil {
		t.Errorf("err = %v", res.Err)
	}
	fv = gogen.NewFlowVars()
	v = testVars("v3")
	fv.Hard[v[0]] = true
	if res := fv.Resolve(v, "use(v3)\n"); res.Err != nil {
		t.Errorf("err = %v", res.Err)
	}
}

func TestFreeIdents(t *testing.T) {
	t.Parallel()
	got := strings.Join(gogen.FreeIdents("x = p.v3 + f(\"v4\") // v5\n/* v6 */ y := v7\n"), " ")
	if want := "x p f y v7"; got != want {
		t.Errorf("freeIdents = %q, want %q (a selector, a string and a comment do not count)", got, want)
	}
}
