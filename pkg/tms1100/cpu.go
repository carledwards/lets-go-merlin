// Package tms1100 is a pure, platform-agnostic emulator of the Texas
// Instruments TMS1100 4-bit microcontroller as used by the Parker
// Brothers Merlin (1978), MP3404 mask-ROM variant.
//
// It is a literal port of the C++ reference at
// carledwards/merlin-tms1100/cpp/tms1xx0.cpp. No I/O, no timing, no
// goroutines, and no allocations after New. The host polls state after
// each Step and sets K lines before each Step (no callbacks).
package tms1100

// rWidth matches R_WIDTH in the C++ reference. The TMS1100 exposes 11 R
// lines; the reference allocates 15 and bounds-checks SETR/RSTR against
// the wider array, so we keep the same width for faithfulness. The
// Merlin wrapper only ever reads R0..R10.
const rWidth = 15

// CPU is the full TMS1100: registers, RAM, and the pre-unscrambled ROM.
// The C++ reference splits this across ROM/CPUState/TMS1100 classes; one
// struct is simpler in Go and behaves identically.
type CPU struct {
	// 4-bit unless noted.
	a  byte // accumulator
	x  byte // 3-bit RAM page select
	y  byte // RAM/output index
	pc byte // 6-bit program counter
	pa byte // current ROM page
	pb byte // branch/call target page
	sr byte // 6-bit return address (subroutine)
	ca byte // 1-bit current chapter
	cb byte // 1-bit chapter buffer
	cs byte // 1-bit chapter save
	s  bool // status (set by ALU ops, consumed by branch/call)
	sl bool // status latch (drives O bit 4 via TDO)
	cl bool // call latch (one-deep subroutine nesting)

	k byte // 4-bit K input lines (set by host before Step)
	o byte // O output value (host polls after Step)

	r [rWidth]bool // R output lines

	rom []byte    // pre-unscrambled, len == 2048 for Merlin
	ram [128]byte // 8 pages x 16 nibbles
}

// New loads and unscrambles a ROM image and returns a CPU in its
// power-on state. The Merlin ROM is exactly 2048 bytes.
func New(rom []byte) (*CPU, error) {
	remapped, err := remapROM(rom)
	if err != nil {
		return nil, err
	}
	c := &CPU{rom: remapped}
	c.Reset()
	return c, nil
}

// Reset restores the documented power-on register state. ROM is
// untouched. RAM is set to 0x0A in every nibble, matching the C++
// reference's SET4(0xAA) fill (uninitialized SRAM is undefined on real
// hardware; the ROM clears what it needs).
func (c *CPU) Reset() {
	c.a = 0xAA & 0x0F  // 0x0A
	c.x = 0xAA & 0x07  // 0x02
	c.y = 0xAA & 0x0F  // 0x0A
	c.pc = 0x00
	c.pa = 0xFF & 0x0F // 0x0F
	c.pb = 0xFF & 0x0F // 0x0F
	c.sr = 0x00
	c.ca, c.cb, c.cs = 0, 0, 0
	c.s, c.sl, c.cl = false, false, false
	c.k = 0x00
	c.o = 0x00
	for i := range c.r {
		c.r[i] = false
	}
	for i := range c.ram {
		c.ram[i] = 0xAA & 0x0F // 0x0A
	}
}

// curRAMAddr is CURR_RAM's index: (x << 4) | y. x is 3-bit, y is 4-bit,
// so the result is always within 0..127.
func (c *CPU) curRAMAddr() int { return int(c.x)<<4 | int(c.y) }

// romAddr is the physical fetch address: (CA << 10) | (PA << 6) | PC.
func (c *CPU) romAddr() int { return int(c.ca)<<10 | int(c.pa)<<6 | int(c.pc) }

// Step fetches one instruction, advances PC, and executes it. Exactly
// one instruction == 6 clock cycles on real silicon, with no exceptions,
// so instruction accuracy is full timing accuracy.
func (c *CPU) Step() {
	opcode := c.rom[c.romAddr()]
	c.pc = (c.pc + 1) & 0x3F // increment_pc: 6-bit wrap
	c.exec(opcode)
}

// exec implements the "last status" pattern: S is the result of the
// *previous* instruction. Capture it, force S true for this one (ALU ops
// may clear it), then dispatch. Branch/call consume the captured value.
func (c *CPU) exec(opcode byte) {
	lastS := c.s
	c.s = true
	if fn := opTable[opcode]; fn != nil {
		fn(c, opcode, lastS)
	}
}

// SetK sets the 4-bit K input lines. The host (Merlin wrapper) computes
// these from button state and the current O value, then calls SetK
// immediately before Step. This replaces the C++ input callback.
func (c *CPU) SetK(k byte) { c.k = k & 0x0F }

// --- State accessors (host polling + debug tracing) ---

// R reports R output line i (i in 0..rWidth-1); out-of-range is false.
func (c *CPU) R(i int) bool {
	if i < 0 || i >= rWidth {
		return false
	}
	return c.r[i]
}

// O is the current O output value (up to 5 bits: A | SL<<4 via TDO).
func (c *CPU) O() byte { return c.o }

// Register/state accessors, primarily for the CLI trace and tests.
func (c *CPU) A() byte  { return c.a }
func (c *CPU) X() byte  { return c.x }
func (c *CPU) Y() byte  { return c.y }
func (c *CPU) PC() byte { return c.pc }
func (c *CPU) PA() byte { return c.pa }
func (c *CPU) PB() byte { return c.pb }
func (c *CPU) S() bool  { return c.s }
func (c *CPU) SL() bool { return c.sl }
func (c *CPU) CL() bool { return c.cl }
func (c *CPU) CA() byte { return c.ca }
func (c *CPU) CB() byte { return c.cb }
func (c *CPU) K() byte  { return c.k }

// CurrentOpcode returns the byte at the current fetch address without
// advancing (for pre-step trace lines that mirror the C++ reference).
func (c *CPU) CurrentOpcode() byte { return c.rom[c.romAddr()] }

// CurrentRAM returns the nibble CURR_RAM currently points at.
func (c *CPU) CurrentRAM() byte { return c.ram[c.curRAMAddr()] }
