package imageproc_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/rickl/quire/backend/imageproc"
)

// These benchmarks exist to be run *on the device* — the backend runs on an
// i.MX8MM with four Cortex-A53 cores, and a host figure says nothing useful
// about whether a 200-page volume is tolerable there:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c ./backend/imageproc
//	scp imageproc.test root@10.11.99.1:/home/root/
//	ssh root@10.11.99.1 '/home/root/imageproc.test -test.bench=. -test.benchtime=5x -test.cpu=1'
//
// Recorded results live in docs/DEVICE-NOTES.md §10.

// comicPage draws a page that compresses like inked line art over flat tone:
// panel borders, hatching, and large even areas.
func comicPage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8(235)
			switch (x/(w/7+1) + y/(h/9+1)) % 3 {
			case 1:
				v = 170
			case 2:
				v = 205
			}
			c := color.RGBA{v, v, v, 255}
			if x < 8 || y < 8 || x >= w-8 || y >= h-8 ||
				x%(w/7+1) < 5 || y%(h/9+1) < 5 || (x+y)%180 < 4 {
				c = color.RGBA{25, 25, 30, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func BenchmarkNormalise(b *testing.B) {
	sources := []struct {
		name string
		w, h int
	}{
		{"a4-scan-2480x3508", 2480, 3508}, // the common case
		{"panel-1620x2160", 1620, 2160},   // already native
		{"huge-5000x7000", 5000, 7000},    // where a prescale can bite
	}
	scalers := []imageproc.Scaler{imageproc.ScalerCatmullRom, imageproc.ScalerBiLinear, imageproc.ScalerApproxBiLinear}

	for _, src := range sources {
		raw := comicPage(src.w, src.h)
		for _, s := range scalers {
			b.Run(fmt.Sprintf("%s/%s", src.name, s), func(b *testing.B) {
				opts := imageproc.DefaultOptions()
				opts.Scaler = s
				opts.MaxBytes = 0 // measuring cost, not enforcing the budget
				b.SetBytes(int64(len(raw)))
				b.ReportAllocs()
				var out bytes.Buffer
				for b.Loop() {
					out.Reset()
					if _, err := imageproc.Normalise(&out, bytes.NewReader(raw), opts); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(out.Len()), "outbytes")
			})
		}
	}
}

// BenchmarkDecodeOnly separates decode cost from resize cost, so the device
// numbers say which half to optimise.
func BenchmarkDecodeOnly(b *testing.B) {
	raw := comicPage(2480, 3508)
	for b.Loop() {
		if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeOnly is the JPEG encode of a finished panel-sized page.
func BenchmarkEncodeOnly(b *testing.B) {
	img, err := jpeg.Decode(bytes.NewReader(comicPage(1620, 2160)))
	if err != nil {
		b.Fatal(err)
	}
	var out bytes.Buffer
	for b.Loop() {
		out.Reset()
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
			b.Fatal(err)
		}
	}
}
