// ogimage composes the social-card preview (Open Graph / Twitter) at
// the conventional 1200x630, by placing the tall faceplate on the
// site's dark background. Run once; the result is committed as
// web/og.png and served by GitHub Pages.
//
//	go run ./internal/tools/ogimage web/assets/merlin.png web/og.png
package main

import (
	"image"
	"image/color"
	"image/png"
	_ "image/png"
	"os"
)

const (
	cardW, cardH = 1200, 630
	faceH        = 560 // faceplate height on the card (leaves a margin)
)

// site background = #0c0c0f
var bg = color.NRGBA{0x0c, 0x0c, 0x0f, 0xff}

func main() {
	if len(os.Args) < 3 {
		println("usage: ogimage <faceplate.png> <out.png>")
		os.Exit(2)
	}
	in, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer in.Close()
	src, _, err := image.Decode(in)
	if err != nil {
		panic(err)
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	// Scale the faceplate to faceH, preserving aspect; centre it.
	scale := float64(faceH) / float64(sh)
	dw := int(float64(sw) * scale)
	x0 := (cardW - dw) / 2
	y0 := (cardH - faceH) / 2

	card := image.NewNRGBA(image.Rect(0, 0, cardW, cardH))
	for y := 0; y < cardH; y++ {
		for x := 0; x < cardW; x++ {
			card.SetNRGBA(x, y, bg)
		}
	}

	// Bilinear sample the source, alpha-composite over the background.
	for y := 0; y < faceH; y++ {
		fy := (float64(y) + 0.5) / scale
		sy0 := clamp(int(fy-0.5), 0, sh-1)
		sy1 := clamp(sy0+1, 0, sh-1)
		wy := (fy - 0.5) - float64(int(fy-0.5))
		for x := 0; x < dw; x++ {
			fx := (float64(x) + 0.5) / scale
			sx0 := clamp(int(fx-0.5), 0, sw-1)
			sx1 := clamp(sx0+1, 0, sw-1)
			wx := (fx - 0.5) - float64(int(fx-0.5))

			r, g, b, a := bilerp(src,
				sb.Min.X+sx0, sb.Min.Y+sy0,
				sb.Min.X+sx1, sb.Min.Y+sy1, wx, wy)

			// Composite src over bg (a in 0..1).
			af := a / 65535.0
			cr := uint8((float64(r)/65535*af + float64(bg.R)/255*(1-af)) * 255)
			cg := uint8((float64(g)/65535*af + float64(bg.G)/255*(1-af)) * 255)
			cb := uint8((float64(b)/65535*af + float64(bg.B)/255*(1-af)) * 255)
			card.SetNRGBA(x0+x, y0+y, color.NRGBA{cr, cg, cb, 0xff})
		}
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}
	defer out.Close()
	if err := png.Encode(out, card); err != nil {
		panic(err)
	}
	println("wrote", os.Args[2])
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bilerp returns the bilinearly interpolated 16-bit RGBA at the four
// given source pixels with weights wx, wy.
func bilerp(img image.Image, x0, y0, x1, y1 int, wx, wy float64) (r, g, b, a float64) {
	r00, g00, b00, a00 := img.At(x0, y0).RGBA()
	r10, g10, b10, a10 := img.At(x1, y0).RGBA()
	r01, g01, b01, a01 := img.At(x0, y1).RGBA()
	r11, g11, b11, a11 := img.At(x1, y1).RGBA()
	lerp := func(c00, c10, c01, c11 uint32) float64 {
		top := float64(c00)*(1-wx) + float64(c10)*wx
		bot := float64(c01)*(1-wx) + float64(c11)*wx
		return top*(1-wy) + bot*wy
	}
	return lerp(r00, r10, r01, r11),
		lerp(g00, g10, g01, g11),
		lerp(b00, b10, b01, b11),
		lerp(a00, a10, a01, a11)
}
