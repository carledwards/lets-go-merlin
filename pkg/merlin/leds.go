package merlin

import "github.com/carledwards/lets-go-merlin/pkg/tms1100"

// ledState smooths the LED matrix scan. The ROM lights pads via the
// latched R lines while scanning hundreds of times per second; mirroring
// raw R changes to a UI flickers. Each pad keeps a "freshness" counter
// reloaded whenever its R line is seen high and decremented otherwise,
// so a pad reads lit for decayTicks steps after its last refresh.
type ledState struct {
	freshness [NumPads]int
	decay     int
}

// newLEDState picks a decay window roughly one matrix-scan cycle wide.
// At ~58 kHz instruction rate a full scan is well under a millisecond;
// ~4000 instructions (~70 us headroom x several scans) keeps pads
// visually solid without smearing distinct animation frames.
func newLEDState() *ledState { return &ledState{decay: 4000} }

// tick samples the R lines once (call exactly once per CPU step).
func (l *ledState) tick(c *tms1100.CPU) {
	for i := 0; i < NumPads; i++ {
		if c.R(i) {
			l.freshness[i] = l.decay
		} else if l.freshness[i] > 0 {
			l.freshness[i]--
		}
	}
}

// lit returns the debounced on/off state of all 11 pads.
func (l *ledState) lit() [NumPads]bool {
	var out [NumPads]bool
	for i := range out {
		out[i] = l.freshness[i] > 0
	}
	return out
}
