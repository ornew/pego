package tsgen_test

import (
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedTypedTSNodeConstructors(t *testing.T) {
	t.Parallel()
	runTypedTSFixture(t, gentest.ConstructorGrammar, `import {parseAST, parse, CodePoints, Bytes} from "./parser.ts";
`+tsTypedAssert+`
for (const unit of [CodePoints, Bytes]) {
  const r = parseAST("😀", unit);
  check(r.error === null && r.value !== null, "parse");
  const v = r.value;
  check(stable(v).length > 0, "serializable output");
  check(v.Items?.length === 2 && v.Items[0]?.Text === "0" && v.Items[1]?.type === "Int" && v.Items[1]?.Text === "😀", "text struct aliases");
  check(v.Nested?.length === 2 && v.Nested[0]?.length === 0 && v.Nested[1]?.[0]?.Text === "1", "nested lists");
  check(v.Maybe?.length === 2 && v.Maybe[0] === null && v.Maybe[1]?.Text === "2" && v.Empty?.length === 0, "nil and empty lists");
  check(v.start === 0 && v.end === (unit === Bytes ? 4 : 1), "span");
  check(parse("😀", unit).error === null, "generic output");
}
`, gen.GenOptions{}, "")
}
