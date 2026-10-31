package tsgen_test

import (
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
)

// TestGeneratedCheckTypeName checks that a grammar type named like the generated check type
// is renamed in typed TypeScript.
func TestGeneratedCheckTypeName(t *testing.T) {
	t.Parallel()
	src := `type Int = { Text string }
type FieldType = { Text string }
type S = { F Int }
rule main = v:val f:ft => S{F: $v.(Int)}
rule ft FieldType = "f" => FieldType{Text: "f"}
rule val _ = "v" => Int{Text: "v"}
`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `const ok = p.parseAST("vf");
check(ok.error === null && ok.value !== null && stable(ok.value.F).length > 0, "constructed S");
const no = p.parseAST("v");
check(no.error !== null, "missing f");
`
	runTypedTSFixture(t, src, consumer, gen.GenOptions{Start: "main"}, "")
}
