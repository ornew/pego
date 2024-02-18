package manual

import (
	"context"
	"fmt"
	"testing"

	"github.com/ornew/pego/pkg/cst/printer"
	"github.com/ornew/pego/pkg/memo"
	inmemoryscanner "github.com/ornew/pego/pkg/scanner/inmemory"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		src string
	}{
		{""},
		{".aaa"},
		{"aaa"},
		{"xxx"},
		{"xxxy"},
		{"axa"},
		{"aaaaaxa"},
		{"aaax"},
		{"x+x"},
		{"x+"},
	} {
		p := inmemoryscanner.New([]byte(tt.src))
		ctx := context.TODO()
		ctx = memo.To(ctx, memo.New())
		res := Parse(ctx, p)
		t.Logf("Source:\n%s\n", tt.src)
		t.Logf("End: %v, Result: %#v", p.IsEnd(), res)
		if res != nil {
			t.Log("\n" + printer.Printer{
				Source: p,
				DecodeNodeType: func(i int) string {
					switch i {
					case Rule_root:
						return "root"
					case Rule__root_1:
						return "_root_1"
					case Rule_a:
						return "a"
					case Rule_b:
						return "b"
					case Rule_c:
						return "c"
					case Rule__c_0:
						return "_c_0"
					case Rule__c_1:
						return "_c_1"
					default:
						return fmt.Sprintf("unknown(%d)", i)
					}
				},
				SourcePrint: true,
			}.PrintString(res))
		}
	}
}
