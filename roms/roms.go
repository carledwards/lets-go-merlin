// Package roms embeds the Merlin ROM image so every build target
// (CLI, WASM, embedded) gets the same bytes without filesystem access.
package roms

import _ "embed"

// MP3404 is the Texas Instruments MP3404 mask ROM dump used by the
// Parker Brothers Merlin (1978). SHA1: 76ca3605d3fde1df62f79b9bb1f534c2a2ae0229.
//
//go:embed mp3404.bin
var MP3404 []byte
