package engine

import (
	"fmt"
	"reflect"
	"testing"
)

// TestTraceFirstCharacterDispatch records executed calls, not synthetic calls
// for rejected alternatives. Closure guards every alternative; the bytecode
// compiler leaves the final alternative unguarded, so both VMs enter tail even
// when its first character cannot match. Profiling reflects that actual work.
func TestTraceFirstCharacterDispatch(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = "a" / tail
rule tail = "b"`)
	type event struct {
		kind                 TraceKind
		rule                 string
		depth, pos, level    int
		lookahead, matched   bool
		end, evals, examined int
		memo                 bool
	}
	enter := func(rule string, depth int) event {
		return event{kind: TraceEnter, rule: rule, depth: depth}
	}
	exit := func(rule string, depth int, matched bool) event {
		end := 0
		if matched {
			end = 1
		}
		// EOF probes at position zero also examine through position one.
		return event{kind: TraceExit, rule: rule, depth: depth,
			matched: matched, end: end, evals: 1, examined: 1}
	}
	for _, tc := range []struct {
		name, input string
		matched     bool
	}{
		{"first alternative matches", "a", true},
		{"last alternative matches", "b", true},
		{"first character rejects both", "z", false},
		{"EOF rejects both", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := prog.ParseWith("main", tc.input, ParseOptions{Backend: Closure})
			wantResult := resultJSON(n, err)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				t.Run(fmt.Sprint(backend), func(t *testing.T) {
					var pr Profile
					var got []event
					n, err := prog.ParseWith("main", tc.input, ParseOptions{
						Backend: backend,
						Trace: func(e TraceEvent) {
							pr.Trace(e)
							got = append(got, event{kind: e.Kind, rule: e.Rule, depth: e.Depth,
								pos: e.Pos, level: e.Level, lookahead: e.Lookahead,
								matched: e.Matched, end: e.End, evals: e.Evals,
								examined: e.Examined, memo: e.Memo})
						},
					})
					if (err == nil) != tc.matched {
						t.Fatalf("match=%v, error=%v", tc.matched, err)
					}
					if result := resultJSON(n, err); result != wantResult {
						t.Fatalf("trace changes result: got %s, want %s", result, wantResult)
					}
					plain, plainErr := prog.ParseWith("main", tc.input, ParseOptions{Backend: backend})
					if result := resultJSON(plain, plainErr); result != wantResult {
						t.Fatalf("untraced backend result: got %s, want %s", result, wantResult)
					}

					enteredTail := tc.input == "b" || tc.input != "a" && backend != Closure
					want := []event{enter("main", 1)}
					if enteredTail {
						want = append(want, enter("tail", 2), exit("tail", 2, tc.matched))
					}
					want = append(want, exit("main", 1, tc.matched))
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("events:\n got %+v\nwant %+v", got, want)
					}

					calls := 1
					if enteredTail {
						calls++
					}
					if pr.Parses != 1 || pr.Calls != calls || pr.Evals != calls ||
						pr.MemoHits != 0 || pr.Examined != 1 || len(pr.Rules) != calls {
						t.Fatalf("profile totals = %+v; want %d executed calls/evaluations", pr, calls)
					}
					for i, r := range pr.Rules {
						name := "main"
						if i == 1 {
							name = "tail"
						}
						matched, failed, consumed, wasted := 0, 1, 0, 1
						if tc.matched {
							matched, failed, consumed, wasted = 1, 0, 1, 0
						}
						if r.Rule != name || r.Calls != 1 || r.Evals != 1 || r.MemoHits != 0 ||
							r.Matched != matched || r.Failed != failed || r.Consumed != consumed ||
							r.Wasted != wasted || r.Repeats != 0 || r.MaxEvals != 1 {
							t.Fatalf("profile row = %+v", r)
						}
					}
				})
			}
		})
	}
}
