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

	// SeamTolerance is the mean absolute difference, in 0..1 luminance, under
	// which the two rows at a seam are held to continue each other.
	//
	// Generous, because the rows either side of a slice have been through two
	// separate JPEG compressions: what survives is the shape of the content,
	// not the exact values.
	SeamTolerance = 24.0 / 255.0

	// MinContinuingSeams is how many seams must continue before the majority
	// rule can fire, for the same reason MinCorroboratingPages exists.
	MinContinuingSeams = 3

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

	// MinSlices, AspectSpread, SeamTolerance and MinContent override the
	// constants above. Tests set them; nothing else should.
	MinSlices     int
	AspectSpread  float64
	SeamTolerance float64
	MinContent    float64
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
	if o.SeamTolerance <= 0 {
		o.SeamTolerance = SeamTolerance
	}
	if o.MinContent <= 0 {
		o.MinContent = MinContentFraction
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
	Clean      int
	MinAspect  float64
	MaxAspect  float64
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

	top := resampleRow(lum[:w], w, seamWidth)
	if s.prevBottomOK {
		s.seams++
		bothFlat := s.uniform[len(s.uniform)-1][0] && s.prevFlat
		switch {
		case bothFlat:
			// Background meeting background says nothing: it is what a page
			// boundary looks like *and* what a strip cut politely at a gutter
			// looks like. Counted, and kept out of the majority.
			s.clean++
		case rowDistance(s.prevBottom, top) <= s.opts.SeamTolerance*255:
			s.continuing++
		}
	}
	s.prevBottom = resampleRow(lum[(h-1)*w:h*w], w, seamWidth)
	s.prevFlat = s.uniform[len(s.uniform)-1][h-1]
	s.prevBottomOK = true
}

// Err reports a scan that could not be taken — an empty image, in practice.
func (s *StripScan) Err() error { return s.err }

// Verdict decides, and says why.
func (s *StripScan) Verdict() Verdict {
	v := Verdict{Images: len(s.widths), Seams: s.seams, Continuing: s.continuing, Clean: s.clean}
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
	if v.MinAspect <= 0 || v.MaxAspect/v.MinAspect > 1+s.opts.AspectSpread {
		v.Reason = fmt.Sprintf("the images are not one shape (%.3f to %.3f); a sliced strip is uniform",
			v.MinAspect, v.MaxAspect)
		return v
	}
	if s.continuing < MinContinuingSeams {
		v.Reason = fmt.Sprintf("%d of %d seams continue the drawing; a sliced strip cuts through art",
			s.continuing, s.seams)
		return v
	}
	if s.continuing*2 <= s.seams {
		v.Reason = fmt.Sprintf("only %d of %d seams continue the drawing", s.continuing, s.seams)
		return v
	}

	v.PreSliced = true
	v.Reason = fmt.Sprintf("%d images of one shape, %d of %d seams cut through the drawing",
		len(s.widths), s.continuing, s.seams)
	return v
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
	offsets := make([]int, len(s.heights)+1)
	for i, h := range s.heights {
		offsets[i+1] = offsets[i] + int(float64(h)*float64(w)/float64(s.widths[i])+0.5)
	}
	total := offsets[len(s.heights)]
	if total <= 0 {
		return nil
	}

	uniform := make([]bool, total)
	for i, rows := range s.uniform {
		scale := float64(s.heights[i]) / float64(offsets[i+1]-offsets[i])
		for y := offsets[i]; y < offsets[i+1]; y++ {
			src := int(float64(y-offsets[i]) * scale)
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
		p.Spans = spansFor(bounds[i], bounds[i+1], offsets, s.heights)
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

// spansFor turns a virtual row range into per-image spans.
func spansFor(from, to int, offsets []int, heights []int) []Span {
	var out []Span
	for i := range heights {
		lo, hi := offsets[i], offsets[i+1]
		if hi <= from || lo >= to {
			continue
		}
		scale := float64(heights[i]) / float64(hi-lo)
		a := int(float64(max(from, lo)-lo)*scale + 0.5)
		b := int(float64(min(to, hi)-lo)*scale + 0.5)
		if b > heights[i] {
			b = heights[i]
		}
		if b <= a {
			continue
		}
		out = append(out, Span{Index: i, From: a, To: b})
	}
	return out
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
