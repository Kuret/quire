package imageproc_test

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"testing"

	"github.com/rickl/quire/backend/imageproc"
)

// strip builds one tall synthetic webtoon: panels of solid colour separated by
// bands of white gutter, with a little texture inside each panel so a row
// through a panel is never mistaken for a gutter.
//
// Built here rather than taken from a site: the point of these tests is that
// the geometry coming out is right for a geometry we chose going in.
func strip(w int, panels []int, gutter int) *image.RGBA {
	h := 0
	for _, p := range panels {
		h += p + gutter
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// White throughout: the gutters are what is left when panels are drawn.
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	y := gutter / 2
	for i, p := range panels {
		base := uint8(40 + 30*(i%6))
		for row := range p {
			for x := range w {
				// Texture that varies across the row and down it, so no row
				// inside a panel is uniform.
				v := base + uint8((x*7+row*13)%40)
				img.Set(x, y+row, color.RGBA{v, v, uint8(int(v) + (x%5)*3), 255})
			}
		}
		y += p + gutter
	}
	return img
}

// slice cuts an image into n equal chunks — a site's mechanical slicing, which
// pays no attention to where the panels are.
func slice(src *image.RGBA, n int) []image.Image {
	b := src.Bounds()
	h := b.Dy() / n
	out := make([]image.Image, 0, n)
	for i := range n {
		from := b.Min.Y + i*h
		to := from + h
		if i == n-1 {
			to = b.Max.Y
		}
		out = append(out, src.SubImage(image.Rect(b.Min.X, from, b.Max.X, to)))
	}
	return out
}

// page builds an ordinary manga page: content with a white margin all round, so
// its top and bottom rows are background and its seams are clean.
func page(w, h int, seed int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	margin := h / 12
	for y := margin; y < h-margin; y++ {
		for x := w / 20; x < w-w/20; x++ {
			v := uint8(30 + (x*3+y*5+seed*17)%120)
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

func scanOf(t *testing.T, imgs []image.Image, opts imageproc.RestitchOptions) *imageproc.StripScan {
	t.Helper()
	s := imageproc.NewStripScan(opts)
	for _, img := range imgs {
		s.Add(img)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return s
}

// The case this exists for: a strip the source cut into equal chunks, through
// the art. It must be recognised, rejoined and re-cut on the gutters.
func TestAPreSlicedStripIsRejoinedAndCutOnGutters(t *testing.T) {
	const w = 600
	// Panels of assorted heights, so the source's equal chunks cannot line up
	// with them.
	src := strip(w, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40)
	imgs := slice(src, 10)

	scan := scanOf(t, imgs, imageproc.RestitchOptions{})
	pages, v := scan.Plan()
	if !v.PreSliced {
		t.Fatalf("a mechanically sliced strip was not recognised: %s", v.Reason)
	}
	if len(pages) == 0 {
		t.Fatal("recognised, but planned no pages")
	}

	// Every cut but the last must have landed in a gutter — that is the whole
	// point of re-cutting rather than passing the source's cuts on.
	for i, p := range pages {
		if i == len(pages)-1 {
			continue
		}
		if !p.Gutter {
			t.Errorf("page %d ends on a least-energy row, not a gutter", i)
		}
	}

	// And the pages must cover every row of *art*, once, in order. The blank
	// margin the strip starts and ends with is the source's padding and is
	// dropped on purpose — see the trim in plan(), which exists because a
	// blank tail was otherwise merged onto the last page.
	total := 0
	for _, img := range imgs {
		total += img.Bounds().Dy()
	}
	const gutter = 40
	wantRows := total - gutter/2 - (gutter - gutter/2) // the leading and trailing blank

	got, prev := 0, imageproc.Span{Index: -1}
	for _, p := range pages {
		for _, sp := range p.Spans {
			if sp.Index < prev.Index || (sp.Index == prev.Index && sp.From < prev.To) {
				t.Fatalf("spans go backwards: %+v after %+v", sp, prev)
			}
			got += sp.To - sp.From
			prev = sp
		}
	}
	// Rounding through the virtual-strip coordinates costs at most a row per
	// image boundary.
	if math.Abs(float64(got-wantRows)) > float64(len(imgs)) {
		t.Errorf("the pages cover %d rows of the %d that hold art (%d in total)", got, wantRows, total)
	}
}

// The failure that matters more: an ordinary page-per-image manga must be left
// completely alone.
func TestAPagePerImageMangaIsLeftAlone(t *testing.T) {
	var imgs []image.Image
	for i := range 12 {
		imgs = append(imgs, page(800, 1130, i))
	}

	scan := scanOf(t, imgs, imageproc.RestitchOptions{})
	pages, v := scan.Plan()
	if v.PreSliced {
		t.Fatalf("an ordinary manga was treated as a sliced strip: %s", v.Reason)
	}
	if pages != nil {
		t.Errorf("planned %d pages for a chapter that should be untouched", len(pages))
	}
	if v.Clean == 0 {
		t.Error("no seam was recognised as a clean page boundary")
	}
}

// A chapter whose images differ in shape is not mechanically sliced, however
// continuous its seams happen to look.
func TestAChapterCarryingASpreadIsRefused(t *testing.T) {
	const w = 600
	src := strip(w, []int{500, 300, 700, 400, 650, 350}, 40)
	imgs := slice(src, 8)
	// One double-page spread among them: twice as wide, half as tall.
	imgs = append(imgs, strip(w*2, []int{300}, 20))

	scan := scanOf(t, imgs, imageproc.RestitchOptions{})
	_, v := scan.Plan()
	if v.PreSliced {
		t.Fatal("a chapter with a spread in it was treated as a sliced strip")
	}
	if v.MaxAspect/v.MinAspect <= 1 {
		t.Errorf("the aspects were reported as uniform: %.3f to %.3f", v.MinAspect, v.MaxAspect)
	}
}

// A tall unbroken panel must be scaled to fit, not sliced through. The strip
// here has one panel far taller than a page and no gutter inside it.
func TestATallUnbrokenPanelIsNotCutThrough(t *testing.T) {
	const w = 600
	pageH := int(float64(w) * 4 / 3)
	src := strip(w, []int{400, 300, pageH * 2, 400, 350, 300}, 40)
	imgs := slice(src, 9)

	scan := scanOf(t, imgs, imageproc.RestitchOptions{})
	pages, v := scan.Plan()
	if !v.PreSliced {
		t.Fatalf("not recognised: %s", v.Reason)
	}

	// The tall panel's rows live in one page: no cut may land inside it. Its
	// virtual rows are known from how the strip was built.
	from := 40/2 + 400 + 40 + 300 + 40
	to := from + pageH*2
	for i, p := range pages {
		if i == len(pages)-1 {
			break
		}
		end := 0
		for _, sp := range p.Spans {
			end = sp.To + sp.Index*imgs[0].Bounds().Dy()
		}
		if end > from+80 && end < to-80 {
			t.Errorf("page %d cuts through the unbroken panel at row %d (panel %d..%d)",
				i, end, from, to)
		}
	}
}

// The user's actual complaint: "a small strip on a separate page which only
// contained dead space between two panels and a small part of the panel above
// and below it". A page that is nearly all gutter must not be emitted.
//
// The band of dead space here is deliberately more than a page tall, so that
// without the merge rule a whole page lands inside it with nothing on it. An
// earlier version of this test used a 260-row gutter and passed with the merge
// disabled — it was asserting nothing.
func TestAPageOfDeadSpaceIsFoldedIntoItsNeighbour(t *testing.T) {
	const w = 600
	pageH := int(float64(w) * 4 / 3) // 800

	// Two panels with a blank band two and a half pages tall between them: an
	// author's pause, a chapter break, the end of a scene.
	src := image.NewRGBA(image.Rect(0, 0, w, 0))
	src = strip(w, []int{900, 700}, 40)
	blank := image.NewRGBA(image.Rect(0, 0, w, pageH*5/2))
	for y := range blank.Bounds().Dy() {
		for x := range w {
			blank.Set(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	tail := strip(w, []int{800, 600, 900}, 40)

	// One strip: panels, dead space, panels.
	h := src.Bounds().Dy() + blank.Bounds().Dy() + tail.Bounds().Dy()
	whole := image.NewRGBA(image.Rect(0, 0, w, h))
	y := 0
	for _, part := range []*image.RGBA{src, blank, tail} {
		for row := range part.Bounds().Dy() {
			for x := range w {
				whole.Set(x, y, part.At(x, row))
			}
			y++
		}
	}
	imgs := slice(whole, 8)

	scan := scanOf(t, imgs, imageproc.RestitchOptions{})
	pages, v := scan.Plan()
	if !v.PreSliced {
		t.Fatalf("not recognised: %s", v.Reason)
	}

	// Render and measure what a reader would actually turn to.
	err := imageproc.RenderPages(pages, scan.PlanWidth(),
		func(i int) (image.Image, error) { return imgs[i], nil },
		func(n int, img image.Image) error {
			if content := contentFraction(img); content < 0.10 {
				return fmt.Errorf("page %d of %d is %.0f%% dead space (%d rows)",
					n, len(pages), (1-content)*100, img.Bounds().Dy())
			}
			return nil
		})
	if err != nil {
		t.Error(err)
	}
}

// Rendering holds two images, not a chapter. The OOM at 1.63 GB earlier in this
// project came from the opposite assumption.
//
// **The fixture must not hold the chapter either**, or the measurement is of
// the test rather than of the renderer: the first version of this test kept 28
// decoded images in a slice and dutifully reported their 163 MiB. So the images
// are painted on demand here, exactly as a decoder would produce them from
// disk, and dropped as soon as they are done with.
func TestRenderingHoldsOnlyTwoImages(t *testing.T) {
	const (
		w      = 1080
		sliceH = 1440
		count  = 28
	)
	// One synthetic strip, painted a chunk at a time. The panel heights repeat
	// with a long period so the source's equal chunks never line up with them.
	gutter := 60
	panels := []int{900, 700, 1200, 800, 1100, 600, 900, 1300, 700, 1000,
		800, 1200, 900, 700, 1100, 800, 950, 1050, 700, 900,
		1150, 750, 1000, 850, 900, 1250, 700, 1100}
	chunk := func(i int) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, w, sliceH))
		for y := range sliceH {
			paintRow(img, y, i*sliceH+y, panels, gutter, w)
		}
		return img
	}

	scan := imageproc.NewStripScan(imageproc.RestitchOptions{})
	for i := range count {
		scan.Add(chunk(i))
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	pages, v := scan.Plan()
	if !v.PreSliced {
		t.Fatalf("not recognised: %s", v.Reason)
	}

	runtime.GC()
	var before, stats runtime.MemStats
	runtime.ReadMemStats(&before)

	var rendered, pageRows int
	var peak uint64
	err := imageproc.RenderPages(pages, scan.PlanWidth(),
		func(i int) (image.Image, error) { return chunk(i), nil },
		func(n int, img image.Image) error {
			rendered++
			// Two things this has to get right, both learned by getting them
			// wrong: HeapAlloc counts garbage that has not been collected yet,
			// so a GC comes first (sampling without it measured 95 MiB of dead
			// source images); and the page must be *kept alive* across that GC,
			// or Go's precise stack liveness collects it while it is nominally
			// in hand and the figure comes back as one source image, 6 MiB,
			// with the page missing from it.
			runtime.GC()
			runtime.ReadMemStats(&stats)
			if live := stats.HeapAlloc; live > peak {
				peak = live
			}
			b := img.Bounds()
			pageRows = max(pageRows, b.Dy())
			runtime.KeepAlive(img)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if rendered == 0 {
		t.Fatal("no pages were rendered")
	}

	whole := float64(w*sliceH*count*4) / (1 << 20)
	oneSource := float64(w*sliceH*4) / (1 << 20)
	tallestPage := float64(w*pageRows*4) / (1 << 20)
	t.Logf("%d images of %d×%d re-cut into %d pages; peak live heap %.1f MiB "+
		"(one source %.1f + tallest page %.1f = %.1f expected), "+
		"against %.0f MiB for the chapter in one buffer",
		count, w, sliceH, rendered, float64(peak)/(1<<20),
		oneSource, tallestPage, oneSource+tallestPage, whole)

	// The figure has to be explicable, not merely small: a peak well *below*
	// one source plus one page means the measurement lost something.
	if float64(peak)/(1<<20) < oneSource {
		t.Errorf("peak %.1f MiB is less than a single source image (%.1f MiB); "+
			"the measurement is not seeing what is live", float64(peak)/(1<<20), oneSource)
	}

	// One source (1080×1440 RGBA ≈ 5.9 MiB) plus one page (up to three pages'
	// worth ≈ 17.8 MiB) plus the profiles. The ceiling is a generous multiple
	// of that and a small fraction of the 166 MiB the chapter would cost whole.
	const ceiling = 64 << 20
	if peak > ceiling {
		t.Errorf("peak heap %.1f MiB, want under %d MiB", float64(peak)/(1<<20), ceiling>>20)
	}
}

// paintRow paints row y of an image from row strip of the synthetic strip, so a
// chunk can be produced without the strip existing anywhere.
func paintRow(img *image.RGBA, y, strip int, panels []int, gutter, w int) {
	at, idx := 0, -1
	row := 0
	for i, p := range panels {
		if strip < at+gutter {
			idx = -1
			break
		}
		if strip < at+gutter+p {
			idx, row = i, strip-(at+gutter)
			break
		}
		at += gutter + p
	}
	for x := range w {
		if idx < 0 {
			img.Set(x, y, color.RGBA{250, 250, 250, 255})
			continue
		}
		base := uint8(40 + 30*(idx%6))
		v := base + uint8((x*7+row*13)%40)
		img.Set(x, y, color.RGBA{v, v, uint8(int(v) + (x%5)*3), 255})
	}
}

// contentFraction is the share of rows that are not flat background.
func contentFraction(img image.Image) float64 {
	b := img.Bounds()
	content := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		var first uint32
		flat := true
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			v := (r*299 + g*587 + bl*114) / 1000 >> 8
			if x == b.Min.X {
				first = v
				continue
			}
			if v > first+6 || v+6 < first {
				flat = false
				break
			}
		}
		if !flat {
			content++
		}
	}
	if b.Dy() == 0 {
		return 0
	}
	return float64(content) / float64(b.Dy())
}
