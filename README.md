# lets-go-merlin

A faithful emulator of **Merlin — The Electronic Wizard** (Parker
Brothers, 1978), running the **original Texas Instruments MP3404 ROM**
on a from-scratch **TMS1100** interpreter written in Go. The same core
runs three ways: a browser build (Go → WebAssembly, audio via an
AudioWorklet), a CLI for tracing/debugging, and — as a pure, dependency-
free package — anywhere Go runs.

Nothing here re-creates Merlin's six games. The 1978 program is executed
one instruction at a time; the light-show, the tones and all six games
*emerge* from running the real chip's code. The interpreter is verified
**bit-for-bit** against a C++ reference across 200,000 instructions.

<p align="center">
  <img src="docs/images/merlin.png" alt="Merlin faceplate" height="420">
</p>

## Play

Live (GitHub Pages): **https://carledwards.github.io/lets-go-merlin/**

Press **Power On**, then click a game in the side panel (it does
*New Game → number* for you), or play the device directly — the 11 lit
pads and the New/Same/Hit/Comp buttons on the faceplate are all live.

## How it works

Short version: a pure TMS1100 interpreter (`pkg/tms1100`) executes the
embedded ROM; a thin Merlin wrapper (`pkg/merlin`) decodes the R/O/K
matrix into 11 LEDs and the buttons, taps the speaker line, and a
resampler (`pkg/audio`) turns the ~58 kHz 1-bit speaker stream into
browser audio. Full design, the matrix mapping, the audio pacing
decision and the verification method: **[docs/architecture.md](docs/architecture.md)**.

## Project layout

```
cmd/merlinweb     Go→WASM browser front-end (syscall/js)
cmd/merlincli     CLI: instruction trace / matrix-scan view
cmd/merlinserve   tiny static server (correct .wasm MIME)
cmd/merlin-mcp    local daemon: page + WebSocket broker + HTTP MCP
pkg/tms1100       pure TMS1100 interpreter (no I/O, no deps)
pkg/merlin        Merlin wrapper: matrix, LED decay, speaker
pkg/audio         1-bit speaker → PCM resampler
roms              embedded MP3404 ROM (2 KB)
web               static site: index.html, main.js, worklet.js
internal/tools    ledscan / btnscan — derive UI coords from the art
docs              architecture.md + images
```

## Quickstart

### Run the web locally

```bash
make serve
# build web/merlin.wasm and serve at http://localhost:8080
```

`web/` is a self-contained static site (no SharedArrayBuffer, no
COOP/COEP). The GitHub Pages workflow at
`.github/workflows/wasm-pages.yml` rebuilds the wasm and deploys `web/`
to Pages on every push to `main`.

#### Interact with Merlin from the console

Open your browser's DevTools console and talk to the global `merlin`
object — handy for demos, testing, or just poking at it:

```js
await merlin.on()          // power on (init or resume)
merlin.game(1)             // New Game → 1   (1–6 = the six games)
merlin.tap(5)              // press + release a pad/button
merlin.press(3)            // hold …
merlin.release(3)          // … then let go
merlin.reset()             // reset the device
merlin.off()               // real power-off (cold start next time)
merlin.speed(1.5)          // clock tuning, 0.1–4× (pitch follows)
merlin.volume(0.4)         // 0..1, post-mix attenuation
merlin.ids                 // { pad0:0 … pad10:10, newGame:11, … }
```

Ids: `0–10` are the pads (`1`–`9` the grid, `10` the bottom “0”, `0`
the top pad), then `11` New Game, `12` Same Game, `13` Hit Me, `14`
Comp Turn.

### Control Merlin from your AI agent (e.g. Claude)

The same page can host an **MCP endpoint** so Claude, a custom script,
or anything that speaks MCP can press buttons and read the LEDs on the
same Merlin you're looking at. The Merlin Machine still lives in the
browser; `cmd/merlin-mcp` is a stateless **broker** — every MCP tool
call hops over a WebSocket to the open page, which presses the actual
buttons and reads the actual LEDs.

Only switches on when the page and the broker are both running on your
machine. The hosted GitHub Pages build has no broker to talk to and
plays solo, silently.

```bash
make mcp                          # build bin/merlin-mcp
./bin/merlin-mcp                  # daemon on localhost:8766
# open http://localhost:8766/  →  green "MCP connected" badge appears

# point Claude Code at the daemon (one-time):
claude mcp add --transport http merlin http://localhost:8766/mcp

# remove the registration when you're done playing:
claude mcp remove merlin
```

Stop the daemon (Ctrl-C) and the AI tools simply stop working — no
auto-spawn, no resident child process. The page keeps playing solo.
`claude mcp remove merlin` only deletes Claude's pointer to the
daemon; the binary and source stay where they are, ready for next time.

Tools the AI gets:

| Tool   | What it does                                                  |
|--------|---------------------------------------------------------------|
| `tap`  | Press + release a named button (`pad0`..`pad10`, `newgame`, `samegame`, `hitme`, `compturn`). Returns the LED state after the press settles. |
| `read` | Current LED state + `last_game` hint (which game was most recently dealt, or `null` if unknown). |
| `reset`| Hardware reset — re-runs the startup light show. |
| `game` | Start a built-in game (1=Tic-Tac-Toe through 6=Mindbender). |

Things to ask Claude once it's wired up:

```
Start tic-tac-toe and tell me which lights are on.
Play a game of Echo against me — I'll go first.
Reset the machine, deal Mindbender, then read the lights every
few seconds and narrate what Merlin is showing me.
```

## Build / test commands

```bash
make test     # full suite incl. the 5000-line CPU golden trace
make cli      # watch the matrix scan in the terminal
make wasm     # build web/merlin.wasm (alias for the Pages build)
make mcp      # build bin/merlin-mcp (the local broker daemon)
make clean    # remove generated artifacts
```

## TMS1100 simulation & verification

`pkg/tms1100` is a pure-Go interpreter for the chip family — no I/O,
no host hooks, no dependencies. It's diffed instruction-for-instruction
against the C++ reference (`carledwards/merlin-tms1100`):
**200,000 / 200,000 byte-identical**. `make ref-verify` re-runs the
live diff (needs `clang++`); a CI-safe golden trace is checked into
`pkg/tms1100/testdata`. `go test ./...` covers the core, the matrix
wrapper and the resampler.

What's interesting about the TMS1100 itself:

- **4-bit, ~350 kHz, no crystal.** A resistor and capacitor set the
  clock — about 58,000 instructions per second.
- **One-deep call stack**, no interrupts, no timers, no DMA. Display
  multiplexing, button scanning, sound, and game AI are *all* hand-
  timed software loops.
- **Program counter counts in a scrambled LFSR order** to save
  transistors. The interpreter applies the same scramble so PC values
  match the original silicon exactly.
- **Mask ROM** — the program is etched into the chip at the factory.
  Unchangeable, nearly free at volume. The game *is* the chip; the 2 KB
  in `roms/mp3404.bin` is that exact mask.

## Credits & provenance

- **Bob Doyle** — designed Merlin (Parker Brothers, 1978). The faceplate
  artwork is derived from his site
  [theelectronicwizard.com](https://www.theelectronicwizard.com/);
  rights remain with the original author.
- **Dominic Thibodeau (hotkeysoft)** — the original C++ TMS1000-family
  emulator this lineage descends from.
- **Carl Edwards** —
  [`merlin-tms1100`](https://github.com/carledwards/merlin-tms1100),
  the C++/Python port used as the bit-exact reference spec.
- **The MAME team** — `hh_tms1k.cpp`, source of the definitive Merlin
  R/O/K matrix wiring and ROM identification (MAME is GPL-2.0; only the
  factual wiring was used — no MAME code is included here).
- **Texas Instruments / Parker Brothers** — the TMS1100 and the MP3404
  ROM (`roms/mp3404.bin`), © 1978. Included for emulation and
  preservation (the same dump MAME uses); copyright is retained by its
  owners and it is **not** covered by this repo's license.

## License

MIT (original code only) — see [LICENSE](LICENSE). The embedded ROM and
the derived faceplate art are third-party material under their own
rights, as noted above and in the LICENSE file.
