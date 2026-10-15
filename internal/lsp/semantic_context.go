package lsp

import "encoding/json"

func (s *Server) semanticTokens(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.semanticTokensControlled(params, control)
	})
}

func (a *analysis) semanticTokens() []int {
	return a.semanticTokensControlled(nil)
}
