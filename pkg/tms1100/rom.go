package tms1100

import "fmt"

// The TMS1100 program counter does not count linearly: it is a 6-bit LFSR
// whose state sequence is fixed in silicon. pcSequence[logical] gives the
// physical low-6-bit ROM address the CPU would have presented on the Nth
// step within a 64-byte page.
//
// We pre-unscramble the ROM at load time so the emulated PC can simply
// increment 0,1,2,... and still fetch the right byte. This mirrors the
// C++ reference (carledwards/merlin-tms1100/cpp/tms1xx0.cpp).
var pcSequence = [64]byte{
	0x00, 0x01, 0x03, 0x07, 0x0F, 0x1F, 0x3F, 0x3E,
	0x3D, 0x3B, 0x37, 0x2F, 0x1E, 0x3C, 0x39, 0x33,
	0x27, 0x0E, 0x1D, 0x3A, 0x35, 0x2B, 0x16, 0x2C,
	0x18, 0x30, 0x21, 0x02, 0x05, 0x0B, 0x17, 0x2E,
	0x1C, 0x38, 0x31, 0x23, 0x06, 0x0D, 0x1B, 0x36,
	0x2D, 0x1A, 0x34, 0x29, 0x12, 0x24, 0x08, 0x11,
	0x22, 0x04, 0x09, 0x13, 0x26, 0x0C, 0x19, 0x32,
	0x25, 0x0A, 0x15, 0x2A, 0x14, 0x28, 0x10, 0x20,
}

// inverseSequence maps a physical low-6-bit address back to its logical
// (linear) position. Built once from pcSequence.
var inverseSequence = func() [64]byte {
	var inv [64]byte
	for logical, physical := range pcSequence {
		inv[physical] = byte(logical)
	}
	return inv
}()

// remapROM unscrambles a raw ROM image so the emulator can increment PC
// linearly. Two transforms, exactly as the C++ reference does at load:
//
//  1. Within every 64-byte page, reorder bytes by pcSequence so logical
//     address N holds the byte the CPU fetches on step N.
//  2. Branch/call operands (opcode bit 7 set) embed a physical target
//     address; rewrite each to its logical equivalent so post-unscramble
//     branches land correctly.
//
// The image must be a whole number of 64-byte pages.
func remapROM(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw)%64 != 0 {
		return nil, fmt.Errorf("tms1100: ROM size %d is not a positive multiple of 64", len(raw))
	}

	out := make([]byte, len(raw))
	for i := range raw {
		// (i &^ 0x3F) is the page base; pcSequence remaps the offset.
		out[i] = raw[(i &^ 0x3F)|int(pcSequence[i&0x3F])]

		// Branch (0x80-0xBF) and call (0xC0-0xFF) carry a 6-bit operand.
		if out[i]&0x80 != 0 {
			newAddr := inverseSequence[out[i]&0x3F]
			out[i] = (out[i] & 0xC0) | newAddr // keep opcode class, remap operand
		}
	}
	return out, nil
}
