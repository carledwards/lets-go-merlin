// Package merlin is the Merlin-specific wrapper around the platform-agnostic
// tms1100 core. It is a pure function of CPU state: it decodes R lines into
// 11 LED pads, synthesizes the K-line response from button state and the
// current O value, and exposes the speaker bit. No timers, no callbacks.
//
// The mapping mirrors the trusted reference front-end
// (carledwards/merlin-tms1100/python/merlin_console.py) and is consistent
// with MAME's hh_tms1k.cpp merlin driver.
package merlin

import "github.com/carledwards/lets-go-merlin/pkg/tms1100"

// Button identifies a physical Merlin input. Pads 0..10 are the 11
// light-up keypad buttons; the rest are the side game buttons.
type Button int

const (
	// Pad0..Pad10: the 11 LED/pushbutton pads. Pad0 is the top single
	// pad, Pad1..Pad9 the 3x3 grid, Pad10 the bottom single pad.
	Pad0 Button = iota
	Pad1
	Pad2
	Pad3
	Pad4
	Pad5
	Pad6
	Pad7
	Pad8
	Pad9
	Pad10

	BtnNewGame
	BtnSameGame
	BtnHitMe
	BtnCompTurn

	numButtons
)

// NumPads is the count of LED/pushbutton pads.
const NumPads = 11

// String gives a stable label for UI and debugging.
func (b Button) String() string {
	switch b {
	case BtnNewGame:
		return "New Game"
	case BtnSameGame:
		return "Same Game"
	case BtnHitMe:
		return "Hit Me"
	case BtnCompTurn:
		return "Comp Turn"
	default:
		if b >= Pad0 && b <= Pad10 {
			return []string{"Pad0", "Pad1", "Pad2", "Pad3", "Pad4", "Pad5",
				"Pad6", "Pad7", "Pad8", "Pad9", "Pad10"}[b]
		}
		return "?"
	}
}

// K-line bit values, matching the reference's k_val constants.
const (
	k1 = 0x1
	k2 = 0x2
	k4 = 0x4
	k8 = 0x8
)

// computeK reproduces cpu_k_input_cb: given the current O value (the
// scan column the ROM selected) and which buttons are pressed, return
// the 4-bit K response. The ROM drives O to exactly 0, 4, 8 or 12 while
// scanning; any other value reads nothing.
func computeK(o byte, pressed *[numButtons]bool) byte {
	var k byte
	switch o {
	case 0: // column O0
		if pressed[Pad0] {
			k |= k1
		}
		if pressed[Pad1] {
			k |= k2
		}
		if pressed[Pad2] {
			k |= k8
		}
		if pressed[Pad3] {
			k |= k4
		}
	case 4: // column O1
		if pressed[Pad4] {
			k |= k1
		}
		if pressed[Pad5] {
			k |= k2
		}
		if pressed[Pad6] {
			k |= k8
		}
		if pressed[Pad7] {
			k |= k4
		}
	case 8: // column O2
		if pressed[Pad8] {
			k |= k1
		}
		if pressed[Pad9] {
			k |= k2
		}
		if pressed[Pad10] {
			k |= k8
		}
		if pressed[BtnSameGame] {
			k |= k4
		}
	case 12: // column O3
		if pressed[BtnCompTurn] {
			k |= k2
		}
		if pressed[BtnNewGame] {
			k |= k8
		}
		if pressed[BtnHitMe] {
			k |= k4
		}
	}
	return k
}

// speakerHigh reports the speaker line state. The reference treats bit 0
// of the (un-PLA'd) O value as the sound bit.
func speakerHigh(c *tms1100.CPU) bool { return c.O()&0x01 != 0 }
