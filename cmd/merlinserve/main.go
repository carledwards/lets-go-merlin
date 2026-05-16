// merlinserve is a minimal static file server for the web build. It
// exists so .wasm is served as application/wasm (required by
// WebAssembly.instantiateStreaming) and nothing is cached during
// development. No COOP/COEP headers: the v1 audio path uses an
// AudioWorklet fed by postMessage, not SharedArrayBuffer.
//
//	go run ./cmd/merlinserve            # serves ./web on :8080
//	go run ./cmd/merlinserve -addr :9000 -dir web
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dir := flag.String("dir", "web", "directory to serve")
	flag.Parse()

	fs := http.FileServer(http.Dir(*dir))
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".wasm"):
			w.Header().Set("Content-Type", "application/wasm")
		case strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "text/javascript")
		}
		w.Header().Set("Cache-Control", "no-store")
		fs.ServeHTTP(w, r)
	})

	log.Printf("merlin: serving %q at http://localhost%s", *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
