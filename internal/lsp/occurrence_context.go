package lsp

import "encoding/json"

func (s *Server) references(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.referencesControlled(params, control)
	})
}

func (s *Server) documentHighlight(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.documentHighlightControlled(params, control)
	})
}

func (s *Server) prepareRename(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.prepareRenameControlled(params, control)
	})
}

func (s *Server) rename(params json.RawMessage) (any, *rpcError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *rpcError) {
		return s.renameControlled(params, control)
	})
}

func (a *analysis) occurrencesOf(o occurrence) []occurrence {
	return a.occurrencesOfControlled(o, nil)
}

func (a *analysis) checkNewName(kind symKind, old, name string) *rpcError {
	return a.checkNewNameControlled(kind, old, name, nil)
}

func (a *analysis) checkUnused(kind symKind, name string) *rpcError {
	return a.checkUnusedControlled(kind, name, nil)
}
