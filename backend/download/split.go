package download

// Strip splitting in the page queue — PLAN §12.3.
//
// # Where the decision is made, and why it is in two places
//
// Detection is per image, but it needs corroboration from the rest of the
// chapter, and pages are fetched concurrently and out of order. Rather than
// stage the whole volume before encoding anything, the queue splits the
// decision by how much evidence one image carries on its own:
//
//   - Past imageproc.SplitExtremeAspect, nothing else explains the shape. The
//     image is split immediately, in the fetch worker, from the decoded pixels
//     it already has.
//   - Below imageproc.SplitAspectThreshold, it is an ordinary page. Normalised
//     immediately, exactly as before this feature existed.
//   - In between — the band where a lone tall image is probably a spread or an
//     author's note — the raw bytes are parked on disk and the decision is
//     taken once the chapter's shape is known. This band is narrow (h:w 3.76
//     to 10) and rare, so the extra disk round-trip is paid almost never.
//
// The deferral is what makes "a single tall page in an otherwise ordinary
// chapter is left alone" true rather than aspirational.
//
// # Naming, and the crash that would otherwise be silent
//
// An unsplit page is `%04d.jpg` as it always was. A split one becomes
// `%04d-%03d-of-%03d.jpg`. The piece *count* is in the name on purpose: a
// crash halfway through writing a strip's pieces would otherwise leave a
// directory that looks complete to the resume check, and the volume would come
// back missing panels with nothing to show for it.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/rickl/quire/backend/imageproc"
)

// peekBytes is how much of a response is inspected for the image header before
// anything is decoded. A JPEG SOF, a PNG IHDR and a WebP VP8X all sit in the
// first few kilobytes; 64 KiB covers a source that puts a large EXIF thumbnail
// in front of it. When the header cannot be read the page is treated as
// ordinary — the bias is always toward doing nothing.
const peekBytes = 64 << 10

// pieceName formats one piece of a split page. The count is part of the name;
// see the package comment above.
func pieceName(page, piece, total int) string {
	return fmt.Sprintf("%04d-%03d-of-%03d.jpg", page, piece, total)
}

func wholeName(page int) string { return fmt.Sprintf("%04d.jpg", page) }

var pieceRE = regexp.MustCompile(`^(\d{4})-(\d{3})-of-(\d{3})\.jpg$`)

// pageFiles is what one source image became on disk.
type pageFiles struct {
	// paths are the output pages, in reading order. One entry for an ordinary
	// page, several for a split strip.
	paths []string

	// tall records that the source image was a split candidate. It feeds the
	// corroboration count, which is why it is carried rather than recomputed:
	// by the time the count is needed the source pixels are long gone.
	tall bool
}

// existingPages reports the output already on disk for one source page, so a
// resumed run skips it. It returns nil when the page is absent or incomplete.
//
// A page found as a whole file is reported as not tall. That is a deliberate
// under-count: we cannot recover the source aspect from a normalised 3:4 page,
// and guessing "tall" would be guessing in the direction that splits things.
// The effect is that a resumed run reaches the same verdict it reached first
// time round, because the naming on disk already records what was decided.
func existingPages(dir string, page int) (pageFiles, bool) {
	whole := filepath.Join(dir, wholeName(page))
	if st, err := os.Stat(whole); err == nil && st.Size() > 0 {
		return pageFiles{paths: []string{whole}}, true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return pageFiles{}, false
	}
	prefix := fmt.Sprintf("%04d-", page)
	var (
		found []string
		total = -1
	)
	// Directories are skipped explicitly rather than left to the filename
	// pattern: the chapter directory now holds a `restitched/` subdirectory
	// (PLAN §12.3), and a scanner that relies on a name not matching is a
	// scanner that breaks the next time a name is added.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		m := pieceRE.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[3])
		if total >= 0 && n != total {
			return pageFiles{}, false // two different splits in one directory
		}
		total = n
		st, err := e.Info()
		if err != nil || st.Size() == 0 {
			return pageFiles{}, false
		}
		found = append(found, filepath.Join(dir, name))
	}
	if total <= 0 || len(found) != total {
		return pageFiles{}, false // partial: redo the whole page
	}
	sort.Strings(found)
	return pageFiles{paths: found, tall: true}, true
}

// removePageFiles clears any output for a source page, so a redo cannot leave
// a mixture of an old split and a new one behind.
func removePageFiles(dir string, page int) {
	os.Remove(filepath.Join(dir, wholeName(page)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := fmt.Sprintf("%04d-", page)
	for _, e := range entries {
		// Directories skipped explicitly, for the reason given in
		// existingPages: `restitched/` lives here too, and it is not a page
		// file however its name reads.
		if e.IsDir() {
			continue
		}
		if name := e.Name(); len(name) >= len(prefix) && name[:len(prefix)] == prefix && pieceRE.MatchString(name) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// peekConfig reads the image header without consuming it, so the body can then
// be streamed to the normaliser as before. An unreadable header returns ok
// false and the caller treats the page as ordinary.
func peekConfig(br *bufio.Reader) (w, h int, ok bool) {
	head, _ := br.Peek(peekBytes)
	if len(head) == 0 {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// verdict is what the fetch worker decides to do with an image on sight.
type verdict int

const (
	verdictNormalise verdict = iota // an ordinary page; encode it now
	verdictSplit                    // unmistakably a strip; cut it now
	verdictDefer                    // tall, but a lone tall image is an outlier
)

// classify applies the per-image half of PLAN §12.3's detection.
func classify(mode imageproc.SplitMode, w, h int, ok bool) verdict {
	if mode == imageproc.SplitNever {
		return verdictNormalise
	}
	if mode == imageproc.SplitAlways {
		// The user has overruled detection, so there is nothing to corroborate.
		// The splitter still leaves anything that fits one page alone.
		return verdictSplit
	}
	if !ok || !imageproc.IsSplitCandidate(w, h) {
		return verdictNormalise
	}
	if imageproc.IsExtremeStrip(w, h) {
		return verdictSplit
	}
	return verdictDefer
}

// deferredStrip is one page held back for the chapter-wide decision.
type deferredStrip struct {
	j    job
	raw  string // the fetched bytes, verbatim
	w, h int
}

// resolveDeferred is the second half of detection: every page in the band that
// needs corroboration, judged now that the chapter's shape is known.
func (r *run) resolveDeferred(ctx context.Context) error {
	r.mu.Lock()
	pending := r.deferred
	r.deferred = nil
	r.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	defer func() {
		for _, d := range pending {
			os.Remove(d.raw)
		}
	}()

	// Corroboration counts, per chapter. A deferred page is a candidate by
	// definition; the rest come from what phase one recorded, including pages
	// skipped as already present.
	tall := make([]int, len(r.results))
	for c, pages := range r.results {
		for _, p := range pages {
			if p.tall {
				tall[c]++
			}
		}
	}
	for _, d := range pending {
		tall[d.j.chapter]++
	}

	for _, d := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		total := len(r.results[d.j.chapter])
		split := imageproc.ShouldSplit(r.q.opts.SplitStrips, d.w, d.h, tall[d.j.chapter], total)

		files, stored, err := r.encodeDeferred(ctx, d, split)
		if err != nil {
			return err
		}
		r.results[d.j.chapter][d.j.page] = files
		r.bytesStored.Add(stored)
		r.checkWarn()
		r.report()
	}
	return nil
}

// encodeDeferred decodes a parked strip and writes its pages.
func (r *run) encodeDeferred(ctx context.Context, d *deferredStrip, split bool) (pageFiles, int64, error) {
	f, err := os.Open(d.raw)
	if err != nil {
		return pageFiles{}, 0, fmt.Errorf("download: reopening deferred page %s: %w", d.j.url, err)
	}
	defer f.Close()

	var (
		files  pageFiles
		stored int64
	)
	err = r.withEncoder(ctx, func() error {
		img, _, derr := image.Decode(bufio.NewReaderSize(f, peekBytes))
		if derr != nil {
			return fmt.Errorf("download: %s: decode: %w", d.j.url, derr)
		}
		files, stored, derr = r.writePages(d.j, img, split)
		return derr
	})
	if err != nil {
		return pageFiles{}, 0, err
	}
	files.tall = true
	return files, stored, nil
}

// writePages normalises an image into one or more page files.
//
// The split happens here, *before* the fit-and-pad resize, so each piece is
// resized on its own rather than the whole strip being resampled once and cut
// afterwards. PLAN §12.3 asks for that for memory reasons and it is also the
// only way the pieces come out at panel resolution: fitting a 800 × 20000
// strip first would leave an 86 px-wide sliver to cut up.
//
// The caller must hold the encode semaphore.
func (r *run) writePages(j job, img image.Image, split bool) (pageFiles, int64, error) {
	dir := filepath.Dir(j.path)
	rects := []image.Rectangle{img.Bounds()}
	if split {
		rects, _ = imageproc.SplitStrip(img, r.q.opts.Split)
	}

	// A "split" that yields one piece is not a split: keep the ordinary name so
	// the file, and a later resume, look exactly like any other page.
	if len(rects) == 1 {
		n, err := r.writeOne(j.url, dir, wholeName(j.page), img)
		if err != nil {
			return pageFiles{}, 0, err
		}
		return pageFiles{paths: []string{filepath.Join(dir, wholeName(j.page))}}, n, nil
	}

	removePageFiles(dir, j.page)
	var (
		out    pageFiles
		stored int64
	)
	for k, rect := range rects {
		name := pieceName(j.page, k, len(rects))
		n, err := r.writeOne(j.url, dir, name, imageproc.Crop(img, rect))
		if err != nil {
			return pageFiles{}, stored, err
		}
		stored += n
		out.paths = append(out.paths, filepath.Join(dir, name))
	}
	r.pagesSplit.Add(1)
	r.pagesFromSplit.Add(int64(len(rects)))
	if cb := r.q.opts.OnSplit; cb != nil {
		r.mu.Lock()
		cb(j.url, len(rects))
		r.mu.Unlock()
	}
	return out, stored, nil
}

// writeOne normalises a single image to dir/name via temp-and-rename, so a page
// file that exists is a page file that is whole.
func (r *run) writeOne(url, dir, name string, img image.Image) (int64, error) {
	tmp, err := os.CreateTemp(dir, ".page-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(tmpName)
		}
	}()

	res, err := imageproc.NormaliseImage(tmp, img, r.q.opts.Image)
	if err != nil {
		return 0, err
	}
	r.countResult(url, res)
	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("download: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("download: close: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return 0, fmt.Errorf("download: rename: %w", err)
	}
	committed = true
	return res.Bytes, nil
}

// parkStrip writes the fetched bytes aside for resolveDeferred.
func (r *run) parkStrip(j job, w, h int, src io.Reader) (*deferredStrip, int64, error) {
	dir := filepath.Dir(j.path)
	tmp, err := os.CreateTemp(dir, ".strip-*.raw")
	if err != nil {
		return nil, 0, fmt.Errorf("download: %w", err)
	}
	n, err := io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return nil, n, fmt.Errorf("download: parking %s: %w", j.url, err)
	}
	return &deferredStrip{j: j, raw: tmp.Name(), w: w, h: h}, n, nil
}

// sweepParked removes raw strips left by an interrupted run.
func sweepParked(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			sweepParked(filepath.Join(dir, e.Name()))
			continue
		}
		if name := e.Name(); len(name) > 7 && name[:7] == ".strip-" && filepath.Ext(name) == ".raw" {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
