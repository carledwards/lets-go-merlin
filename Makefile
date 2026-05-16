# Merlin TMS1100 emulator — build helpers.
GOROOT := $(shell go env GOROOT)

.PHONY: test cli web wasm serve clean ref-verify

wasm: web                   ## alias used by the GitHub Pages workflow

test:                       ## run all Go tests (CPU golden trace + wrapper)
	go test ./...

cli:                        ## watch the matrix scan in the terminal
	go run ./cmd/merlincli -steps 60000 -every 6000

web: web/merlin.wasm web/wasm_exec.js  ## build the WASM front-end

web/merlin.wasm: $(shell find pkg cmd/merlinweb roms -name '*.go') roms/mp3404.bin
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o $@ ./cmd/merlinweb

web/wasm_exec.js:
	cp "$(GOROOT)/lib/wasm/wasm_exec.js" $@

serve: web                  ## build web + serve at http://localhost:8080
	go run ./cmd/merlinserve

# Rebuild the C++ reference with its step-trace printf enabled and diff
# the first N instructions against this port (bit-exact regression).
ref-verify:                 ## diff Go core vs C++ reference (needs clang++)
	@bash scripts/ref-verify.sh

clean:
	rm -f web/merlin.wasm web/wasm_exec.js
