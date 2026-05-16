//go:build js && wasm

// merlinweb is the browser front-end: vanilla Go compiled to WASM. It
// owns the Machine, runs it in real-time chunks driven by the page's
// requestAnimationFrame loop, exposes button/reset controls to JS, and
// streams resampled speaker audio to an AudioWorklet (no Web Worker, no
// SharedArrayBuffer).
//
// JS contract (all on window):
//
//	merlinInit(sampleRate)        -> sets up audio at the context rate
//	merlinReset()
//	merlinPress(buttonId)         buttonId 0..10 = pads, 11..14 = game btns
//	merlinRelease(buttonId)
//	merlinPump(nowMillis)         -> samples produced this call; also
//	                                 fills merlinLeds (Uint8Array[11]) and
//	                                 merlinAudio (Float32Array) in place
//	merlinSpeed(mult)             clock tuning (1.0 = ~stock 350 kHz)
package main

import (
	"math"
	"syscall/js"

	"github.com/carledwards/lets-go-merlin/pkg/audio"
	"github.com/carledwards/lets-go-merlin/pkg/merlin"
	"github.com/carledwards/lets-go-merlin/roms"
)

// instrHz is the stock TMS1100 instruction rate: 350 kHz RC oscillator,
// 6 clocks per instruction.
const instrHz = 350000.0 / 6.0

// maxSamplesPerPump caps one frame's audio so the JS-side typed arrays
// can be fixed size (well above a 100 ms @ 48 kHz worst case).
const maxSamplesPerPump = 8192

type app struct {
	m        *merlin.Machine
	sampler  *audio.Sampler
	speed    float64 // clock multiplier (tuning knob)
	sampleHz float64 // audio context rate (set in init)

	lastMs   float64
	instrRem float64 // fractional instruction carry (no truncation drift)
	started  bool

	// Persistent JS-side buffers (created once in merlinInit).
	jsLeds  js.Value // Uint8Array(11)
	jsAudioRaw js.Value // Uint8Array(maxSamplesPerPump*4) — Float32 view in JS

	// Reusable Go scratch (no per-frame allocation).
	ledBytes   [merlin.NumPads]byte
	floatBuf   []float32
	audioBytes []byte
}

func main() {
	m, err := merlin.New(roms.MP3404)
	if err != nil {
		panic(err)
	}
	a := &app{m: m, speed: 1.0,
		floatBuf:   make([]float32, maxSamplesPerPump),
		audioBytes: make([]byte, maxSamplesPerPump*4),
	}

	g := js.Global()
	g.Set("merlinInit", js.FuncOf(a.init))
	g.Set("merlinReset", js.FuncOf(a.reset))
	g.Set("merlinPress", js.FuncOf(a.press))
	g.Set("merlinRelease", js.FuncOf(a.release))
	g.Set("merlinPump", js.FuncOf(a.pump))
	g.Set("merlinSpeed", js.FuncOf(a.setSpeed))
	g.Get("console").Call("log", "merlin wasm ready")

	select {} // keep the Go runtime alive
}

// init(sampleRate, ledsUint8Array, audioRawUint8Array)
func (a *app) init(_ js.Value, args []js.Value) any {
	sampleHz := args[0].Float()
	a.jsLeds = args[1]
	a.jsAudioRaw = args[2]
	a.sampleHz = sampleHz
	// Ring buffer ~0.25 s of audio; comfortably absorbs rAF jitter.
	a.sampler = audio.New(instrHz, sampleHz, int(sampleHz/4))
	a.m.SetSpeakerObserver(a.sampler)
	return nil
}

func (a *app) reset(js.Value, []js.Value) any {
	a.m.Reset()
	a.m.SetSpeakerObserver(a.sampler)
	if a.sampler != nil {
		a.sampler.Reset() // drop stale audio so the cold start is clean
	}
	a.started = false
	a.instrRem = 0
	return nil
}

func (a *app) press(_ js.Value, args []js.Value) any {
	a.m.Press(merlin.Button(args[0].Int()))
	return nil
}

func (a *app) release(_ js.Value, args []js.Value) any {
	a.m.Release(merlin.Button(args[0].Int()))
	return nil
}

func (a *app) setSpeed(_ js.Value, args []js.Value) any {
	if s := args[0].Float(); s > 0.1 && s < 4.0 {
		a.speed = s
	}
	return nil
}

// pump(nowMillis) runs the emulator for the wall-clock time elapsed
// since the last call, then drains LEDs and audio into the shared JS
// buffers. Returns the number of audio samples written.
func (a *app) pump(_ js.Value, args []js.Value) any {
	now := args[0].Float()
	if !a.started {
		a.started, a.lastMs = true, now
		return 0
	}
	dt := now - a.lastMs
	a.lastMs = now
	if dt < 0 {
		dt = 0
	}
	if dt > 100 { // clamp: tab was backgrounded; don't spiral
		dt = 100
	}

	// Real-time pacing: run exactly the instructions for the elapsed
	// wall-clock. The audio jitter cushion lives in the AudioWorklet
	// (pre-roll), not here — Go can't see the worklet's queue depth,
	// and steering on the local sampler (drained every frame) makes
	// the emulator race. Carry the fractional instruction remainder
	// so truncation doesn't slowly lose time / drift pitch.
	want := dt/1000.0*instrHz*a.speed + a.instrRem
	steps := int(want)
	a.instrRem = want - float64(steps)
	a.m.RunSteps(steps)

	// LEDs -> Uint8Array(11)
	leds := a.m.LEDs()
	for i, on := range leds {
		if on {
			a.ledBytes[i] = 1
		} else {
			a.ledBytes[i] = 0
		}
	}
	js.CopyBytesToJS(a.jsLeds, a.ledBytes[:])

	// Audio: drain resampler -> Float32 LE bytes -> JS Uint8Array
	n := a.sampler.Read(a.floatBuf)
	for i := 0; i < n; i++ {
		bits := math.Float32bits(a.floatBuf[i])
		o := i * 4
		a.audioBytes[o] = byte(bits)
		a.audioBytes[o+1] = byte(bits >> 8)
		a.audioBytes[o+2] = byte(bits >> 16)
		a.audioBytes[o+3] = byte(bits >> 24)
	}
	js.CopyBytesToJS(a.jsAudioRaw, a.audioBytes[:n*4])
	return n
}
