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
let audioCtx = null, node = null;
let started = false;                   // one-time audio/init done
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
  node.connect(audioCtx.destination);

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

function frame(now) {
  if (!running) return;
  const n = window.merlinPump(now);
  if (n > 0) node.port.postMessage(audioF32.slice(0, n)); // copy; buffer reused
  for (let i = 0; i < 11; i++) ledEls[i].classList.toggle('on', leds[i] !== 0);
  requestAnimationFrame(frame);
}

// ---- Input wiring ----
function press(id)   { if (window.merlinPress) window.merlinPress(id); }
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
  if (k === 'r') { if (window.merlinReset) window.merlinReset(); return; }
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
};
console.log('merlin console API ready — try: await merlin.on(); merlin.game(1)');
