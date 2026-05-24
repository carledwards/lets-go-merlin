# Merlin TMS1100 emulator — build helpers.
#
# GOWORK=off everywhere: this repo can sit beside other modules that
# are listed in a parent-dir go.work file; without disabling workspace
# mode, the go toolchain refuses to build a module that isn't listed.
# Setting GOWORK=off locally keeps lets-go-merlin self-contained
# regardless of what the surrounding workspace declares.
export GOWORK = off
GOROOT := $(shell go env GOROOT)

.PHONY: test cli web wasm serve clean ref-verify mcp mcp-run

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

# merlin-mcp — daemon binary serving the page, the browser WebSocket,
# and the MCP HTTP endpoint. Run it when you want AI in the loop:
#
#   make mcp-run             # interactive, foreground
#   make mcp && ./bin/merlin-mcp  # build then run the binary directly
#
# Configure your MCP client to POST JSON-RPC to http://localhost:8766/mcp
# (e.g.  claude mcp add --transport http merlin http://localhost:8766/mcp ).
# Stop the daemon and AI tools simply stop working — no auto-spawn.
mcp: web                    ## build cmd/merlin-mcp daemon
	go build -o bin/merlin-mcp ./cmd/merlin-mcp

mcp-run: web                ## run merlin-mcp daemon (foreground)
	go run ./cmd/merlin-mcp

# Rebuild the C++ reference with its step-trace printf enabled and diff
# the first N instructions against this port (bit-exact regression).
ref-verify:                 ## diff Go core vs C++ reference (needs clang++)
	@bash scripts/ref-verify.sh

clean:
	rm -f web/merlin.wasm web/wasm_exec.js
