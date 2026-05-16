package merlin

import "github.com/carledwards/lets-go-merlin/pkg/tms1100"

// minHoldScans guarantees a brief tap is still seen by the ROM. The
// hardware reads a held key across many scans for switch debounce; the
// reference faked this with a 32-read hold. We hold a tapped button at
// least this many CPU steps even if released early. It is generous
// (steps, not scans) and harmless: a real hold simply stays down.
const minHoldScans = 4000

// SpeakerObserver receives the speaker line state once per CPU step, in
// execution order. The audio sampler implements this; nil disables it.
type SpeakerObserver interface {
	OnStep(speakerHigh bool)
}

// Machine ties the CPU core to Merlin's matrix, LED decay, and button
// state. Step is the single advance primitive; everything else is pure
// accessors. Not safe for concurrent use.
type Machine struct {
	cpu *tms1100.CPU
	led *ledState

	down [numButtons]bool // raw pressed state (UI mousedown/up)
	hold [numButtons]int  // remaining forced-hold steps after a tap

	eff [numButtons]bool // scratch: effective pressed state (no alloc)

	spk SpeakerObserver
}

// New loads the ROM and returns a Machine in power-on state.
func New(rom []byte) (*Machine, error) {
	cpu, err := tms1100.New(rom)
	if err != nil {
		return nil, err
	}
	return &Machine{cpu: cpu, led: newLEDState()}, nil
}

// CPU exposes the core for debugging/tracing.
func (m *Machine) CPU() *tms1100.CPU { return m.cpu }

// SetSpeakerObserver installs (or clears, with nil) the per-step
// speaker sampler.
func (m *Machine) SetSpeakerObserver(o SpeakerObserver) { m.spk = o }

// Press marks a button held (UI mousedown / keydown). It also arms a
// minimum hold so a fast tap still registers.
func (m *Machine) Press(b Button) {
	if b < 0 || b >= numButtons {
		return
	}
	m.down[b] = true
	m.hold[b] = minHoldScans
}

// Release clears the held state (UI mouseup / keyup). Any armed minimum
// hold continues to expire naturally.
func (m *Machine) Release(b Button) {
	if b < 0 || b >= numButtons {
		return
	}
	m.down[b] = false
}

// Reset returns the CPU to power-on state (the Merlin "reset"/power
// switch; not present on real hardware). LED decay and button state are
// cleared too.
func (m *Machine) Reset() {
	m.cpu.Reset()
	m.led = newLEDState()
	m.down = [numButtons]bool{}
	m.hold = [numButtons]int{}
}

// Step advances exactly one instruction: synthesize K from the current
// scan column and button state, execute, then sample LEDs and speaker.
func (m *Machine) Step() {
	for b := 0; b < int(numButtons); b++ {
		m.eff[b] = m.down[b] || m.hold[b] > 0
		if !m.down[b] && m.hold[b] > 0 {
			m.hold[b]--
		}
	}
	m.cpu.SetK(computeK(m.cpu.O(), &m.eff))
	m.cpu.Step()
	m.led.tick(m.cpu)
	if m.spk != nil {
		m.spk.OnStep(speakerHigh(m.cpu))
	}
}

// RunSteps advances n instructions.
func (m *Machine) RunSteps(n int) {
	for i := 0; i < n; i++ {
		m.Step()
	}
}

// LEDs returns the debounced on/off state of the 11 pads (index 0..10).
func (m *Machine) LEDs() [NumPads]bool { return m.led.lit() }

// SpeakerHigh is the instantaneous speaker line state.
func (m *Machine) SpeakerHigh() bool { return speakerHigh(m.cpu) }
