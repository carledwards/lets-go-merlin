'use strict';

// LED cut-out hole centers in merlin.png (290x700), in Merlin pad order
// (0 = top, 1..9 = 3x3 grid, 10 = bottom). Auto-detected from the
// faceplate alpha channel by `go run ./internal/tools/ledscan`.
const LED_HOLES = [
  { cx: 144, cy: 242, r: 8 },
  { cx: 90,  cy: 295, r: 8 }, { cx: 144, cy: 295, r: 8 }, { cx: 198, cy: 296, r: 8 },
  { cx: 91,  cy: 349, r: 8 }, { cx: 144, cy: 349, r: 8 }, { cx: 198, cy: 350, r: 8 },
  { cx: 91,  cy: 403, r: 8 }, { cx: 144, cy: 404, r: 8 }, { cx: 198, cy: 404, r: 8 },
  { cx: 143, cy: 456, r: 8 },
];
const HIT_R = 22; // clickable radius around each pad (bigger than the LED)

const statusEl = document.getElementById('status');
const setStatus = (s) => { statusEl.textContent = s; };

// Build the LED glow elements (behind the faceplate) and the
// transparent click targets (above it) from the detected coordinates.
const ledsBox = document.getElementById('leds');
const hitsBox = document.getElementById('hits');
const ledEls = [];
LED_HOLES.forEach((hole, i) => {
  const d = hole.r * 2 + 4;             // fill the hole, slight bleed
  const led = document.createElement('div');
  led.className = 'led';
  led.style.cssText =
    `width:${d}px;height:${d}px;left:${hole.cx - d / 2}px;top:${hole.cy - d / 2}px`;
  ledsBox.appendChild(led);
  ledEls.push(led);

  const hit = document.createElement('div');
  hit.className = 'pad-hit';
  hit.style.cssText =
    `width:${HIT_R * 2}px;height:${HIT_R * 2}px;` +
    `left:${hole.cx - HIT_R}px;top:${hole.cy - HIT_R}px`;
  hit.dataset.btn = String(i);          // pad index 0..10 == button id
  hitsBox.appendChild(hit);
});

// Printed control buttons on the faceplate (NEW GAME / SAME GAME /
// HIT ME / COMP TURN). Boxes auto-detected from merlin.png by
// `go run ./internal/tools/btnscan`. Button ids match the labelled
// row below (New Game 11, Same Game 12, Hit Me 13, Comp Turn 14).
const CTRL_BUTTONS = [
  { id: 11, x: 95,  y: 544, w: 39, h: 33 }, // New Game
  { id: 12, x: 156, y: 544, w: 40, h: 33 }, // Same Game
  { id: 13, x: 101, y: 598, w: 34, h: 31 }, // Hit Me
  { id: 14, x: 156, y: 598, w: 34, h: 31 }, // Comp Turn
];
const CTRL_PAD = 6; // enlarge the hit area slightly past the printed box
CTRL_BUTTONS.forEach((b) => {
  const hit = document.createElement('div');
  hit.className = 'ctrl-hit';
  hit.style.cssText =
    `left:${b.x - CTRL_PAD}px;top:${b.y - CTRL_PAD}px;` +
    `width:${b.w + CTRL_PAD * 2}px;height:${b.h + CTRL_PAD * 2}px`;
  hit.dataset.btn = String(b.id);
  hitsBox.appendChild(hit);
});

// ---- Load and start the Go/WASM module ----
const go = new Go();
WebAssembly.instantiateStreaming(fetch('merlin.wasm'), go.importObject)
  .then((res) => {
    go.run(res.instance);              // runs main(); never resolves (select{})
    return waitFor(() => window.merlinPump);
  })
  .then(() => setStatus('Ready. Press Power On.'))
  .catch((e) => setStatus('Load error: ' + e));

function waitFor(pred) {
  return new Promise((resolve) => {
    (function poll() { pred() ? resolve() : setTimeout(poll, 20); })();
  });
}

// ---- Audio + run loop ----
const MAX_SAMPLES = 8192;              // must match maxSamplesPerPump in Go
let audioCtx = null, node = null, gain = null;
let started = false;                   // one-time audio/init done
// 0.6 = -40% (linear amplitude). The wasm emits samples at ±1.0 full
// scale; that's loud on most speaker/headphone setups, especially the
// idle DC + tone bursts. GainNode lives between the worklet and the
// destination so this stays a pure post-mix attenuation — the audio
// thread, the wasm, and the underrun-decay logic see no change.
const DEFAULT_VOLUME = 0.6;
let running = false;                   // powered on (loop active)
let leds = null, audioRaw = null, audioF32 = null;

const powerBtn = document.getElementById('power');

function setPowered(on) {
  running = on;
  powerBtn.classList.toggle('on', on);
  powerBtn.textContent = on ? '⏻ Power Off' : '⏻ Power On';
}

// One-time AudioContext + worklet + Go init. Must run from a user
// gesture (the click that calls it).
async function initOnce() {
  if (!window.merlinInit) { setStatus('Still loading…'); return false; }
  audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  await audioCtx.audioWorklet.addModule('worklet.js');
  node = new AudioWorkletNode(audioCtx, 'merlin-processor', { outputChannelCount: [1] });
  gain = audioCtx.createGain();
  gain.gain.value = DEFAULT_VOLUME;
  node.connect(gain);
  gain.connect(audioCtx.destination);

  leds = new Uint8Array(11);
  audioRaw = new Uint8Array(MAX_SAMPLES * 4);
  audioF32 = new Float32Array(audioRaw.buffer);
  window.merlinInit(audioCtx.sampleRate, leds, audioRaw);
  started = true;
  return true;
}

// Power on: first call initializes; later calls just resume. Returns
// true once running. Doubles as the audio user-gesture for game/pad
// clicks. Idempotent.
async function startConsole() {
  if (running) return true;
  if (!started && !(await initOnce())) return false;
  await audioCtx.resume();
  setPowered(true);
  setStatus('Running — pick a game on the left, or use New Game + a number.');
  requestAnimationFrame(frame);
  return true;
}

// Power off: a real off, not a pause. Stop the loop, blank the LEDs,
// reset the emulator to its power-on state, drop queued audio and
// silence the context — so the next Power On cold-starts and replays
// the startup light-show and tone, like the real toy.
async function powerOff() {
  if (!running) return;
  setPowered(false);                   // frame() exits on next tick
  for (const el of ledEls) el.classList.remove('on'); // device goes dark
  if (window.merlinReset) window.merlinReset();        // back to power-on
  if (node) node.port.postMessage({ flush: true });    // drop queued samples
  clearGameStarted();
  setStatus('Powered off.');
  try { await audioCtx.suspend(); } catch (e) { /* ignore */ }
}

let powering = false; // guard against double-clicks during transition
powerBtn.addEventListener('click', async () => {
  if (powering) return;
  powering = true;
  try { await (running ? powerOff() : startConsole()); }
  finally { powering = false; }
});

// Audio backlog suppression. Tabs that get backgrounded see rAF
// throttle to ~1 Hz; when they return, merlinPump emits its full
// backlog (capped at MAX_SAMPLES = 8192, ~170 ms) in one call. Playing
// that compressed burst is the "fast-forward pulse." We still pump it
// (so the Machine's clock advances correctly) but skip posting the
// audio. The worklet's existing decay-on-underrun then gently fades
// the held sample toward 0 over ~200 ms while we're skipping; once
// rAF is steady again we post normal-sized chunks and the worklet's
// pre-roll + fade-in logic re-engages cleanly. No flush, no click.
//
// Two thresholds, OR'd:
//   MAX_FRAME_DT_MS = 100   — well above a steady frame (16 ms @ 60 Hz,
//                              33 ms @ 30 Hz); catches tab-return gaps.
//   STALE_BATCH     = 2000  — guards the case where rAF dt looks fine
//                              but the wasm pump's internal clock saw a
//                              gap (e.g. first frame after Power On or
//                              after the visibility-return baseline reset).
//
// Audio path debug counters — always tallying (cheap), printable on
// demand. Toggle live-logging from DevTools:
//   merlin.audioDebug(true)   // console.log every stale frame
//   merlin.audioStats()       // dump current totals
// Useful when you hear a pulse: stats tell you whether it was stale-
// by-dt (tab return), stale-by-batch (wasm pump catch-up), or neither
// (likely the toy's own button-press tone — the ROM responding, not
// an audio-path glitch).
const audioStats = {
  frames:        0, // total rAF frames where we pumped
  posted:        0, // frames where audio was actually posted
  staleByDt:     0, // frames suppressed because dt > MAX_FRAME_DT_MS
  staleByBatch:  0, // frames suppressed because n > STALE_BATCH
  maxDt:         0,
  maxN:          0,
  lastStaleAt:   0,
  lastStaleDt:   0,
  lastStaleN:    0,
};
let audioDebugLog = false;

const MAX_FRAME_DT_MS = 100;
// A normal rAF tick at 60 Hz produces ~768 samples @ 48 kHz; at 30 Hz
// ~1600. Anything past 2000 means the pump's internal clock saw a
// gap, even if rAF's dt looked OK (e.g. the first frame after
// AudioContext.resume or after a visibility-return baseline reset).
// Treat oversize batches as stale too.
const STALE_BATCH = 2000;
let lastFrameNow = 0;

function frame(now) {
  if (!running) return;
  const dt = lastFrameNow ? now - lastFrameNow : 0;
  lastFrameNow = now;

  const n = window.merlinPump(now);
  audioStats.frames++;
  if (dt > audioStats.maxDt) audioStats.maxDt = dt;
  if (n  > audioStats.maxN)  audioStats.maxN  = n;
  const staleDt    = dt > MAX_FRAME_DT_MS;
  const staleBatch = n  > STALE_BATCH;
  if (staleDt || staleBatch) {
    // Long gap (tab return, GC pause, MCP-induced stall). Drop the
    // backlog — playing seconds of compressed audio is the "fast-
    // forward pulse." We deliberately do NOT send {flush:true} here:
    // flush snaps the worklet's `last` value to 0, which the speaker
    // hears as a step change = audible click. Instead, we simply stop
    // feeding; the worklet's existing decay-on-underrun gently fades
    // the held sample toward 0 over ~200 ms (0.9994/sample), then
    // re-arms with a smooth fade-in once we start posting again.
    if (staleDt)    audioStats.staleByDt++;
    if (staleBatch) audioStats.staleByBatch++;
    audioStats.lastStaleAt = now;
    audioStats.lastStaleDt = dt;
    audioStats.lastStaleN  = n;
    if (audioDebugLog) {
      console.log('merlin audio: stale frame  dt=%dms  n=%d  (byDt=%s byBatch=%s)',
        Math.round(dt), n, staleDt, staleBatch);
    }
  } else if (n > 0) {
    audioStats.posted++;
    node.port.postMessage(audioF32.slice(0, n)); // copy; buffer reused
  }
  for (let i = 0; i < 11; i++) ledEls[i].classList.toggle('on', leds[i] !== 0);
  requestAnimationFrame(frame);
}

// Visibility hook: only resets the dt baseline on return so the first
// post-return frame doesn't see "5 seconds since last rAF" and
// over-trigger the stale path on a normal pump. We deliberately do
// NOT flush on hidden: rAF will throttle, our stale check will catch
// the catch-up burst, and the worklet's underrun decay handles the
// rest — silently. Flushing would just add a click on tab exit.
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) {
    lastFrameNow = 0;
  }
});

// ---- Input wiring ----
// NEW GAME (id 11) drops the toy into select-mode; whatever game was
// running is no longer current. Clear last_game so the MCP snapshot
// doesn't lie. selectGame() / opGame() re-set it after the digit tap.
function press(id)   {
  if (id === 11) clearGameStarted();
  if (window.merlinPress) window.merlinPress(id);
}
function release(id) { if (window.merlinRelease) window.merlinRelease(id); }

document.querySelectorAll('[data-btn]').forEach((el) => {
  const id = parseInt(el.dataset.btn, 10);
  const down = (e) => { e.preventDefault(); press(id); };
  const up   = (e) => { e.preventDefault(); release(id); };
  el.addEventListener('pointerdown', down);
  el.addEventListener('pointerup', up);
  el.addEventListener('pointerleave', up);
  el.addEventListener('pointercancel', up);
  el.addEventListener('contextmenu', (e) => e.preventDefault());
});

document.getElementById('reset').addEventListener('click', () => {
  if (window.merlinReset) window.merlinReset();
  clearGameStarted();
});

// Keyboard: ` top pad, 1-9 grid, 0 bottom, N/S/H/C game, R reset.
const KEYMAP = {
  '`': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5,
  '6': 6, '7': 7, '8': 8, '9': 9, '0': 10,
  'n': 11, 's': 12, 'h': 13, 'c': 14,
};
const pressedKeys = new Set();
window.addEventListener('keydown', (e) => {
  const k = e.key.toLowerCase();
  if (k === 'r') { if (window.merlinReset) window.merlinReset(); clearGameStarted(); return; }
  if (k in KEYMAP && !pressedKeys.has(k)) { pressedKeys.add(k); press(KEYMAP[k]); }
});
window.addEventListener('keyup', (e) => {
  const k = e.key.toLowerCase();
  if (k in KEYMAP) { pressedKeys.delete(k); release(KEYMAP[k]); }
});

// ---- Clickable game list: auto New Game -> number ----
// On a real Merlin you press NEW GAME, the wand acknowledges, then you
// press a number pad (1-6). Game N is the pad labelled "N" == button N.
const NEW_GAME = 11;
const GAME_NAMES = {
  1: 'Tic-Tac-Toe', 2: 'Music Machine', 3: 'Echo',
  4: 'Blackjack 13', 5: 'Magic Square', 6: 'Mindbender',
};
const TAP_MS = 170;   // synthetic button hold (plenty of K scans)
const GAP_MS = 950;   // let the ROM finish acknowledging New Game
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let selecting = false;
const gamesEl = document.getElementById('games');

// Last-dealt game tracker. Set whenever NEW GAME + digit is driven
// through selectGame() or the MCP `game` op; cleared on reset and
// power-off so the MCP snapshot doesn't lie after a cold restart.
// Null means "I don't know" — useful signal: AI joining mid-session
// can ask the human instead of guessing from LEDs.
let lastGameStarted = null;
function markGameStarted(n) {
  lastGameStarted = { n, name: GAME_NAMES[n] || ('Game ' + n), startedAt: Date.now() };
}
function clearGameStarted() { lastGameStarted = null; }

async function tap(id) {
  press(id);
  await sleep(TAP_MS);
  release(id);
  await sleep(120); // settle before the next press
}

async function selectGame(n) {
  if (selecting) return;
  if (!(await startConsole())) return;   // also serves as the audio gesture
  selecting = true;
  gamesEl.classList.add('busy');
  try {
    setStatus(`Dealing ${GAME_NAMES[n]} — New Game…`);
    await tap(NEW_GAME);
    await sleep(GAP_MS);
    setStatus(`Dealing ${GAME_NAMES[n]} — pressing ${n}…`);
    await tap(n);
    setStatus(`${GAME_NAMES[n]} — go!`);
    markGameStarted(n);
  } finally {
    selecting = false;
    gamesEl.classList.remove('busy');
  }
}

gamesEl.querySelectorAll('li[data-game]').forEach((li) => {
  const n = parseInt(li.dataset.game, 10);
  li.addEventListener('click', () => selectGame(n));
  li.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectGame(n); }
  });
});

// ---- Console remote-control API ----
// Drive the device from DevTools, e.g.  await merlin.on();  merlin.game(1)
// Note: a console call is not a user gesture — if you power on without
// having clicked the page first, emulation + LEDs run but the
// AudioContext stays suspended (silent) until a real click/key.
window.merlin = {
  on:      startConsole,                 // power on (init or resume)
  off:     powerOff,                     // real power-off (cold reset)
  power:   () => (running ? powerOff() : startConsole()),
  isOn:    () => running,
  reset:   () => window.merlinReset && window.merlinReset(),
  press,                                 // press(id), held until release
  release,                               // release(id)
  tap,                                   // tap(id) — press + release
  game:    selectGame,                   // game(1..6): New Game + number
  speed:   (m) => window.merlinSpeed && window.merlinSpeed(m), // 0.1..4
  ids: {
    pad0: 0, pad1: 1, pad2: 2, pad3: 3, pad4: 4, pad5: 5,
    pad6: 6, pad7: 7, pad8: 8, pad9: 9, pad10: 10,
    newGame: 11, sameGame: 12, hitMe: 13, compTurn: 14,
  },
  // Audio-path diagnostics. Counters tally every frame regardless;
  // setting audioDebug(true) also prints each stale frame to console
  // so you can see the timestamp of every suppression event.
  audioDebug: (on) => { audioDebugLog = !!on; return audioDebugLog; },
  audioStats: () => ({ ...audioStats }),
  audioReset: () => {
    for (const k of Object.keys(audioStats)) audioStats[k] = 0;
  },
  // Volume control: 0.0..1.0, post-mix attenuation.
  //   merlin.volume()    → read current value
  //   merlin.volume(0.5) → set, returns clamped new value
  // The setter uses setTargetAtTime (not direct .value =) so the
  // change schedules a 10 ms ramp instead of a click on the speaker.
  volume: (v) => {
    if (!gain) return null;
    if (v === undefined) return gain.gain.value;
    const clamped = Math.max(0, Math.min(1, Number(v)));
    gain.gain.setTargetAtTime(clamped, audioCtx.currentTime, 0.01);
    return clamped;
  },
};
console.log('merlin console API ready — try: await merlin.on(); merlin.game(1)');

// ---- Volume slider wiring ----
// The slider tracks 0..100 (whole percents) to match what humans
// expect from a "volume" control; we translate to 0..1 amplitude
// for the GainNode. We deliberately initialize the slider's UI to
// DEFAULT_VOLUME so the displayed value matches the GainNode's
// initial value the first time Power On creates the audio chain —
// no "the slider says 60% but the actual gain is something else"
// drift on first interaction.
(function () {
  const slider = document.getElementById('volume');
  const label  = document.getElementById('volume-value');
  if (!slider || !label) return;

  // Sync HTML default to the JS constant so they can't get out of step.
  const initPct = Math.round(DEFAULT_VOLUME * 100);
  slider.value = String(initPct);
  label.textContent = initPct + '%';

  slider.addEventListener('input', () => {
    const pct = parseInt(slider.value, 10);
    label.textContent = pct + '%';
    // window.merlin.volume() handles the case where the GainNode
    // hasn't been created yet (Power not pressed) — returns null and
    // the slider just stores the user's intent until Power On.
    window.merlin.volume(pct / 100);
  });

  // First time Power On creates the audio chain, the slider's value
  // was set before the GainNode existed — push it through so the
  // GainNode starts at the slider position (in case the user moved
  // the slider before pressing Power On).
  const origOn = window.merlin.on;
  window.merlin.on = async function () {
    const ok = await origOn();
    if (ok) window.merlin.volume(parseInt(slider.value, 10) / 100);
    return ok;
  };
})();

// ---- Optional MCP bridge (WebSocket → broker → MCP host) ----
// The page tries to open a WebSocket to <same origin>/ws on boot. If
// nothing is on the other end (typical GitHub Pages deploy, or no
// merlin-mcp running locally) the open fails silently and the page
// just plays solo — no change in behavior. If the broker IS running,
// inbound JSON commands are dispatched to the same window.merlin API
// the human uses, and replies carry the LED state back.
//
// Wire protocol (each frame is one JSON object on the WS):
//   server → browser:  { id, op: "tap"|"read"|"reset"|"game", args }
//   browser → server:  { id, result: { leds: bool[11], ... } }
//                  or  { id, error: "..." }
(function () {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${scheme}//${location.host}/ws`;

  // Tiny visible badge in the corner so the human knows when an MCP
  // client is wired in. Only shown once the WS connects — never
  // appears on GitHub Pages or other no-broker deploys.
  let badge = null;
  function ensureBadge() {
    if (badge) return badge;
    badge = document.createElement('div');
    badge.id = 'mcp-badge';
    badge.style.cssText =
      'position:fixed;right:8px;top:8px;padding:4px 8px;border-radius:6px;' +
      'font:11px ui-monospace,monospace;background:#1a3a1a;color:#9f9;' +
      'border:1px solid #2f5;z-index:9999;opacity:.85';
    badge.textContent = 'MCP connected';
    document.body.appendChild(badge);
    return badge;
  }
  function dropBadge() {
    if (badge) { badge.remove(); badge = null; }
  }

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  // Op handlers — every one returns the current LED snapshot so the
  // MCP client always gets fresh state back without a follow-up read.
  async function snapshot() {
    // leds is the live Uint8Array shared with the wasm pump; copy by
    // value into a plain bool[] so JSON.stringify is stable.
    const arr = [];
    if (leds) {
      for (let i = 0; i < 11; i++) arr.push(leds[i] !== 0);
    } else {
      for (let i = 0; i < 11; i++) arr.push(false);
    }
    // last_game is best-effort: it reflects the most recent game
    // dealt through selectGame() or the MCP `game` op. null means
    // "unknown" (AI joined mid-session, or never started one) — a
    // more honest signal than guessing from LEDs.
    const lg = lastGameStarted
      ? { n: lastGameStarted.n,
          name: lastGameStarted.name,
          ms_ago: Date.now() - lastGameStarted.startedAt }
      : null;
    return { leds: arr, powered: running, last_game: lg };
  }

  async function ensureRunning() {
    if (!running) {
      // No user gesture context here, so AudioContext.resume() will
      // be a no-op (silent), but emulation + LEDs run normally —
      // which is all the MCP client cares about.
      await window.merlin.on();
    }
  }

  async function opTap(args) {
    await ensureRunning();
    const id = args && Number.isInteger(args.id) ? args.id : -1;
    if (id < 0 || id > 14) throw new Error('tap: bad button id ' + id);
    const holdMs = (args && Number(args.hold_ms)) || 170;
    press(id);
    await sleep(holdMs);
    release(id);
    await sleep(150); // ROM settle window
    return await snapshot();
  }

  async function opRead() {
    return await snapshot();
  }

  async function opReset() {
    await ensureRunning();
    if (window.merlinReset) window.merlinReset();
    clearGameStarted();
    await sleep(150);
    return await snapshot();
  }

  async function opGame(args) {
    await ensureRunning();
    const n = args && Number.isInteger(args.n) ? args.n : -1;
    if (n < 1 || n > 6) throw new Error('game: n must be 1..6');
    // Mirror the local selectGame() timing — NEW GAME, settle, then digit.
    await window.merlin.tap(11);     // newgame
    await sleep(950);
    await window.merlin.tap(n);
    await sleep(200);
    markGameStarted(n);
    return await snapshot();
  }

  const OPS = { tap: opTap, read: opRead, reset: opReset, game: opGame };

  // Reconnect loop with capped exponential backoff. Silent if the
  // server is missing — only the console gets a one-time note.
  let backoff = 500;
  let everConnected = false;
  function connect() {
    let ws;
    try { ws = new WebSocket(url); }
    catch (e) { schedule(); return; }

    ws.addEventListener('open', () => {
      backoff = 500;
      everConnected = true;
      console.log('merlin: MCP broker connected at', url);
      ensureBadge();
    });
    ws.addEventListener('message', async (ev) => {
      let msg;
      try { msg = JSON.parse(ev.data); }
      catch { return; }
      const { id, op, args } = msg;
      const handler = OPS[op];
      if (!handler) {
        ws.send(JSON.stringify({ id, error: 'unknown op: ' + op }));
        return;
      }
      try {
        const result = await handler(args || {});
        ws.send(JSON.stringify({ id, result }));
      } catch (e) {
        ws.send(JSON.stringify({ id, error: String(e && e.message || e) }));
      }
    });
    ws.addEventListener('close', () => { dropBadge(); schedule(); });
    ws.addEventListener('error', () => { /* close fires next; let it retry */ });
  }
  function schedule() {
    // Only log the very first failure — after that, retries are silent.
    if (!everConnected && backoff === 500) {
      console.log('merlin: no MCP broker on', url, '(playing solo)');
    }
    setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, 10_000);
  }
  connect();
})();
