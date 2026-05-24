// merlin-mcp is a single daemon binary that hosts:
//
//   - the static web/ assets (HTML, JS, the existing wasm Machine)
//   - a WebSocket endpoint /ws that the browser page connects to
//   - an HTTP MCP endpoint /mcp that AI clients (Claude, etc.) POST to
//
// Architecture:
//
//	Claude  ◀──HTTP/JSON-RPC──▶  merlin-mcp  ◀──WebSocket──▶  browser
//	                              (broker)                    (Machine)
//
// The Machine lives entirely in the browser. This process is a
// stateless broker: every MCP tool call is forwarded over the
// WebSocket as a JSON request; the browser executes against the live
// wasm Machine and replies with the resulting LED state.
//
// "MCP can join at any time" is the design point. The browser-side
// page is unchanged in behavior whether the broker is running or not
// — if nothing is on the other end (GitHub Pages, no daemon running),
// the page plays solo. Boot this binary and the same page "comes
// alive" with AI-pokeable inputs.
//
// HTTP MCP (not stdio) is deliberate:
//
//   - You run the daemon explicitly when you want AI in the loop. No
//     auto-spawn from Claude Code launches; no surprise child process
//     attached to every session.
//   - Multiple MCP clients (or none) can attach at will. Each request
//     is independent — the broker holds no per-client session state.
//   - The OS firewall is the trust boundary. By default we bind only
//     to localhost (-addr=:8766); change with -addr=<host>:<port>.
//
// One browser at a time (latest WS connection wins). The intermediate
// pending-request map serializes browser-side calls so the Machine
// never sees interleaved frames.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ── browser-side button id mapping ────────────────────────────────────
//
// Matches main.js: pad0..pad10 = 0..10, then the four side buttons.
// Kept as a slice-of-pairs (not a map) so we can also dump a stable
// enum list to the tool's JSON schema.
var buttonIDs = []struct {
	name string
	id   int
}{
	{"pad0", 0}, {"pad1", 1}, {"pad2", 2}, {"pad3", 3}, {"pad4", 4},
	{"pad5", 5}, {"pad6", 6}, {"pad7", 7}, {"pad8", 8}, {"pad9", 9},
	{"pad10", 10},
	{"newgame", 11}, {"samegame", 12}, {"hitme", 13}, {"compturn", 14},
}

func buttonID(name string) (int, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, b := range buttonIDs {
		if b.name == name {
			return b.id, true
		}
	}
	return 0, false
}

func buttonNames() []string {
	out := make([]string, len(buttonIDs))
	for i, b := range buttonIDs {
		out[i] = b.name
	}
	return out
}

// ── broker state ──────────────────────────────────────────────────────
//
// One active browser at a time. New browser displaces the old one.
// All MCP tool calls fan through forward(): they wait on a per-request
// reply channel, with a hard timeout so a missing browser doesn't hang
// the MCP host indefinitely.
type broker struct {
	mu         sync.Mutex
	conn       *websocket.Conn // current browser; nil if none
	pending    map[int64]chan json.RawMessage
	nextID     atomic.Int64
	writeMu    sync.Mutex // serializes WS writes
	callMu     sync.Mutex // serializes outbound MCP calls (one in flight)
	tapTimeout time.Duration
}

func newBroker() *broker {
	return &broker{
		pending:    map[int64]chan json.RawMessage{},
		tapTimeout: 3 * time.Second,
	}
}

// attachConn registers a freshly upgraded browser connection. Any
// previous connection is closed; previously-pending requests are
// failed so callers unblock with an error instead of waiting forever.
func (b *broker) attachConn(c *websocket.Conn) {
	b.mu.Lock()
	old := b.conn
	b.conn = c
	pending := b.pending
	b.pending = map[int64]chan json.RawMessage{}
	b.mu.Unlock()

	if old != nil {
		_ = old.Close(websocket.StatusGoingAway, "replaced by new browser")
	}
	for _, ch := range pending {
		close(ch)
	}
}

// detachConn drops the current connection (called on read error). Any
// in-flight requests get nil replies so they return an error.
func (b *broker) detachConn(c *websocket.Conn) {
	b.mu.Lock()
	if b.conn == c {
		b.conn = nil
	}
	pending := b.pending
	b.pending = map[int64]chan json.RawMessage{}
	b.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
}

// forward sends one op to the browser and waits for the reply. Returns
// the raw result payload (whatever the browser put in "result"), or an
// error if no browser is connected, the request times out, or the
// browser reports a structured error.
func (b *broker) forward(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	b.callMu.Lock()
	defer b.callMu.Unlock()

	b.mu.Lock()
	c := b.conn
	b.mu.Unlock()
	if c == nil {
		return nil, errors.New("no browser connected — open the merlin page to enable MCP control")
	}

	id := b.nextID.Add(1)
	ch := make(chan json.RawMessage, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	req := map[string]any{"id": id, "op": op, "args": args}
	body, _ := json.Marshal(req)

	b.writeMu.Lock()
	wctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	err := c.Write(wctx, websocket.MessageText, body)
	cancel()
	b.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case reply, ok := <-ch:
		if !ok {
			return nil, errors.New("browser disconnected mid-request")
		}
		// reply shape: {"id":..., "result":..., "error":"..."}
		var env struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal(reply, &env); err != nil {
			return nil, fmt.Errorf("bad browser reply: %w", err)
		}
		if env.Error != "" {
			return nil, errors.New(env.Error)
		}
		return env.Result, nil
	case <-time.After(b.tapTimeout):
		return nil, errors.New("browser reply timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readLoop drains messages from one browser connection and routes
// each reply to the waiting forward() caller. Returns when the
// connection closes or errors.
func (b *broker) readLoop(ctx context.Context, c *websocket.Conn) {
	defer b.detachConn(c)
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var head struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			continue
		}
		b.mu.Lock()
		ch := b.pending[head.ID]
		b.mu.Unlock()
		if ch != nil {
			select {
			case ch <- data:
			default:
			}
		}
	}
}

// ── HTTP server: static web/ + /ws upgrade ────────────────────────────

func (b *broker) wsHandler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow any browser tab to connect — this is a dev tool, not
		// a public endpoint. The OS firewall is the trust boundary.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	log.Printf("browser connected: %s", r.RemoteAddr)
	b.attachConn(c)
	// Use the request context so the read loop tears down on server
	// shutdown; the connection itself owns its lifetime otherwise.
	b.readLoop(r.Context(), c)
	log.Printf("browser disconnected: %s", r.RemoteAddr)
}

// ── MCP HTTP transport ────────────────────────────────────────────────
//
// MCP "Streamable HTTP" transport: a single endpoint accepts POST with
// one JSON-RPC 2.0 request per call and returns one JSON-RPC response.
// We don't implement the optional GET-SSE channel because this server
// has no notifications to push — every interaction is request/reply.
//
// Method coverage (tool-only server):
//   initialize → return serverInfo + capabilities.tools
//   notifications/initialized → 202, no body
//   tools/list → return our four tool definitions
//   tools/call → forward to broker, return structured content
//
// Anything else → JSON-RPC error -32601 method not found.

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// dispatch is the shared method router. Returns nil for notifications
// (no reply expected) or a populated rpcResp to send back. Errors are
// encoded into the response itself — never returned as Go errors —
// because the HTTP transport handles JSON-RPC errors in-band.
func (b *broker) dispatch(ctx context.Context, req *rpcReq) *rpcResp {
	isNotification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		return &rpcResp{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{
					"name": "merlin-mcp", "version": "0.1.0",
				},
			},
		}
	case "notifications/initialized", "initialized":
		return nil
	case "tools/list":
		return &rpcResp{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{"tools": toolList()},
		}
	case "tools/call":
		if isNotification {
			return nil
		}
		result, err := dispatchTool(ctx, b, req.Params)
		if err != nil {
			return &rpcResp{
				JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{
					"isError": true,
					"content": []map[string]any{{"type": "text", "text": err.Error()}},
				},
			}
		}
		return &rpcResp{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": result}},
			},
		}
	}
	if isNotification {
		return nil
	}
	return &rpcResp{
		JSONRPC: "2.0", ID: req.ID,
		Error: &rpcErr{Code: -32601, Message: "method not found: " + req.Method},
	}
}

// mcpHandler exposes dispatch over HTTP POST. The dispatch + browser-
// forward chain takes up to b.tapTimeout (≈3s) per request; the HTTP
// server's default ReadHeader/Idle timeouts are sufficient.
func (b *broker) mcpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Some MCP clients may probe with GET for SSE; we don't offer it.
		http.Error(w, "POST application/json with a JSON-RPC body", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad json-rpc body", http.StatusBadRequest)
		return
	}
	resp := b.dispatch(r.Context(), &req)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted) // notification, no reply
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── tool surface ─────────────────────────────────────────────────────

func toolList() []map[string]any {
	names := buttonNames()
	return []map[string]any{
		{
			"name":        "tap",
			"description": "Press and release a Merlin button. Returns the LED state after the press settles. button is one of the named pads/controls; hold_ms is how long to hold (default 170 ms — plenty for the ROM's K-line scan to sample it).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"button":  map[string]any{"type": "string", "enum": names},
					"hold_ms": map[string]any{"type": "integer", "minimum": 50, "maximum": 2000},
				},
				"required": []string{"button"},
			},
		},
		{
			"name":        "read",
			"description": "Read the current LED state and game status. Returns {leds: bool[11], powered: bool, last_game: {n, name, ms_ago} | null}. LED index 0 is the top pad, 1..9 the 3x3 grid (left-to-right, top-to-bottom), 10 the bottom pad. last_game is the most recently DEALT game (via game tool or human's selection UI); it is null when nothing has been started yet, after reset, after power-off, or after a bare NEW GAME press waiting on a digit — meaning the AI legitimately doesn't know what's running and may ask the human.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "reset",
			"description": "Hardware-reset the Merlin. Re-runs the startup light show and tone.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "game",
			"description": "Start a built-in game by number (1..6). Equivalent to tap('newgame') then tap(<digit pad>). Games: 1=Tic-Tac-Toe, 2=Music Machine, 3=Echo, 4=Blackjack 13, 5=Magic Square, 6=Mindbender.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"n": map[string]any{"type": "integer", "minimum": 1, "maximum": 6},
				},
				"required": []string{"n"},
			},
		},
	}
}

// dispatchTool unpacks a tools/call params block, calls the right
// forward() with the right op, and returns a human-readable text body.
// We return the LED snapshot as a compact JSON line so the MCP client
// can either show it as-is or parse it back.
func dispatchTool(ctx context.Context, b *broker, params json.RawMessage) (string, error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("bad params: %w", err)
	}
	switch p.Name {
	case "tap":
		name, _ := p.Arguments["button"].(string)
		id, ok := buttonID(name)
		if !ok {
			return "", fmt.Errorf("unknown button %q", name)
		}
		holdMs, _ := p.Arguments["hold_ms"].(float64)
		if holdMs <= 0 {
			holdMs = 170
		}
		res, err := b.forward(ctx, "tap", map[string]any{"id": id, "hold_ms": holdMs})
		if err != nil {
			return "", err
		}
		return string(res), nil
	case "read":
		res, err := b.forward(ctx, "read", nil)
		if err != nil {
			return "", err
		}
		return string(res), nil
	case "reset":
		res, err := b.forward(ctx, "reset", nil)
		if err != nil {
			return "", err
		}
		return string(res), nil
	case "game":
		n, _ := p.Arguments["n"].(float64)
		if n < 1 || n > 6 {
			return "", fmt.Errorf("game n must be 1..6")
		}
		res, err := b.forward(ctx, "game", map[string]any{"n": int(n)})
		if err != nil {
			return "", err
		}
		return string(res), nil
	}
	return "", fmt.Errorf("unknown tool %q", p.Name)
}

// ── main ─────────────────────────────────────────────────────────────

func main() {
	addr := flag.String("addr", "localhost:8766", "HTTP listen address (browser + WS + MCP). Bind to localhost by default; explicit host:port lets you expose it.")
	webDir := flag.String("web", "./web", "directory of static web assets to serve (HTML + wasm + JS)")
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	if st, err := os.Stat(*webDir); err != nil || !st.IsDir() {
		log.Fatalf("web dir %q not found or not a directory; run from repo root or pass -web=PATH", *webDir)
	}

	b := newBroker()

	fileServer := http.FileServer(http.Dir(*webDir))
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", b.wsHandler)
	mux.HandleFunc("/mcp", b.mcpHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".wasm"):
			w.Header().Set("Content-Type", "application/wasm")
		case strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "text/javascript")
		}
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{Addr: *addr, Handler: mux}

	log.Printf("merlin-mcp daemon ready")
	log.Printf("  page (open in browser):  http://%s/", *addr)
	log.Printf("  MCP endpoint:            http://%s/mcp", *addr)
	log.Printf("  WebSocket (browser):     ws://%s/ws", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}
