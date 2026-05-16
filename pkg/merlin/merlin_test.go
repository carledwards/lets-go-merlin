package merlin

import (
	"testing"

	"github.com/carledwards/lets-go-merlin/pkg/audio"
	"github.com/carledwards/lets-go-merlin/roms"
)

func TestComputeKColumns(t *testing.T) {
	cases := []struct {
		o    byte
		btn  Button
		want byte
	}{
		{0, Pad0, k1}, {0, Pad1, k2}, {0, Pad2, k8}, {0, Pad3, k4},
		{4, Pad4, k1}, {4, Pad5, k2}, {4, Pad6, k8}, {4, Pad7, k4},
		{8, Pad8, k1}, {8, Pad9, k2}, {8, Pad10, k8}, {8, BtnSameGame, k4},
		{12, BtnCompTurn, k2}, {12, BtnNewGame, k8}, {12, BtnHitMe, k4},
		// Wrong column -> no response.
		{4, Pad0, 0}, {0, BtnNewGame, 0}, {7, Pad1, 0},
	}
	for _, c := range cases {
		var p [numButtons]bool
		p[c.btn] = true
		if got := computeK(c.o, &p); got != c.want {
			t.Errorf("computeK(O=%d, %s) = %#x, want %#x", c.o, c.btn, got, c.want)
		}
	}

	// Two buttons in the same column OR together.
	var p [numButtons]bool
	p[Pad0], p[Pad3] = true, true
	if got := computeK(0, &p); got != k1|k4 {
		t.Errorf("Pad0+Pad3 @O0 = %#x, want %#x", got, k1|k4)
	}
}

// spyObserver records whether the speaker line ever changes.
type spyObserver struct {
	first   bool
	toggled bool
	prev    bool
}

func (s *spyObserver) OnStep(high bool) {
	if !s.first {
		s.first, s.prev = true, high
		return
	}
	if high != s.prev {
		s.toggled = true
	}
	s.prev = high
}

// TestStartupBehavior is a smoke test: a fresh Merlin runs its power-on
// light show (LEDs light) and produces speaker activity.
func TestStartupBehavior(t *testing.T) {
	m, err := New(roms.MP3404)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	spy := &spyObserver{}
	m.SetSpeakerObserver(spy)

	anyLit := false
	for i := 0; i < 400_000; i++ {
		m.Step()
		for _, on := range m.LEDs() {
			if on {
				anyLit = true
			}
		}
	}
	if !anyLit {
		t.Error("no LED ever lit during 400k-step startup")
	}
	if !spy.toggled {
		t.Error("speaker line never toggled during 400k-step startup")
	}
}

func TestResetRestoresPowerOn(t *testing.T) {
	m, err := New(roms.MP3404)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.RunSteps(50_000)
	m.Reset()
	c := m.CPU()
	if c.PA() != 0x0F || c.PC() != 0x00 || c.CA() != 0 {
		t.Fatalf("post-Reset fetch regs: PA=%x PC=%x CA=%x", c.PA(), c.PC(), c.CA())
	}
	for i, on := range m.LEDs() {
		if on {
			t.Fatalf("LED %d lit immediately after Reset", i)
		}
	}
}

// TestSamplerProducesAudioRate checks the resampler emits roughly
// sampleHz/instrHz samples per instruction and survives overrun.
func TestSamplerProducesAudioRate(t *testing.T) {
	const instrHz, sampleHz = 58333.0, 48000.0
	s := audio.New(instrHz, sampleHz, 256)
	steps := 100_000
	got := 0
	tmp := make([]float32, 512)
	for i := 0; i < steps; i++ {
		s.OnStep(i%2 == 0) // square wave
		if i%64 == 0 {
			got += s.Read(tmp)
		}
	}
	got += s.Read(tmp)
	want := int(float64(steps) * sampleHz / instrHz)
	if d := got - want; d < -2000 || d > want { // generous: overrun may drop
		t.Fatalf("produced %d samples for %d steps, expected ~%d", got, steps, want)
	}
}
