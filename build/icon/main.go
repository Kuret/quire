// Command icon generates the Quire launcher icon.
//
// The icon is committed as build/../icon.png; this generator exists so it can
// be reproduced rather than hand-edited. It draws a quire — a small gathering
// of folded sheets — as plain black on white. The Paper Pro panel is
// effectively 1-bit-ish for line art, so: no greys, no gradients, no colour.
//
// Usage: go run ./build/icon -o icon.png
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	size   = 100
	stroke = 4
)

var (
	black = color.NRGBA{0, 0, 0, 255}
	white = color.NRGBA{255, 255, 255, 255}
)

func main() {
	out := flag.String("o", "icon.png", "output path")
	flag.Parse()

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fill(img, image.Rect(0, 0, size, size), white)

	// Back sheet, offset up and right.
	outline(img, image.Rect(30, 12, 90, 74))
	// Front sheet, drawn over it, with a fold down the middle.
	fill(img, image.Rect(10, 26, 70, 88), white)
	outline(img, image.Rect(10, 26, 70, 88))
	fill(img, image.Rect(38, 26, 38+stroke, 88), black)

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func fill(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// outline draws a stroke-wide black frame just inside r.
func outline(img *image.NRGBA, r image.Rectangle) {
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+stroke), black)
	fill(img, image.Rect(r.Min.X, r.Max.Y-stroke, r.Max.X, r.Max.Y), black)
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+stroke, r.Max.Y), black)
	fill(img, image.Rect(r.Max.X-stroke, r.Min.Y, r.Max.X, r.Max.Y), black)
}
