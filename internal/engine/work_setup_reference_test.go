//go:build pego_work_controls_reference

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestWorkSetupReferenceAdmission(t *testing.T) {
	t.Parallel()
	for _, omitted := range []bool{false, true} {
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, api := range []string{"batch", "document", "stream"} {
				prog := mustCompile(t, gentest.Records)
				if omitted {
					prog = moduleProgram(prog.Module())
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				callbacks := 0
				o := ParseOptions{Backend: backend, Context: ctx, Trace: func(TraceEvent) { callbacks++ }}
				reader := &streamCallbackReader{text: "#records\na=1\n", onRead: func() { callbacks++ }}
				var n *value.Node
				var d *Document
				var err error
				switch api {
				case "batch":
					n, err = prog.ParseWith("main", "#records\na=1\n", o)
				case "document":
					d, err = prog.NewDocumentWith("main", "#records\na=1\n", o)
				case "stream":
					err = prog.ParseStreamWith("main", reader, func(*value.Node) error { callbacks++; return nil }, o)
				}
				if err == nil || !strings.Contains(err.Error(), "reference build") || n != nil || d != nil || callbacks != 0 || reader.offset != 0 || prog.vm != nil || prog.ivm != nil || prog.rec != nil {
					t.Fatalf("omitted=%t backend=%s api=%s error=%v callbacks=%d readeroffset=%d vm=%p ivm=%p rec=%p", omitted, backend, api, err, callbacks, reader.offset, prog.vm, prog.ivm, prog.rec)
				}
			}
		}
	}
}
