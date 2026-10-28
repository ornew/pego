package lsp

import (
	"context"
	"fmt"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// Keep the cancellation sentinel inside the metadata invocation, before the
// outer analysis safety boundary converts unrelated panics into diagnostics.
func runMetadataContext(ctx context.Context, action func(*renderControl) error) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	// A nil Done alone is insufficient: custom contexts can report errors via Err.
	if ctx == context.Background() || ctx == context.TODO() {
		return action(nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(renderCancellation); ok {
				err = cancelled.err
			} else {
				panic(failure)
			}
		}
	}()
	control := &renderControl{ctx: ctx}
	err = action(control)
	control.check()
	return err
}

func (a *analysis) finishMetadataContext(ctx context.Context) error {
	return runMetadataContext(ctx, func(control *renderControl) error { return a.finishMetadata(ctx, control) })
}
func (a *analysis) finishMetadata(ctx context.Context, control *renderControl) error {
	control.poll()
	a.indexDeclarationStartsControlled(control)
	a.collectControlled(control)
	a.mapSyntaxDiagnostics(control)
	if len(a.syntaxErrs) == 0 {
		if err := a.compileContextControlled(ctx, control); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.sortDiagnostics(control)
	return nil
}
func (a *analysis) mapSyntaxDiagnostics(control *renderControl) {
	control.poll()
	for _, e := range a.syntaxErrs {
		control.poll()
		r := a.errorRangeControlled(e.Pos, false, control)
		if e.End.IsValid() {
			r = a.idx.rangeOf(a.idx.pegoOffset(e.Pos), a.idx.pegoOffset(e.End))
		}
		a.diag(r, e.Msg)
	}
}
func (a *analysis) mapCompileDiagnostics(list engine.ErrorList, control *renderControl) {
	control.poll()
	for i, e := range list {
		control.poll()
		if i == maxCompileErrors {
			a.diag(a.errorRangeControlled(e.Pos, true, control), fmt.Sprintf("%d more errors", len(list)-i))
			break
		}
		a.diag(a.errorRangeControlled(e.Pos, true, control), e.Msg)
	}
}
func (a *analysis) sortOccurrences(control *renderControl) {
	control.poll()
	sort.SliceStable(a.occs, func(i, j int) bool { control.poll(); return a.occs[i].start < a.occs[j].start })
}
func (a *analysis) sortDiagnostics(control *renderControl) {
	control.poll()
	sort.SliceStable(a.diags, func(i, j int) bool {
		control.poll()
		pi, pj := a.diags[i].Range.Start, a.diags[j].Range.Start
		return pi.Line < pj.Line || pi.Line == pj.Line && pi.Character < pj.Character
	})
}
func (control *renderControl) expressionArgs(a *grammar.Attribute) []grammar.Expr {
	if control == nil {
		return a.ExpressionArgs()
	}
	return grammaranalysis.ExpressionArgsWithPoll(a, control.poll)
}

func (a *analysis) collect()                { a.collectControlled(nil) }
func (a *analysis) indexDeclarationStarts() { a.indexDeclarationStartsControlled(nil) }
func (a *analysis) errorRange(pos grammar.Pos, definitions bool) Range {
	return a.errorRangeControlled(pos, definitions, nil)
}
func (a *analysis) compileContext(ctx context.Context) error {
	previousProgram, previousDiagnostics := a.prog, a.diags
	err := runMetadataContext(ctx, func(control *renderControl) error { return a.compileContextControlled(ctx, control) })
	if err != nil {
		clear(a.diags[len(previousDiagnostics):])
		a.prog, a.diags = previousProgram, previousDiagnostics
	}
	return err
}
