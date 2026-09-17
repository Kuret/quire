package download_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/imageproc"
)

// slicedStrip serves one synthetic webtoon cut into equal chunks, the way a
// source that pre-slices does: panels of assorted heights, gutters between
// them, and cuts that pay no attention to either.
type slicedStrip struct {
	bodies map[string][]byte
	served int
}

func (s *slicedStrip) Get(ctx context.Context, url, referer string) (io.ReadCloser, error) {
	b, ok := s.bodies[url]
	if !ok {
		return nil, os.ErrNotExist
	}
	s.served++
	return io.NopCloser(bytes.NewReader(b)), nil
}

// newSlicedStrip builds a strip and serves it the way the measured source does:
// cut into equal chunks and each chunk letterboxed into a fixed frame.
//
// Both properties come from the corpus rather than from convenience. Sinners'
// Game Ch 29 is 58% white margin at the median, and its art is vertically
// continuous, so a cut through it leaves two nearly identical rows either side —
// which is what the detector measures. A fixture that slices edge to edge, or
// whose texture changes every row, is refused by the real rule and rightly so.
func newSlicedStrip(t *testing.T, w int, panels []int, gutter, n int) (*slicedStrip, download.Chapter) {
	t.Helper()

	h := 0
	for _, p := range panels {
		h += p + gutter
	}
	strip := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			strip.Set(x, y, color.RGBA{250, 250, 250, 255})
		}
	}
	y := gutter / 2
	for i, p := range panels {
		base := 40 + 30*(i%6)
		for row := range p {
			// Vertically smooth: neighbouring rows differ a little, as a
			// drawing does, so a cut through a panel leaves two rows that go
			// on from each other.
			shade := base + row/24
			for x := range w {
				v := uint8((shade + (x*7)%50) % 240)
				strip.Set(x, y+row, color.RGBA{v, v, uint8(int(v)+(x%5)*3) % 255, 255})
			}
		}
		y += p + gutter
	}

	f := &slicedStrip{bodies: map[string][]byte{}}
	ch := download.Chapter{ID: "ch-1", Title: "Chapter 1", Number: "1"}
	chunk := h / n
	pad := chunk / 3 // the frame, ~40% of the delivered image
	for i := range n {
		from := i * chunk
		to := from + chunk
		if i == n-1 {
			to = h
		}
		art := to - from
		framed := image.NewRGBA(image.Rect(0, 0, w, art+2*pad))
		for fy := range art + 2*pad {
			for x := range w {
				framed.Set(x, fy, color.RGBA{255, 255, 255, 255})
			}
		}
		for fy := range art {
			for x := range w {
				framed.Set(x, pad+fy, strip.At(x, from+fy))
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, framed, &jpeg.Options{Quality: 92}); err != nil {
			t.Fatal(err)
		}
		url := "https://example.invalid/strip/" + string(rune('a'+i)) + ".jpg"
		f.bodies[url] = buf.Bytes()
		ch.PageURLs = append(ch.PageURLs, url)
	}
	return f, ch
}

func chapterDir(root string, ch download.Chapter) string {
	return download.ChapterDir(root, ch.ID)
}

// The whole point: a chapter the source pre-sliced comes out re-cut, in its own
// directory, with the source images left where they are so a resume still
// works.
func TestAPreSlicedChapterIsReCut(t *testing.T) {
	dir := t.TempDir()
	f, ch := newSlicedStrip(t, 600, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40, 10)
	q := download.New(f, download.Options{MinFreeBytes: -1})

	out, stats, err := q.Run(t.Context(), dir, []download.Chapter{ch})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.ChaptersRestitched != 1 {
		t.Fatalf("ChaptersRestitched = %d, want 1", stats.ChaptersRestitched)
	}
	if stats.PagesFromRestitch == 0 {
		t.Error("no pages were reported as coming from the re-cut")
	}

	rd := filepath.Join(chapterDir(dir, ch), download.RestitchDirName)
	if _, err := os.Stat(filepath.Join(rd, "quire-restitch.json")); err != nil {
		t.Fatalf("no marker: %v", err)
	}

	// The chapter's pages are the re-cut ones, in order, and they are inside
	// the subdirectory rather than beside the sources.
	if len(out) != 1 || len(out[0].Pages) == 0 {
		t.Fatal("no pages came back")
	}
	for i, p := range out[0].Pages {
		if filepath.Dir(p.Path) != rd {
			t.Errorf("page %d is %s, want it under %s", i, p.Path, rd)
		}
		if p.Index != i {
			t.Errorf("page %d has index %d", i, p.Index)
		}
	}

	// The sources are still there: that is what resume runs on.
	sources := 0
	entries, err := os.ReadDir(chapterDir(dir, ch))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			sources++
		}
	}
	if sources != len(ch.PageURLs) {
		t.Errorf("%d source images left, want %d", sources, len(ch.PageURLs))
	}
}

// A re-cut that died before its marker landed must be redone. A partial result
// that looks whole is worse than no result.
func TestAReCutWithoutItsMarkerIsRedone(t *testing.T) {
	dir := t.TempDir()
	f, ch := newSlicedStrip(t, 600, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40, 10)
	q := download.New(f, download.Options{MinFreeBytes: -1})

	if _, _, err := q.Run(t.Context(), dir, []download.Chapter{ch}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	rd := filepath.Join(chapterDir(dir, ch), download.RestitchDirName)

	// The crash: pages on disk, marker never written.
	if err := os.Remove(filepath.Join(rd, "quire-restitch.json")); err != nil {
		t.Fatal(err)
	}
	before := namesIn(t, rd)
	if len(before) == 0 {
		t.Fatal("the first run wrote no pages")
	}
	// And one page lost with it, so "redone" is observable rather than assumed.
	if err := os.Remove(filepath.Join(rd, before[0])); err != nil {
		t.Fatal(err)
	}

	_, stats, err := q.Run(t.Context(), dir, []download.Chapter{ch})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if stats.ChaptersRestitched != 1 {
		t.Errorf("ChaptersRestitched = %d; the chapter was not re-cut again", stats.ChaptersRestitched)
	}
	if _, err := os.Stat(filepath.Join(rd, "quire-restitch.json")); err != nil {
		t.Errorf("the marker did not land on the redo: %v", err)
	}
	if got := namesIn(t, rd); len(got) != len(before) {
		t.Errorf("%d pages after the redo, want %d", len(got), len(before))
	}
}

// A marker that matches the sources is reused: the pages are not made again.
func TestAValidMarkerIsReusedWithoutReCutting(t *testing.T) {
	dir := t.TempDir()
	f, ch := newSlicedStrip(t, 600, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40, 10)
	q := download.New(f, download.Options{MinFreeBytes: -1})

	if _, _, err := q.Run(t.Context(), dir, []download.Chapter{ch}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	rd := filepath.Join(chapterDir(dir, ch), download.RestitchDirName)

	// Modification times are the evidence that the files were not rewritten.
	stamps := map[string]time.Time{}
	for _, name := range namesIn(t, rd) {
		st, err := os.Stat(filepath.Join(rd, name))
		if err != nil {
			t.Fatal(err)
		}
		stamps[name] = st.ModTime()
	}

	out, stats, err := q.Run(t.Context(), dir, []download.Chapter{ch})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if stats.ChaptersRestitched != 0 {
		t.Errorf("ChaptersRestitched = %d; the chapter was re-cut a second time", stats.ChaptersRestitched)
	}
	for name, was := range stamps {
		st, err := os.Stat(filepath.Join(rd, name))
		if err != nil {
			t.Fatalf("%s went missing: %v", name, err)
		}
		if !st.ModTime().Equal(was) {
			t.Errorf("%s was rewritten (%s -> %s)", name, was, st.ModTime())
		}
	}
	// And the chapter still comes back pointing at them.
	if len(out) != 1 || len(out[0].Pages) != len(stamps) {
		t.Errorf("the reused chapter has %d pages, want %d", len(out[0].Pages), len(stamps))
	}
}

// A marker made from different sources is not a result for these sources.
func TestAMarkerFromOtherSourcesIsIgnored(t *testing.T) {
	dir := t.TempDir()
	f, ch := newSlicedStrip(t, 600, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40, 10)
	q := download.New(f, download.Options{MinFreeBytes: -1})

	if _, _, err := q.Run(t.Context(), dir, []download.Chapter{ch}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	rd := filepath.Join(chapterDir(dir, ch), download.RestitchDirName)

	// Rewrite the marker as if it had been made from sources of other sizes —
	// a chapter the site re-cut and re-served.
	var m struct {
		Version int `json:"version"`
		Sources []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"sources"`
		Pages []string `json:"pages"`
	}
	b, err := os.ReadFile(filepath.Join(rd, "quire-restitch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for i := range m.Sources {
		m.Sources[i].Size += 17
	}
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rd, "quire-restitch.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	_, stats, err := q.Run(t.Context(), dir, []download.Chapter{ch})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if stats.ChaptersRestitched != 1 {
		t.Errorf("ChaptersRestitched = %d; a stale marker was served instead of re-cutting",
			stats.ChaptersRestitched)
	}
}

// "never" means leave this source alone, and that has to keep being true of the
// chapter-level path as well as the per-image one.
func TestNeverKeepsTheSourcePages(t *testing.T) {
	dir := t.TempDir()
	f, ch := newSlicedStrip(t, 600, []int{500, 300, 700, 400, 650, 350, 500, 450}, 40, 10)
	q := download.New(f, download.Options{MinFreeBytes: -1, SplitStrips: imageproc.SplitNever})

	out, stats, err := q.Run(t.Context(), dir, []download.Chapter{ch})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.ChaptersRestitched != 0 {
		t.Errorf("ChaptersRestitched = %d with splitStrips never", stats.ChaptersRestitched)
	}
	if _, err := os.Stat(filepath.Join(chapterDir(dir, ch), download.RestitchDirName)); !os.IsNotExist(err) {
		t.Errorf("a restitched directory exists though the source is set to never: %v", err)
	}
	if len(out[0].Pages) != len(ch.PageURLs) {
		t.Errorf("%d pages, want the %d the source sent", len(out[0].Pages), len(ch.PageURLs))
	}
}

// An ordinary page-per-image chapter is left completely alone by the whole
// path, on disk as well as in the plan.
func TestAnOrdinaryChapterGetsNoRestitchDirectory(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1200, 1700)}
	q := download.New(f, download.Options{MinFreeBytes: -1})

	out, stats, err := q.Run(t.Context(), dir, chapters(1, 8))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.ChaptersRestitched != 0 {
		t.Errorf("an ordinary chapter was re-cut (%d)", stats.ChaptersRestitched)
	}
	if len(out[0].Pages) != 8 {
		t.Errorf("%d pages, want the 8 that were downloaded", len(out[0].Pages))
	}
}

func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "quire-restitch.json" {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// "always" is the way in when detection is wrong about a chapter. It waives the
// evidence — the padding and seam tests — and keeps only the minimum image
// count, because re-cutting three images is meaningless whatever the user asks.
//
// The chapter here is an ordinary one that detection correctly leaves alone, so
// the only thing that can re-cut it is the override.
func TestAlwaysForcesAChapterDetectionWouldRefuse(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1200, 1700)}
	q := download.New(f, download.Options{MinFreeBytes: -1, SplitStrips: imageproc.SplitAlways})

	_, stats, err := q.Run(t.Context(), dir, chapters(1, 8))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.ChaptersRestitched != 1 {
		t.Errorf("ChaptersRestitched = %d with splitStrips always", stats.ChaptersRestitched)
	}
}

// Three images is not a chapter to re-cut, whatever the override says.
func TestAlwaysStillNeedsEnoughImages(t *testing.T) {
	dir := t.TempDir()
	f := &stubFetcher{body: synthJPEG(t, 1200, 1700)}
	q := download.New(f, download.Options{MinFreeBytes: -1, SplitStrips: imageproc.SplitAlways})

	_, stats, err := q.Run(t.Context(), dir, chapters(1, 3))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.ChaptersRestitched != 0 {
		t.Errorf("ChaptersRestitched = %d for a three-image chapter", stats.ChaptersRestitched)
	}
}
