// merlincli runs the TMS1100 core against the embedded Merlin ROM for
// debugging and for verifying the port against the C++ reference.
//
//	merlincli -steps 10000 -trace      # one line per instruction, C++ format
//	merlincli -steps 50000 -every 2000 # periodic R/O scan snapshot
//
// With -trace the output is byte-identical to the C++ reference's
// step() debug printf (with K held at 0), so the two can be diffed.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/carledwards/lets-go-merlin/pkg/tms1100"
	"github.com/carledwards/lets-go-merlin/roms"
)

func b(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func main() {
	steps := flag.Int("steps", 10000, "number of instructions to execute")
	trace := flag.Bool("trace", false, "print one C++-format trace line per instruction")
	every := flag.Int("every", 2000, "with -trace off, print an R/O snapshot every N steps")
	flag.Parse()

	cpu, err := tms1100.New(roms.MP3404)
	if err != nil {
		fmt.Fprintln(os.Stderr, "merlincli:", err)
		os.Exit(1)
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for i := 0; i < *steps; i++ {
		if *trace {
			// Mirror the C++ reference debug line, emitted BEFORE the
			// instruction executes (pre-increment PC, pre-exec state):
			// "%1x:%02x %02x x:%02x y:%02x a:%02x s:%1x ram:%02x cl:%02x ca:%02x cb:%02x"
			fmt.Fprintf(out, "%1x:%02x %02x x:%02x y:%02x a:%02x s:%1x ram:%02x cl:%02x ca:%02x cb:%02x\n",
				cpu.PA(), cpu.PC(), cpu.CurrentOpcode(), cpu.X(), cpu.Y(), cpu.A(),
				b(cpu.S()), cpu.CurrentRAM(), b(cpu.CL()), cpu.CA(), cpu.CB())
		} else if *every > 0 && i%*every == 0 {
			fmt.Fprintf(out, "step %6d  O=%02x  R=", i, cpu.O())
			for r := 0; r < 11; r++ {
				if cpu.R(r) {
					fmt.Fprintf(out, "%d", r)
				} else {
					out.WriteByte('.')
				}
			}
			out.WriteByte('\n')
		}
		cpu.Step()
	}
}
