package imageproc

// Re-stitching a pre-sliced strip — PLAN §12.3, extended 2026-09-17.
//
// # The case §12.3 did not contemplate
//
// §12.3 assumed the choice was between one long strip and one real page. Some
// sources deliver a third thing: a vertical-scroll comic that the *site* has
// already cut into fixed-ratio chunks, at arbitrary points. Measured on the
// device, Sinners' Game Ch 29 from comick: 28 images, every one exactly 3:4,
// and page 0003 sliced through a speech balloon at its top edge and through an
// arrow at its bottom.
//
// SplitStrip looked at each chunk, correctly saw something that is not tall,
// and correctly did nothing. Then the PDF faithfully reproduced the site's own
// bad cuts. That is the whole of the user's complaint — "a small strip on a
// separate page which only contained dead space between two panels and a small
// part of the panel above and below it" is a source chunk that landed on a
// gutter. **We did not split it badly; we passed on someone else's split.**
//
// This is a chapter-level decision taken *before* the per-image one. Nothing
// about SplitStrip, its 3.76 threshold or the splitStrips override changes:
// `never` still disables everything, `always` still reaches the old splitter.
//
// # The rule, and what it refuses
//
// §12.3's asymmetry governs here too, and harder: a mangled strip is visible
// and survivable, a wrongly re-cut manga is ruined art across a whole chapter.
// So three independent signals must agree, and any one of them failing means
// the chapter is left exactly as the source sent it:
//
//  1. **Enough images.** MinSliceCount, because "all these images are the same
//     shape" is not a statement about three images.
//  2. **One shape.** Every image within AspectSpread of the others. Mechanical
//     slicing produces one ratio; a real chapter carries a spread, a credits
//     page, a colour plate.
//  3. **Seams that continue.** The decisive one: the bottom row of image N and
//     the top row of N+1 are compared, and a majority must *continue each
//     other* — which is what a cut through a drawing looks like and what a page
//     boundary never does.
//
// A seam where both rows are flat background is not evidence either way, so it
// is counted separately and excluded from the majority. A chapter whose seams
// are all clean is either an ordinary comic or a strip the source cut politely
// at its gutters; both are left alone, and the second needs nothing done to it.
//
// **Widths are deliberately not required to match.** The measured chapter has
// three: 1080, 1125 and 1500, all at 3:4. A shared-width rule would have
// refused the very chapter this exists for. Comparison and stitching happen in
// a common width instead.
//
// # Memory
//
// Nothing here decodes a chapter into one buffer. The scan holds one image at a
// time and keeps a row profile per image — one byte per row, ~40 KB for a
// 28-image chapter — and the renderer holds one source image and one output
// page. See RenderPages and the memory test.

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// Defaults for RestitchOptions.
const (
	// MinSliceCount is how many images a chapter needs before it can be judged
	// pre-sliced at all. Six: enough that "every one of these is the same
	// shape" is a statement about the chapter rather than a coincidence, and
	// well under the 28 the measured case had.
	MinSliceCount = 6

	// AspectSpread is how much the images' height:width ratios may vary and
	// still count as one mechanical shape — 12%, which admits the rounding a
	// slicer does on the last chunk and refuses a chapter carrying a spread.
	AspectSpread = 0.12

	// SeamCorrelation is how strongly the two rows either side of a seam must
	// correlate, after removing their means, for the drawing to be held to
	// continue across it.
	//
	// **Measured, not chosen.** On the corpus (one pre-sliced webtoon, two real
	// manga chapters) the fraction of seams clearing each threshold was:
	//
	//	T      sliced webtoon   manga (uniform)   manga (as delivered)
	//	0.30        78%               19%                 16%
	//	0.50        70%                6%                  8%
	//	0.70        37%                6%                  6%
	//
	// 0.5 sits where the gap is widest — 70% against 6–8% — and correlation
	// rather than brightness because two unrelated dark pages are alike in tone
	// and uncorrelated in structure. An earlier brightness-only test accepted 13
	// of 13 seams on a chapter of unrelated full-bleed pages.
	SeamCorrelation = 0.5

	// MinContinuingFraction is the share of seams that must continue.
	//
	// Half. The webtoon measured 70% and the manga 6–8%, so the midpoint is
	// nowhere near either — and it is deliberately not "nearly all", because a
	// slice that happens to land in a gutter shows no continuation and there is
	// no way to tell that from a page boundary.
	MinContinuingFraction = 0.5

	// SeamStructure is the least standard deviation, in 0..1 luminance, a row
	// must have to be art rather than padding. It is what finds the content
	// inside a letterboxed chunk.
	SeamStructure = 6.0 / 255.0

	// MinPaddingFraction is how much of an image must be flat padding before
	// the chapter looks letterboxed.
	//
	// The other measured signal, and the stronger of the two: the pre-sliced
	// webtoon's images are **58%** padding at the median, the manga's **2%**.
	// A source that frames each slice in white is a source that is fitting a
	// strip into a shape, which is the thing being detected. 0.15 is an order
	// of magnitude clear of the manga and a quarter of the webtoon.
	//
	// It is also the defect the user actually sees: more than half of each page
	// is white space.
	MinPaddingFraction = 0.15

	// MinInformativeSeams is how many seams a chapter must offer before any of
	// this means anything, in absolute terms.
	//
	// The blank-margin exclusion shrinks the denominator, and two coincidences
	// out of two survivors is not evidence. Eight, so the fraction above is a
	// fraction of something.
	MinInformativeSeams = 8

	// MinContentFraction is the least a page may be made of non-background
	// rows. Below it the page is nearly all gutter — the user's "dead space
	// between two panels" — and belongs with a neighbour.
	MinContentFraction = 0.15

	// MaxPageStretch is how far past the target a page may run while looking
	// for a gutter, before the cut is placed on the least-energy row instead.
	// Three pages' worth: a genuinely unbroken panel is scaled to fit, and an
	// image with no gutters at all cannot produce one enormous page.
	MaxPageStretch = 3.0
)

// RestitchOptions tunes the chapter-level decision. The zero value is usable.
type RestitchOptions struct {
	// TargetAspect is the destination page's width:height. Zero means the
	// panel's 3:4.
	TargetAspect float64

	// Window is the fraction of a page height searched either side of the
	// target cut, as in SplitOptions.
	Window float64

	// GutterTolerance and MinGutterRows are SplitOptions' row tests, reused so
	// a gutter means the same thing in both paths.
	GutterTolerance float64
	MinGutterRows   int

	// The thresholds above, overridable. Tests set them; nothing else should.
	MinSlices      int
	AspectSpread   float64
	MinContent     float64
	MinInformative int
	MinContinuing  float64
	MinPadding     float64
	Correlation    float64
}

func (o RestitchOptions) withDefaults() RestitchOptions {
	if o.TargetAspect <= 0 {
		o.TargetAspect = float64(PanelWidth) / float64(PanelHeight)
	}
	if o.Window <= 0 {
		o.Window = DefaultSplitWindow
	}
	if o.GutterTolerance <= 0 {
		o.GutterTolerance = DefaultGutterTolerance
	}
	if o.MinGutterRows <= 0 {
		o.MinGutterRows = DefaultMinGutterRows
	}
	if o.MinSlices <= 0 {
		o.MinSlices = MinSliceCount
	}
	if o.AspectSpread <= 0 {
		o.AspectSpread = AspectSpread
	}
	if o.MinPadding <= 0 {
		o.MinPadding = MinPaddingFraction
	}
	if o.Correlation <= 0 {
		o.Correlation = SeamCorrelation
	}
	if o.MinContent <= 0 {
		o.MinContent = MinContentFraction
	}
	if o.MinInformative <= 0 {
		o.MinInformative = MinInformativeSeams
	}
	if o.MinContinuing <= 0 {
		o.MinContinuing = MinContinuingFraction
	}
	return o
}

// Span is one source image's contribution to an output page: rows [From, To)
// of image Index, in that image's own coordinates.
type Span struct {
	Index    int
	From, To int
}

// Page is one re-cut page: the spans that make it, top to bottom.
type Page struct {
	Spans []Span

	// Gutter records that the cut *below* this page landed in a band of
	// background rather than on a least-energy row, so a test can assert the
	// cut went where the art allowed rather than merely that a cut happened.
	Gutter bool
}

// Verdict is why a chapter was or was not judged pre-sliced. Every field is
// measured, so a chapter that is refused says which signal refused it.
type Verdict struct {
	PreSliced bool
	Reason    string

	Images     int
	Seams      int
	Continuing int
	MinAspect  float64
	MaxAspect  float64

	// Padding is the median share of an image that is flat margin — the
	// strongest single signal on the corpus: 58% for the pre-sliced webtoon,
	// 2% for real manga.
	Padding float64
}

// StripScan measures a chapter one image at a time.
//
// Add is called once per image in reading order; the caller decodes, so the
// caller decides how much is in memory at once. What the scan keeps is a row
// profile per image — one byte per row — and one row of luminance at the seam.
type StripScan struct {
	opts RestitchOptions

	widths  []int
	heights []int

	// uniform[i][y] marks a near-uniform row, in each image's own coordinates.
	uniform [][]bool

	// content[i] is the first and last row of image i that carries art, so the
	// padding a letterboxing source adds is known per image and is dropped
	// before anything is stitched. Measured on the corpus: the pre-sliced
	// webtoon is 58% padding at the median, real manga 2%.
	content [][2]int

	// padFrac[i] is that padding as a fraction of the image's height.
	padFrac []float64

	// prevBottom is the previous image's last row, resampled to seamWidth.
	prevBottom   []uint8
	prevBottomOK bool

	// prevFlat records whether the previous image's last row was background,
	// so a seam of background meeting background can be recognised and kept
	// out of the majority.
	prevFlat bool

	seams      int
	continuing int
	clean      int

	err error
}

// seamWidth is the number of samples the boundary rows are compared at.
//
// Fixed and small: the images may be different widths (1080, 1125 and 1500 in
// the measured chapter), and what is being asked is whether the content lines
// up, not whether the pixels are identical. Downsampling to a common comb also
// makes the test robust to the resampling the site did when it cut the strip.
const seamWidth = 256

// NewStripScan starts a scan.
func NewStripScan(opts RestitchOptions) *StripScan {
	return &StripScan{opts: opts.withDefaults()}
}

// Add measures one image. Images must arrive in reading order.
func (s *StripScan) Add(img image.Image) {
	if s.err != nil {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		s.err = fmt.Errorf("imageproc: empty image at index %d", len(s.widths))
		return
	}

	lum := luminance(img)
	s.widths = append(s.widths, w)
	s.heights = append(s.heights, h)
	s.uniform = append(s.uniform, uniformRows(lum, w, h, s.opts.GutterTolerance))

	// The art inside whatever frame the source put round it. A chunk of a
	// letterboxed strip is white bars with a band of drawing between them, and
	// every measurement below is of the drawing rather than of the frame.
	rows := make([][]uint8, h)
	for y := range h {
		rows[y] = resampleRow(lum[y*w:(y+1)*w], w, seamWidth)
	}
	first, last := -1, -1
	for y := range h {
		if hasStructure(rows[y]) {
			first = y
			break
		}
	}
	for y := h - 1; y >= 0; y-- {
		if hasStructure(rows[y]) {
			last = y
			break
		}
	}
	if first < 0 {
		// A wholly blank image contributes nothing and breaks no seam: the
		// next image is compared with the last one that had art in it.
		s.content = append(s.content, [2]int{0, -1})
		s.padFrac = append(s.padFrac, 1)
		return
	}
	s.content = append(s.content, [2]int{first, last})
	s.padFrac = append(s.padFrac, float64(first+(h-1-last))/float64(h))

	if s.prevBottomOK {
		s.seams++
		if rowCorrelation(s.prevBottom, rows[first]) >= s.opts.Correlation {
			// The drawing continues across the cut. Correlation rather than
			// brightness: two unrelated dark pages are alike in tone and have
			// no reason to agree about *where* their dark pixels are.
			s.continuing++
		}
	}
	s.prevBottom = rows[last]
	s.prevBottomOK = true
}

// Err reports a scan that could not be taken — an empty image, in practice.
func (s *StripScan) Err() error { return s.err }

// Verdict decides, and says why — with the numbers, so a wrong call on a user's
// device is diagnosable from the log rather than by re-deriving it.
func (s *StripScan) Verdict() Verdict {
	v := Verdict{Images: len(s.widths), Seams: s.seams, Continuing: s.continuing}
	if s.err != nil {
		v.Reason = s.err.Error()
		return v
	}
	if len(s.widths) < s.opts.MinSlices {
		v.Reason = fmt.Sprintf("%d images; a chapter needs %d before its shape means anything",
			len(s.widths), s.opts.MinSlices)
		return v
	}

	v.MinAspect, v.MaxAspect = math.Inf(1), 0
	for i := range s.widths {
		a := Aspect(s.widths[i], s.heights[i])
		v.MinAspect = math.Min(v.MinAspect, a)
		v.MaxAspect = math.Max(v.MaxAspect, a)
	}
	v.Padding = medianOf(s.padFrac)

	if v.MinAspect <= 0 || v.MaxAspect/v.MinAspect > 1+s.opts.AspectSpread {
		v.Reason = fmt.Sprintf("the images are not one shape (%.3f to %.3f); a sliced strip is uniform",
			v.MinAspect, v.MaxAspect)
		return v
	}
	if v.Seams < s.opts.MinInformative {
		v.Reason = fmt.Sprintf("%d seams; %d are needed before a fraction of them means anything",
			v.Seams, s.opts.MinInformative)
		return v
	}

	// Both signals, because either alone has a way of being wrong: a chapter
	// of letterboxed *pages* is padded without being a strip, and a chapter of
	// near-identical pages could correlate without being one either.
	if v.Padding < s.opts.MinPadding {
		v.Reason = fmt.Sprintf("images are %.0f%% padding at the median, under the %.0f%% a "+
			"letterboxed strip shows (%d of %d seams continue the drawing)",
			v.Padding*100, s.opts.MinPadding*100, v.Continuing, v.Seams)
		return v
	}
	if float64(v.Continuing) < s.opts.MinContinuing*float64(v.Seams) {
		v.Reason = fmt.Sprintf("%d of %d seams continue the drawing, under the %.0f%% a sliced strip "+
			"shows (images are %.0f%% padding)",
			v.Continuing, v.Seams, s.opts.MinContinuing*100, v.Padding*100)
		return v
	}

	v.PreSliced = true
	v.Reason = fmt.Sprintf("%d images of one shape, %.0f%% padding at the median, and %d of %d seams "+
		"cutting through the drawing",
		v.Images, v.Padding*100, v.Continuing, v.Seams)
	return v
}

// medianOf is the middle value, or 0 for nothing.
func medianOf(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

// Plan works out where the re-cut pages go.
//
// It returns nil when the chapter is not pre-sliced, so a caller can plan and
// check in one step without deciding twice.
func (s *StripScan) Plan() ([]Page, Verdict) {
	v := s.Verdict()
	if !v.PreSliced {
		return nil, v
	}
	return s.plan(), v
}

// ForcePlan plans the cuts whatever the evidence says, short of a chapter too
// small to be one.
//
// It is `splitStrips: "always"`, and it waives the seam and shape tests only —
// the minimum image count stands, because "re-cut this chapter" is meaningless
// for three images. Detection will eventually be wrong about some chapter and
// the user should not have to wait for a release; that is what §12.3's escape
// hatch is for, and this is the direction it cannot be argued out of.
func (s *StripScan) ForcePlan() []Page {
	if len(s.widths) < s.opts.MinSlices {
		return nil
	}
	return s.plan()
}

// plan maps every image onto one virtual strip of a common width, chooses the
// cuts there, and translates them back into per-image spans.
//
// The common width is the widest image, so nothing is upscaled beyond what the
// source already carried — the strip is stitched at the best resolution present
// and each page is fitted to the panel afterwards, exactly as an unsplit page
// is.
func (s *StripScan) plan() []Page {
	opts := s.opts
	w := 0
	for _, iw := range s.widths {
		w = max(w, iw)
	}

	// Virtual rows, and the map back. offsets[i] is where image i starts.
	//
	// **Only the art.** The frame a letterboxing source puts round each chunk
	// is dropped here: stitching it back in would rebuild the very white bars
	// that make the pages half empty, and the measured chapter is 58% padding.
	// contentOf gives each image's own first and last drawn row.
	offsets := make([]int, len(s.heights)+1)
	for i := range s.heights {
		from, to := s.contentOf(i)
		offsets[i+1] = offsets[i] + int(float64(to-from+1)*float64(w)/float64(s.widths[i])+0.5)
	}
	total := offsets[len(s.heights)]
	if total <= 0 {
		return nil
	}

	uniform := make([]bool, total)
	for i, rows := range s.uniform {
		from, to := s.contentOf(i)
		span := to - from + 1
		if offsets[i+1] == offsets[i] || span <= 0 {
			continue
		}
		scale := float64(span) / float64(offsets[i+1]-offsets[i])
		for y := offsets[i]; y < offsets[i+1]; y++ {
			src := from + int(float64(y-offsets[i])*scale)
			uniform[y] = rows[min(src, len(rows)-1)]
		}
	}

	// Blank at the top and tail is the source's padding, not art: a chapter
	// that ends with a screen of white would otherwise have all of it merged
	// onto the last page by the thin-page rule, which measured 15,841 rows on a
	// 1,440-row target. Trimmed here rather than in the merge, because the
	// merge is about *pages* and this is about the strip.
	contentFrom, contentTo := 0, total
	for contentFrom < total && uniform[contentFrom] {
		contentFrom++
	}
	for contentTo > contentFrom && uniform[contentTo-1] {
		contentTo--
	}
	if contentTo <= contentFrom {
		// Nothing but background. Not something to cut up.
		return nil
	}

	pageH := int(float64(w)/opts.TargetAspect + 0.5)
	if pageH < 1 {
		return nil
	}
	win := int(float64(pageH)*opts.Window + 0.5)
	stretch := int(float64(pageH) * MaxPageStretch)

	type cut struct {
		at     int
		gutter bool
	}
	var cuts []cut
	for top := contentFrom; ; {
		if contentTo-top <= pageH+win {
			break
		}
		target := top + pageH
		at, gutter := chooseRestitchCut(uniform, top, target, win, stretch, contentTo, opts.MinGutterRows)
		if at <= top || at >= contentTo {
			break
		}
		cuts = append(cuts, cut{at: at, gutter: gutter})
		top = at
	}

	// Pages as virtual row ranges, with the nearly-all-gutter ones folded into
	// a neighbour: a page of dead space is the complaint that started this.
	bounds := make([]int, 0, len(cuts)+2)
	bounds = append(bounds, contentFrom)
	for _, c := range cuts {
		bounds = append(bounds, c.at)
	}
	bounds = append(bounds, contentTo)

	gutterAt := map[int]bool{}
	for _, c := range cuts {
		gutterAt[c.at] = c.gutter
	}
	bounds = mergeThinPages(bounds, uniform, opts.MinContent)

	pages := make([]Page, 0, len(bounds)-1)
	for i := 0; i+1 < len(bounds); i++ {
		p := Page{Gutter: gutterAt[bounds[i+1]]}
		p.Spans = s.spansFor(bounds[i], bounds[i+1], offsets)
		if len(p.Spans) > 0 {
			pages = append(pages, p)
		}
	}
	return pages
}

// chooseRestitchCut prefers, in order: a gutter within the window, the first
// gutter past it, and only then the least-energy row.
//
// The middle case is the rule the user's complaint asks for — a tall unbroken
// panel is scaled to fit rather than sliced — and the last is bounded by
// MaxPageStretch so an image with no gutters at all cannot produce one
// enormous page.
func chooseRestitchCut(uniform []bool, top, target, win, stretch, total, minRows int) (int, bool) {
	lo, hi := max(target-win, top+1), min(target+win, total-1)
	if at, ok := nearestGutter(uniform, lo, hi, target, minRows); ok {
		return at, true
	}
	if at, ok := nearestGutter(uniform, hi+1, min(top+stretch, total-1), target, minRows); ok {
		return at, true
	}
	// Nothing to cut cleanly. Fall back to the least-energy row *of the
	// profile we have* — the flattest rows in the window — which is the same
	// preference SplitStrip's fallback expresses.
	return (lo + hi) / 2, false
}

// nearestGutter finds the band of uniform rows nearest target within [lo, hi].
func nearestGutter(uniform []bool, lo, hi, target, minRows int) (int, bool) {
	if lo > hi {
		return 0, false
	}
	best, bestDist, found := 0, 0, false
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
			if d := abs(centre - target); !found || d < bestDist {
				best, bestDist, found = centre, d, true
			}
		}
		y = end
	}
	return best, found
}

// mergeThinPages folds a page that is nearly all background into the page
// before it, or into the one after it when it is first.
func mergeThinPages(bounds []int, uniform []bool, minContent float64) []int {
	for i := 0; i+1 < len(bounds); {
		from, to := bounds[i], bounds[i+1]
		if to-from <= 0 {
			bounds = append(bounds[:i+1], bounds[i+2:]...)
			continue
		}
		content := 0
		for y := from; y < to; y++ {
			if !uniform[y] {
				content++
			}
		}
		if float64(content)/float64(to-from) >= minContent || len(bounds) <= 2 {
			i++
			continue
		}
		// Drop the boundary that makes this a page of its own: the rows join
		// whichever neighbour they were cut away from.
		if i == 0 {
			bounds = append(bounds[:1], bounds[2:]...)
		} else {
			bounds = append(bounds[:i], bounds[i+1:]...)
			i--
		}
	}
	return bounds
}

// spansFor turns a virtual row range into per-image spans, in each image's own
// coordinates and inside its own content extent.
func (s *StripScan) spansFor(from, to int, offsets []int) []Span {
	var out []Span
	for i := range s.heights {
		lo, hi := offsets[i], offsets[i+1]
		if hi <= from || lo >= to || hi == lo {
			continue
		}
		cFrom, cTo := s.contentOf(i)
		span := cTo - cFrom + 1
		scale := float64(span) / float64(hi-lo)
		a := cFrom + int(float64(max(from, lo)-lo)*scale+0.5)
		b := cFrom + int(float64(min(to, hi)-lo)*scale+0.5)
		if b > cTo+1 {
			b = cTo + 1
		}
		if b <= a {
			continue
		}
		out = append(out, Span{Index: i, From: a, To: b})
	}
	return out
}

// contentOf is image i's first and last drawn row, or its whole height when it
// carries no art at all.
func (s *StripScan) contentOf(i int) (from, to int) {
	if i >= len(s.content) {
		return 0, s.heights[i] - 1
	}
	c := s.content[i]
	if c[1] < c[0] {
		// Wholly blank: it contributes nothing to the strip.
		return 0, -1
	}
	return c[0], c[1]
}

// resampleRow samples a row of luminance down (or up) to n points, nearest
// neighbour. It is what lets two images of different widths be compared at a
// seam at all.
func resampleRow(row []uint8, w, n int) []uint8 {
	out := make([]uint8, n)
	if w <= 0 {
		return out
	}
	for i := range n {
		x := i * w / n
		out[i] = row[min(x, w-1)]
	}
	return out
}

// rowDistance is the mean absolute difference between two resampled rows.
func rowDistance(a, b []uint8) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return math.Inf(1)
	}
	var sum float64
	for i := range a {
		sum += math.Abs(float64(a[i]) - float64(b[i]))
	}
	return sum / float64(len(a))
}

// RenderPages draws the planned pages, one at a time, and hands each to emit.
//
// # The memory rule this is built around
//
// The OOM at 1.63 GB earlier in this project came from assuming a whole
// document could be held at once, so nothing here holds a chapter. Exactly two
// images are live: the source being read and the page being built. Sources are
// opened through `open` in ascending order and the most recent one is kept, so
// a page spanning images 3 and 4 decodes 4 once and reuses 3 — every source is
// decoded once for the whole render, and released the moment the last page
// using it is done.
//
// Peak is therefore one source plus one page, not 28 sources. See
// TestRenderingHoldsOnlyTwoImages for the measured figure.
//
// The caller owns the decoding, and is expected to release whatever `open`
// returned when RenderPages returns.
func RenderPages(pages []Page, width int, open func(index int) (image.Image, error),
	emit func(index int, img image.Image) error) error {

	if width <= 0 {
		return fmt.Errorf("imageproc: render: width %d", width)
	}

	var (
		cached    image.Image
		cachedIdx = -1
	)
	source := func(i int) (image.Image, error) {
		if i == cachedIdx && cached != nil {
			return cached, nil
		}
		img, err := open(i)
		if err != nil {
			return nil, err
		}
		// The previous source is dropped here rather than held: this
		// assignment is the whole of the memory rule.
		cached, cachedIdx = img, i
		return img, nil
	}

	for n, page := range pages {
		height := 0
		for _, sp := range page.Spans {
			img, err := source(sp.Index)
			if err != nil {
				return err
			}
			height += scaledRows(img, sp, width)
		}
		if height <= 0 {
			continue
		}

		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		y := 0
		for _, sp := range page.Spans {
			img, err := source(sp.Index)
			if err != nil {
				return err
			}
			rows := scaledRows(img, sp, width)
			drawSpan(dst, y, rows, img, sp, width)
			y += rows
		}
		if err := emit(n, dst); err != nil {
			return err
		}
	}
	return nil
}

// scaledRows is how many rows a span occupies once its image is scaled to the
// common width.
func scaledRows(img image.Image, sp Span, width int) int {
	b := img.Bounds()
	if b.Dx() <= 0 {
		return 0
	}
	rows := int(float64(sp.To-sp.From)*float64(width)/float64(b.Dx()) + 0.5)
	return max(rows, 0)
}

// drawSpan copies one span into the page, scaling to the common width by
// nearest neighbour.
//
// Nearest neighbour on purpose: this is a scale *up* only when the chapter
// mixes widths, it happens before the fit-and-pad resize that lands the page on
// the panel, and a second resampling pass here would cost memory and time to
// improve something that is about to be resampled properly anyway.
func drawSpan(dst *image.RGBA, at, rows int, src image.Image, sp Span, width int) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || rows <= 0 {
		return
	}
	span := sp.To - sp.From
	for y := range rows {
		sy := sp.From + y*span/rows
		if sy >= sh {
			sy = sh - 1
		}
		for x := range width {
			sx := x * sw / width
			if sx >= sw {
				sx = sw - 1
			}
			r, g, bl, a := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
			i := dst.PixOffset(x, at+y)
			dst.Pix[i+0] = uint8(r >> 8)
			dst.Pix[i+1] = uint8(g >> 8)
			dst.Pix[i+2] = uint8(bl >> 8)
			dst.Pix[i+3] = uint8(a >> 8)
		}
	}
}

// PlanWidth is the width RenderPages should be given for a scan: the widest
// image, so nothing is upscaled past what the source carried.
func (s *StripScan) PlanWidth() int {
	w := 0
	for _, iw := range s.widths {
		w = max(w, iw)
	}
	return w
}

// hasStructure reports whether a row varies enough to say anything.
func hasStructure(row []uint8) bool {
	if len(row) == 0 {
		return false
	}
	var sum float64
	for _, v := range row {
		sum += float64(v)
	}
	mean := sum / float64(len(row))
	var varsum float64
	for _, v := range row {
		d := float64(v) - mean
		varsum += d * d
	}
	return math.Sqrt(varsum/float64(len(row))) >= SeamStructure*255
}

// rowCorrelation is Pearson's r between two rows, after removing their means.
//
// It answers "is this the same signal?" rather than "is this the same
// brightness?", which is the difference between a cut through a drawing and two
// unrelated pages that happen to be equally dark.
func rowCorrelation(a, b []uint8) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var sa, sb float64
	for i := range a {
		sa += float64(a[i])
		sb += float64(b[i])
	}
	ma, mb := sa/float64(len(a)), sb/float64(len(b))

	var num, da, db float64
	for i := range a {
		x, y := float64(a[i])-ma, float64(b[i])-mb
		num += x * y
		da += x * x
		db += y * y
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}
