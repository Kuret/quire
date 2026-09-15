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
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"io"
	"runtime"
	"sync"

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

	// RetryQuality is the JPEG quality to re-encode a page at when the first
	// encode exceeds MaxBytes. Zero disables the retry, so the first overrun
	// is fatal. See Result.Requantised.
	RetryQuality int

	// Scaler selects the resampling kernel. The zero value is the default,
	// measured on the device — see the Scaler docs.
	Scaler Scaler
}

// Scaler names a resampling kernel.
//
// # Measured on the device, not the host (docs/DEVICE-NOTES.md §10)
//
// Four Cortex-A53 cores, one page at a time, decode + resize + encode of a
// 2480 × 3508 scan onto the panel grid:
//
//	CatmullRom       4.15 s/page   sharp; the default
//	BiLinear         3.17 s/page   -24%, softer edges, still area-correct
//	ApproxBiLinear   1.67 s/page   -60%, but samples only four source pixels
//	                               per destination pixel regardless of the
//	                               ratio, so it aliases screentone
//
// Of that, ~0.52 s is the JPEG decode and ~0.39 s the encode, so the kernel is
// the whole of the difference.
//
// CatmullRom stays the default. Manga is line art on a 227 DPI e-ink panel:
// softening it is the one visible regression, and ApproxBiLinear's aliasing on
// screentone is worse than softness. The cost is paid once, at save time, and
// the queue bounds how many encodes run at once so the reader keeps its cores
// (see download.DefaultEncodeWorkers).
//
// A cheap box pre-pass to ~2× target before the quality kernel — the usual
// trick for large downscales — was implemented and measured, and **removed**:
// comic sources run 1.5–3× larger than the panel, and a pre-pass needs a
// ≥4× total ratio before it can leave 2× for the kernel. It never engaged on a
// real page, and at 5000 × 7000 it still did not (12.34 s with it, 12.39 s
// without).
type Scaler int

const (
	// ScalerCatmullRom is a sharp bicubic kernel: the right choice for line
	// art, and the default.
	ScalerCatmullRom Scaler = iota

	// ScalerApproxBiLinear is x/image's fast path. Faster, visibly softer on
	// inked lines and screentone; kept for measurement and for anyone who
	// needs the speed more than the edges.
	ScalerApproxBiLinear

	// ScalerBiLinear is the exact bilinear kernel.
	ScalerBiLinear
)

// kernel returns the underlying x/image kernel, if this scaler has one.
func (s Scaler) kernel() (*xdraw.Kernel, bool) {
	switch s {
	case ScalerBiLinear:
		return xdraw.BiLinear, true
	case ScalerApproxBiLinear:
		return nil, false
	default:
		return xdraw.CatmullRom, true
	}
}

func (s Scaler) scaler() xdraw.Scaler {
	switch s {
	case ScalerApproxBiLinear:
		return xdraw.ApproxBiLinear
	case ScalerBiLinear:
		return xdraw.BiLinear
	default:
		return xdraw.CatmullRom
	}
}

// String names the kernel, for logs and benchmark output.
func (s Scaler) String() string {
	switch s {
	case ScalerApproxBiLinear:
		return "ApproxBiLinear"
	case ScalerBiLinear:
		return "BiLinear"
	default:
		return "CatmullRom"
	}
}

// DefaultMaxPageBytes is the per-page budget. A dense 1620 × 2160 colour page
// at q85 measures roughly 300–700 KiB, so 1 MiB is a loud-failure ceiling for
// pathological input rather than a tight budget.
const DefaultMaxPageBytes = 1 << 20

// DefaultRetryQuality is the second-attempt JPEG quality for a page that
// busts the budget at DefaultOptions().Quality.
const DefaultRetryQuality = 65

// DefaultOptions returns the panel-native settings: 1620 × 2160, JPEG q85
// (q65 on a retry), white padding, 1% aspect tolerance, CatmullRom resampling.
func DefaultOptions() Options {
	return Options{
		MaxWidth:        PanelWidth,
		MaxHeight:       PanelHeight,
		Quality:         85,
		RetryQuality:    DefaultRetryQuality,
		Background:      color.White,
		AspectTolerance: 0.01,
		MaxBytes:        DefaultMaxPageBytes,
		Scaler:          ScalerCatmullRom,
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

	// Requantised reports that the page busted MaxBytes at Options.Quality and
	// was re-encoded once at Options.RetryQuality. A source where this happens
	// on every page is systematically oversized and worth surfacing.
	Requantised bool
	// FirstBytes is the size of the rejected first encode, set only when
	// Requantised.
	FirstBytes int64
	// Quality is the JPEG quality the written bytes were encoded at.
	Quality int
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

	sb := img.Bounds()
	ob := out.Bounds()
	res := Result{
		Width:     ob.Dx(),
		Height:    ob.Dy(),
		SrcWidth:  sb.Dx(),
		SrcHeight: sb.Dy(),
		SrcFormat: format,
		Padded:    padded,
		Quality:   opts.Quality,
	}

	// Encode into memory rather than straight to dst: the budget is only
	// knowable after the fact, and a page that busts it gets one cheaper
	// attempt before the whole volume fails. A page is ~0.5 MiB, so this
	// buffer is not the memory that matters.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: opts.Quality}); err != nil {
		return Result{}, fmt.Errorf("imageproc: encode: %w", err)
	}

	// One re-encode at a lower quality. Failing a 200-page volume because a
	// single noisy page missed the budget is the wrong trade; silently
	// degrading every page would be worse, so the retry is reported in
	// Result.Requantised for the caller to log.
	if opts.MaxBytes > 0 && int64(buf.Len()) > opts.MaxBytes && opts.RetryQuality > 0 && opts.RetryQuality < opts.Quality {
		var retry bytes.Buffer
		if err := jpeg.Encode(&retry, out, &jpeg.Options{Quality: opts.RetryQuality}); err != nil {
			return Result{}, fmt.Errorf("imageproc: re-encode: %w", err)
		}
		res.FirstBytes = int64(buf.Len())
		res.Requantised = true
		res.Quality = opts.RetryQuality
		buf = retry
	}

	n, err := dst.Write(buf.Bytes())
	res.Bytes = int64(n)
	if err != nil {
		return res, fmt.Errorf("imageproc: write: %w", err)
	}
	if opts.MaxBytes > 0 && res.Bytes > opts.MaxBytes {
		return res, fmt.Errorf("%w: %d bytes > %d at quality %d", ErrTooLarge, res.Bytes, opts.MaxBytes, res.Quality)
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
	src = fastSource(src)
	var dst stddraw.Image
	if opts.Grayscale {
		dst = image.NewGray(dstRect)
	} else {
		dst = image.NewRGBA(dstRect)
	}
	if !imgRect.Eq(dstRect) {
		xdraw.Draw(dst, dstRect, image.NewUniform(opts.Background), image.Point{}, xdraw.Src)
	}
	scalerFor(opts.Scaler, imgRect, src.Bounds()).Scale(dst, imgRect, src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// The scaler cache is bounded by **retained bytes, not entry count**, and that
// distinction is the whole point.
//
// x/image's kernel scalers allocate an intermediate buffer of
// dstW × srcH × 32 bytes on every Scale call — about 170 MB for an A4 page.
// NewScaler pools that buffer across calls, which is why caching a scaler is
// worth anything at all; it is also why a cached scaler *retains* a buffer of
// that size until the next GC drops the pool.
//
// An earlier version of this cache bounded itself at eight entries on the
// premise that "a volume is one geometry". That premise came from synthetic
// fixtures, which were all the same size. Real sources are not: MangaDex
// serves pages that vary page to page, so the cache filled with eight distinct
// geometries, pinned eight intermediate buffers, and the backend was
// OOM-killed on the device at 1.63 GB resident (anon-rss, 2 GB device shared
// with xochitl). Eight entries was never a memory bound — one entry can be
// 170 MB and another 20 MB.
//
// So: a byte budget, worst-case per-entry costs, and LRU eviction rather than
// dropping the whole map and losing the hot entry with the cold ones. A
// geometry whose buffer alone exceeds the budget is never cached; it falls
// back to Kernel.Scale, which allocates transiently and lets the GC reclaim.
// That is slower in garbage terms and survivable in memory terms, which is the
// right direction for this failure.
//
// One consequence, and it is the honest one: with the default budget a
// full-size page's intermediate does not fit, so those pages are not cached at
// all. That costs nothing measurable — on device the cache never changed wall
// clock, only garbage — and it is what keeps the bound truthful.
//
// Measured on device: docs/DEVICE-NOTES.md §10.4.
const DefaultScalerCacheBytes = 256 << 20

type scalerKey struct {
	kernel         Scaler
	dw, dh, sw, sh int
}

// bytes is what a cached scaler can retain.
//
// The intermediate is one [4]float64 per (destination column × source row) —
// and it is held in a sync.Pool, which keeps a *per-P* slot. So the worst case
// is one buffer per GOMAXPROCS, not one per scaler, and counting one was how
// the first version of this cache convinced itself that eight entries were
// affordable. Count the worst case.
func (k scalerKey) bytes() int64 {
	return int64(k.dw) * int64(k.sh) * 32 * int64(poolFactor())
}

// poolFactor bounds how many copies of an intermediate a sync.Pool can hold.
func poolFactor() int {
	return min(runtime.GOMAXPROCS(0), 4)
}

type scalerCacheT struct {
	mu      sync.Mutex
	budget  int64
	held    int64
	entries map[scalerKey]*scalerEntry
	// lru is most-recently-used last.
	lru []scalerKey
}

type scalerEntry struct {
	scaler xdraw.Scaler
	bytes  int64
}

var scalerCache = &scalerCacheT{budget: DefaultScalerCacheBytes}

// SetScalerCacheBytes sets the byte budget for cached resamplers and evicts
// down to it. Zero disables caching entirely. It is exported for the device
// measurement harness and for a caller that knows its memory is tighter than
// the default assumes.
func SetScalerCacheBytes(n int64) {
	scalerCache.mu.Lock()
	defer scalerCache.mu.Unlock()
	scalerCache.budget = n
	scalerCache.evictLocked()
}

// ScalerCacheBytes reports the bytes currently retained by cached resamplers.
func ScalerCacheBytes() int64 {
	scalerCache.mu.Lock()
	defer scalerCache.mu.Unlock()
	return scalerCache.held
}

func (c *scalerCacheT) get(k Scaler, key scalerKey, kern *xdraw.Kernel) xdraw.Scaler {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[key]; ok {
		c.touchLocked(key)
		return e.scaler
	}

	s := kern.NewScaler(key.dw, key.dh, key.sw, key.sh)

	// Too big to be worth retaining, or caching is off: hand it back uncached.
	// The scaler still works; it simply is not held, so its pooled buffer dies
	// with it.
	n := key.bytes()
	if c.budget <= 0 || n > c.budget {
		return s
	}

	if c.entries == nil {
		c.entries = make(map[scalerKey]*scalerEntry)
	}
	c.entries[key] = &scalerEntry{scaler: s, bytes: n}
	c.lru = append(c.lru, key)
	c.held += n
	c.evictLocked()
	return s
}

// evictLocked drops least-recently-used entries until the budget is met.
func (c *scalerCacheT) evictLocked() {
	for c.held > c.budget && len(c.lru) > 0 {
		oldest := c.lru[0]
		c.lru = c.lru[1:]
		if e, ok := c.entries[oldest]; ok {
			c.held -= e.bytes
			delete(c.entries, oldest)
		}
	}
}

func (c *scalerCacheT) touchLocked(key scalerKey) {
	for i, k := range c.lru {
		if k == key {
			c.lru = append(c.lru[:i], c.lru[i+1:]...)
			break
		}
	}
	c.lru = append(c.lru, key)
}

func scalerFor(k Scaler, dst, src image.Rectangle) xdraw.Scaler {
	kern, ok := k.kernel()
	if !ok {
		// ApproxBiLinear has no weight tables and no intermediate buffer.
		return k.scaler()
	}
	return scalerCache.get(k, scalerKey{k, dst.Dx(), dst.Dy(), src.Dx(), src.Dy()}, kern)
}

// fastSource converts an image x/image/draw has no specialised path for into
// *image.RGBA.
//
// image/jpeg returns *image.YCbCr, and x/image/draw's kernel scalers have fast
// paths only for RGBA, NRGBA and Gray sources; anything else falls back to a
// generic per-pixel At()/RGBA() path. image/draw's own YCbCr → RGBA conversion
// is specialised and cheap by comparison.
func fastSource(src image.Image) image.Image {
	switch src.(type) {
	case *image.RGBA, *image.NRGBA, *image.Gray:
		return src
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	stddraw.Draw(out, out.Bounds(), src, b.Min, stddraw.Src)
	return out
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
