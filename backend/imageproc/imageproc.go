// Package imageproc normalises a downloaded comic page into the form the
// stock xochitl reader is fastest with: a JPEG no larger than the panel's
// native pixel grid, in the panel's exact 3:4 aspect.
//
// Panel geometry is measured, not assumed (docs/DEVICE-NOTES.md §4): the
// Paper Pro panel is 1620 × 2160 px at ≈227 DPI, and xochitl's own renderer
// emits MediaBox [0 0 514 685] for a full-bleed page. 1620:2160 is exactly 3:4
// (0.75), so a page normalised here fills a 514 × 685 pt PDF page with no
// letterboxing added by the reader.
//
// This runs at *save* time, once per page, never at read time. PLAN §3.1's
// "the stock reader is fast enough" finding depends on the reader never being
// handed a 3000 px scan.
//
// # Aspect mismatch
//
// Comic pages are frequently not 3:4 — double-page spreads are wider, many
// scans are A4-ish (0.707), webtoon strips are far taller. Three options and
// why this package picks the one it does:
//
//   - Crop to 3:4: never. It silently deletes artwork and, on a page where the
//     gutter is off-centre, dialogue. A reader cannot recover what is not in
//     the file.
//   - Stretch to 3:4: never. Distorted faces and lettering are obvious.
//   - Fit (contain) and pad to 3:4 with a background colour: chosen. Content is
//     preserved intact and the page still fills the panel edge to edge, so the
//     reader has no decision to make.
//
// Padding is only applied when the mismatch exceeds AspectTolerance; below
// that the image is resized straight onto the 3:4 grid, which absorbs
// off-by-a-pixel source dimensions without inventing margins.
//
// Sources smaller than the panel are **not** upscaled. Upscaling adds bytes
// and no detail. Instead the whole 3:4 canvas shrinks with the image, so the
// output is still exactly 3:4 and still full-bleed — just fewer pixels.
//
// Everything here is pure Go (image/jpeg, image/png, golang.org/x/image/webp);
// the device binary is built with CGO_ENABLED=0 and must stay static.
package imageproc

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"io"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register the WebP decoder: common for comic sources

	_ "image/gif"
	_ "image/png"
)

// Panel geometry, measured on the device. See docs/DEVICE-NOTES.md §4.
const (
	// PanelWidth and PanelHeight are the panel's native pixel grid.
	PanelWidth  = 1620
	PanelHeight = 2160

	// PanelDPI is the measured resolution: 1620 × 72 / 514 = 226.9.
	PanelDPI = 227
)

// ErrTooLarge reports an encoded page over the configured per-page byte
// budget. It is deliberately fatal rather than a warning: PLAN §8 lists
// "oversized images erode reader performance" as a core-UX risk, and a PDF
// that makes the stock reader crawl is worse than no PDF.
var ErrTooLarge = errors.New("imageproc: encoded page exceeds the per-page byte budget")

// Options controls normalisation. The zero value is not usable; start from
// DefaultOptions.
type Options struct {
	// MaxWidth and MaxHeight bound the output. Defaults to the panel grid.
	MaxWidth, MaxHeight int

	// Quality is the JPEG quality, 1..100.
	Quality int

	// Background fills the padding introduced by an aspect mismatch.
	Background color.Color

	// AspectTolerance is the relative aspect deviation absorbed by resizing
	// rather than padding, e.g. 0.01 for 1%.
	AspectTolerance float64

	// Grayscale converts to 8-bit grey before encoding. Off by default: the
	// Paper Pro panel is a colour panel, so discarding colour is a loss, not a
	// free saving. Worth turning on for sources that are greyscale anyway.
	Grayscale bool

	// MaxBytes is the per-page budget. Exceeding it returns ErrTooLarge.
	// Zero disables the check.
	MaxBytes int64
}

// DefaultMaxPageBytes is the per-page budget. A dense 1620 × 2160 colour page
// at q85 measures roughly 300–700 KiB, so 1 MiB is a loud-failure ceiling for
// pathological input rather than a tight budget.
const DefaultMaxPageBytes = 1 << 20

// DefaultOptions returns the panel-native settings: 1620 × 2160, JPEG q85,
// white padding, 1% aspect tolerance.
func DefaultOptions() Options {
	return Options{
		MaxWidth:        PanelWidth,
		MaxHeight:       PanelHeight,
		Quality:         85,
		Background:      color.White,
		AspectTolerance: 0.01,
		MaxBytes:        DefaultMaxPageBytes,
	}
}

// Result describes one normalised page.
type Result struct {
	Width, Height int // output pixel dimensions, always MaxWidth:MaxHeight aspect
	SrcWidth      int // decoded source dimensions, for diagnostics
	SrcHeight     int
	SrcFormat     string // "jpeg", "png", "webp", ...
	Bytes         int64  // encoded size
	Padded        bool   // true if an aspect mismatch was padded
}

// Normalise decodes src, fits it to the panel grid and writes a JPEG to dst.
//
// The returned Result reports what was actually written. On ErrTooLarge the
// bytes have already been written to dst; the caller owns discarding them,
// which is cheap because callers write to a temp file anyway.
func Normalise(dst io.Writer, src io.Reader, opts Options) (Result, error) {
	if opts.MaxWidth <= 0 || opts.MaxHeight <= 0 {
		return Result{}, fmt.Errorf("imageproc: bad target size %dx%d", opts.MaxWidth, opts.MaxHeight)
	}
	if opts.Quality <= 0 || opts.Quality > 100 {
		return Result{}, fmt.Errorf("imageproc: bad quality %d", opts.Quality)
	}
	if opts.Background == nil {
		opts.Background = color.White
	}

	img, format, err := image.Decode(src)
	if err != nil {
		return Result{}, fmt.Errorf("imageproc: decode: %w", err)
	}

	out, padded := fit(img, opts)

	cw := &countingWriter{w: dst}
	if err := jpeg.Encode(cw, out, &jpeg.Options{Quality: opts.Quality}); err != nil {
		return Result{}, fmt.Errorf("imageproc: encode: %w", err)
	}

	sb := img.Bounds()
	ob := out.Bounds()
	res := Result{
		Width:     ob.Dx(),
		Height:    ob.Dy(),
		SrcWidth:  sb.Dx(),
		SrcHeight: sb.Dy(),
		SrcFormat: format,
		Bytes:     cw.n,
		Padded:    padded,
	}
	if opts.MaxBytes > 0 && cw.n > opts.MaxBytes {
		return res, fmt.Errorf("%w: %d bytes > %d", ErrTooLarge, cw.n, opts.MaxBytes)
	}
	return res, nil
}

// fit produces the output image: the source scaled to fit inside the target
// grid (never upscaled), centred on the smallest canvas of the target aspect
// that contains it.
func fit(src image.Image, opts Options) (image.Image, bool) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	targetAspect := float64(opts.MaxWidth) / float64(opts.MaxHeight)
	srcAspect := float64(sw) / float64(sh)

	// Within tolerance: resize straight onto the target grid (bounded by the
	// source resolution, so a small page stays small).
	if relDiff(srcAspect, targetAspect) <= opts.AspectTolerance {
		w, h := opts.MaxWidth, opts.MaxHeight
		if sw < w || sh < h {
			s := min(float64(sw)/float64(w), float64(sh)/float64(h))
			w, h = scaleDim(w, h, s)
		}
		return render(src, image.Rect(0, 0, w, h), image.Rect(0, 0, w, h), opts), false
	}

	// Scale to contain, never above 1.
	s := min(float64(opts.MaxWidth)/float64(sw), float64(opts.MaxHeight)/float64(sh), 1)
	iw, ih := scaleDim(sw, sh, s)

	// Smallest canvas of the target aspect containing iw × ih, capped at the
	// target grid.
	cw, ch := iw, ih
	if float64(iw)/float64(ih) > targetAspect {
		ch = int(float64(iw)/targetAspect + 0.5)
	} else {
		cw = int(float64(ih)*targetAspect + 0.5)
	}
	cw = min(cw, opts.MaxWidth)
	ch = min(ch, opts.MaxHeight)
	// Re-impose the exact aspect after clamping, so the PDF page never
	// letterboxes.
	if float64(cw)/float64(ch) > targetAspect {
		cw = int(float64(ch)*targetAspect + 0.5)
	} else {
		ch = int(float64(cw)/targetAspect + 0.5)
	}
	iw, ih = min(iw, cw), min(ih, ch)

	dstRect := image.Rect(0, 0, cw, ch)
	imgRect := image.Rect((cw-iw)/2, (ch-ih)/2, (cw-iw)/2+iw, (ch-ih)/2+ih)
	return render(src, dstRect, imgRect, opts), true
}

// render draws src scaled into imgRect on a canvas of dstRect, filling the
// remainder with the background colour.
func render(src image.Image, dstRect, imgRect image.Rectangle, opts Options) image.Image {
	var dst stddraw.Image
	if opts.Grayscale {
		dst = image.NewGray(dstRect)
	} else {
		dst = image.NewRGBA(dstRect)
	}
	if !imgRect.Eq(dstRect) {
		xdraw.Draw(dst, dstRect, image.NewUniform(opts.Background), image.Point{}, xdraw.Src)
	}
	// CatmullRom is the best of x/image/draw's kernels for downscaling line
	// art; the cost is paid once, at save time.
	xdraw.CatmullRom.Scale(dst, imgRect, src, src.Bounds(), xdraw.Src, nil)
	return dst
}

func scaleDim(w, h int, s float64) (int, int) {
	nw := max(int(float64(w)*s+0.5), 1)
	nh := max(int(float64(h)*s+0.5), 1)
	return nw, nh
}

func relDiff(a, b float64) float64 {
	if b == 0 {
		return 1
	}
	d := (a - b) / b
	if d < 0 {
		return -d
	}
	return d
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
