// Package audio resamples the emulator's 1-bit speaker line to PCM
// audio-rate samples using fractional accumulation. It is the host's
// SpeakerObserver: OnStep is called once per CPU instruction.
package audio

// Sampler converts per-instruction speaker bits into Float32 samples at
// the audio device rate. The ring buffer is pre-allocated; OnStep does
// no allocation. The consumer (AudioWorklet glue) drains via Read.
type Sampler struct {
	step float64 // audio samples produced per CPU instruction
	acc  float64

	buf        []float32
	r, w       int
	count      int
	lastDrop   int  // diagnostics: samples dropped on overrun
	highLevel  float32
	lowLevel   float32
}

// New creates a Sampler. instrHz is the emulated instruction rate
// (~58333 for a 350 kHz / 6-cycle TMS1100), sampleHz the audio context
// rate (typically 44100 or 48000). bufSamples sizes the ring buffer;
// a few audio frames' worth is plenty.
func New(instrHz, sampleHz float64, bufSamples int) *Sampler {
	if bufSamples < 2 {
		bufSamples = 2
	}
	return &Sampler{
		step:      sampleHz / instrHz,
		buf:       make([]float32, bufSamples),
		highLevel: 0.3,
		lowLevel:  -0.3,
	}
}

// OnStep records one instruction's speaker state, emitting zero or more
// audio samples as the fractional accumulator crosses 1.0.
func (s *Sampler) OnStep(speakerHigh bool) {
	sample := s.lowLevel
	if speakerHigh {
		sample = s.highLevel
	}
	for s.acc += s.step; s.acc >= 1.0; s.acc -= 1.0 {
		if s.count == len(s.buf) {
			// Overrun: consumer is behind. Drop oldest to stay live.
			s.r = (s.r + 1) % len(s.buf)
			s.count--
			s.lastDrop++
		}
		s.buf[s.w] = sample
		s.w = (s.w + 1) % len(s.buf)
		s.count++
	}
}

// Read drains up to len(dst) samples into dst, returning the number
// written. Missing samples (underrun) are left as written by the caller
// (the worklet should zero-fill the remainder for silence).
func (s *Sampler) Read(dst []float32) int {
	n := 0
	for n < len(dst) && s.count > 0 {
		dst[n] = s.buf[s.r]
		s.r = (s.r + 1) % len(s.buf)
		s.count--
		n++
	}
	return n
}

// Available reports buffered sample count (for diagnostics/pacing).
func (s *Sampler) Available() int { return s.count }

// Reset discards all buffered samples and the fractional accumulator,
// so a power-cycle/reset starts from silence with no stale blip.
func (s *Sampler) Reset() {
	s.acc = 0
	s.r, s.w, s.count, s.lastDrop = 0, 0, 0, 0
}
