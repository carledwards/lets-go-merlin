// AudioWorklet processor: a FIFO of Float32 sample chunks pushed from
// the main thread via port.postMessage. No SharedArrayBuffer. The Go
// side feeds samples at real time; ALL the jitter tolerance lives here.
//
//  - Pre-roll: buffer a cushion before starting (and re-arm it after an
//    underrun) so rAF jitter / long frames don't starve playback.
//  - Fade-in: ramp the first samples up from 0 on every (re)start, so
//    the resting DC level doesn't begin with a click — kills the
//    startup pop and post-underrun ticks.
//  - Underrun: hold + decay the last sample toward 0 instead of
//    slamming to 0 (a hard step = a pop).
const PREROLL = 3072;   // ~64 ms @ 48 kHz cushion before (re)starting
const FADE = 256;       // ~5 ms linear fade-in on (re)start
const DECAY = 0.9994;   // per-sample decay of the held value on underrun

class MerlinProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.queue = [];      // array of Float32Array chunks
    this.head = 0;        // read offset into queue[0]
    this.buffered = 0;    // total queued samples
    this.armed = false;   // true once pre-roll is satisfied
    this.fade = 0;        // samples remaining in the fade-in ramp
    this.last = 0;        // last emitted sample (underrun hold)
    this.port.onmessage = (e) => {
      if (e.data && e.data.flush) {     // power-off / reset
        this.queue = [];
        this.head = 0;
        this.buffered = 0;
        this.armed = false;
        this.fade = 0;
        this.last = 0;
        return;
      }
      this.queue.push(e.data);
      this.buffered += e.data.length;
    };
  }

  process(_inputs, outputs) {
    const out = outputs[0];
    const ch = out[0];
    const frames = ch.length;

    // Hold for the cushion before (re)starting; emit decaying silence.
    if (!this.armed) {
      if (this.buffered >= PREROLL) {
        this.armed = true;
        this.fade = FADE;             // ramp back in smoothly
      } else {
        for (let i = 0; i < frames; i++) ch[i] = (this.last *= DECAY);
        for (let c = 1; c < out.length; c++) out[c].set(ch);
        return true;
      }
    }

    for (let i = 0; i < frames; i++) {
      if (this.buffered === 0) {       // underrun: hold + decay, re-arm
        ch[i] = (this.last *= DECAY);
        this.armed = false;
        continue;
      }
      const chunk = this.queue[0];
      let s = chunk[this.head++];
      this.buffered--;
      if (this.head >= chunk.length) {
        this.queue.shift();
        this.head = 0;
      }
      if (this.fade > 0) {             // linear 0->1 fade-in
        s *= (FADE - this.fade) / FADE;
        this.fade--;
      }
      this.last = s;
      ch[i] = s;
    }
    for (let c = 1; c < out.length; c++) out[c].set(ch);
    return true; // keep processor alive
  }
}

registerProcessor('merlin-processor', MerlinProcessor);
