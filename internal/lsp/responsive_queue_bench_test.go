package lsp

import (
	"encoding/json"
	"strconv"
	"testing"
)

// Setup, analysis and shutdown are outside timing. The loop measures sequential
// framed request/reply dispatch on one responsive connection, including client
// and server wire allocations, without a worker hook or cancellation barrier.
func BenchmarkResponsiveDispatch(b *testing.B) {
	for _, method := range []string{"textDocument/hover", "textDocument/documentSymbol"} {
		b.Run(method, func(b *testing.B) {
			b.StopTimer()
			peer := newF08BenchmarkPeer(b)
			uri := "file:///dispatch.pego"
			peer.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": `rule main = "a"`}}, false)
			notification := peer.receive()
			if notification.Method != "textDocument/publishDiagnostics" {
				b.Fatalf("unexpected initial message: %+v", notification)
			}
			var diagnostics publishDiagnosticsParams
			if err := json.Unmarshal(notification.Params, &diagnostics); err != nil || len(diagnostics.Diagnostics) != 0 {
				b.Fatalf("invalid initial diagnostics: %s", notification.Params)
			}
			var params any = at(uri, 0, 5)
			if method == "textDocument/documentSymbol" {
				params = docParams(uri)
			}
			b.ReportAllocs()
			b.StartTimer()
			for b.Loop() {
				id := peer.send(method, params, true)
				reply := peer.receive()
				if string(reply.ID) != strconv.Itoa(id) || len(reply.Error) != 0 || string(reply.Result) == "null" {
					b.Fatalf("unexpected response: %+v", reply)
				}
			}
			b.StopTimer()
			peer.shutdown()
		})
	}
}
