#!/usr/bin/env bash
# Bit-exact regression: compile the C++ reference with its step() debug
# printf enabled (K held at 0, matching the Go poll model), run N
# instructions, and diff against `merlincli -trace`.
#
# The reference is expected at $MERLIN_REF (default below). Override:
#   MERLIN_REF=/path/to/merlin-tms1100 STEPS=200000 bash scripts/ref-verify.sh
set -euo pipefail

REF="${MERLIN_REF:-$HOME/dev/carledwards/merlin-tms1100}"
STEPS="${STEPS:-200000}"
SRC="$REF/cpp"
B="$(mktemp -d)"
trap 'rm -rf "$B"' EXIT

[ -f "$SRC/tms1xx0.cpp" ] || { echo "C++ reference not found at $SRC (set MERLIN_REF)"; exit 2; }
command -v clang++ >/dev/null || { echo "clang++ not found"; exit 2; }

cp "$SRC/tms1xx0.cpp" "$SRC/tms1xx0.h" "$B/"
cp roms/mp3404.bin "$B/mp3404.bin"

# Enable the commented-out step() debug printf.
python3 - "$B/tms1xx0.cpp" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('#include "tms1xx0.h"', '#include "tms1xx0.h"\n#include <cstdio>', 1)
old = '''    // useful for debugging
    // printf("%1x:%02x %02x x:%02x y:%02x a:%02x s:%1x ram:%02x cl:%02x ca:%02x cb:%02x\\n",
    //     cpu_->get_pa(), cpu_->get_pc(), opcode, cpu_->get_x(), cpu_->get_y(), cpu_->get_a(),
    //     cpu_->get_s(), CURR_RAM, cpu_->get_cl(), cpu_->get_ca(), cpu_->get_cb());'''
new = '''    printf("%1x:%02x %02x x:%02x y:%02x a:%02x s:%1x ram:%02x cl:%02x ca:%02x cb:%02x\\n",
        cpu_->get_pa(), cpu_->get_pc(), opcode, cpu_->get_x(), cpu_->get_y(), cpu_->get_a(),
        cpu_->get_s(), CURR_RAM, cpu_->get_cl(), cpu_->get_ca(), cpu_->get_cb());'''
assert old in s, "reference step() debug printf block not found verbatim"
open(p, 'w').write(s.replace(old, new, 1))
PY

cat > "$B/main_trace.cpp" <<'CPP'
#include <cstdlib>
#include "tms1xx0.h"
int main(int argc, char** argv) {
    long n = (argc > 1) ? atol(argv[1]) : 10000;
    ROM* rom = new ROM(); rom->load_rom("mp3404.bin");
    TMS1100 emu = TMS1100(rom);
    for (long i = 0; i < n; ++i) emu.step();
    return 0;
}
CPP

clang++ -std=c++17 -O2 -o "$B/merlinref" "$B/tms1xx0.cpp" "$B/main_trace.cpp"
( cd "$B" && ./merlinref "$STEPS" > cpp_trace.txt )
go run ./cmd/merlincli -steps "$STEPS" -trace > "$B/go_trace.txt"

if cmp -s "$B/go_trace.txt" "$B/cpp_trace.txt"; then
	echo "OK: $STEPS/$STEPS instructions byte-identical to C++ reference"
else
	echo "MISMATCH:"; cmp "$B/go_trace.txt" "$B/cpp_trace.txt"; exit 1
fi
