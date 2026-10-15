package engine

import (
	"fmt"
	"strings"
)

// typedDocumentRuntime adds Document-only state and entry points without changing
// the batch runtime layout of packages generated without direct typed Documents.
func typedDocumentRuntime(source string) (string, error) {
	replacements := []struct{ old, new string }{
		{"type tparser struct {\n", "type tparser struct {\n typedDocument *typedDocumentState\n"},
		{"p.memo.sparse = sparseSeen", "if p.typedDocument != nil { p.document = p.typedDocument.owner };\n p.memo.sparse = sparseSeen"},
		{"func (p *tparser) call(r *trule, min int) (any, bool) {", "func (p *tparser) call(r *trule, min int) (any, bool) {\n if p.typedDocument != nil { return p.documentCall(r, min, nil) }"},
		{"func (p *tparser) callMemo(r *trule, inv func(*tparser) (any, bool)) (any, bool) {", "func (p *tparser) callMemo(r *trule, inv func(*tparser) (any, bool)) (any, bool) {\n if p.typedDocument != nil { return p.documentCall(r, 0, inv) }"},
		{"func (p *tparser) invoke(r *trule, min int) (any, bool) {", "func (p *tparser) invoke(r *trule, min int) (any, bool) {\n if p.typedDocument != nil { p.document.stats.Evaluated++ }"},
	}
	for _, r := range replacements {
		if strings.Count(source, r.old) != 1 {
			return "", fmt.Errorf("typed Document runtime anchor changed: %s", r.old)
		}
		source = strings.Replace(source, r.old, r.new, 1)
	}
	return source, nil
}

const typedDocumentSource = `
// typedDocumentState owns private typed memo values. The fresh invocation arenas
// that contain those values must never be reset or returned to the batch pool.
type typedDocumentState struct {
 owner *documentState
 slots []*tmemoEntry
 meta map[*tmemoEntry]typedDocumentMeta
}
type typedDocumentMeta struct {
 examined int
 provisional bool
}

func (d *typedDocumentState) invalidate(start int) {
 kept := make([]*tmemoEntry, min(start,len(d.slots)))
 meta := make(map[*tmemoEntry]typedDocumentMeta,min(len(d.meta),start))
 var entries tarena[tmemoEntry]
 for pos, head := range d.slots[:len(kept)] {
  for e:=head;e!=nil;e=e.next {
   m,complete:=d.meta[e]
   if !complete || e.growing || m.provisional || m.examined>start {continue}
   out:=entries.one()
   *out=*e
   // Environments, expectations and private diagnostic slices are immutable.
   // Their arenas are never recycled; only the memo chain link needs copying.
   out.next=kept[pos]
   kept[pos]=out
   meta[out]=m
  }
 }
 d.slots,d.meta=kept,meta
}

func typedDocumentParse[T any](d *Document, root *trule, o parseOptions, a *tslabs, publish func(any,*typedDocumentClone)T)(res T,err error) {
 defer recoverParse(&res,&err)
 if o.unit!=d.state.unit {return res,fmt.Errorf("document parse cannot change unit")}
 cache,ok:=d.typedMemo.(*typedDocumentState)
 if !ok {
  cache=&typedDocumentState{owner:d.state,meta:make(map[*tmemoEntry]typedDocumentMeta)}
  d.typedMemo=cache
 }
 // Establish cleanup before preparing controls: even caller-owned context code
 // can panic during its first checkpoint.
 completed:=false
 defer func(){if !completed {d.typedMemo=nil}}()
 o=prepareOptions(o)
 p:=&tparser{parser:&parser{control:o.control},typedDocument:cache,memoSlots:cache.slots}
 // Do not recycle p: memo values and expectations may refer into its arenas.
 a.setWorkControl(o.control)
 defer a.setWorkControl(nil)
 c:=&typedDocumentClone{control:o.control}
 defer c.release()
 d.state.stats=documentStats{}
 v,parseErr:=p.run(root,d.state.text,o,a)
 // Context implementations may return any public error type. Fatal work
 // interruption must not be mistaken for a completed syntax failure.
 if o.control!=nil && o.control.interrupted {return res,parseErr}
 if parseErr!=nil {
  switch parseErr.(type) {
  case *SyntaxError,SyntaxErrors:
  default: return res,parseErr
  }
 }
 o.control.check()
 if v!=nil {res=publish(v,c)}
 err=typedDocumentError(parseErr,c)
 c.finish()
 cache.slots=p.memoSlots
 completed=true
 return res,err
}

// Diagnostics are private memo data too; publishing their original pointers or
// Expected/Messages slices would let caller mutation alter a later parse.
func typedDocumentError(err error,c *typedDocumentClone)error {
 clone:=func(e *SyntaxError)*SyntaxError {
  c.step(1)
  if e==nil {return nil}
  out:=new(SyntaxError);*out=*e
  if e.Expected!=nil {
   c.step(uint64(len(e.Expected)));out.Expected=make([]string,len(e.Expected))
   for i,s:=range e.Expected {c.step(1);out.Expected[i]=s}
  }
  if e.Messages!=nil {
   c.step(uint64(len(e.Messages)));out.Messages=make([]string,len(e.Messages))
   for i,s:=range e.Messages {c.step(1);out.Messages[i]=s}
  }
  return out
 }
 switch e:=err.(type) {
 case *SyntaxError:return clone(e)
 case SyntaxErrors:
  c.step(uint64(len(e)))
  out:=make(SyntaxErrors,len(e))
  for i,item:=range e {out[i]=clone(item)}
  return out
 }
 return err
}

// documentCall memoizes all typed rule entries, including generated direct
// wrappers. Examined and provisional metadata stays outside batch memo entries.
func(p *tparser) documentCall(r *trule,min int,inv func(*tparser)(any,bool))(any,bool) {
 p.stepWork()
 start:=p.pos
 oldHigh,oldProvisional:=p.docHigh,p.docProvisional
 p.docHigh,p.docProvisional=start+1,false
 defer func(){p.docHigh=max(oldHigh,p.docHigh);p.docProvisional=oldProvisional||p.docProvisional}()
 env:=p.envValues(r.vars)
 old:=p.memoGet(r.id,start,min,env)
 if old!=nil && (old.growing || (!old.silent || p.silent>0) && (len(old.errs)==0 || old.silent==(p.silent>0))) {
  m:=p.typedDocument.meta[old]
  p.documentTouch(m.examined)
  p.docProvisional=p.docProvisional||old.growing||m.provisional
  p.document.stats.Reused++
  if !old.growing {p.mergeExpected(old.far,old.expected)}
  if !old.ok {return nil,false}
  p.pos=old.end
  p.recovered=p.copyErrors(p.recovered,old.errs)
  return old.val,true
 }
 // A context-incompatible entry may be replaced. Do not keep it alive through
 // stale metadata once its memo chain no longer refers to it.
 if old!=nil {delete(p.typedDocument.meta,old)}
 rec:=len(p.recovered)
 m:=p.isolate(start)
 var v any
 var ok bool
 var e *tmemoEntry
 if r.leader {
  v,ok=p.grow(r,min,env)
  e=p.memoGet(r.id,start,min,env)
 } else {
  if inv!=nil {
   p.document.stats.Evaluated++
   v,ok=inv(p)
  } else {v,ok=p.invoke(r,min)}
  if !ok {p.recovered=p.recovered[:rec]}
  e=p.memoSlab.one()
  *e=tmemoEntry{val:v,ok:ok,end:p.pos,silent:p.silent>0,errs:p.copyErrors(nil,p.recovered[rec:])}
  p.memoPut(r.id,start,min,env,e)
 }
 far,inner:=p.unisolate(m)
 e.far,e.expected=far,p.keep(inner)
 p.documentTouch(p.pos+1)
 p.typedDocument.meta[e]=typedDocumentMeta{examined:p.docHigh,provisional:p.docProvisional||r.leader}
 p.mergeExpected(far,inner)
 if !ok {p.pos=start}
 return v,ok
}
`
