package lsp

import "encoding/json"

func (s *Server) codeActions(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.codeActionsControlled(params, control)
	})
}
