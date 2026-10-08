//go:build js && wasm

package main

import "syscall/js"

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
