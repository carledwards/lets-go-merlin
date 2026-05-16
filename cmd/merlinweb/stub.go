//go:build !(js && wasm)

// This package is the browser front-end and only builds for WASM. This
// stub keeps `go build ./...` / `go test ./...` working on the host.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr,
		"merlinweb is the WASM front-end; build it with:\n"+
			"  GOOS=js GOARCH=wasm go build -o web/merlin.wasm ./cmd/merlinweb")
	os.Exit(1)
}
