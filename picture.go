package main

import (
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

// Screen size of the C69 M Star.
const (
	screenW         = 758
	screenH         = 1024
	maxPictureBytes = 10_000_000
)

// fitToScreen scales src to fit inside w×h without cropping, centres it on
// white and converts it to greyscale, which is what the e-ink screen shows.
func fitToScreen(src image.Image, w, h int) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, w, h))
	for i := range dst.Pix {
		dst.Pix[i] = 255
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return dst
	}
	scale := math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	tw := max(1, int(math.Round(float64(sw)*scale)))
	th := max(1, int(math.Round(float64(sh)*scale)))
	ox, oy := (w-tw)/2, (h-th)/2

	// Box filter: each output pixel averages the source pixels it covers.
	for y := 0; y < th; y++ {
		y0 := int(float64(y) / scale)
		y1 := max(y0+1, int(math.Ceil(float64(y+1)/scale)))
		y1 = min(y1, sh)
		for x := 0; x < tw; x++ {
			x0 := int(float64(x) / scale)
			x1 := max(x0+1, int(math.Ceil(float64(x+1)/scale)))
			x1 = min(x1, sw)
			var sum, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sum += uint64(lumaOnWhite(src.At(sb.Min.X+sx, sb.Min.Y+sy)))
					n++
				}
			}
			dst.SetGray(ox+x, oy+y, color.Gray{Y: uint8(sum / max(n, 1))})
		}
	}
	return dst
}

// lumaOnWhite returns 0–255 brightness, treating transparency as white.
func lumaOnWhite(c color.Color) uint8 {
	r, g, b, a := c.RGBA() // premultiplied, 0–0xffff
	bg := 0xffff - a
	r, g, b = r+bg, g+bg, b+bg
	y := (19595*r + 38470*g + 7471*b + 1<<15) >> 24
	return uint8(y)
}
