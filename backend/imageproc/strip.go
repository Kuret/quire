package imageproc

// Strip splitting — PLAN §12.3.
//
// A vertical-scroll ("webtoon") page is one image thousands of pixels tall.
// Normalise fits it into the panel's 3:4 grid, which is correct and unreadable:
// 800 × 20000 contains down to an 86 × 2160 sliver with white either side. The
// fix is to cut the strip into panel-shaped pages before that fit happens.
//
// # The asymmetry that decides every judgement call here
//
// Failing to split a strip leaves the user where they already are: a sliver,
// bad but survivable, and obvious the moment they open it. *Wrongly* splitting
// an ordinary manga page mangles content that was fine, across a whole volume,
// silently. The two errors are not comparable, so detection is biased hard
// toward doing nothing and every ambiguous case resolves to "leave it alone".
// This is the same rule as PLAN §7.5's challenge tiers, for the same reason.
//
// Detection is therefore per *image*, never per source: a single index — and a
// single series — hosts both formats, so a per-source flag or a hardcoded list
// is the wrong unit and would be wrong immediately.
//
// # Why cutting works at all
//
// Vertical-scroll comics are *authored* with horizontal gutters: bands of flat
// background between panels. That is a format convention, not luck, and it is
// what SplitStrip looks for. Where the convention is not honoured there is
// still a minimum-energy row, which always finds something and degrades
// gracefully.

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// The measured geometry the thresholds are derived from (PLAN §12.3). These
// are real page dimensions, not round numbers picked for the look of them, and
// every threshold below is computed from them rather than asserted.
const (
	// SpreadAspect is a double-page spread, 3200 × 2200. The *widest* thing we
	// handle, and the furthest any real page gets from a strip.
	SpreadAspect = 2200.0 / 3200.0 // 0.6875

	// A4ScanAspect is a 2480 × 3508 A4 scan.
	A4ScanAspect = 3508.0 / 2480.0 // 1.4145

	// TypicalPageAspect is a 1200 × 1700 web-sized page.
	TypicalPageAspect = 1700.0 / 1200.0 // 1.4167

	// TallestOrdinaryAspect is two typical pages stacked vertically in one
	// image, 1200 × 3400. Rare but real, and the tallest shape anything
	// legitimate reaches — so it sets the floor the threshold must clear.
	TallestOrdinaryAspect = 3400.0 / 1200.0 // 2.8333

	// ShortestStripAspect is a short webtoon strip, 800 × 4000: the *least*
	// extreme real strip, so it sets the ceiling the threshold must stay under.
	ShortestStripAspect = 4000.0 / 800.0 // 5.0

	// WebtoonStripAspect is an ordinary webtoon strip, 800 × 8000.
	WebtoonStripAspect = 8000.0 / 800.0 // 10.0

	// LongStripAspect is a long strip, 800 × 20000.
	LongStripAspect = 20000.0 / 800.0 // 25.0
)

// SplitAspectThreshold is the height:width ratio at which an image becomes a
// split *candidate*.
//
// It is the geometric mean of the two measurements that bracket it —
// TallestOrdinaryAspect (2.83) below and ShortestStripAspect (5.0) above — so
// it sits in the middle of the empty band in log space and clears each side by
// the same factor, ≈1.33×. Nothing legitimate lives between 3 and 5; the
// threshold lands at ≈3.76, inside that gap, and a full 2.65× above a typical
// 1200 × 1700 page.
//
// A candidate is not yet a decision. See ShouldSplit.
var SplitAspectThreshold = math.Sqrt(TallestOrdinaryAspect * ShortestStripAspect)

// SplitExtremeAspect is the ratio at which an image is a strip on its own
// evidence, with no corroboration from the rest of the chapter.
//
// Set at the measured ordinary webtoon strip (800 × 8000 = 10.0), which is
// 3.53× the tallest legitimate shape we have ever measured. Below it a lone
// tall image is treated as an outlier — a spread, a credits page, an author's
// note — because that is exactly the false positive that matters. Above it
// there is no other explanation: no scan, no spread and no stack of pages is
// ten times taller than it is wide.
const SplitExtremeAspect = WebtoonStripAspect

// MinCorroboratingPages is how many tall images a chapter must contain before
// the majority rule in ShouldSplit can fire at all.
//
// Two, so that "most of this chapter is tall" cannot be satisfied by a single
// image — in a one- or two-page chapter a bare majority is one page, which is
// the outlier case the corroboration rule exists to reject. A genuine
// single-image webtoon chapter is still handled: it clears SplitExtremeAspect
// on its own.
const MinCorroboratingPages = 2

// SplitMode is the per-source override from PLAN §12.3. Detection decides; the
// user overrules.
type SplitMode int

const (
	// SplitAuto detects per image: aspect past SplitAspectThreshold, plus
	// corroboration. The default, and the zero value.
	SplitAuto SplitMode = iota

	// SplitNever disables splitting for a source, whatever detection thinks.
	SplitNever

	// SplitAlways waives the aspect gate and the corroboration rule: every
	// image is handed to the splitter, which cuts it into as many panel-shaped
	// pages as it is tall enough to fill.
	//
	// Note what this does *not* mean. The splitter returns a single piece for
	// anything that already fits one page within its search window, so an
	// ordinary 1.42 page still comes out as one page under SplitAlways. What
	// the mode really buys is splitting in the band detection refuses to touch.
	SplitAlways
)

// String renders the mode as it appears in the source schema.
func (m SplitMode) String() string {
	switch m {
	case SplitNever:
		return "never"
	case SplitAlways:
		return "always"
	default:
		return "auto"
	}
}

// ParseSplitMode reads a schema value. An empty string means the default.
func ParseSplitMode(s string) (SplitMode, error) {
	switch s {
	case "", "auto":
		return SplitAuto, nil
	case "never":
		return SplitNever, nil
	case "always":
		return SplitAlways, nil
	default:
		return SplitAuto, fmt.Errorf("imageproc: unknown splitStrips value %q (want auto, never or always)", s)
	}
}

// Aspect is an image's height:width ratio, the quantity every threshold in this
// file is expressed in. Zero for a degenerate size, which then fails every
// comparison — the safe direction.
func Aspect(w, h int) float64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	return float64(h) / float64(w)
}

// IsSplitCandidate reports the primary signal only: tall enough to be worth a
// second look. It is deliberately not a decision — see ShouldSplit.
func IsSplitCandidate(w, h int) bool { return Aspect(w, h) >= SplitAspectThreshold }

// IsExtremeStrip reports an image tall enough to need no corroboration.
func IsExtremeStrip(w, h int) bool { return Aspect(w, h) >= SplitExtremeAspect }

// ShouldSplit is the whole decision for one image.
//
// tallInChapter is how many images in the same chapter are split candidates
// (this one included), and pagesInChapter how many there are in total. One
// signal is how the challenge detector went wrong, so aspect alone is not
// allowed to be enough: either the rest of the chapter agrees, or the ratio is
// past SplitExtremeAspect and nothing else explains it.
func ShouldSplit(mode SplitMode, w, h, tallInChapter, pagesInChapter int) bool {
	switch mode {
	case SplitNever:
		return false
	case SplitAlways:
		return true
	}
	if !IsSplitCandidate(w, h) {
		return false
	}
	if IsExtremeStrip(w, h) {
		return true
	}
	// A strict majority, and never on the strength of one image.
	return tallInChapter >= MinCorroboratingPages && tallInChapter*2 > pagesInChapter
}

// Defaults for SplitOptions. Each is explained on its field.
const (
	DefaultSplitWindow     = 0.20
	DefaultSplitOverlap    = 0.05
	DefaultGutterTolerance = 6.0 / 255.0
	DefaultMinGutterRows   = 2
	DefaultMaxSplitPieces  = 400
)

// SplitOptions tunes SplitStrip. The zero value is usable and means the
// defaults above.
type SplitOptions struct {
	// TargetAspect is the destination page's width:height. Zero means the
	// panel's 3:4, so a piece of a W-wide strip is 4W/3 tall and fills the page
	// edge to edge with no padding.
	TargetAspect float64

	// Window is the fraction of a piece height searched either side of each
	// target cut. Cutting at exactly the target is what produces mid-panel
	// slices; searching ±20% means pages come out slightly uneven and nearly
	// every cut lands where the author already put a break.
	Window float64

	// Overlap is the fraction of a piece height repeated across each cut, so a
	// panel spanning a boundary appears whole on both pages. It costs a little
	// space and guarantees nothing is lost, which matters because a reader
	// cannot scroll across our page break.
	Overlap float64

	// GutterTolerance is the mean absolute deviation, in 0..1 luminance, under
	// which a row counts as uniform. JPEG noise and gradients mean "uniform" is
	// never exact.
	GutterTolerance float64

	// MinGutterRows is how many consecutive uniform rows make a gutter. A
	// single flat row is a coincidence; a band is an authored break.
	MinGutterRows int

	// MaxPieces is a runaway guard, not a policy. It exists so a corrupt or
	// absurd height cannot produce an unbounded page count.
	MaxPieces int
}

func (o SplitOptions) withDefaults() SplitOptions {
	if o.TargetAspect <= 0 {
		o.TargetAspect = float64(PanelWidth) / float64(PanelHeight)
	}
	if o.Window <= 0 {
		o.Window = DefaultSplitWindow
	}
	if o.Overlap < 0 {
		o.Overlap = 0
	} else if o.Overlap == 0 {
		o.Overlap = DefaultSplitOverlap
	}
	if o.GutterTolerance <= 0 {
		o.GutterTolerance = DefaultGutterTolerance
	}
	if o.MinGutterRows <= 0 {
		o.MinGutterRows = DefaultMinGutterRows
	}
	if o.MaxPieces <= 0 {
		o.MaxPieces = DefaultMaxSplitPieces
	}
	return o
}

// SplitCut records one cut and how it was chosen, so a test can assert the cut
// landed in a gutter it planted rather than merely that some cut happened.
type SplitCut struct {
	// Y is the absolute row the piece below starts at, before overlap.
	Y int
	// Gutter is true when Y is the centre of a band of uniform rows, false
	// when it came from the minimum-energy fallback.
	Gutter bool
}

// SplitStrip returns the pieces src should be cut into, top to bottom, in src's
// own coordinate space. Consecutive pieces overlap by SplitOptions.Overlap.
//
// A single rectangle covering the whole image means "no split needed", which is
// what anything short enough to fill one page gets. The result always covers
// the image: the first piece starts at the top edge and the last ends at the
// bottom, so nothing is dropped.
//
// The cuts are returned alongside for callers that want to report or test them.
func SplitStrip(src image.Image, opts SplitOptions) ([]image.Rectangle, []SplitCut) {
	opts = opts.withDefaults()
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	whole := []image.Rectangle{b}
	if w <= 0 || h <= 0 {
		return whole, nil
	}

	// One page's worth of strip, at the destination's aspect.
	pieceH := int(float64(w)/opts.TargetAspect + 0.5)
	if pieceH < 1 {
		return whole, nil
	}
	win := int(float64(pieceH)*opts.Window + 0.5)

	// Short enough that the first target cut is at or past the bottom edge:
	// one page, untouched. This is the clause that keeps every ordinary page
	// whole even under SplitAlways.
	if h <= pieceH+win {
		return whole, nil
	}

	lum := luminance(src)
	energy := rowEnergy(lum, w, h)
	uniform := uniformRows(lum, w, h, opts.GutterTolerance)

	overlap := int(float64(pieceH)*opts.Overlap + 0.5)

	var (
		rects []image.Rectangle
		cuts  []SplitCut
		top   int
	)
	for {
		if len(rects) >= opts.MaxPieces-1 || h-top <= pieceH+win {
			// The remainder is one page's worth (or the guard tripped): take it
			// all rather than leaving a sliver behind.
			rects = append(rects, image.Rect(b.Min.X, b.Min.Y+top, b.Max.X, b.Max.Y))
			break
		}

		target := top + pieceH
		lo := max(target-win, top+1)
		hi := min(target+win, h-1)
		if lo > hi {
			lo = hi
		}

		cut, isGutter := chooseCut(uniform, energy, lo, hi, target, opts.MinGutterRows)
		rects = append(rects, image.Rect(b.Min.X, b.Min.Y+top, b.Max.X, b.Min.Y+cut))
		cuts = append(cuts, SplitCut{Y: b.Min.Y + cut, Gutter: isGutter})

		// Repeat a little of what we just cut off, so a panel straddling the
		// boundary survives on both pages. Progress is guaranteed: cut is at
		// least top + pieceH*(1-Window) and overlap is pieceH*Overlap, with
		// Overlap well below 1-Window.
		top = max(cut-overlap, top+1)
	}
	return rects, cuts
}

// chooseCut picks the row to cut at within [lo, hi].
//
// Preference order is PLAN §12.3's: an authored gutter first, the row of least
// gradient energy second. Among gutters the one nearest the target wins, which
// keeps pages even; the *longest* gutter does not win, because a long flat
// region in the middle of a panel is not a better break than a narrow band the
// author drew where they meant the reader to pause.
func chooseCut(uniform []bool, energy []float64, lo, hi, target, minRows int) (int, bool) {
	best, found := 0, false
	bestDist := 0
	for y := lo; y <= hi; y++ {
		if !uniform[y] {
			continue
		}
		end := y
		for end+1 <= hi && uniform[end+1] {
			end++
		}
		if end-y+1 >= minRows {
			centre := (y + end) / 2
			d := abs(centre - target)
			if !found || d < bestDist {
				best, bestDist, found = centre, d, true
			}
		}
		y = end
	}
	if found {
		return best, true
	}

	// No gutter in the window. Cut where least is happening.
	best = lo
	bestE := math.Inf(1)
	for y := lo; y <= hi; y++ {
		e := energy[y]
		if e < bestE || (e == bestE && abs(y-target) < abs(best-target)) {
			best, bestE = y, e
		}
	}
	return best, false
}

// uniformRows marks each row whose mean absolute deviation from its own mean is
// within tol — the "near-uniform across the full width" test. Deviation rather
// than min/max, because a single dark pixel of JPEG ringing at a panel border
// should not disqualify an obvious gutter.
func uniformRows(lum []uint8, w, h int, tol float64) []bool {
	out := make([]bool, h)
	limit := tol * 255
	for y := range h {
		row := lum[y*w : (y+1)*w]
		var sum int
		for _, v := range row {
			sum += int(v)
		}
		mean := float64(sum) / float64(w)
		var dev float64
		for _, v := range row {
			dev += math.Abs(float64(v) - mean)
		}
		out[y] = dev/float64(w) <= limit
	}
	return out
}

// rowEnergy is the mean absolute vertical gradient across a row: how much
// changes when you step over it. A cut through a low-energy row is a cut
// through something that was already continuous, which is the least damaging
// place to put one.
func rowEnergy(lum []uint8, w, h int) []float64 {
	out := make([]float64, h)
	for y := 1; y < h; y++ {
		var sum float64
		prev := lum[(y-1)*w : y*w]
		row := lum[y*w : (y+1)*w]
		for x := range row {
			sum += math.Abs(float64(row[x]) - float64(prev[x]))
		}
		out[y] = sum / float64(w)
	}
	if h > 0 {
		out[0] = math.Inf(1) // never cut at the very top
	}
	return out
}

// luminance flattens src to one byte per pixel, with fast paths for the types a
// decoder actually produces. A 20000-row strip is 16 MB here, which is small
// beside the decoded image it came from.
func luminance(src image.Image) []uint8 {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]uint8, w*h)
	switch s := src.(type) {
	case *image.Gray:
		for y := range h {
			copy(out[y*w:(y+1)*w], s.Pix[s.PixOffset(b.Min.X, b.Min.Y+y):])
		}
		return out
	case *image.YCbCr:
		for y := range h {
			row := s.Y[s.YOffset(b.Min.X, b.Min.Y+y):]
			copy(out[y*w:(y+1)*w], row[:w])
		}
		return out
	case *image.RGBA:
		for y := range h {
			i := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := range w {
				p := s.Pix[i+x*4:]
				out[y*w+x] = grey(uint32(p[0]), uint32(p[1]), uint32(p[2]))
			}
		}
		return out
	case *image.NRGBA:
		for y := range h {
			i := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := range w {
				p := s.Pix[i+x*4:]
				out[y*w+x] = grey(uint32(p[0]), uint32(p[1]), uint32(p[2]))
			}
		}
		return out
	}
	for y := range h {
		for x := range w {
			r, g, bl, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[y*w+x] = grey(r>>8, g>>8, bl>>8)
		}
	}
	return out
}

// grey is Rec. 601 luma on 8-bit components.
func grey(r, g, b uint32) uint8 {
	return uint8((r*299 + g*587 + b*114) / 1000)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Crop returns the sub-image of src inside r, without copying pixels where the
// concrete type allows it.
func Crop(src image.Image, r image.Rectangle) image.Image {
	r = r.Intersect(src.Bounds())
	if sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return sub.SubImage(r)
	}
	return &cropped{src: src, rect: r}
}

// cropped is the fallback for a decoder type that cannot sub-image itself.
type cropped struct {
	src  image.Image
	rect image.Rectangle
}

func (c *cropped) ColorModel() color.Model { return c.src.ColorModel() }
func (c *cropped) Bounds() image.Rectangle { return c.rect }
func (c *cropped) At(x, y int) color.Color { return c.src.At(x, y) }
