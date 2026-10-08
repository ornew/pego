package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// This file implements the base protocol of LSP: JSON-RPC 2.0 messages, each preceded by a header
// with its Content-Length.

// maxMessage is the size of the largest message read. A larger Content-Length is taken to be a
// corrupt header rather than allocated.
const maxMessage = 64 << 20

// JSON-RPC and LSP error codes.
const (
	codeParseError           = -32700
	codeInvalidRequest       = -32600
	codeMethodNotFound       = -32601
	codeInvalidParams        = -32602
	codeInternalError        = -32603
	codeServerNotInitialized = -32002
	codeRequestFailed        = -32803
)

// rpcError is the error of a response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// incoming is a message received from the client: a request (with an id), a notification
// (without one), or a response to a request of the server.
type incoming struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// hasID reports whether the message has a non-null id.
func (m *incoming) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

// readMessage reads the content of the next message. It returns io.EOF at the end of the input
// before a message starts.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	first := true
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && first && line == "" {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("reading header: %w", noEOF(err))
		}
		first = false
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header line %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length %q", strings.TrimSpace(value))
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("message without Content-Length")
	}
	if length > maxMessage {
		return nil, fmt.Errorf("message of %d bytes is too large", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading message: %w", noEOF(err))
	}
	return body, nil
}

func noEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// conn writes messages to the client. It is safe for concurrent use.
type conn struct {
	mu sync.Mutex
	w  io.Writer
}

func (c *conn) write(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var b bytes.Buffer
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n", len(data))
	b.Write(data)
	_, err = c.w.Write(b.Bytes())
	return err
}

// reply sends the response to the request id: the result, or the error if err is not nil.
func (c *conn) reply(id json.RawMessage, result any, err *rpcError) error {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	if err != nil {
		return c.write(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *rpcError       `json:"error"`
		}{"2.0", id, err})
	}
	// A successful response always has a result, null if there is none.
	return c.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
}

// notify sends a notification.
func (c *conn) notify(method string, params any) error {
	return c.write(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}
