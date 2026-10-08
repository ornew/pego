//go:build js && wasm

package main

import "syscall/js"

func init() {
	// In WebAssembly, Go function calls nest on the stack of the JavaScript engine, which is much
	// smaller than a goroutine's: a parse that nests too deeply, or the recursive encoding of a deep
	// tree, ends with "Maximum call stack size exceeded" and stops the program. Measured with the
	// examples (json, calculator, minilang), the stack of a Web Worker in Chromium 2026 overflowed
	// at about 1,450 nested rule calls with the closure and bytecode backends and at trees nested
	// about 880 levels deep; Node 24's stack holds about twice as much. The limits leave a margin
	// of more than two, because a rule call takes more stack in rules with more deeply nested
	// expressions. See docs/design/016-web-site-and-playground.md.
	maxDepth = 600
	maxTreeDepth = 400
}

func main() {
	api := js.Global().Get("Object").New()
	for _, method := range []string{"compile", "parse", "format", "generate", "version"} {
		api.Set(method, js.FuncOf(func(this js.Value, args []js.Value) any {
			var req string
			if len(args) > 0 && args[0].Type() == js.TypeString {
				req = args[0].String()
			}
			return string(handle(method, []byte(req)))
		}))
	}
	js.Global().Set("pego", api)
	// Tell a loader that is waiting for the API that it is ready.
	if ready := js.Global().Get("pegoReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
