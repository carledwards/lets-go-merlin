package tms1100

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	_ "embed"
	"fmt"
	"strings"
	"testing"

	"github.com/carledwards/lets-go-merlin/roms"
)

// trace_5000.golden is the first 5000 instruction-trace lines, captured
// from the C++ reference (carledwards/merlin-tms1100) and verified
// identical to this port across 200,000 instructions. It is the spec.
//
//go:embed testdata/trace_5000.golden
var goldenTrace string

func newCPU(t *testing.T) *CPU {
	t.Helper()
	c, err := New(roms.MP3404)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// traceLine reproduces the C++ reference's pre-step debug printf.
func traceLine(c *CPU) string {
	b := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	return fmt.Sprintf("%1x:%02x %02x x:%02x y:%02x a:%02x s:%1x ram:%02x cl:%02x ca:%02x cb:%02x",
		c.PA(), c.PC(), c.CurrentOpcode(), c.X(), c.Y(), c.A(),
		b(c.S()), c.CurrentRAM(), b(c.CL()), c.CA(), c.CB())
}

// TestROMChecksum guards against a corrupted or wrong ROM image; the
// rest of the suite is meaningless if these bytes are wrong.
func TestROMChecksum(t *testing.T) {
	const want = "76ca3605d3fde1df62f79b9bb1f534c2a2ae0229"
	sum := sha1.Sum(roms.MP3404)
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("ROM SHA1 = %s, want %s", got, want)
	}
	if len(roms.MP3404) != 2048 {
		t.Fatalf("ROM length = %d, want 2048", len(roms.MP3404))
	}
}

// TestPowerOnState pins the documented reset state and the first fetch.
func TestPowerOnState(t *testing.T) {
	c := newCPU(t)
	if c.PA() != 0x0F || c.PC() != 0x00 || c.CA() != 0 {
		t.Fatalf("fetch addr regs: PA=%x PC=%x CA=%x, want F 0 0", c.PA(), c.PC(), c.CA())
	}
	// First opcode after the PC-sequence unscramble is TCY-page setup
	// (0x2f); the golden trace's first line confirms the full state.
	if got := c.CurrentOpcode(); got != 0x2f {
		t.Fatalf("first opcode = %02x, want 2f", got)
	}
	if got, want := traceLine(c), "f:00 2f x:02 y:0a a:0a s:0 ram:0a cl:00 ca:00 cb:00"; got != want {
		t.Fatalf("power-on trace:\n got %q\nwant %q", got, want)
	}
}

// TestGoldenTrace is the definitive port-correctness test: execute and
// compare every instruction's pre-step state against the C++ reference.
func TestGoldenTrace(t *testing.T) {
	c := newCPU(t)
	sc := bufio.NewScanner(strings.NewReader(goldenTrace))
	for line := 1; sc.Scan(); line++ {
		want := sc.Text()
		if got := traceLine(c); got != want {
			t.Fatalf("instruction %d mismatch:\n got %q\nwant %q", line, got, want)
		}
		c.Step()
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning golden: %v", err)
	}
}

// --- Targeted opcode unit tests (the tricky semantics) ---

func TestUADCCarrySetsStatus(t *testing.T) {
	c := newCPU(t)
	c.a = 0x0F
	c.uADCa(0x01) // 0x10 -> wraps to 0, carry out
	if c.a != 0x00 || !c.s {
		t.Fatalf("0xF+1: a=%x s=%v, want a=0 s=true", c.a, c.s)
	}
	c.a = 0x07
	c.uADCa(0x01) // 0x08, no carry
	if c.a != 0x08 || c.s {
		t.Fatalf("7+1: a=%x s=%v, want a=8 s=false", c.a, c.s)
	}
}

func TestSamanIsTwosComplementSubtract(t *testing.T) {
	c := newCPU(t)
	c.a = 3
	*c.curRAM() = 5 // SAMAN computes M + ~A + 1 == M - A; 5-3=2, borrow=>S
	opSaman(c, 0, false)
	if c.a != 0x02 || !c.s {
		t.Fatalf("5-3: a=%x s=%v, want a=2 s=true", c.a, c.s)
	}
}

func TestTDOPlacesStatusLatchInBit4(t *testing.T) {
	c := newCPU(t)
	c.a, c.sl = 0x0A, true
	opTdo(c, 0, false)
	if c.o != 0x1A {
		t.Fatalf("O = %02x, want 1a (A=0xA | SL<<4)", c.o)
	}
	c.sl = false
	opTdo(c, 0, false)
	if c.o != 0x0A {
		t.Fatalf("O = %02x, want 0a (SL clear)", c.o)
	}
}

func TestBranchConsumesPreviousStatus(t *testing.T) {
	c := newCPU(t)
	c.pa, c.pb, c.pc, c.cl = 0x3, 0x5, 0x10, false
	opBr(c, 0x80|0x07, false) // lastS=false: branch is a no-op
	if c.pc != 0x10 || c.pa != 0x3 {
		t.Fatalf("untaken branch changed state: pc=%x pa=%x", c.pc, c.pa)
	}
	opBr(c, 0x80|0x07, true) // lastS=true: take it (PA<=PB, PC<=operand)
	if c.pc != 0x07 || c.pa != 0x5 {
		t.Fatalf("taken branch: pc=%x pa=%x, want pc=7 pa=5", c.pc, c.pa)
	}
}
