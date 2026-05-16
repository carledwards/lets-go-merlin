package tms1100

// Opcode implementations, ported one-to-one from the C++ reference's
// op_* methods. Signature mirrors the reference dispatch: the opcode
// byte (for embedded-constant lookup) and lastS (the previous
// instruction's status, consumed only by branch/call).

type opFn func(c *CPU, opcode byte, lastS bool)

// opTable / opConst are the C++ op_code_func_[256] / op_code_constant_[256]
// dispatch tables, built once at package init so there are zero
// allocations during execution.
var (
	opTable [256]opFn
	opConst [256]byte
)

// --- register helpers (mirror CPUState setters and their masking) ---

func (c *CPU) setA(v byte)  { c.a = v & 0x0F }
func (c *CPU) setX(v byte)  { c.x = v & 0x07 }
func (c *CPU) setY(v byte)  { c.y = v & 0x0F }
func (c *CPU) setPA(v byte) { c.pa = v & 0x0F }
func (c *CPU) setPB(v byte) { c.pb = v & 0x0F }
func (c *CPU) setPC(v byte) { c.pc = v & 0x3F }
func (c *CPU) setCA(v byte) { c.ca = v & 0x01 }
func (c *CPU) setCB(v byte) { c.cb = v & 0x01 }
func (c *CPU) setCS(v byte) { c.cs = v & 0x01 }
func (c *CPU) incY()        { c.setY(c.y + 1) }
func (c *CPU) decY()        { c.setY(c.y - 1) }
func (c *CPU) comX()        { c.x = (c.x ^ 4) & 0x07 }
func (c *CPU) comCB()       { c.cb = (^c.cb) & 0x01 }
func (c *CPU) curRAM() *byte   { return &c.ram[c.curRAMAddr()] }

// uADCa: A = A + val (4-bit); status set on carry out of bit 3.
func (c *CPU) uADCa(val byte) {
	sum := int(c.a) + int(val)
	c.s = sum > 0x0F
	c.setA(byte(sum))
}

// uADCy: same, on Y.
func (c *CPU) uADCy(val byte) {
	sum := int(c.y) + int(val)
	c.s = sum > 0x0F
	c.setY(byte(sum))
}

// not4 is NOT4(X): one's complement within 4 bits.
func not4(x byte) byte { return (^x) & 0x0F }

// --- register to register ---

func opTay(c *CPU, _ byte, _ bool) { c.setY(c.a) }
func opTya(c *CPU, _ byte, _ bool) { c.setA(c.y) }
func opCla(c *CPU, _ byte, _ bool) { c.setA(0) }

// --- transfer register to memory ---

func opTam(c *CPU, _ byte, _ bool) { *c.curRAM() = c.a }

func opTamiyc(c *CPU, _ byte, _ bool) {
	*c.curRAM() = c.a
	c.s = c.y == 0x0F
	c.incY()
}

func opTamdyn(c *CPU, _ byte, _ bool) {
	*c.curRAM() = c.a
	c.s = c.y >= 1
	c.decY()
}

func opTamza(c *CPU, _ byte, _ bool) {
	*c.curRAM() = c.a
	c.setA(0)
}

// --- memory to register ---

func opTmy(c *CPU, _ byte, _ bool) { c.setY(*c.curRAM()) }
func opTma(c *CPU, _ byte, _ bool) { c.setA(*c.curRAM()) }

func opXma(c *CPU, _ byte, _ bool) {
	r := c.curRAM()
	tmp := *r
	*r = c.a
	c.setA(tmp)
}

// --- arithmetic ---

func opAmaac(c *CPU, _ byte, _ bool) { c.uADCa(*c.curRAM()) }

func opSaman(c *CPU, _ byte, _ bool) {
	sum := int(not4(c.a)) + int(*c.curRAM()) + 1
	c.s = sum > 0x0F
	c.setA(byte(sum))
}

func opImac(c *CPU, _ byte, _ bool) {
	c.setA(*c.curRAM())
	c.uADCa(0x01)
}

func opDman(c *CPU, _ byte, _ bool) {
	c.setA(*c.curRAM())
	c.uADCa(0x0F)
}

func opAAac(c *CPU, opcode byte, _ bool) { c.uADCa(opConst[opcode]) }

func opIyc(c *CPU, _ byte, _ bool) { c.uADCy(0x01) }
func opDyn(c *CPU, _ byte, _ bool) { c.uADCy(0x0F) }

func opCpaiz(c *CPU, _ byte, _ bool) {
	sum := int(not4(c.a)) + 1
	c.s = sum > 0x0F
	c.setA(byte(sum))
}

// --- arithmetic compare ---

func opAlem(c *CPU, _ byte, _ bool) {
	sum := int(not4(c.a)) + int(*c.curRAM()) + 1
	c.s = sum > 0x0F
}

// --- logical compare ---

func opMnea(c *CPU, _ byte, _ bool) { c.s = *c.curRAM() != c.a }
func opMnez(c *CPU, _ byte, _ bool) { c.s = *c.curRAM() != 0 }

func opYnea(c *CPU, _ byte, _ bool) {
	c.s = c.a != c.y
	c.sl = c.s
}

func opLdp(c *CPU, opcode byte, _ bool)  { c.setPB(opConst[opcode]) }
func opTcy(c *CPU, opcode byte, _ bool)  { c.setY(opConst[opcode]) }
func opYnec(c *CPU, opcode byte, _ bool) { c.s = c.y != opConst[opcode] }

func opTcmiy(c *CPU, opcode byte, _ bool) {
	*c.curRAM() = opConst[opcode]
	c.incY()
}

// --- bits in memory ---

func opComx(c *CPU, _ byte, _ bool) { c.comX() }
func opComc(c *CPU, _ byte, _ bool) { c.comCB() }

func opSbit(c *CPU, opcode byte, _ bool) { *c.curRAM() |= 1 << opConst[opcode] }

func opRbit(c *CPU, opcode byte, _ bool) {
	*c.curRAM() &= (^(1 << opConst[opcode])) & 0x0F
}

func opTbit1(c *CPU, opcode byte, _ bool) {
	c.s = *c.curRAM()&(1<<opConst[opcode]) != 0
}

// --- input (poll model: K is set by host before Step) ---

func opKnez(c *CPU, _ byte, _ bool) { c.s = c.k != 0 }
func opTka(c *CPU, _ byte, _ bool)  { c.setA(c.k) }

// --- output ---

func opSetr(c *CPU, _ byte, _ bool) {
	if c.x <= 3 && c.y <= 10 {
		c.r[c.y] = true
	}
}

func opRstr(c *CPU, _ byte, _ bool) {
	if c.x <= 3 && c.y <= 10 {
		c.r[c.y] = false
	}
}

func opTdo(c *CPU, _ byte, _ bool) {
	// SL is the MSB (inverted relative to the fuse map per the reference).
	o := c.a
	if c.sl {
		o |= 0x10
	}
	c.o = o
}

func opLdx(c *CPU, opcode byte, _ bool) { c.setX(opConst[opcode]) }

// --- ROM addressing ---

func opBr(c *CPU, opcode byte, lastS bool) {
	if !lastS {
		return
	}
	c.setCA(c.cb)
	c.setPC(opcode & 0x3F)
	if !c.cl {
		c.setPA(c.pb)
	}
}

func opCall(c *CPU, opcode byte, lastS bool) {
	if !lastS {
		return
	}
	if c.cl {
		c.setPB(c.pa)
	} else {
		c.setCS(c.ca)
		c.sr = c.pc
		c.pa, c.pb = c.pb, c.pa // PB <=> PA
		c.cl = true
	}
	c.setCA(c.cb)
	c.setPC(opcode & 0x3F)
}

func opRetn(c *CPU, _ byte, _ bool) {
	c.setPA(c.pb)
	if c.cl {
		c.setCA(c.cs)
		c.setPC(c.sr)
		c.cl = false
	}
}

// setupOpcodes wires the dispatch tables exactly as the C++
// setup_op_codes() does, including the parameterized constant groups.
func setupOpcodes() {
	opTable[0x20] = opTay
	opTable[0x23] = opTya
	opTable[0x7F] = opCla

	opTable[0x27] = opTam
	opTable[0x25] = opTamiyc
	opTable[0x24] = opTamdyn
	opTable[0x26] = opTamza

	opTable[0x22] = opTmy
	opTable[0x21] = opTma
	opTable[0x03] = opXma

	opTable[0x06] = opAmaac
	opTable[0x3C] = opSaman
	opTable[0x3E] = opImac
	opTable[0x07] = opDman

	aAac := []byte{1, 9, 5, 13, 3, 11, 7, 15, 2, 10, 6, 14, 4, 12, 8}
	for i, k := range aAac {
		op := byte(0x70 + i)
		opTable[op] = opAAac
		opConst[op] = k
	}

	opTable[0x05] = opIyc
	opTable[0x04] = opDyn
	opTable[0x3D] = opCpaiz

	opTable[0x01] = opAlem

	opTable[0x00] = opMnea
	opTable[0x3F] = opMnez
	opTable[0x02] = opYnea

	c1 := []byte{0, 8, 4, 12, 2, 10, 6, 14, 1, 9, 5, 13, 3, 11, 7, 15}
	for i, k := range c1 {
		opTable[0x10+i], opConst[0x10+i] = opLdp, k
		opTable[0x40+i], opConst[0x40+i] = opTcy, k
		opTable[0x50+i], opConst[0x50+i] = opYnec, k
		opTable[0x60+i], opConst[0x60+i] = opTcmiy, k
	}

	opTable[0x09] = opComx
	opTable[0x0B] = opComc

	c2 := []byte{0, 2, 1, 3}
	for i, k := range c2 {
		opTable[0x30+i], opConst[0x30+i] = opSbit, k
		opTable[0x34+i], opConst[0x34+i] = opRbit, k
		opTable[0x38+i], opConst[0x38+i] = opTbit1, k
	}

	opTable[0x0E] = opKnez
	opTable[0x08] = opTka

	opTable[0x0D] = opSetr
	opTable[0x0C] = opRstr
	opTable[0x0A] = opTdo

	c3 := []byte{0, 4, 2, 6, 1, 5, 3, 7}
	for i, k := range c3 {
		op := byte(0x28 + i)
		opTable[op] = opLdx
		opConst[op] = k
	}

	for i := 0; i < 0x40; i++ {
		opTable[0x80+i] = opBr
		opTable[0xC0+i] = opCall
	}

	opTable[0x0F] = opRetn
}

func init() { setupOpcodes() }
