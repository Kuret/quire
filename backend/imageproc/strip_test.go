package imageproc_test

// Strip splitting tests — PLAN §12.3, under §9's circular-fixture warning.
//
// Every strip here is synthetic, because §1.3 forbids committing source
// content. §9's warning is that a fixture you generated yourself only tests the
// shape you imagined, so these are built to be *awkward*: gutters at irregular
// intervals, a strip with no usable gutter anywhere, a single panel taller than
// a page, and a chapter where exactly one image is tall.
//
// The load-bearing trick is that each strip is built from a recorded list of
// gutter bands, so a test can assert a cut landed *inside a band the fixture
// planted* rather than merely that some cut happened. A test that only checked
// "it split into N pieces" would pass on a splitter that cut at fixed offsets
// and sliced every panel in half.

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/rickl/quire/backend/imageproc"
)

// band is a planted gutter: rows [Start, End).
type band struct{ Start, End int }

func (b band) contains(y int) bool { return y >= b.Start && y < b.End }

// noisy fills rows [y0, y1) with deterministic per-pixel noise, so no row in a
// panel can pass the uniformity test by accident.
func noisy(img *image.RGBA, w, y0, y1 int, seed uint32) {
	s := seed | 1
	for y := y0; y < y1; y++ {
		for x := range w {
			s = s*1664525 + 1013904223
			v := uint8(40 + (s>>16)%176)
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
}

func flat(img *image.RGBA, w, y0, y1 int, v uint8) {
	for y := y0; y < y1; y++ {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
}

// buildStrip lays out panels of the given heights separated by gutter bands of
// gutterH flat rows, and returns the image with the bands it planted.
func buildStrip(w int, panels []int, gutterH int) (*image.RGBA, []band) {
	h := 0
	for i, p := range panels {
		h += p
		if i < len(panels)-1 {
			h += gutterH
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var bands []band
	y := 0
	for i, p := range panels {
		noisy(img, w, y, y+p, uint32(i*7919+13))
		y += p
		if i < len(panels)-1 {
			flat(img, w, y, y+gutterH, 245)
			bands = append(bands, band{y, y + gutterH})
			y += gutterH
		}
	}
	return img, bands
}

// checkCoverage is the invariant every split must hold, whatever it cut on:
// the pieces run top to bottom, cover every row, and overlap rather than gap.
func checkCoverage(t *testing.T, img image.Image, rects []image.Rectangle) {
	t.Helper()
	b := img.Bounds()
	if len(rects) == 0 {
		t.Fatal("no pieces")
	}
	if rects[0].Min.Y != b.Min.Y {
		t.Errorf("first piece starts at %d, want the top edge %d", rects[0].Min.Y, b.Min.Y)
	}
	if last := rects[len(rects)-1]; last.Max.Y != b.Max.Y {
		t.Errorf("last piece ends at %d, want the bottom edge %d", last.Max.Y, b.Max.Y)
	}
	for i, r := range rects {
		if r.Dy() <= 0 {
			t.Fatalf("piece %d is empty: %v", i, r)
		}
		if r.Min.X != b.Min.X || r.Max.X != b.Max.X {
			t.Errorf("piece %d is %d..%d wide; a strip is only ever cut horizontally", i, r.Min.X, r.Max.X)
		}
		if i > 0 {
			prev := rects[i-1]
			if r.Min.Y > prev.Max.Y {
				t.Errorf("piece %d starts at %d but %d ended at %d: rows %d..%d are lost",
					i, r.Min.Y, i-1, prev.Max.Y, prev.Max.Y, r.Min.Y)
			}
			if r.Min.Y >= prev.Min.Y+prev.Dy() {
				// no overlap at all
				t.Errorf("piece %d does not overlap piece %d; a panel across the cut is lost to both", i, i-1)
			}
			if r.Min.Y <= prev.Min.Y {
				t.Fatalf("piece %d does not advance past piece %d", i, i-1)
			}
		}
	}
}

// TestSplitCutsLandInPlantedGuttersAtRegularIntervals is the easy case, and it
// is here to assert the *positions*, not the count.
func TestSplitCutsLandInPlantedGuttersAtRegularIntervals(t *testing.T) {
	img, bands := buildStrip(800, repeat(500, 10), 24)
	rects, cuts := imageproc.SplitStrip(img, imageproc.SplitOptions{})

	if len(rects) < 4 {
		t.Fatalf("a %d-row strip became %d pieces; it should be about 5", img.Bounds().Dy(), len(rects))
	}
	checkCoverage(t, img, rects)

	for i, c := range cuts {
		if !inAnyBand(bands, c.Y) {
			t.Errorf("cut %d at row %d is inside a panel; planted gutters are at %v", i, c.Y, bands)
		}
		if !c.Gutter {
			t.Errorf("cut %d at row %d was reported as an energy fallback, but the window contains a gutter", i, c.Y)
		}
	}
}

// TestSplitCutsLandInGuttersAtIrregularIntervals is the awkward one. The panel
// heights are deliberately uneven, so a splitter that cuts at a fixed pitch
// walks straight into a panel, and one window contains no gutter at all — so a
// single strip exercises both the gutter path and the fallback.
func TestSplitCutsLandInGuttersAtIrregularIntervals(t *testing.T) {
	panels := []int{420, 900, 1500, 260, 380, 1100, 640, 310}
	img, bands := buildStrip(800, panels, 18)

	rects, cuts := imageproc.SplitStrip(img, imageproc.SplitOptions{})
	checkCoverage(t, img, rects)

	gutterCuts := 0
	for i, c := range cuts {
		if c.Gutter {
			gutterCuts++
			if !inAnyBand(bands, c.Y) {
				t.Errorf("cut %d claims to be a gutter at row %d, but no band contains it (bands %v)", i, c.Y, bands)
			}
		}
	}
	if gutterCuts == 0 {
		t.Errorf("not one of %d cuts found a planted gutter; the gutter search is not working", len(cuts))
	}
	if gutterCuts == len(cuts) {
		t.Logf("note: every cut found a gutter (%d of %d) — the 1500px panel window was reachable after all", gutterCuts, len(cuts))
	}
	t.Logf("irregular strip: %d pieces, %d of %d cuts landed in a planted gutter", len(rects), gutterCuts, len(cuts))
}

// TestSplitFallsBackToLeastEnergyWithNoGutter is the degradation case. There is
// no flat row anywhere, so every cut must come from the minimum-energy seam —
// and must still be inside the search window, not at some arbitrary row.
func TestSplitFallsBackToLeastEnergyWithNoGutter(t *testing.T) {
	const w, h = 800, 6000
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	noisy(img, w, 0, h, 991)

	rects, cuts := imageproc.SplitStrip(img, imageproc.SplitOptions{})
	checkCoverage(t, img, rects)
	if len(cuts) == 0 {
		t.Fatal("a 7.5:1 strip with no gutters was not cut at all; the fallback must always find something")
	}
	for i, c := range cuts {
		if c.Gutter {
			t.Errorf("cut %d at row %d claims a gutter in an image with no flat row", i, c.Y)
		}
	}

	// Every cut must sit within the ±Window of its target, which is what keeps
	// pages from coming out wildly uneven when the fallback is doing the work.
	pieceH := pieceHeight(w)
	win := int(float64(pieceH)*imageproc.DefaultSplitWindow + 0.5)
	for i, r := range rects[:len(rects)-1] {
		if d := abs(r.Dy() - pieceH); d > win+1 {
			t.Errorf("piece %d is %d rows, %d away from the %d-row target (window %d)", i, r.Dy(), d, pieceH, win)
		}
	}
	t.Logf("no-gutter strip: %d pieces, heights %v (target %d, window ±%d), all cuts by minimum energy",
		len(rects), heights(rects), pieceH, win)
}

// TestSplitCutsASinglePanelTallerThanAPage is the unavoidable case PLAN §12.3
// states honestly: a full-height splash must be cut or scaled below legibility.
func TestSplitCutsASinglePanelTallerThanAPage(t *testing.T) {
	// One panel, 3.5 pages tall, with gutters only at the very top and bottom
	// where no search window can reach them.
	const w = 800
	img, _ := buildStrip(w, []int{40, 3700, 40}, 20)

	rects, cuts := imageproc.SplitStrip(img, imageproc.SplitOptions{})
	checkCoverage(t, img, rects)
	if len(rects) < 3 {
		t.Fatalf("a single %d-row panel became %d pieces; it must be cut, not left as a sliver", 3700, len(rects))
	}
	for _, c := range cuts {
		if c.Y <= 60 || c.Y >= img.Bounds().Dy()-60 {
			t.Errorf("cut at row %d is at the very edge; the splash was not actually divided", c.Y)
		}
	}
	t.Logf("tall splash: %d pieces, heights %v", len(rects), heights(rects))
}

// TestSplitOverlapsEveryCut — a reader cannot scroll across our page break, so
// a panel that straddles one has to appear whole on both sides.
func TestSplitOverlapsEveryCut(t *testing.T) {
	img, _ := buildStrip(800, repeat(500, 12), 24)
	rects, cuts := imageproc.SplitStrip(img, imageproc.SplitOptions{})
	pieceH := pieceHeight(800)
	want := int(float64(pieceH)*imageproc.DefaultSplitOverlap + 0.5)
	if want < 1 {
		t.Fatal("the default overlap rounds to nothing")
	}
	for i := range cuts {
		overlap := rects[i].Max.Y - rects[i+1].Min.Y
		if overlap != want {
			t.Errorf("cut %d repeats %d rows across the boundary, want %d (%.0f%% of a %d-row page)",
				i, overlap, want, imageproc.DefaultSplitOverlap*100, pieceH)
		}
	}
}

// ---------------------------------------------------------------------------
// The shapes that must NOT split. This is the regression that matters: a strip
// left unsplit is a sliver the user can see; a manga page wrongly split is
// mangled art across a whole volume, silently.
// ---------------------------------------------------------------------------

// ordinaryShapes is the measured geometry from PLAN §12.3's table.
//
// fitsOnePage records which of them are short enough that the splitter itself
// would refuse to cut them, independent of detection. Two pages stacked in one
// image are *not*: they are genuinely two pages tall, and if the splitter ever
// saw one it would cut it in two. Nothing protects that shape except detection
// refusing to hand it over — which is precisely why the threshold sits 1.33×
// above it and why the chapter-wide case below is asserted explicitly.
var ordinaryShapes = []struct {
	name        string
	w, h        int
	fitsOnePage bool
}{
	{"double-page spread", 3200, 2200, true},
	{"A4 scan", 2480, 3508, true},
	{"typical page", 1200, 1700, true},
	{"letter scan", 2550, 3300, true},
	{"two pages stacked vertically", 1200, 3400, false},
	{"two A4 pages stacked vertically", 2480, 7016, false},
}

func TestOrdinaryPagesAreNeverSplitCandidates(t *testing.T) {
	for _, s := range ordinaryShapes {
		a := imageproc.Aspect(s.w, s.h)
		if imageproc.IsSplitCandidate(s.w, s.h) {
			t.Errorf("%s (%d×%d, h:w %.2f) is a split candidate at threshold %.2f",
				s.name, s.w, s.h, a, imageproc.SplitAspectThreshold)
		}
		// And not on corroboration either: a whole chapter of these shapes must
		// still be left alone. This is the case that mangles a volume.
		if imageproc.ShouldSplit(imageproc.SplitAuto, s.w, s.h, 20, 20) {
			t.Errorf("%s (%d×%d, h:w %.2f) was split in a chapter of twenty identical pages",
				s.name, s.w, s.h, a)
		}
		if !s.fitsOnePage {
			continue
		}
		// Belt and braces for the shapes that fit: even with detection
		// bypassed, the splitter leaves them as one page.
		img := image.NewRGBA(image.Rect(0, 0, s.w, s.h))
		noisy(img, s.w, 0, s.h, 4242)
		rects, _ := imageproc.SplitStrip(img, imageproc.SplitOptions{})
		if len(rects) != 1 {
			t.Errorf("%s (%d×%d) was cut into %d pieces by the splitter itself", s.name, s.w, s.h, len(rects))
		}
	}
}

// TestOrdinaryPagesSurviveTheAlwaysOverride — `always` is a user override, not
// a licence to shred ordinary pages. Anything that already fits a page stays
// one page even then.
func TestOrdinaryPagesSurviveTheAlwaysOverride(t *testing.T) {
	for _, s := range ordinaryShapes {
		if !s.fitsOnePage {
			continue // a stack of pages genuinely is two pages; see above
		}
		img := image.NewRGBA(image.Rect(0, 0, s.w, s.h))
		noisy(img, s.w, 0, s.h, 77)
		rects, _ := imageproc.SplitStrip(img, imageproc.SplitOptions{})
		if len(rects) != 1 {
			t.Errorf("under always, %s (%d×%d) became %d pages", s.name, s.w, s.h, len(rects))
		}
	}
}

// TestLoneTallPageInAnOrdinaryChapterIsLeftAlone is the false positive PLAN
// §12.3 singles out: a spread, a credits page or an author's note is an
// outlier, and an outlier is not evidence of a format.
func TestLoneTallPageInAnOrdinaryChapterIsLeftAlone(t *testing.T) {
	// 20 pages, one of which is a 800×3600 author's note at h:w 4.5 — past the
	// candidate threshold, short of the extreme.
	const pages, tall = 20, 1
	if !imageproc.IsSplitCandidate(800, 3600) {
		t.Fatal("the fixture is not even a candidate, so it proves nothing about corroboration")
	}
	if imageproc.ShouldSplit(imageproc.SplitAuto, 800, 3600, tall, pages) {
		t.Errorf("a lone 800×3600 image in a %d-page chapter was split", pages)
	}

	// The same image in a chapter that is mostly tall is a strip, and splits.
	if !imageproc.ShouldSplit(imageproc.SplitAuto, 800, 3600, 18, pages) {
		t.Error("an 800×3600 image in a chapter of 18 tall pages was left unsplit")
	}
}

func TestCorroborationNeedsAStrictMajorityAndMoreThanOnePage(t *testing.T) {
	const w, h = 800, 3600 // candidate, not extreme
	cases := []struct {
		tall, pages int
		want        bool
	}{
		{1, 1, false},   // a one-image chapter: could be a note, could be a strip
		{1, 2, false},   // one of two is not "most"
		{2, 4, false},   // exactly half is not a majority
		{3, 4, true},    // a majority, and more than one
		{2, 3, true},    // ditto
		{10, 20, false}, // exactly half again, at scale
	}
	for _, c := range cases {
		if got := imageproc.ShouldSplit(imageproc.SplitAuto, w, h, c.tall, c.pages); got != c.want {
			t.Errorf("ShouldSplit(auto, %d tall of %d) = %v, want %v", c.tall, c.pages, got, c.want)
		}
	}

	// A single-image webtoon chapter is the case the majority rule cannot
	// reach, and it is covered by the extreme ratio instead.
	if !imageproc.ShouldSplit(imageproc.SplitAuto, 800, 8000, 1, 1) {
		t.Error("a lone 800×8000 strip — the measured webtoon shape — was not split")
	}
}

func TestOverrideBeatsDetectionBothWays(t *testing.T) {
	if imageproc.ShouldSplit(imageproc.SplitNever, 800, 20000, 10, 10) {
		t.Error("never did not stop a 25:1 strip")
	}
	if !imageproc.ShouldSplit(imageproc.SplitAlways, 1200, 1700, 0, 20) {
		t.Error("always did not reach the splitter for an ordinary page (the splitter, not detection, keeps it whole)")
	}
	for _, s := range []string{"auto", "never", "always", ""} {
		m, err := imageproc.ParseSplitMode(s)
		if err != nil {
			t.Errorf("ParseSplitMode(%q): %v", s, err)
		}
		if s != "" && m.String() != s {
			t.Errorf("ParseSplitMode(%q).String() = %q", s, m.String())
		}
	}
	if _, err := imageproc.ParseSplitMode("sometimes"); err == nil {
		t.Error("an unknown splitStrips value was accepted silently")
	}
}

// TestSplitThresholdClearsRealGeometryWithMargin states the measured margin, as
// §9 requires of TestFingerprintsClearTheThresholdWithMargin. The point is not
// that the numbers pass but by *how much*: a threshold five percent above the
// tallest real page would be one unusual scan away from mangling a volume.
func TestSplitThresholdClearsRealGeometryWithMargin(t *testing.T) {
	const wantMargin = 1.25 // 25%, in either direction

	below := imageproc.SplitAspectThreshold / imageproc.TallestOrdinaryAspect
	above := imageproc.ShortestStripAspect / imageproc.SplitAspectThreshold
	if below < wantMargin {
		t.Errorf("threshold %.3f is only %.2f× the tallest ordinary shape (%.3f); want at least %.2f×",
			imageproc.SplitAspectThreshold, below, imageproc.TallestOrdinaryAspect, wantMargin)
	}
	if above < wantMargin {
		t.Errorf("threshold %.3f is only %.2f× below the shortest real strip (%.3f); want at least %.2f×",
			imageproc.SplitAspectThreshold, above, imageproc.ShortestStripAspect, wantMargin)
	}
	// The threshold must sit in the band PLAN §12.3 records as empty.
	if imageproc.SplitAspectThreshold <= 3 || imageproc.SplitAspectThreshold >= 5 {
		t.Errorf("threshold %.3f is outside the empty 3–5 band", imageproc.SplitAspectThreshold)
	}
	if d := math.Abs(below - above); d > 0.01 {
		t.Errorf("margins are lopsided (%.3f vs %.3f); the geometric mean should clear both sides equally", below, above)
	}

	t.Logf("measured margins, threshold %.3f (geometric mean of %.3f and %.3f):",
		imageproc.SplitAspectThreshold, imageproc.TallestOrdinaryAspect, imageproc.ShortestStripAspect)
	for _, s := range ordinaryShapes {
		a := imageproc.Aspect(s.w, s.h)
		t.Logf("  %-32s %d×%d  h:w %5.2f  threshold is %4.2f× higher", s.name, s.w, s.h, a,
			imageproc.SplitAspectThreshold/a)
	}
	for _, s := range []struct {
		name string
		w, h int
	}{
		{"short webtoon strip", 800, 4000},
		{"webtoon strip", 800, 8000},
		{"long strip", 800, 20000},
	} {
		a := imageproc.Aspect(s.w, s.h)
		t.Logf("  %-32s %d×%d  h:w %5.2f  %4.2f× above the threshold", s.name, s.w, s.h, a,
			a/imageproc.SplitAspectThreshold)
	}
	t.Logf("  extreme (no corroboration needed) at %.2f: %.2f× the tallest ordinary shape",
		imageproc.SplitExtremeAspect, imageproc.SplitExtremeAspect/imageproc.TallestOrdinaryAspect)
}

// TestCropKeepsPixels guards the piece-extraction path: a cut piece must carry
// the rows it names, whatever concrete image type the decoder produced.
func TestCropKeepsPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 100))
	flat(img, 10, 0, 100, 0)
	flat(img, 10, 40, 60, 200)

	c := imageproc.Crop(img, image.Rect(0, 40, 10, 60))
	if got := c.Bounds(); got.Dy() != 20 {
		t.Fatalf("crop is %d rows, want 20", got.Dy())
	}
	r, _, _, _ := c.At(5, 45).RGBA()
	if r>>8 != 200 {
		t.Errorf("cropped pixel is %d, want 200; the crop is not addressing the source rows", r>>8)
	}
}

// pieceHeight is one page's worth of a w-wide strip at the panel's 3:4.
func pieceHeight(w int) int {
	return int(float64(w)/(float64(imageproc.PanelWidth)/float64(imageproc.PanelHeight)) + 0.5)
}

func repeat(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func inAnyBand(bands []band, y int) bool {
	for _, b := range bands {
		if b.contains(y) {
			return true
		}
	}
	return false
}

func heights(rects []image.Rectangle) []int {
	out := make([]int, len(rects))
	for i, r := range rects {
		out[i] = r.Dy()
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
