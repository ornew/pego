package lsp

import (
	"bufio"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestReadMessageHeaderLimit(t *testing.T) {
	body := `{}`
	tail := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n"
	padding := "X-Padding: " + strings.Repeat("a", maxHeader-len(tail)-len("X-Padding: \r\n")) + "\r\n"
	if len(padding)+len(tail) != maxHeader {
		t.Fatalf("header of %d bytes", len(padding)+len(tail))
	}
	got, err := readMessage(bufio.NewReader(strings.NewReader(padding + tail + body)))
	if err != nil || string(got) != body {
		t.Fatalf("header at the limit: %q, %v", got, err)
	}
	_, err = readMessage(bufio.NewReader(strings.NewReader("X" + padding + tail + body)))
	if !errors.Is(err, errHeaderTooLarge) {
		t.Fatalf("header over the limit: %v", err)
	}

	// Neither an unterminated line nor endless short lines are accumulated.
	for name, line := range map[string]string{"unterminated line": "a", "short fields": "X: y\r\n"} {
		r := &repeatReader{line: line}
		_, err := readMessage(bufio.NewReader(r))
		if !errors.Is(err, errHeaderTooLarge) {
			t.Fatalf("%s: %v", name, err)
		}
		// The limit plus at most one buffer fill.
		if r.read > maxHeader+4096 {
			t.Errorf("%s: read %d bytes", name, r.read)
		}
	}
}

// repeatReader yields one line endlessly and counts how much was read.
type repeatReader struct {
	line string
	off  int
	read int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.line[r.off]
		r.off = (r.off + 1) % len(r.line)
	}
	r.read += len(p)
	return len(p), nil
}

func TestOversizedHeaderStopsServer(t *testing.T) {
	c := newInitialized(t)
	// The reader checks the limit as its 4096-byte buffer fills, so send one fill past it.
	go c.w.Write([]byte(strings.Repeat("a", maxHeader+4096)))
	select {
	case err := <-c.done:
		if !errors.Is(err, errHeaderTooLarge) {
			t.Errorf("error %v", err)
		}
	case <-timeoutC():
		t.Fatal("the server did not stop")
	}
}
