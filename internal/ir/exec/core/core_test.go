package core

import (
	"os"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/ir/exec"
)

// TestGoRuntimeCurrent checks that the printed compositions in the Go runtime are those the
// compositions print now.
func TestGoRuntimeCurrent(t *testing.T) {
	want, err := GoRuntime()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(GoRuntimeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; run go generate ./internal/ir/exec/core ./gen", GoRuntimeFile)
	}
}

// TestSpecializationsVerify checks that every operation the compositions call is declared,
// and that the functions verify for every specialization a target prints.
func TestSpecializationsVerify(t *testing.T) {
	m := Build()
	if _, err := GoRuntime(); err != nil {
		t.Fatal(err)
	}
	ms, err := TSMethods()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ms {
		if !strings.Contains(x.Text, x.Name+"(") {
			t.Errorf("method %s prints as\n%s", x.Name, x.Text)
		}
	}
	for _, f := range m.Funcs {
		if f.Doc == "" {
			t.Errorf("composition %s has no doc", f.Name)
		}
		t.Log(exec.FuncString(f))
	}
}
