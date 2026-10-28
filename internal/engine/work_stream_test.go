//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func workStreamVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{Bytes, CodePoints} {
			t.Run(fmt.Sprintf("%s/%s", b, u), func(t *testing.T) { run(t, ParseOptions{Backend: b, Unit: u}) })
		}
	}
}

type workStreamReader struct {
	chunks    []string
	err       error
	cancel    func()
	cancelAt  int
	dataError bool
	reads     int
	closed    bool
}

func (r *workStreamReader) Read(dst []byte) (int, error) {
	r.reads++
	if r.cancel != nil && r.reads == r.cancelAt {
		r.cancel()
	}
	if len(r.chunks) > 0 {
		chunk := r.chunks[0]
		n := copy(dst, chunk)
		if n < len(chunk) {
			r.chunks[0] = chunk[n:]
		} else {
			r.chunks = r.chunks[1:]
		}
		if r.dataError && len(r.chunks) == 0 {
			return n, r.err
		}
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, io.EOF
}
func (r *workStreamReader) Close() error { r.closed = true; return nil }

func TestWorkStreamAdmission(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o.Context = ctx
		r := &workStreamReader{chunks: []string{"#records\na=1\n"}}
		callbacks := 0
		o.Trace = func(TraceEvent) { callbacks++ }
		err := prog.ParseStreamWith("main", r, func(*Node) error { callbacks++; return nil }, o)
		if err != context.Canceled || r.reads != 0 || callbacks != 0 || r.closed {
			t.Fatalf("error=%v reads=%d callbacks=%d closed=%t", err, r.reads, callbacks, r.closed)
		}
		for _, phase := range []string{"Done", "Err"} {
			t.Run(phase, func(t *testing.T) {
				token := &workContextPanicToken{phase: phase}
				if phase == "Done" {
					o.Context = &workContextDonePanic{Context: context.Background(), token: token}
				} else {
					c := newWorkContextErrPanic(token, 1)
					c.armed = true
					o.Context = c
				}
				got := workContextRecover(func() { _ = prog.ParseStreamWith("main", r, nil, o) })
				if got != token || r.reads != 0 {
					t.Fatalf("panic=%v reads=%d", got, r.reads)
				}
				// Fixed errors precede caller Context methods and reader wrapping.
				if got := workContextRecover(func() { err = prog.ParseStreamWith("missing", r, nil, o) }); got != nil || err == nil {
					t.Fatalf("invalid start panic=%v error=%v", got, err)
				}
			})
		}
	})
}

func TestWorkStreamUnlimitedParity(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, text := range []string{"#records\na=1\nbc=22\n", "#records\na=1\nbad=x\n"} {
			for _, withError := range []bool{false, true} {
				stop := errors.New("data and error")
				var results [2][]string
				var errs [2]error
				for i := range 2 {
					o := base
					if i == 1 {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						o.Context = ctx
					}
					r := &workStreamReader{chunks: []string{text}, dataError: withError}
					if withError {
						r.err = stop
					}
					errs[i] = prog.ParseStreamWith("main", r, func(n *Node) error { results[i] = append(results[i], resultJSON(n, nil)); return nil }, o)
				}
				if !reflect.DeepEqual(results[0], results[1]) || fmt.Sprint(errs[0]) != fmt.Sprint(errs[1]) {
					t.Fatalf("controlled results/errors differ: %v %v; %v %v", results[0], errs[0], results[1], errs[1])
				}
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		base.Context = ctx
		if err := prog.ParseStreamWith("main", strings.NewReader("#records\na=1\n"), nil, base); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWorkStreamReaderErrorPrecedence(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, dataError := range []bool{false, true} {
			t.Run(fmt.Sprintf("data=%t", dataError), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o := base
				o.Context = ctx
				stop := errors.New("reader error wins")
				r := &workStreamReader{chunks: []string{"#records\na=1\n"}, err: stop, cancel: cancel, cancelAt: 2}
				if dataError {
					r.chunks = []string{"#records\na=1\n", "bc=22\n"}
					r.dataError = true
				}
				count := 0
				err := prog.ParseStreamWith("main", r, func(*Node) error { count++; return nil }, o)
				if err != stop || count != 1 || r.reads != 2 || r.closed {
					t.Fatalf("error=%v emits=%d reads=%d closed=%t", err, count, r.reads, r.closed)
				}
			})
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		base.Context = ctx
		r := &workStreamReader{chunks: []string{"#records\na=1\n"}, cancel: cancel, cancelAt: 2}
		if err := prog.ParseStreamWith("main", r, func(*Node) error { return nil }, base); err != context.Canceled {
			t.Fatalf("EOF error=%v", err)
		}
	})
}

func TestWorkStreamEmitPrecedenceAndOwnership(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, fail := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			o := base
			o.Context = ctx
			stop := errors.New("emit error wins")
			var node *Node
			var snapshot string
			count := 0
			err := prog.ParseStreamWith("main", strings.NewReader("#records\na=1\nbc=22\n"), func(n *Node) error {
				count++
				node = n
				snapshot = resultJSON(n, nil)
				cancel()
				if fail {
					return stop
				}
				return nil
			}, o)
			cancel()
			want := error(context.Canceled)
			if fail {
				want = stop
			}
			if err != want || count != 1 || node == nil || resultJSON(node, nil) != snapshot {
				t.Fatalf("error=%v emits=%d snapshot changed=%t", err, count, resultJSON(node, nil) != snapshot)
			}
			if err := prog.ParseStreamWith("main", strings.NewReader("#records\nxy=3\n"), func(*Node) error { return nil }, base); err != nil || resultJSON(node, nil) != snapshot {
				t.Fatalf("fresh replay error=%v", err)
			}
		}
	})
}

type workStreamGatedReader struct {
	entered chan struct{}
	release chan struct{}
	reads   int
	closed  bool
}

func (r *workStreamGatedReader) Read(dst []byte) (int, error) {
	r.reads++
	if r.reads == 1 {
		close(r.entered)
		<-r.release
		return copy(dst, "#records\na=1\n"), nil
	}
	return 0, io.EOF
}
func (r *workStreamGatedReader) Close() error { r.closed = true; return nil }

func TestWorkStreamBlockedReaderCooperation(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		o.Context = ctx
		r := &workStreamGatedReader{entered: make(chan struct{}), release: make(chan struct{})}
		var release sync.Once
		defer release.Do(func() { close(r.release) })
		result := make(chan error, 1)
		emits := 0
		go func() { result <- prog.ParseStreamWith("main", r, func(*Node) error { emits++; return nil }, o) }()
		select {
		case <-r.entered:
		case err := <-result:
			t.Fatalf("returned before Read: %v", err)
		}
		cancel()
		release.Do(func() { close(r.release) })
		if err := <-result; err != context.Canceled || r.reads != 1 || emits != 0 || r.closed {
			t.Fatalf("error=%v reads=%d emits=%d closed=%t", err, r.reads, emits, r.closed)
		}
	})
}

func TestWorkStreamLimitsAndCallerPanics(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, kind := range []limits.WorkLimitKind{limits.InputWorkLimit, limits.StepWorkLimit} {
			o := base
			if kind == limits.InputWorkLimit {
				o.WorkLimits.MaxInputWork = 16
			} else {
				o.WorkLimits.MaxSteps = 1
			}
			count := 0
			var saved TraceEvent
			o.Trace = func(e TraceEvent) {
				if saved.p == nil {
					saved = e
				}
			}
			err := prog.ParseStreamWith("main", strings.NewReader("#records\n"+strings.Repeat("a", 20000)+"=1\n"), func(*Node) error { count++; return nil }, o)
			var we *limits.WorkLimitError
			if !errors.As(err, &we) || we.Kind != kind || count != 0 {
				t.Fatalf("error=%v emits=%d", err, count)
			}
			assertTraceParserDetached(t, saved.p, false)
		}
		for _, phase := range []string{"reader", "emit", "trace", "context"} {
			t.Run(phase, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o := base
				o.Context = ctx
				token := &streamRuntimePanicToken{phase: phase}
				r := &streamCallbackReader{text: "#records\na=1\n", panicValue: token}
				if phase != "reader" {
					r.panicValue = nil
				}
				var saved TraceEvent
				o.Trace = func(e TraceEvent) {
					if saved.p == nil {
						saved = e
					}
					if e.Kind == TraceEnter && e.Rule == "record" {
						if phase == "trace" {
							panic(token)
						}
						if phase == "context" {
							c := newWorkContextErrPanic(&workContextPanicToken{phase: phase}, 1)
							c.armed = true
							e.p.control.ctx = c
							token = nil
						}
					}
				}
				got := catchStreamPanic(func() {
					_ = prog.ParseStreamWith("main", r, func(*Node) error {
						if phase == "emit" {
							panic(token)
						}
						return nil
					}, o)
				})
				if phase == "context" {
					if _, ok := got.(*workContextPanicToken); !ok {
						t.Fatalf("context panic=%v", got)
					}
				} else if got != token {
					t.Fatalf("panic=%v want %v", got, token)
				}
				assertTraceParserDetached(t, saved.p, false)
				if r.closed {
					t.Fatal("reader closed")
				}
			})
		}
	})
}

func TestWorkStreamCompactionChunks(t *testing.T) {
	for _, shift := range []int{1, 127, 128, 129, 512} {
		source := make([]int, 1024)
		for i := range source {
			source[i] = i
		}
		want := append([]int(nil), source...)
		n := copy(want, want[shift:])
		c := newWorkControl(nil, limits.WorkLimits{MaxSteps: uint64(n)})
		if got := copyStreamPrefix(c, source, source[shift:]); got != n || !reflect.DeepEqual(source, want) || c.stepsUsed != uint64(n) {
			t.Fatalf("shift=%d result differs", shift)
		}
	}
	v := make([]int, 258)
	for i := range v {
		v[i] = i
	}
	c := newWorkControl(nil, limits.WorkLimits{MaxSteps: 128})
	got := catchStreamPanic(func() { copyStreamPrefix(c, v, v[1:]) })
	f, ok := got.(fatal)
	if !ok || !errors.Is(f.err, limits.ErrWorkLimit) || c.stepsUsed != 128 || v[128] != 128 {
		t.Fatalf("copy interruption=%v steps=%d", got, c.stepsUsed)
	}
}

func TestWorkStreamCommitReservations(t *testing.T) {
	// The frame scan and suffix clear are separately counted; neither touches
	// a Node already delivered to the caller.
	node := &Node{text: "delivered"}
	p := &parser{input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 1})}, frameSlab: make([]frame, 258)}
	p.frame = &p.frameSlab[0]
	p.frameSlab[1].vals = []*Node{node}
	got := catchStreamPanic(func() { p.resetStreamFrames() })
	if _, ok := got.(fatal); !ok || len(p.frameSlab) != 258 || p.frameSlab[1].vals[0] != node || node.text != "delivered" {
		t.Fatalf("frame interruption=%v", got)
	}
	p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 258})
	p.resetStreamFrames()
	if len(p.frameSlab) != 1 || p.control.stepsUsed != 258 || node.text != "delivered" {
		t.Fatal("frame retry changed ownership or count")
	}

	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%t", all), func(t *testing.T) {
			memo := newMemoTable()
			memo.slots = make([]*memoEntry, 260)
			a, b, growing := memo.alloc(), memo.alloc(), memo.alloc()
			a.node, b.node, growing.node = node, node, node
			growing.growing = true
			a.next = b
			memo.slots[0] = a
			memo.slots[1] = growing
			p := &parser{input: input{control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 2})}, memo: memo}
			pos := 129
			if all {
				pos = 260
			}
			got := catchStreamPanic(func() { p.pruneStreamMemo(pos) })
			if _, ok := got.(fatal); !ok || a.node != nil || b.node != node || memo.base != 0 || growing.node != node {
				t.Fatalf("memo partial prune=%v", got)
			}
			// An interrupted stream has no continuation guarantee: check a new
			// table for complete prune parity, instead of retrying partial links.
			fresh := newMemoTable()
			fresh.slots = make([]*memoEntry, 260)
			seed := fresh.alloc()
			seed.growing = true
			seed.node = node
			fresh.slots[1] = seed
			keep := fresh.alloc()
			keep.node = node
			fresh.slots[200] = keep
			p.memo = fresh
			p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 2000})
			p.pruneStreamMemo(pos)
			if fresh.base != pos || seed.node != node || node.text != "delivered" || len(fresh.slots) != max(260-pos, 0) {
				t.Fatal("prune ownership/base differs")
			}
			if !all && fresh.slots[200-pos] != keep {
				t.Fatal("overlap compaction lost live entry")
			}
		})
	}
}

func TestWorkStreamCommitAfterDelivery(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		o.WorkLimits.MaxSteps = 1 << 20
		var saved TraceEvent
		o.Trace = func(e TraceEvent) {
			if saved.p == nil {
				saved = e
			}
		}
		var node *Node
		var before string
		count := 0
		var controller *workControl
		err := prog.ParseStreamWith("main", strings.NewReader("#records\na=1\nbc=22\n"), func(n *Node) error {
			count++
			node = n
			before = resultJSON(n, nil)
			p := saved.p
			controller = p.control
			// Force the normal inactive-capacity clear at the next commit.
			p.trail = make([]undo, 0, 129)
			p.pruned = p.pos - 1024
			frames, keep := 0, 0
			for i := range p.frameSlab {
				frames++
				if &p.frameSlab[i] == p.frame {
					keep = i + 1
					break
				}
			}
			frames += len(p.frameSlab) - keep
			copyCount := 0
			if removed := p.pos - 1 - p.base; removed > 0 && 2*removed >= len(p.in)+len(p.bs) {
				copyCount = len(p.in) + len(p.bs) - removed
			}
			lineSteps := 0
			if copyCount > 0 {
				if p.eof && p.base == 0 {
					if p.lines == nil {
						lineSteps = p.loaded()
					}
				} else {
					lineSteps = min(p.pos-1, p.loaded()) - p.base
				}
			}
			controller.limits.MaxSteps = controller.stepsUsed + uint64(lineSteps+copyCount+frames+128)
			return nil
		}, o)
		var we *limits.WorkLimitError
		if !errors.As(err, &we) || we.Kind != limits.StepWorkLimit || we.Requested != 1 || we.Used != controller.limits.MaxSteps || count != 1 {
			t.Fatalf("normal commit interruption error=%v emits=%d", err, count)
		}
		assertTraceParserDetached(t, saved.p, false)
		if node == nil || resultJSON(node, nil) != before {
			t.Fatal("commit changed emitted Node")
		}
		o.WorkLimits = limits.WorkLimits{}
		o.Trace = nil
		replay := 0
		if err := prog.ParseStreamWith("main", strings.NewReader("#records\na=1\nbc=22\n"), func(*Node) error { replay++; return nil }, o); err != nil || replay != 2 || resultJSON(node, nil) != before {
			t.Fatalf("fresh replay error=%v emits=%d", err, replay)
		}
	})
}

func TestWorkStreamDiscardAndStackClear(t *testing.T) {
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		t.Run(u.String(), func(t *testing.T) {
			in := input{unit: u, eof: true, baseLine: 1, baseCol: 1, lines: []int{0}, control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 128})}
			if u == Bytes {
				in.bs = []byte(strings.Repeat("a", 258))
			} else {
				in.in = []rune(strings.Repeat("a", 258))
			}
			got := catchStreamPanic(func() { in.discard(129) })
			if _, ok := got.(fatal); !ok || in.base != 0 || in.control.stepsUsed != 128 {
				t.Fatalf("discard interruption=%v base=%d steps=%d", got, in.base, in.control.stepsUsed)
			}
		})
	}
	for _, phase := range []string{"trail", "values"} {
		t.Run(phase, func(t *testing.T) {
			node := &Node{text: "owned"}
			p := &parser{input: input{unit: Bytes, eof: true, bs: []byte(strings.Repeat("a", 3000)), baseLine: 1, baseCol: 1, lines: []int{0}, control: newWorkControl(nil, limits.WorkLimits{MaxSteps: 953 + 128})}, memo: newMemoTable()}
			if phase == "trail" {
				p.trail = make([]undo, 0, 129)
			} else {
				p.vals = make([]any, 1, 130)
				p.vals[0] = node
				for i := 1; i < cap(p.vals); i++ {
					p.vals[:cap(p.vals)][i] = node
				}
			}
			got := catchStreamPanic(func() { p.commit(2048) })
			if _, ok := got.(fatal); !ok || p.base != 2047 || p.pruned != 0 || node.text != "owned" {
				t.Fatalf("commit %s interruption=%v base=%d pruned=%d", phase, got, p.base, p.pruned)
			}
			if phase == "values" && (p.vals[0] != node || p.vals[:cap(p.vals)][129] != node) {
				t.Fatal("clear crossed reservation or live values")
			}
		})
	}
}

func TestWorkStreamEmitBoundary(t *testing.T) {
	for _, phase := range []string{"before", "after", "error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &parser{input: input{control: newWorkControl(ctx, limits.WorkLimits{})}}
			stop := errors.New("emit")
			calls := 0
			p.emit = func(*Node) error {
				calls++
				cancel()
				if phase == "error" {
					return stop
				}
				return nil
			}
			if phase == "before" {
				cancel()
			}
			var err error
			got := catchStreamPanic(func() { err = p.emitStream(nil) })
			if phase == "error" {
				if got != nil || err != stop || calls != 1 {
					t.Fatalf("panic=%v error=%v calls=%d", got, err, calls)
				}
				return
			}
			f, ok := got.(fatal)
			wantCalls := 1
			if phase == "before" {
				wantCalls = 0
			}
			if !ok || f.err != context.Canceled || calls != wantCalls || p.emitCall {
				t.Fatalf("panic=%v calls=%d caller flag=%t", got, calls, p.emitCall)
			}
		})
	}
}

func TestWorkStreamReaderPanicInsideTrace(t *testing.T) {
	prog := compile(t, `rule main = "z"* #stream $$`)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		o.Context = ctx
		token := &streamRuntimePanicToken{phase: "trace reader"}
		r := &streamCallbackReader{panicValue: token}
		var saved TraceEvent
		o.Trace = func(e TraceEvent) {
			if e.Kind == TraceEnter && e.Rule == "main" {
				saved = e
				e.LineCol(1)
			}
		}
		got := catchStreamPanic(func() { _ = prog.ParseStreamWith("main", r, nil, o) })
		if got != token || r.closed {
			t.Fatalf("panic=%v closed=%t", got, r.closed)
		}
		assertTraceParserDetached(t, saved.p, false)
	})
}

func TestWorkStreamVMFaultAttribution(t *testing.T) {
	for _, b := range []Backend{Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) {
			prog := compile(t, `rule main = "z"* #stream $$`)
			if _, err := prog.rule(b, "main"); err != nil {
				t.Fatal(err)
			}
			vm := prog.vmFor(b)
			ip := -1
			for i, ins := range vm.m.Code {
				if ins.Op == OpStr {
					ip = i
					break
				}
			}
			if ip < 0 {
				t.Fatal("missing literal instruction")
			}
			old := vm.m.Code[ip]
			vm.m.Code[ip].A = int32(len(vm.runes))
			defer func() { vm.m.Code[ip] = old }()
			var saved TraceEvent
			err := prog.ParseStreamWith("main", strings.NewReader("z"), func(*Node) error { t.Fatal("emitted from corrupt VM"); return nil }, ParseOptions{Backend: b, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 20}, Trace: func(e TraceEvent) {
				if saved.p == nil {
					saved = e
				}
			}})
			if err == nil || !strings.HasPrefix(err.Error(), "invalid bytecode:") {
				t.Fatalf("VM error=%v", err)
			}
			assertTraceParserDetached(t, saved.p, false)
			vm.m.Code[ip] = old
			if err := prog.ParseStreamWith("main", strings.NewReader("z"), nil, ParseOptions{Backend: b}); err != nil {
				t.Fatalf("restored VM: %v", err)
			}
		})
	}
}

func TestWorkStreamConcurrentCounters(t *testing.T) {
	prog := compile(t, records)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type result struct {
			control *workControl
			err     error
			count   int
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for i := range 2 {
			go func() {
				<-start
				o := base
				o.WorkLimits = limits.WorkLimits{MaxInputWork: 100000, MaxSteps: 100000}
				if i == 0 {
					o.Context = ctx
				}
				var run result
				o.Trace = func(e TraceEvent) {
					if e.Kind == TraceEnter && e.Rule == "main" {
						run.control = e.p.control
						if run.control.inputUsed != 0 {
							panic("stream inherited input-work counters")
						}
					}
				}
				run.err = prog.ParseStreamWith("main", strings.NewReader("#records\na=1\nbc=22\n"), func(*Node) error {
					run.count++
					if i == 0 {
						cancel()
					}
					return nil
				}, o)
				results <- run
			}()
		}
		close(start)
		a, b := <-results, <-results
		if a.control == nil || b.control == nil || a.control == b.control || a.control.inputUsed == 0 || b.control.inputUsed == 0 {
			t.Fatal("concurrent invocation counters shared or empty")
		}
		if a.err == nil {
			a, b = b, a
		}
		if a.err != context.Canceled || a.count != 1 || b.err != nil || b.count != 2 {
			t.Fatalf("concurrent results: %v/%d, %v/%d", a.err, a.count, b.err, b.count)
		}
	})
}
