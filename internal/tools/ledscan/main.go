// ledscan finds the LED cut-out holes in the faceplate PNG by scanning
// its alpha channel, clustering transparent pixels into connected
// components, and printing each hole's center/size in Merlin pad order
// (0 = top, 1..9 = 3x3 grid, 10 = bottom). Output is JSON for baking
// into the web layer.
//
//	go run ./internal/tools/ledscan web/assets/merlin.png
package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sort"
)

type hole struct {
	Pad int `json:"pad"`
	CX  int `json:"cx"`
	CY  int `json:"cy"`
	W   int `json:"w"`
	H   int `json:"h"`
	R   int `json:"r"` // suggested radius
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ledscan <faceplate.png>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		panic(err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// transparent = alpha below threshold (the cut-out holes).
	const alphaThresh = 128 << 8 // RGBA() returns 16-bit
	trans := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			trans[y*w+x] = a < alphaThresh
		}
	}

	// Flood-fill connected components (4-neighbour).
	comp := make([]int, w*h)
	for i := range comp {
		comp[i] = -1
	}
	type box struct{ minX, minY, maxX, maxY, n, sumX, sumY int }
	var boxes []box
	stack := make([]int, 0, 1024)
	for start := 0; start < w*h; start++ {
		if !trans[start] || comp[start] != -1 {
			continue
		}
		id := len(boxes)
		bx := box{minX: w, minY: h}
		stack = stack[:0]
		stack = append(stack, start)
		comp[start] = id
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			px, py := p%w, p/w
			if px < bx.minX {
				bx.minX = px
			}
			if px > bx.maxX {
				bx.maxX = px
			}
			if py < bx.minY {
				bx.minY = py
			}
			if py > bx.maxY {
				bx.maxY = py
			}
			bx.n++
			bx.sumX += px
			bx.sumY += py
			for _, np := range [4]int{p - 1, p + 1, p - w, p + w} {
				if np < 0 || np >= w*h {
					continue
				}
				// avoid horizontal wrap
				if (np == p-1 && px == 0) || (np == p+1 && px == w-1) {
					continue
				}
				if trans[np] && comp[np] == -1 {
					comp[np] = id
					stack = append(stack, np)
				}
			}
		}
		boxes = append(boxes, bx)
	}

	// Keep only LED-sized blobs (drop stray AA pixels / border alpha).
	var holes []hole
	for _, bx := range boxes {
		bw, bh := bx.maxX-bx.minX+1, bx.maxY-bx.minY+1
		if bx.n < 16 || bw < 4 || bh < 4 || bw > w/2 || bh > h/2 {
			continue
		}
		holes = append(holes, hole{
			CX: bx.sumX / bx.n, CY: bx.sumY / bx.n,
			W: bw, H: bh, R: (bw + bh) / 4,
		})
	}

	if len(holes) != 11 {
		fmt.Fprintf(os.Stderr,
			"WARNING: found %d holes, expected 11 (check alpha threshold / source)\n",
			len(holes))
	}

	// Order into Merlin pads: cluster by Y into rows, then sort each row
	// left-to-right. Expected rows: [1],[3],[3],[3],[1].
	sort.Slice(holes, func(i, j int) bool { return holes[i].CY < holes[j].CY })
	var rows [][]hole
	for _, hl := range holes {
		placed := false
		for r := range rows {
			if abs(rows[r][0].CY-hl.CY) < 30 { // same row tolerance
				rows[r] = append(rows[r], hl)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, []hole{hl})
		}
	}
	ordered := make([]hole, 0, len(holes))
	pad := 0
	for _, row := range rows {
		sort.Slice(row, func(i, j int) bool { return row[i].CX < row[j].CX })
		for _, hl := range row {
			hl.Pad = pad
			pad++
			ordered = append(ordered, hl)
		}
	}
	holes = ordered

	out := struct {
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Holes  []hole `json:"holes"`
	}{w, h, holes}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
