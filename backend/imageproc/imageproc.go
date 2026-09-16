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

	// MaxResampleBytes caps the intermediate buffer a single resize may
	// allocate. Zero means DefaultMaxResampleBytes; negative disables the
	// guard. See guardFactor.
	MaxResampleBytes int64

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

// DefaultMaxResampleBytes caps the intermediate buffer one resize may
// allocate, before the guard in guardFactor steps in.
//
// Chosen from the arithmetic, not from taste. The intermediate is
// destinationWidth × sourceHeight × 32 bytes, so ordinary pages land at:
//
//	2480 × 3508 A4 scan       1527 × 3508 × 32 = 171 MB
//	2550 × 3300 letter scan   1620 × 3300 × 32 = 171 MB
//	2000 × 2828 web scan      1527 × 2828 × 32 = 138 MB
//	1620 × 2160 panel-native  1620 × 2160 × 32 = 112 MB
//
// 256 MiB sits ~50% above the worst ordinary page, so the guard never fires on
// normal manga — a guard that quietly engaged on everything would be a silent
// quality regression — while bounding two concurrent encodes (the default) to
// 512 MiB of intermediate, which is the soft heap limit the queue sets.
const DefaultMaxResampleBytes = 256 << 20

// DefaultOptions returns the panel-native settings: 1620 × 2160, JPEG q85
// (q65 on a retry), white padding, 1% aspect tolerance, CatmullRom resampling.
func DefaultOptions() Options {
	return Options{
		MaxWidth:         PanelWidth,
		MaxHeight:        PanelHeight,
		Quality:          85,
		RetryQuality:     DefaultRetryQuality,
		Background:       color.White,
		AspectTolerance:  0.01,
		MaxBytes:         DefaultMaxPageBytes,
		MaxResampleBytes: DefaultMaxResampleBytes,
		Scaler:           ScalerCatmullRom,
	}
}

func (o Options) maxResampleBytes() int64 {
	switch {
	case o.MaxResampleBytes == 0:
		return DefaultMaxResampleBytes
	case o.MaxResampleBytes < 0:
		return 0
	default:
		return o.MaxResampleBytes
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

	// GuardFactor is the integer box pre-downscale the memory guard applied
	// before resampling, or 1 when it did not fire. See guardFactor.
	GuardFactor int

	// ImageWidth and ImageHeight are the content box inside the output: the
	// same as Width/Height unless padding was added, in which case they are
	// the fitted image and the rest is background. This is also the rectangle
	// the resampler actually wrote, so it — not Width — is what the
	// intermediate's size is computed from.
	ImageWidth, ImageHeight int
}

// Normalise decodes src, fits it to the panel grid and writes a JPEG to dst.
//
// The returned Result reports what was actually written. On ErrTooLarge the
// bytes have already been written to dst; the caller owns discarding them,
// which is cheap because callers write to a temp file anyway.
func Normalise(dst io.Writer, src io.Reader, opts Options) (Result, error) {
	if err := opts.check(); err != nil {
		return Result{}, err
	}
	img, format, err := image.Decode(src)
	if err != nil {
		return Result{}, fmt.Errorf("imageproc: decode: %w", err)
	}
	return normalise(dst, img, format, opts)
}

// NormaliseImage is Normalise for an image already in memory.
//
// It exists for the strip splitter (PLAN §12.3): a tall strip is decoded once,
// cut into pieces, and each piece normalised from the decoded pixels. Going
// back through an encoder between the cut and the fit would cost a JPEG
// generation for nothing — and, more to the point, PLAN §12.3 requires the
// split to happen *before* the fit-and-pad resize, so that each piece is
// resized rather than the whole strip. Result.SrcFormat is empty here: the
// caller knows the source format and this function does not.
func NormaliseImage(dst io.Writer, img image.Image, opts Options) (Result, error) {
	if err := opts.check(); err != nil {
		return Result{}, err
	}
	return normalise(dst, img, "", opts)
}

// check validates and fills the options both entry points share.
func (o *Options) check() error {
	if o.MaxWidth <= 0 || o.MaxHeight <= 0 {
		return fmt.Errorf("imageproc: bad target size %dx%d", o.MaxWidth, o.MaxHeight)
	}
	if o.Quality <= 0 || o.Quality > 100 {
		return fmt.Errorf("imageproc: bad quality %d", o.Quality)
	}
	if o.Background == nil {
		o.Background = color.White
	}
	return nil
}

func normalise(dst io.Writer, img image.Image, format string, opts Options) (Result, error) {
	out, padded, guard, imgRect := fit(img, opts)

	sb := img.Bounds()
	ob := out.Bounds()
	res := Result{
		Width:       ob.Dx(),
		Height:      ob.Dy(),
		SrcWidth:    sb.Dx(),
		SrcHeight:   sb.Dy(),
		SrcFormat:   format,
		Padded:      padded,
		Quality:     opts.Quality,
		GuardFactor: guard,
		ImageWidth:  imgRect.Dx(),
		ImageHeight: imgRect.Dy(),
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
func fit(src image.Image, opts Options) (image.Image, bool, int, image.Rectangle) {
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
		r := image.Rect(0, 0, w, h)
		out, guard := render(src, r, r, opts)
		return out, false, guard, r
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
	out, guard := render(src, dstRect, imgRect, opts)
	return out, true, guard, imgRect
}

// render draws src scaled into imgRect on a canvas of dstRect, filling the
// remainder with the background colour.
func render(src image.Image, dstRect, imgRect image.Rectangle, opts Options) (image.Image, int) {
	// Bound the resampler's intermediate *before* converting or scaling: the
	// box pass reads the source once and everything downstream then works on a
	// smaller image.
	guard := guardFactor(imgRect, src.Bounds(), opts.maxResampleBytes())
	if guard > 1 {
		src = boxDownsample(src, guard)
	}
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
	return dst, guard
}

// resampleBytes is the intermediate x/image's kernel scalers allocate for one
// Scale call: one [4]float64 per (destination column × source row).
func resampleBytes(dstW, srcH int) int64 {
	return int64(dstW) * int64(srcH) * 32
}

// guardFactor returns the integer box pre-downscale needed to keep a single
// resize's intermediate under budget, or 1 when none is needed.
//
// # This is a memory guard, not the speed trick that was removed
//
// An earlier box pre-pass tried to make ordinary 1.5–3× downscales faster,
// measured as doing nothing, and was deleted. This is a different thing with a
// different job: it only engages on pathological sources, and it exists to
// bound memory, not time. Do not delete it for the old reason.
//
// The reason it is needed at all is that the intermediate is
// destinationWidth × **sourceHeight** × 32 — it scales with the *input*, so no
// soft heap limit can prevent the allocation; the limit only decides how hard
// the GC works around it. A 5000 × 7000 scan needs 346 MB, an 8000 × 10000 one
// 518 MB, and with two encode workers that is a gigabyte of transient the
// device does not have.
//
// Integer factors only: an exact N×N box average is seam-free and trivially
// correct, which is why it is preferred here to banding the resample. The
// smallest factor that fits the budget is used, so a guarded page degrades as
// little as it must. The factor is also capped so the box result never falls
// below the destination size — shrinking past that would mean upscaling
// afterwards, trading a memory problem for a quality one.
func guardFactor(dst, src image.Rectangle, budget int64) int {
	if budget <= 0 {
		return 1
	}
	dw, dh := dst.Dx(), dst.Dy()
	sw, sh := src.Dx(), src.Dy()
	if dw <= 0 || dh <= 0 || sw <= 0 || sh <= 0 {
		return 1
	}
	need := resampleBytes(dw, sh)
	if need <= budget {
		return 1
	}

	f := int((need + budget - 1) / budget)
	// Never shrink below the destination.
	if maxF := min(sw/dw, sh/dh); f > maxF {
		f = maxF
	}
	if f < 2 {
		return 1
	}
	return f
}

// boxDownsample averages src down by an exact integer factor.
//
// Every source pixel is read exactly once and contributes to exactly one
// output pixel, so there is no seam, no phase error and no kernel support to
// reason about — the property that makes this safe as a guard.
func boxDownsample(src image.Image, factor int) image.Image {
	b := src.Bounds()
	w, h := b.Dx()/factor, b.Dy()/factor
	if w < 1 || h < 1 || factor < 2 {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(factor * factor)
	for y := range h {
		for x := range w {
			var r, g, bl uint32
			for dy := range factor {
				for dx := range factor {
					pr, pg, pb, _ := src.At(b.Min.X+x*factor+dx, b.Min.Y+y*factor+dy).RGBA()
					r += pr >> 8
					g += pg >> 8
					bl += pb >> 8
				}
			}
			i := out.PixOffset(x, y)
			out.Pix[i+0] = uint8(r / n)
			out.Pix[i+1] = uint8(g / n)
			out.Pix[i+2] = uint8(bl / n)
			out.Pix[i+3] = 0xff
		}
	}
	return out
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
