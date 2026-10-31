package core

import "github.com/ornew/zeroparse/internal/value"

// EmitStream marks only caller execution. Reset before handling a returned
// error or committing the prefix, so later VM faults retain their attribution.
func (p *Parser) EmitStream(n *value.Node) error {
	if BudgetControlsEnabled {
		p.Control.Check()
	}
	if p.Diagnostic != nil || p.DiagnosticTail.Limit > 0 {
		if err := p.ReportRecovered(); err != nil {
			return err
		}
	}
	p.EmitCall = true
	err := p.Emit(n)
	p.EmitCall = false
	if err == nil && BudgetControlsEnabled {
		p.Control.Check()
	}
	return err
}

// ReportRecovered runs only at an irreversible stream boundary. Earlier
// recoveries in a speculative branch may have been removed by backtracking.
func (p *Parser) ReportRecovered() error {
	if p.Diagnostic == nil && p.DiagnosticTail.Limit == 0 {
		return nil
	}
	if p.Diagnosed > len(p.Recovered) {
		p.Diagnosed = len(p.Recovered)
	}
	for p.Diagnosed < len(p.Recovered) {
		if BudgetControlsEnabled {
			p.Control.Check()
		}
		original := p.Recovered[p.Diagnosed]
		if p.Diagnostic != nil {
			e := *original
			e.Expected = append([]string(nil), e.Expected...)
			e.Messages = append([]string(nil), e.Messages...)
			p.EmitCall = true
			err := p.Diagnostic(&e)
			p.EmitCall = false
			if err != nil {
				return err
			}
		}
		if p.DiagnosticTail.Limit > 0 {
			p.DiagnosticTail.Add(original)
		}
		p.Diagnosed++
		if BudgetControlsEnabled {
			p.Control.Check()
		}
	}
	return nil
}
