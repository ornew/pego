package engine

import (
	"fmt"
	"strings"
)

// Document observers belong to invocations, never to reusable memo entries.
// Constructor options remain explicit Document defaults; they are not observers.
func generatedDocumentTraceRuntime(source string) (string, error) {
	const anchor = "p := &parser{unit: d.unit, maxDepth: o.maxDepth, control: o.control}"
	if strings.Count(source, anchor) != 1 {
		return "", fmt.Errorf("generated Document observation anchor changed")
	}
	source = strings.Replace(source, anchor, anchor+`
 p.observation = newDocumentObservation(o)
 defer p.releaseDocumentObservation()
`, 1)
	return source + generatedDocumentObservationHelpers, nil
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
