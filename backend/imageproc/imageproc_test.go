package imageproc_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"github.com/rickl/quire/backend/imageproc"
)

// Every test image in this package is generated here, in process. PLAN §1.3 /
// §1.4: nothing is fetched from a network in a test, and no image sourced from
// a real site is committed to this repository.

// synthPage draws a page-like test image: a light background, a dark border,
// and diagonal ruling, so a resize that flips, crops or stretches is visible
// in the pixels rather than only in the dimensions.
func synthPage(w, h int, seed uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{230, 230, 230, 255}
			switch {
			case x < 4 || y < 4 || x >= w-4 || y >= h-4:
				c = color.RGBA{16, 16, 16, 255}
			case (x+y)%64 < 8:
				c = color.RGBA{seed, 64, 128, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

const panelAspect = float64(imageproc.PanelWidth) / float64(imageproc.PanelHeight)

func TestNormaliseSizesAndAspects(t *testing.T) {
	cases := []struct {
		name       string
		w, h       int
		png        bool
		wantW      int
		wantH      int
		wantPadded bool
		aspectTol  float64 // defaults to 0.01
	}{
		// Exactly the panel grid: unchanged.
		{name: "panel native", w: 1620, h: 2160, wantW: 1620, wantH: 2160},
		// Larger, same aspect: downscaled onto the grid.
		{name: "oversized 3:4", w: 3240, h: 4320, wantW: 1620, wantH: 2160},
		// A4-ish scan, taller box than 3:4 content: padded left/right.
		{name: "a4 scan", w: 2480, h: 3508, wantW: 1620, wantH: 2160, wantPadded: true},
		// Double-page spread, much wider: padded top/bottom.
		{name: "spread", w: 3200, h: 2200, wantW: 1620, wantH: 2160, wantPadded: true},
		// Square.
		{name: "square", w: 1000, h: 1000, wantW: 1000, wantH: 1333, wantPadded: true},
		// Smaller than the panel, 3:4: not upscaled, canvas shrinks with it.
		{name: "small 3:4", w: 810, h: 1080, wantW: 810, wantH: 1080},
		// Smaller than the panel, wrong aspect.
		{name: "small wide", w: 900, h: 600, wantW: 900, wantH: 1200, wantPadded: true},
		// PNG source.
		{name: "png source", w: 1200, h: 1600, png: true, wantW: 1200, wantH: 1600},
		// Tiny. At a handful of pixels the 3:4 grid cannot be hit exactly —
		// rounding is a whole pixel wide — so this case documents the floor
		// rather than pretending the aspect is exact.
		{name: "tiny", w: 10, h: 12, wantW: 10, wantH: 13, wantPadded: true, aspectTol: 0.05},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := synthPage(tc.w, tc.h, 200)
			var raw []byte
			if tc.png {
				raw = encodePNG(t, src)
			} else {
				raw = encodeJPEG(t, src)
			}

			var out bytes.Buffer
			res, err := imageproc.Normalise(&out, bytes.NewReader(raw), imageproc.DefaultOptions())
			if err != nil {
				t.Fatalf("Normalise: %v", err)
			}
			if res.Width != tc.wantW || res.Height != tc.wantH {
				t.Errorf("output %dx%d, want %dx%d", res.Width, res.Height, tc.wantW, tc.wantH)
			}
			if res.Padded != tc.wantPadded {
				t.Errorf("Padded = %v, want %v", res.Padded, tc.wantPadded)
			}
			if res.SrcWidth != tc.w || res.SrcHeight != tc.h {
				t.Errorf("SrcWidth/Height = %dx%d, want %dx%d", res.SrcWidth, res.SrcHeight, tc.w, tc.h)
			}
			if res.Width > imageproc.PanelWidth || res.Height > imageproc.PanelHeight {
				t.Errorf("output %dx%d exceeds the panel grid", res.Width, res.Height)
			}

			// The output must carry the panel aspect to within a pixel of
			// rounding, or the PDF page letterboxes.
			tol := tc.aspectTol
			if tol == 0 {
				tol = 0.01
			}
			gotAspect := float64(res.Width) / float64(res.Height)
			if math.Abs(gotAspect-panelAspect)/panelAspect > tol {
				t.Errorf("aspect %.4f, want %.4f", gotAspect, panelAspect)
			}

			// Read it back: it must be a decodable JPEG of the reported size.
			dec, format, err := image.Decode(bytes.NewReader(out.Bytes()))
			if err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if format != "jpeg" {
				t.Errorf("output format = %q, want jpeg", format)
			}
			if b := dec.Bounds(); b.Dx() != res.Width || b.Dy() != res.Height {
				t.Errorf("decoded %dx%d, disagrees with Result %dx%d", b.Dx(), b.Dy(), res.Width, res.Height)
			}
			if int64(out.Len()) != res.Bytes {
				t.Errorf("Bytes = %d, wrote %d", res.Bytes, out.Len())
			}
		})
	}
}

// A page is never cropped: the source's dark border must survive somewhere
// inside the output, and padding must be the configured background colour.
func TestNormalisePadsRatherThanCrops(t *testing.T) {
	src := synthPage(3200, 2200, 200) // very wide: padded top and bottom
	raw := encodeJPEG(t, src)

	opts := imageproc.DefaultOptions()
	opts.Background = color.RGBA{255, 0, 0, 255}

	var out bytes.Buffer
	res, err := imageproc.Normalise(&out, bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	if !res.Padded {
		t.Fatal("expected padding for a 1.45 aspect source")
	}
	dec, _, err := image.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Top row is padding, so it is the background colour.
	r, g, b, _ := dec.At(res.Width/2, 0).RGBA()
	if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
		t.Errorf("top row = (%d,%d,%d), want the red background", r>>8, g>>8, b>>8)
	}

	// The source's dark border survives: the middle column has a dark run
	// where the image content starts.
	foundDark := false
	for y := range res.Height {
		if y, _, _, _ := dec.At(res.Width/2, y).RGBA(); y>>8 < 80 {
			foundDark = true
			break
		}
	}
	if !foundDark {
		t.Error("source border missing from the output: content was cropped")
	}
}

func TestNormaliseGrayscale(t *testing.T) {
	raw := encodeJPEG(t, synthPage(1620, 2160, 200))

	opts := imageproc.DefaultOptions()
	colourRes, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("colour: %v", err)
	}

	opts.Grayscale = true
	var out bytes.Buffer
	greyRes, err := imageproc.Normalise(&out, bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("grey: %v", err)
	}
	if greyRes.Bytes >= colourRes.Bytes {
		t.Errorf("grayscale %d bytes, not smaller than colour %d", greyRes.Bytes, colourRes.Bytes)
	}
	dec, _, err := image.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := dec.(*image.Gray); !ok {
		t.Errorf("decoded %T, want *image.Gray", dec)
	}
}

func TestNormaliseRejectsOversizedPage(t *testing.T) {
	raw := encodeJPEG(t, synthPage(1620, 2160, 200))

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 1024

	res, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if !errors.Is(err, imageproc.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if res.Bytes <= opts.MaxBytes {
		t.Errorf("Result.Bytes = %d, should report the offending size", res.Bytes)
	}
}

func TestNormaliseRejectsGarbage(t *testing.T) {
	if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader([]byte("not an image")), imageproc.DefaultOptions()); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestNormaliseValidatesOptions(t *testing.T) {
	raw := encodeJPEG(t, synthPage(100, 133, 200))

	bad := imageproc.DefaultOptions()
	bad.Quality = 0
	if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), bad); err == nil {
		t.Error("expected an error for quality 0")
	}

	bad = imageproc.DefaultOptions()
	bad.MaxWidth = 0
	if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), bad); err == nil {
		t.Error("expected an error for a zero target width")
	}
}

// The WebP decoder must be registered — many comic sources serve WebP. There
// is no pure-Go WebP *encoder* to generate a fixture with, and committing a
// real .webp is forbidden by §1.3, so this asserts registration instead: a
// truncated RIFF/WEBP header must fail as a *WebP* decode, not as
// image.ErrFormat ("unknown format").
func TestWebPDecoderIsRegistered(t *testing.T) {
	header := []byte("RIFF\x24\x00\x00\x00WEBPVP8L\x18\x00\x00\x00")
	_, _, err := image.Decode(bytes.NewReader(header))
	if err == nil {
		t.Fatal("expected an error from a truncated webp")
	}
	if errors.Is(err, image.ErrFormat) {
		t.Fatalf("webp decoder not registered: %v", err)
	}
}

// Reports the real per-page byte cost at the panel grid, so "sane file size"
// in the M4 acceptance test is a number rather than an adjective.
func TestReportPageBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"panel native", 1620, 2160},
		{"oversized scan", 2480, 3508},
		{"spread", 3200, 2200},
	} {
		raw := encodeJPEG(t, synthPage(tc.w, tc.h, 200))
		res, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), imageproc.DefaultOptions())
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		t.Logf("%-16s %dx%d -> %dx%d  %d bytes (%.0f KiB)", tc.name, tc.w, tc.h, res.Width, res.Height, res.Bytes, float64(res.Bytes)/1024)
	}
}
