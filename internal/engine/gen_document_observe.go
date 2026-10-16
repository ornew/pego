package engine

import (
	"fmt"
	"strings"
)

// Document observers belong to invocations, never to reusable memo entries.
// Constructor options remain explicit Document defaults; they are not observers.
//
// Callbacks run inside a parse, so an observed Document also rejects a Parse,
// ParseAST or Edit that a callback makes on the same Document.
func generatedDocumentTraceRuntime(source string) (string, error) {
	const anchor = "p := &parser{unit: d.unit, maxDepth: o.maxDepth, control: o.control}"
	const fields = "typedMemo interface{ invalidate(int) }\n}"
	const parse = "func (d *Document) Parse(opts ...ParseOption) (*Node, error) {\n"
	const edit = "func (d *Document) Edit(start, end int, text string) error {\n"
	for _, s := range []string{anchor, fields, parse, edit} {
		if strings.Count(source, s) != 1 {
			return "", fmt.Errorf("generated Document observation anchor changed")
		}
	}
	source = strings.Replace(source, anchor, anchor+`
 p.observation = newDocumentObservation(o)
 defer p.releaseDocumentObservation()
`, 1)
	source = strings.Replace(source, fields, "typedMemo interface{ invalidate(int) }\n\tparsing   bool\n}", 1)
	source = strings.Replace(source, parse, parse+"\tif err := d.beginParse(); err != nil {\n\t\treturn nil, err\n\t}\n\tdefer d.endParse()\n", 1)
	source = strings.Replace(source, edit, edit+"\tif d.parsing {\n\t\treturn ErrDocumentBusy\n\t}\n", 1)
	return source + generatedDocumentObservationHelpers, nil
}

// generatedDocumentParseGuard begins a generated Document.ParseAST with a
// result of type typ. It is empty without observability.
func generatedDocumentParseGuard(observability bool, typ string) string {
	if !observability {
		return ""
	}
	return fmt.Sprintf(" if err := d.beginParse(); err != nil { var zero %s; return zero, err }\n defer d.endParse()\n", typ)
}

func generatedTypedDocumentTraceRuntime(source string) (string, error) {
	const anchor = "p:=&tparser{parser:&parser{control:o.control},typedDocument:cache,memoSlots:cache.slots}"
	const memo = "if !old.growing {p.mergeExpected(old.far,old.expected)}"
	if strings.Count(source, anchor) != 1 || strings.Count(source, memo) != 1 {
		return "", fmt.Errorf("generated typed Document observation anchors changed")
	}
	source = strings.Replace(source, anchor, anchor+`
 p.observation = newDocumentObservation(o)
 defer p.releaseDocumentObservation()
`, 1)
	return strings.Replace(source, memo, "p.observation.memoHit()\n "+memo, 1), nil
}

const generatedDocumentObservationHelpers = `
// ErrDocumentBusy is returned by a Parse, ParseAST or Edit of a Document that
// is already parsing, such as one made by a trace callback of that parse.
// Schedule the work after the parse returns, or use another Document.
var ErrDocumentBusy = fmt.Errorf("document is already parsing")

func (d *Document) beginParse() error {
 if d.parsing {return ErrDocumentBusy}
 d.parsing = true
 return nil
}

func (d *Document) endParse() { d.parsing = false }

func (p *parser) releaseDocumentObservation() {
 if o:=p.observation; o!=nil {
  if o.profile!=nil { *o.profile=profileObservation{} }
  *o=observation{}
  p.observation=nil
 }
}

func newDocumentObservation(o parseOptions) *observation {
 if o.trace==nil && o.profile==nil {return nil}
 out:=&observation{trace:o.trace,maxDepth:o.traceMaxDepth,profile:newProfileObservation(o.profile)}
 if len(o.traceRules)!=0 {
  out.rules=make(map[string]bool,len(o.traceRules))
  for _,name:=range o.traceRules {out.rules[name]=true}
 }
 return out
}
`
