package engine

import (
	"strings"
	"testing"
)

func TestGeneratedTSWideCharacterClasses(t *testing.T) {
	var singles, ranges strings.Builder
	for i := 0; i < 800; i++ {
		singles.WriteRune(rune(0x1000 + i*2))
	}
	for i := 0; i < 400; i++ {
		lo := rune(0x3000 + i*3)
		ranges.WriteRune(lo)
		ranges.WriteByte('-')
		ranges.WriteRune(lo + 1)
	}
	src := "rule main = [" + singles.String() + `] / "z"
rule negative = [^` + singles.String() + `]
rule ranges = [` + ranges.String() + `]`
	consumer := `import * as p from "./parser.ts";
` + tsTypedAssert + `
for(const unit of [p.CodePoints,p.Bytes]) {
 for(const input of ["z",String.fromCodePoint(0x1000),String.fromCodePoint(0x1000+798),String.fromCodePoint(0x1000+1598)]) {
  const result=p.parse(input,unit),typed=p.parseAST(input,unit);
  check(result.error===null&&result.node!==null&&result.node.text===input&&typed.error===null&&typed.value!==null&&stable(typed.value).length>0&&p.recognize(input,unit)===null,"wide class accepts first, middle, last and fallback");
  check(result.node.start===0&&result.node.end===(unit===p.Bytes?(input==="z"?1:3):1),"position unit preserved");
 }
 for(const input of ["x",String.fromCodePoint(0x1001)])check(p.parse(input,unit).node===null&&p.parse(input,unit).error!==null&&p.recognize(input,unit)!==null,"wide class rejects gaps");
 for(const input of ["z",String.fromCodePoint(0x1001)])check(p.parseRule("negative",input,unit).error===null,"negated class accepts complements");
 for(const input of [String.fromCodePoint(0x1000),String.fromCodePoint(0x1000+1598)])check(p.parseRule("negative",input,unit).error!==null,"negated class rejects members");
 for(const input of [String.fromCodePoint(0x3000),String.fromCodePoint(0x3001),String.fromCodePoint(0x3000+399*3+1)])check(p.parseRule("ranges",input,unit).node?.text===input&&p.parseRule("ranges",input,unit).error===null,"range AND/OR groups preserve endpoints");
 check(p.parseRule("ranges",String.fromCodePoint(0x3002),unit).error!==null,"range groups preserve gaps");
}
`

	runTypedTSFixture(t, src, consumer, GenOptions{Start: "main", Recognize: true}, "")
}
