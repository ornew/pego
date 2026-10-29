package engine

import "github.com/ornew/pego/internal/value"

// emitStream marks only caller execution. Reset before handling a returned
// error or committing the prefix, so later VM faults retain their attribution.
func (p *parser) emitStream(n *value.Node) error {
	if workControlsEnabled {
		p.control.check()
	}
	if p.diagnostic != nil || p.diagnosticTail.limit > 0 {
		if err := p.reportRecovered(); err != nil {
			return err
		}
	}
	p.emitCall = true
	err := p.emit(n)
	p.emitCall = false
	if err == nil && workControlsEnabled {
		p.control.check()
	}
	return err
}

// reportRecovered runs only at an irreversible stream boundary. Earlier
// recoveries in a speculative branch may have been removed by backtracking.
func (p *parser) reportRecovered() error {
	if p.diagnostic == nil && p.diagnosticTail.limit == 0 {
		return nil
	}
	if p.diagnosed > len(p.recovered) {
		p.diagnosed = len(p.recovered)
	}
	for p.diagnosed < len(p.recovered) {
		if workControlsEnabled {
			p.control.check()
		}
		original := p.recovered[p.diagnosed]
		if p.diagnostic != nil {
			e := *original
			e.Expected = append([]string(nil), e.Expected...)
			e.Messages = append([]string(nil), e.Messages...)
			p.emitCall = true
			err := p.diagnostic(&e)
			p.emitCall = false
			if err != nil {
				return err
			}
		}
		if p.diagnosticTail.limit > 0 {
			p.diagnosticTail.add(original)
		}
		p.diagnosed++
		if workControlsEnabled {
			p.control.check()
		}
	}
	return nil
}
