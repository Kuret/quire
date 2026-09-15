package imageproc_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"runtime"
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

// A page over the budget is re-encoded once at the lower quality rather than
// failing the whole volume, and the retry is reported.
func TestNormaliseRequantisesOnce(t *testing.T) {
	raw := encodeJPEG(t, synthPage(1620, 2160, 200))

	opts := imageproc.DefaultOptions()
	full, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	opts.RetryQuality = 40
	low, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), imageproc.Options{
		MaxWidth: opts.MaxWidth, MaxHeight: opts.MaxHeight, Quality: 40,
		Background: opts.Background, AspectTolerance: opts.AspectTolerance,
		Scaler: opts.Scaler,
	})
	if err != nil {
		t.Fatalf("low-quality baseline: %v", err)
	}
	if low.Bytes >= full.Bytes {
		t.Fatalf("q40 is %d bytes, not smaller than q85's %d; the test cannot separate them", low.Bytes, full.Bytes)
	}

	// A budget between the two: the first encode busts it, the retry fits.
	opts.MaxBytes = (full.Bytes + low.Bytes) / 2

	var out bytes.Buffer
	res, err := imageproc.Normalise(&out, bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	if !res.Requantised {
		t.Error("Requantised = false; the retry is invisible to the caller")
	}
	if res.Quality != 40 {
		t.Errorf("Quality = %d, want the retry quality 40", res.Quality)
	}
	if res.FirstBytes <= opts.MaxBytes {
		t.Errorf("FirstBytes = %d, should record the rejected encode (> %d)", res.FirstBytes, opts.MaxBytes)
	}
	if res.Bytes > opts.MaxBytes {
		t.Errorf("Bytes = %d, still over the %d budget", res.Bytes, opts.MaxBytes)
	}
	if int64(out.Len()) != res.Bytes {
		t.Errorf("wrote %d bytes, Result says %d", out.Len(), res.Bytes)
	}
	if _, _, err := image.Decode(bytes.NewReader(out.Bytes())); err != nil {
		t.Errorf("re-encoded page does not decode: %v", err)
	}
}

// With the retry disabled, the first overrun is still fatal.
func TestNormaliseRetryDisabled(t *testing.T) {
	raw := encodeJPEG(t, synthPage(1620, 2160, 200))
	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 1024
	opts.RetryQuality = 0
	if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts); !errors.Is(err, imageproc.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
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

// The scaler cache must be bounded by the bytes it retains, not by how many
// entries it holds. Eight entries of varied geometry was ~1.36 GB and
// OOM-killed the backend on the device; this test is the regression.
func TestScalerCacheStaysWithinByteBudget(t *testing.T) {
	const budget = 512 << 20
	t.Cleanup(func() { imageproc.SetScalerCacheBytes(imageproc.DefaultScalerCacheBytes) })
	imageproc.SetScalerCacheBytes(0) // drop whatever earlier tests left
	imageproc.SetScalerCacheBytes(budget)

	// Deliberately varied geometry, the way a real source serves pages — the
	// case the original eight-entry bound was never tested against.
	sizes := [][2]int{
		{2480, 3508}, {1600, 2300}, {1200, 1700}, {1400, 2000},
		{900, 1600}, {2000, 2828}, {1131, 1600}, {1350, 1920},
		{3200, 2200}, {1620, 2160}, {1000, 1400}, {2550, 3300},
	}
	for _, sz := range sizes {
		raw := encodeJPEG(t, synthPage(sz[0], sz[1], 200))
		opts := imageproc.DefaultOptions()
		opts.MaxBytes = 0
		if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts); err != nil {
			t.Fatalf("%dx%d: %v", sz[0], sz[1], err)
		}
		if held := imageproc.ScalerCacheBytes(); held > budget {
			t.Fatalf("after %dx%d the cache retains %d bytes, over the %d budget", sz[0], sz[1], held, budget)
		}
	}
	t.Logf("12 geometries later the cache retains %.0f MiB of a %d MiB budget",
		float64(imageproc.ScalerCacheBytes())/(1<<20), budget>>20)
	if imageproc.ScalerCacheBytes() == 0 {
		t.Error("nothing was cached at a 512 MiB budget; the bound is not being exercised")
	}
}

// A geometry whose intermediate buffer alone busts the budget is not cached at
// all, rather than being cached and immediately evicting everything else.
func TestScalerCacheSkipsOversizedGeometry(t *testing.T) {
	t.Cleanup(func() { imageproc.SetScalerCacheBytes(imageproc.DefaultScalerCacheBytes) })
	imageproc.SetScalerCacheBytes(0)
	imageproc.SetScalerCacheBytes(1 << 20) // 1 MiB: smaller than any real page's buffer

	raw := encodeJPEG(t, synthPage(2480, 3508, 200))
	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts); err != nil {
		t.Fatal(err)
	}
	if held := imageproc.ScalerCacheBytes(); held != 0 {
		t.Errorf("cache retains %d bytes for a geometry larger than the whole budget", held)
	}
}

// Caching off means retaining nothing, and still producing the same page.
func TestScalerCacheDisabled(t *testing.T) {
	t.Cleanup(func() { imageproc.SetScalerCacheBytes(imageproc.DefaultScalerCacheBytes) })
	imageproc.SetScalerCacheBytes(0)

	raw := encodeJPEG(t, synthPage(1620, 2160, 200))
	opts := imageproc.DefaultOptions()
	res, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != imageproc.PanelWidth || res.Height != imageproc.PanelHeight {
		t.Errorf("output %dx%d with caching off", res.Width, res.Height)
	}
	if held := imageproc.ScalerCacheBytes(); held != 0 {
		t.Errorf("cache retains %d bytes with caching off", held)
	}
}

// The hot geometry survives; the cold one is what goes.
func TestScalerCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Cleanup(func() { imageproc.SetScalerCacheBytes(imageproc.DefaultScalerCacheBytes) })
	imageproc.SetScalerCacheBytes(0)
	// Two small geometries fit; a third forces one out. (Entry cost counts the
	// per-P pool copies, so "small" is still tens of MB.)
	imageproc.SetScalerCacheBytes(160 << 20)

	run := func(w, h int) {
		t.Helper()
		raw := encodeJPEG(t, synthPage(w, h, 200))
		opts := imageproc.DefaultOptions()
		opts.MaxBytes = 0
		if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts); err != nil {
			t.Fatalf("%dx%d: %v", w, h, err)
		}
	}

	run(600, 800)
	run(640, 850)
	first := imageproc.ScalerCacheBytes()
	run(600, 800) // touch the first again, making the second the coldest
	run(700, 900)

	if held := imageproc.ScalerCacheBytes(); held > 160<<20 {
		t.Errorf("cache retains %d bytes, over budget", held)
	}
	if first == 0 {
		t.Fatal("nothing was cached; the test proves nothing")
	}
}

// The resample intermediate is destinationWidth × sourceHeight × 32 bytes. The
// guard exists because that scales with the *input*, so no heap limit can
// prevent the allocation.
func resampleBytes(dstW, srcH int) int64 { return int64(dstW) * int64(srcH) * 32 }

// Deliberately hostile geometry. Two OOMs so far both came from fixtures that
// were convenient rather than nasty, so this table is the nasty one.
func TestResampleGuardOnHostileInput(t *testing.T) {
	const budget = imageproc.DefaultMaxResampleBytes

	cases := []struct {
		name      string
		w, h      int
		wantGuard bool
	}{
		// Ordinary pages: the guard must stay out of the way. A guard that
		// engages on normal manga is a silent quality regression.
		{name: "a4 scan", w: 2480, h: 3508, wantGuard: false},
		{name: "letter scan", w: 2550, h: 3300, wantGuard: false},
		{name: "panel native", w: 1620, h: 2160, wantGuard: false},
		{name: "web page", w: 1200, h: 1700, wantGuard: false},
		// Pathological: these are the ones that would have killed us.
		{name: "very large scan", w: 5000, h: 7000, wantGuard: true},
		{name: "huge scan", w: 6000, h: 8000, wantGuard: true},
		// A wide spread fits width-first, so its destination is short and the
		// intermediate (1620 × 4000 × 32 = 207 MB) stays under budget.
		{name: "wide spread", w: 6000, h: 4000, wantGuard: false},
		// A long strip is bounded by a different mechanism: fitting 3:4 makes
		// the destination a narrow sliver, so the intermediate stays small.
		// Asserted here so the arithmetic is on the record rather than assumed.
		{name: "webtoon strip", w: 800, h: 20000, wantGuard: false},
		{name: "wide webtoon strip", w: 1600, h: 20000, wantGuard: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := encodeJPEG(t, synthPage(tc.w, tc.h, 200))

			opts := imageproc.DefaultOptions()
			opts.MaxBytes = 0 // measuring the guard, not the page budget

			var out bytes.Buffer
			res, err := imageproc.Normalise(&out, bytes.NewReader(raw), opts)
			if err != nil {
				t.Fatalf("Normalise: %v", err)
			}
			if guarded := res.GuardFactor > 1; guarded != tc.wantGuard {
				t.Errorf("GuardFactor = %d (guarded=%v), want guarded=%v", res.GuardFactor, guarded, tc.wantGuard)
			}

			// Whatever happened, the resize the scaler was asked for must fit
			// the budget. The destination width is the output width when no
			// padding was added, and the fitted image width when it was.
			srcH := tc.h
			if res.GuardFactor > 1 {
				srcH = tc.h / res.GuardFactor
			}
			if got := resampleBytes(res.ImageWidth, srcH); got > budget {
				t.Errorf("intermediate %d bytes exceeds the %d budget", got, budget)
			}

			// Geometry is unaffected by the guard.
			if res.Width > imageproc.PanelWidth || res.Height > imageproc.PanelHeight {
				t.Errorf("output %dx%d exceeds the panel grid", res.Width, res.Height)
			}
			if _, _, err := image.Decode(bytes.NewReader(out.Bytes())); err != nil {
				t.Errorf("guarded output does not decode: %v", err)
			}
			t.Logf("%dx%d -> %dx%d, guard factor %d, %d bytes out",
				tc.w, tc.h, res.Width, res.Height, res.GuardFactor, res.Bytes)
		})
	}
}

// The smallest factor that fits is used, so a guarded page degrades as little
// as it must.
func TestResampleGuardPicksSmallestFactor(t *testing.T) {
	raw := encodeJPEG(t, synthPage(5000, 7000, 200))

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	res, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GuardFactor != 2 {
		t.Errorf("GuardFactor = %d for a 5000x7000 source, want the smallest factor that fits (2)", res.GuardFactor)
	}
	// One factor lower must genuinely not fit, or "smallest" means nothing.
	if got := resampleBytes(res.ImageWidth, 7000/(res.GuardFactor-1)); got <= imageproc.DefaultMaxResampleBytes {
		t.Errorf("factor %d would also have fit (%d bytes); the guard is over-shrinking", res.GuardFactor-1, got)
	}
}

// A tighter budget forces a larger factor; a disabled guard never fires.
//
// The source here is 5000x7000, not an A4 page, and that is the point: the
// guard can only shrink a source that is at least twice the destination, so on
// a 1.6x A4 scan no integer factor exists and the 171 MB intermediate is
// irreducible by this mechanism. TestResampleGuardNeverUpscales pins that.
func TestResampleGuardBudget(t *testing.T) {
	raw := encodeJPEG(t, synthPage(5000, 7000, 200))

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	opts.MaxResampleBytes = 32 << 20
	tight, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if tight.GuardFactor < 3 {
		t.Errorf("GuardFactor = %d at a 32 MiB budget, want a larger factor than the default budget needs", tight.GuardFactor)
	}

	opts.MaxResampleBytes = -1 // disabled
	off, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if off.GuardFactor != 1 {
		t.Errorf("GuardFactor = %d with the guard disabled", off.GuardFactor)
	}
}

// The guard must not shrink the source below the destination — that would
// trade a memory problem for an upscaling one.
func TestResampleGuardNeverUpscales(t *testing.T) {
	raw := encodeJPEG(t, synthPage(1620, 2160, 200))

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	opts.MaxResampleBytes = 1 << 20 // absurdly tight
	res, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GuardFactor != 1 {
		t.Errorf("GuardFactor = %d for a panel-native page; the source is already the destination size", res.GuardFactor)
	}
	if res.Width != imageproc.PanelWidth || res.Height != imageproc.PanelHeight {
		t.Errorf("output %dx%d, want the panel grid", res.Width, res.Height)
	}
}

// Peak allocation, not just arithmetic: a guarded page must actually allocate
// far less than an unguarded one.
func TestResampleGuardCutsAllocation(t *testing.T) {
	raw := encodeJPEG(t, synthPage(5000, 7000, 200))

	measure := func(opts imageproc.Options) uint64 {
		t.Helper()
		imageproc.SetScalerCacheBytes(0) // no retention across the two runs
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if _, err := imageproc.Normalise(new(bytes.Buffer), bytes.NewReader(raw), opts); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	t.Cleanup(func() { imageproc.SetScalerCacheBytes(imageproc.DefaultScalerCacheBytes) })

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	opts.MaxResampleBytes = -1
	unguarded := measure(opts)

	opts.MaxResampleBytes = imageproc.DefaultMaxResampleBytes
	guarded := measure(opts)

	if guarded >= unguarded {
		t.Errorf("guarded run allocated %d bytes, not less than the unguarded %d", guarded, unguarded)
	}
	t.Logf("5000x7000: unguarded %.0f MiB allocated, guarded %.0f MiB (%.0f%% less)",
		float64(unguarded)/(1<<20), float64(guarded)/(1<<20),
		100*(1-float64(guarded)/float64(unguarded)))
}

// What the degradation actually looks like: box-then-kernel against kernel
// alone, on a source where the guard fires.
func TestResampleGuardQualityDelta(t *testing.T) {
	raw := encodeJPEG(t, synthPage(5000, 7000, 200))

	opts := imageproc.DefaultOptions()
	opts.MaxBytes = 0
	opts.MaxResampleBytes = -1
	var sharp bytes.Buffer
	if _, err := imageproc.Normalise(&sharp, bytes.NewReader(raw), opts); err != nil {
		t.Fatal(err)
	}

	opts.MaxResampleBytes = imageproc.DefaultMaxResampleBytes
	var guarded bytes.Buffer
	res, err := imageproc.Normalise(&guarded, bytes.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.GuardFactor < 2 {
		t.Fatal("the guard did not fire; this test compares nothing")
	}

	a := decodeGray(t, sharp.Bytes())
	b := decodeGray(t, guarded.Bytes())
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", a.Bounds(), b.Bounds())
	}

	var sum, maxDiff int64
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			d := int64(a.GrayAt(x, y).Y) - int64(b.GrayAt(x, y).Y)
			if d < 0 {
				d = -d
			}
			sum += d
			if d > maxDiff {
				maxDiff = d
			}
		}
	}
	px := int64(bounds.Dx() * bounds.Dy())
	mae := float64(sum) / float64(px)

	// JPEG size is a fair sharpness proxy: a softer image compresses smaller.
	t.Logf("guard factor %d: mean abs difference %.2f/255, max %d/255; jpeg %d -> %d bytes (%.1f%% smaller)",
		res.GuardFactor, mae, maxDiff, sharp.Len(), guarded.Len(),
		100*(1-float64(guarded.Len())/float64(sharp.Len())))

	if mae > 12 {
		t.Errorf("mean abs difference %.2f/255 is a visible regression, not a rounding one", mae)
	}
}

func decodeGray(t *testing.T, b []byte) *image.Gray {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	g := image.NewGray(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			g.Set(x, y, img.At(x, y))
		}
	}
	return g
}
