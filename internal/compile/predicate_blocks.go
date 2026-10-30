package compile

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

func predicateBlock(e *grammar.Predicate, block *grammar.Block, control *Control) grammar.Expr {
	var poll func()
	if control != nil {
		poll = control.poll
	}
	control.check()
	if len(block.Statements) == 1 {
		copy := *e
		copy.Term = grammaranalysis.UngroupTermWithPoll(block.Statements[0], poll)
		return &copy
	}
	items := make([]grammar.Expr, len(block.Statements))
	for i, statement := range block.Statements {
		control.poll()
		items[i] = &grammar.Predicate{
			SourceInfo: *statement.(grammar.SourceNode).Source(),
			Term:       grammaranalysis.UngroupTermWithPoll(statement, poll),
		}
	}
	return &grammar.Seq{
		SourceInfo: e.SourceInfo,
		Items:      items,
	}
}

// expandPredicateBlocks splices the predicates of every multi-statement predicate block into the
// list, and reports whether any block was expanded. normalized[i] is the normalized form of
// source[i], and a normalized block is the sequence of its predicates. It returns normalized
// itself when no block has more than one statement.
func expandPredicateBlocks(source, normalized []grammar.Expr, control *Control) ([]grammar.Expr, bool) {
	expanded := 0
	for _, item := range source {
		control.poll()
		if count := predicateStatementCount(item); count > 1 {
			expanded += count - 1
		}
	}
	if expanded == 0 {
		return normalized, false
	}
	control.check()
	items := make([]grammar.Expr, 0, len(normalized)+expanded)
	for i, item := range normalized {
		control.poll()
		if predicateStatementCount(source[i]) > 1 {
			items = appendNormalizedPredicates(items, item.(*grammar.Seq).Items, control)
		} else {
			items = append(items, item)
		}
	}
	return items, true
}

func predicateStatementCount(expr grammar.Expr) int {
	predicate, ok := expr.(*grammar.Predicate)
	if !ok {
		return 0
	}
	block, ok := predicate.Term.(*grammar.Block)
	if !ok {
		return 0
	}
	return len(block.Statements)
}
