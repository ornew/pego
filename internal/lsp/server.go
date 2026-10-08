// Package lsp implements a Language Server Protocol server for PEGO grammar files (.pego).
//
// The server speaks LSP 3.17 over a byte stream (standard input and output for pego lsp). It keeps
// the open documents in memory, analyzes a document again after every change, and publishes its
// syntax, compile and type errors as diagnostics. It answers formatting, go to definition, find
// references, document highlights, document symbols, hover, rename, completion and semantic token
// requests. Each document is analyzed on its own: a .pego file is a whole grammar. The design is
// in docs/design/017-language-server.md.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Server is a language server for one client connection.
type Server struct {
	in   *bufio.Reader
	out  *conn
	docs map[string]*document
	// state is the stage of the connection.
	state state
	// version is reported to the client as the server's version.
	version string
	// client holds what the client supports, from its initialize request.
	client clientCapabilities
}

// clientCapabilities are the capabilities of the client that change the server's responses.
type clientCapabilities struct {
	TextDocument struct {
		Completion struct {
			CompletionItem struct {
				// SnippetSupport allows completion items with placeholders.
				SnippetSupport bool `json:"snippetSupport"`
			} `json:"completionItem"`
		} `json:"completion"`
		DocumentSymbol struct {
			// HierarchicalDocumentSymbolSupport allows DocumentSymbol results (otherwise the
			// result is a flat list of SymbolInformation).
			HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport"`
		} `json:"documentSymbol"`
	} `json:"textDocument"`
}

type state int

const (
	stateNew         state = iota // before the initialize request
	stateInitialized              // after the initialize request
	stateShutdown                 // after the shutdown request
)

// document is an open text document.
type document struct {
	uri     string
	version int
	text    string
	an      *analysis // analysis of text, nil until needed
}

func (d *document) analysis() *analysis {
	if d.an == nil {
		d.an = analyze(d.text)
	}
	return d.an
}

// ErrExitWithoutShutdown is returned by Serve when the client sends exit without shutdown first.
var ErrExitWithoutShutdown = errors.New("exit notification before shutdown")

// Serve runs a server that reads messages from r and writes messages to w until the client sends
// the exit notification. It returns nil if the client shut the server down first, as the protocol
// requires, ErrExitWithoutShutdown if it did not, and an error if the input ends or cannot be read
// as messages. version is reported to the client as the server's version.
func Serve(r io.Reader, w io.Writer, version string) error {
	s := &Server{in: bufio.NewReader(r), out: &conn{w: w}, docs: map[string]*document{}, version: version}
	return s.run()
}

func (s *Server) run() error {
	for {
		body, err := readMessage(s.in)
		if err == io.EOF {
			return errors.New("the input ended before the exit notification")
		}
		if err != nil {
			return err
		}
		var msg incoming
		if err := json.Unmarshal(body, &msg); err != nil {
			if err := s.out.reply(nil, nil, &rpcError{Code: codeParseError, Message: "invalid JSON: " + err.Error()}); err != nil {
				return err
			}
			continue
		}
		if msg.Method == "exit" {
			if s.state == stateShutdown {
				return nil
			}
			return ErrExitWithoutShutdown
		}
		if err := s.handle(&msg); err != nil {
			return err
		}
	}
}

// handle handles a message. It returns an error only if the response cannot be written.
func (s *Server) handle(msg *incoming) error {
	switch {
	case msg.Method == "" && (len(msg.Result) > 0 || len(msg.Error) > 0):
		return nil // a response; the server sends no requests, so there is nothing to do
	case msg.Method == "" || msg.JSONRPC != "2.0" && msg.hasID():
		return s.out.reply(msg.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "invalid request"})
	case msg.JSONRPC != "2.0":
		return nil // an invalid notification
	case !msg.hasID():
		s.notification(msg)
		return nil
	}
	result, rerr := s.request(msg)
	return s.out.reply(msg.ID, result, rerr)
}

// request handles a request and returns its result or error.
func (s *Server) request(msg *incoming) (result any, rerr *rpcError) {
	defer func() {
		if x := recover(); x != nil {
			result, rerr = nil, &rpcError{Code: codeInternalError, Message: fmt.Sprintf("internal error: %v", x)}
		}
	}()
	switch s.state {
	case stateNew:
		if msg.Method != "initialize" {
			return nil, &rpcError{Code: codeServerNotInitialized, Message: "the server is not initialized"}
		}
	case stateInitialized:
		if msg.Method == "initialize" {
			return nil, &rpcError{Code: codeInvalidRequest, Message: "the server is already initialized"}
		}
	case stateShutdown:
		return nil, &rpcError{Code: codeInvalidRequest, Message: "the server is shut down"}
	}
	h, ok := requestHandlers[msg.Method]
	if !ok {
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + msg.Method}
	}
	return h(s, msg.Params)
}

// notification handles a notification. Errors are not reported: a notification has no response.
func (s *Server) notification(msg *incoming) {
	defer func() { _ = recover() }()
	if s.state != stateInitialized {
		return // dropped before initialize and after shutdown
	}
	switch msg.Method {
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) == nil {
			s.docs[p.TextDocument.URI] = &document{uri: p.TextDocument.URI, version: p.TextDocument.Version, text: p.TextDocument.Text}
			s.publish(s.docs[p.TextDocument.URI])
		}
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		d := s.docs[p.TextDocument.URI]
		if d == nil {
			return
		}
		for _, c := range p.ContentChanges {
			d.text = applyChange(d.text, c)
		}
		d.version, d.an = p.TextDocument.Version, nil
		s.publish(d)
	case "textDocument/didSave":
		var p didSaveParams
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		if d := s.docs[p.TextDocument.URI]; d != nil {
			if p.Text != nil && *p.Text != d.text {
				d.text, d.an = *p.Text, nil
			}
			s.publish(d)
		}
	case "textDocument/didClose":
		var p didCloseParams
		if json.Unmarshal(msg.Params, &p) == nil && s.docs[p.TextDocument.URI] != nil {
			delete(s.docs, p.TextDocument.URI)
			// Clear the document's diagnostics.
			_ = s.out.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: []Diagnostic{}})
		}
	}
}

// publish sends the diagnostics of d.
func (s *Server) publish(d *document) {
	diags := d.analysis().diags
	if diags == nil {
		diags = []Diagnostic{}
	}
	v := d.version
	_ = s.out.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: d.uri, Version: &v, Diagnostics: diags})
}

type requestHandler func(s *Server, params json.RawMessage) (any, *rpcError)

var requestHandlers map[string]requestHandler

func init() {
	requestHandlers = map[string]requestHandler{
		"initialize":                       (*Server).initialize,
		"shutdown":                         (*Server).shutdown,
		"textDocument/formatting":          (*Server).formatting,
		"textDocument/definition":          (*Server).definition,
		"textDocument/references":          (*Server).references,
		"textDocument/documentHighlight":   (*Server).documentHighlight,
		"textDocument/documentSymbol":      (*Server).documentSymbol,
		"textDocument/hover":               (*Server).hover,
		"textDocument/prepareRename":       (*Server).prepareRename,
		"textDocument/rename":              (*Server).rename,
		"textDocument/completion":          (*Server).completion,
		"textDocument/semanticTokens/full": (*Server).semanticTokens,
	}
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Capabilities clientCapabilities `json:"capabilities"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	s.client = p.Capabilities
	s.state = stateInitialized
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding": "utf-16",
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    2, // incremental
				"save":      map[string]any{"includeText": false},
			},
			"documentFormattingProvider": true,
			"definitionProvider":         true,
			"referencesProvider":         true,
			"documentHighlightProvider":  true,
			"documentSymbolProvider":     true,
			"hoverProvider":              true,
			"renameProvider":             map[string]any{"prepareProvider": true},
			"completionProvider":         map[string]any{"triggerCharacters": []string{"#", "$", "."}},
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{"tokenTypes": semanticTokenTypes, "tokenModifiers": semanticTokenModifiers},
				"full":   true,
			},
		},
		"serverInfo": map[string]any{"name": "pego", "version": s.version},
	}, nil
}

func (s *Server) shutdown(json.RawMessage) (any, *rpcError) {
	s.state = stateShutdown
	return nil, nil
}

// decode decodes the parameters of a request into v.
func decode(params json.RawMessage, v any) *rpcError {
	if len(params) == 0 || string(params) == "null" {
		return &rpcError{Code: codeInvalidParams, Message: "missing parameters"}
	}
	if err := json.Unmarshal(params, v); err != nil {
		return &rpcError{Code: codeInvalidParams, Message: "invalid parameters: " + err.Error()}
	}
	return nil
}

// doc returns the open document uri.
func (s *Server) doc(uri string) (*document, *rpcError) {
	d := s.docs[uri]
	if d == nil {
		return nil, &rpcError{Code: codeRequestFailed, Message: "document not open: " + uri}
	}
	return d, nil
}

// position decodes text document position parameters and returns the document and the byte
// offset of the position.
func (s *Server) position(params json.RawMessage) (*document, int, *rpcError) {
	var p textDocumentPositionParams
	if err := decode(params, &p); err != nil {
		return nil, 0, err
	}
	d, err := s.doc(p.TextDocument.URI)
	if err != nil {
		return nil, 0, err
	}
	return d, d.analysis().idx.offset(p.Position), nil
}
