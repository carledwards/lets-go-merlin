// btnscan locates the four printed control buttons on the Merlin
// faceplate (NEW GAME / SAME GAME / HIT ME / COMP TURN). Unlike the LED
// holes they are not transparent — they are light "cream" rectangles on
// the red plastic. Detect by: opaque + light + low redness, in the
// lower band of the image, clustered into connected components.
//
//	go run ./internal/tools/btnscan web/assets/merlin.png
package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"sort"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: btnscan <faceplate.png>")
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
	bnd := img.Bounds()
	w, h := bnd.Dx(), bnd.Dy()

	// Control buttons live below the LED grid. Start well under the
	// bottom LED pad (~y472) so its cream surround isn't mistaken for
	// a button.
	yStart := h * 72 / 100

	light := make([]bool, w*h)
	for y := yStart; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(bnd.Min.X+x, bnd.Min.Y+y).RGBA()
			r8, g8, b8, a8 := r>>8, g>>8, b>>8, a>>8
			// opaque, bright, and not dominated by red (the plastic).
			if a8 > 200 && g8 > 150 && b8 > 130 &&
				r8 > 150 && int(r8)-int(b8) < 70 {
				light[y*w+x] = true
			}
		}
	}

	comp := make([]int, w*h)
	for i := range comp {
		comp[i] = -1
	}
	type box struct{ minX, minY, maxX, maxY, n int }
	var boxes []box
	st := make([]int, 0, 4096)
	for s := yStart * w; s < w*h; s++ {
		if !light[s] || comp[s] != -1 {
			continue
		}
		id := len(boxes)
		bx := box{minX: w, minY: h}
		st = st[:0]
		st = append(st, s)
		comp[s] = id
		for len(st) > 0 {
			p := st[len(st)-1]
			st = st[:len(st)-1]
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
			for _, np := range [4]int{p - 1, p + 1, p - w, p + w} {
				if np < 0 || np >= w*h {
					continue
				}
				if (np == p-1 && px == 0) || (np == p+1 && px == w-1) {
					continue
				}
				if light[np] && comp[np] == -1 {
					comp[np] = id
					st = append(st, np)
				}
			}
		}
		boxes = append(boxes, bx)
	}

	type rect struct {
		Name                 string `json:"name"`
		X, Y, W, H, CX, CY, N int    `json:"-"`
	}
	var rs []rect
	for _, b := range boxes {
		bw, bh := b.maxX-b.minX+1, b.maxY-b.minY+1
		// Real buttons are chunky rectangles, not specks or stripes.
		if b.n < 300 || bw < 24 || bh < 16 || bw > w*8/10 || bh > h/4 {
			continue
		}
		rs = append(rs, rect{
			X: b.minX, Y: b.minY, W: bw, H: bh,
			CX: b.minX + bw/2, CY: b.minY + bh/2, N: b.n,
		})
	}

	// Order: top row then bottom row, left then right ->
	// New Game, Same Game, Hit Me, Comp Turn.
	sort.Slice(rs, func(i, j int) bool {
		if d := rs[i].Y - rs[j].Y; d < -20 || d > 20 {
			return rs[i].Y < rs[j].Y
		}
		return rs[i].X < rs[j].X
	})
	names := []string{"New Game", "Same Game", "Hit Me", "Comp Turn"}
	for i := range rs {
		if i < len(names) {
			rs[i].Name = names[i]
		}
	}

	fmt.Fprintf(os.Stderr, "found %d candidate button(s) (want 4)\n", len(rs))
	type pub struct {
		Name string `json:"name"`
		X    int    `json:"x"`
		Y    int    `json:"y"`
		W    int    `json:"w"`
		H    int    `json:"h"`
		CX   int    `json:"cx"`
		CY   int    `json:"cy"`
	}
	pubs := make([]pub, len(rs))
	for i, r := range rs {
		pubs[i] = pub{r.Name, r.X, r.Y, r.W, r.H, r.CX, r.CY}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(struct {
		Width   int   `json:"width"`
		Height  int   `json:"height"`
		Buttons []pub `json:"buttons"`
	}{w, h, pubs})
}
