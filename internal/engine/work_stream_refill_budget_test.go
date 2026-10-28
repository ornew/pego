//go:build !pego_work_controls_reference

package engine

import (
	"bufio"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

type streamRefillResult struct {
	data []byte
	err  error
}

type streamRefillScript struct {
	results []streamRefillResult
	onRead  func(int)
	reads   int
}

func (r *streamRefillScript) Read(dst []byte) (int, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead(r.reads)
	}
	if len(r.results) == 0 {
		return 0, io.EOF
	}
	result := r.results[0]
	r.results = r.results[1:]
	return copy(dst, result.data), result.err
}

func streamRefillObserver(reader io.Reader, control *workControl) (*streamReaderObserver, *error) {
	var readErr error
	return &streamReaderObserver{reader: reader, result: &readErr, control: control}, &readErr
}

func TestStreamReaderObserverChargesEachUnderlyingRead(t *testing.T) {
	reader := &streamRefillScript{results: []streamRefillResult{{data: []byte("a")}, {data: []byte("b")}}}
	control := newWorkControl(nil, limits.WorkLimits{MaxSteps: 2})
	observer, _ := streamRefillObserver(reader, control)
	for i, want := range []byte("ab") {
		var dst [1]byte
		n, err := observer.Read(dst[:])
		if err != nil || n != 1 || dst[0] != want {
			t.Fatalf("read %d = %d,%q,%v; want one byte %q", i, n, dst[:n], err, want)
		}
		if control.stepsUsed != uint64(i+1) || control.inputUsed != 0 {
			t.Fatalf("after read %d counters steps/input=%d/%d; want %d/0", i, control.stepsUsed, control.inputUsed, i+1)
		}
	}
	if reader.reads != 2 {
		t.Fatalf("underlying Read calls=%d, want 2", reader.reads)
	}
}

func TestStreamReaderObserverBoundsBufioRetries(t *testing.T) {
	t.Run("empty retries", func(t *testing.T) {
		reader := &streamRefillScript{results: []streamRefillResult{{}, {}, {}, {}}}
		control := newWorkControl(nil, limits.WorkLimits{MaxSteps: 3})
		observer, _ := streamRefillObserver(reader, control)
		buffered := bufio.NewReader(observer)
		limitErr := catchFatal(t, func() { _, _ = buffered.ReadByte() })
		var limit *limits.WorkLimitError
		if !errors.As(limitErr, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != 3 || limit.Requested != 1 || reader.reads != 3 || control.inputUsed != 0 {
			t.Fatalf("error=%v reads=%d input=%d", limitErr, reader.reads, control.inputUsed)
		}
	})
	for _, max := range []uint64{1, 2} {
		t.Run(map[uint64]string{1: "one-short split rune", 2: "exact split rune"}[max], func(t *testing.T) {
			reader := &streamRefillScript{results: []streamRefillResult{{data: []byte{0xc3}}, {data: []byte{0xa9}}}}
			control := newWorkControl(nil, limits.WorkLimits{MaxSteps: max})
			observer, _ := streamRefillObserver(reader, control)
			buffered := bufio.NewReader(observer)
			var r rune
			var size int
			var readErr error
			limitErr := catchFatal(t, func() { r, size, readErr = buffered.ReadRune() })
			if max == 1 {
				var limit *limits.WorkLimitError
				if !errors.As(limitErr, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != 1 || limit.Requested != 1 || reader.reads != 1 || size != 0 {
					t.Fatalf("error=%v reads=%d rune=%q size=%d", limitErr, reader.reads, r, size)
				}
			} else if limitErr != nil || readErr != nil || r != 'é' || size != 2 || reader.reads != 2 {
				t.Fatalf("error=%v readErr=%v rune=%q size=%d reads=%d", limitErr, readErr, r, size, reader.reads)
			}
			if control.stepsUsed != max || control.inputUsed != 0 {
				t.Fatalf("steps=%d input=%d", control.stepsUsed, control.inputUsed)
			}
		})
	}
}

func TestWorkStreamEmptyRefillBudget(t *testing.T) {
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = .+ #atomic`)
	workStreamVariants(t, func(t *testing.T, o ParseOptions) {
		reader := &workCancelledRefillReader{cancel: func() {}}
		o.WorkLimits = limits.WorkLimits{MaxSteps: 64}
		emits := 0
		err := prog.ParseStreamWith("main", reader, func(*value.Node) error { emits++; return nil }, o)
		var limit *limits.WorkLimitError
		if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != 64 || limit.Requested != 1 || reader.calls == 0 || reader.calls > 64 || emits != 0 {
			t.Fatalf("error=%v calls=%d emits=%d", err, reader.calls, emits)
		}
	})
}

func TestStreamReaderObserverOneShortFailsBeforeCallerRead(t *testing.T) {
	reader := &streamRefillScript{results: []streamRefillResult{{data: []byte("a")}, {data: []byte("b")}}}
	control := newWorkControl(nil, limits.WorkLimits{MaxSteps: 1})
	observer, _ := streamRefillObserver(reader, control)
	var first [1]byte
	if n, err := observer.Read(first[:]); n != 1 || err != nil || first[0] != 'a' {
		t.Fatalf("first read = %d,%q,%v", n, first[:n], err)
	}
	var second [1]byte
	limitErr := catchFatal(t, func() { _, _ = observer.Read(second[:]) })
	if !errors.Is(limitErr, limits.ErrWorkLimit) {
		t.Fatalf("second read error=%v, want ErrWorkLimit", limitErr)
	}
	if reader.reads != 1 || control.stepsUsed != 1 || control.inputUsed != 0 {
		t.Fatalf("after rejected refill reads=%d counters steps/input=%d/%d", reader.reads, control.stepsUsed, control.inputUsed)
	}
}

func TestStreamReaderObserverCancellationAndReadErrorPrecedence(t *testing.T) {
	t.Run("cancellation before exhausted reservation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		control := newWorkControl(ctx, limits.WorkLimits{MaxSteps: 1})
		control.charge(0, 1)
		cancel()
		reader := &streamRefillScript{results: []streamRefillResult{{data: []byte("x")}}}
		observer, _ := streamRefillObserver(reader, control)
		fatalErr := catchFatal(t, func() { _, _ = observer.Read(make([]byte, 1)) })
		if !errors.Is(fatalErr, context.Canceled) || reader.reads != 0 || control.stepsUsed != 1 {
			t.Fatalf("error=%v reads=%d steps=%d; want cancellation before caller Read", fatalErr, reader.reads, control.stepsUsed)
		}
	})

	for _, returned := range []error{io.EOF, errors.New("reader failed")} {
		name := "EOF"
		if returned != io.EOF {
			name = "non-EOF error"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &streamRefillScript{results: []streamRefillResult{{err: returned}}, onRead: func(int) { cancel() }}
			control := newWorkControl(ctx, limits.WorkLimits{MaxSteps: 1})
			observer, result := streamRefillObserver(reader, control)
			fatalErr := catchFatal(t, func() { _, _ = observer.Read(make([]byte, 1)) })
			if returned == io.EOF {
				if !errors.Is(fatalErr, context.Canceled) {
					t.Fatalf("error=%v, want cancellation to supersede EOF", fatalErr)
				}
			} else if fatalErr != returned {
				t.Fatalf("error=%v, want exact reader error %v", fatalErr, returned)
			}
			if reader.reads != 1 || control.stepsUsed != 1 || *result != returned {
				t.Fatalf("reads=%d steps=%d recorded=%v", reader.reads, control.stepsUsed, *result)
			}
		})
	}
}

func TestStreamReaderObserverNilControlRemainsUnbounded(t *testing.T) {
	reader := &streamRefillScript{results: []streamRefillResult{{data: []byte("x")}, {data: []byte("y")}}}
	observer, _ := streamRefillObserver(reader, nil)
	for range 2 {
		if n, err := observer.Read(make([]byte, 1)); n != 1 || err != nil {
			t.Fatalf("nil-control read = %d,%v", n, err)
		}
	}
	if reader.reads != 2 {
		t.Fatalf("nil-control underlying reads=%d, want 2", reader.reads)
	}
}
