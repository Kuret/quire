package download_test

// Strip splitting through the queue — PLAN §12.3.
//
// The imageproc tests prove the cuts land in gutters. These prove the queue
// does the right things around them: the detection is per image and needs
// corroboration, one source image becoming eight pages renumbers the chapter
// from 0, the override works, and a resumed run reaches the same verdict it
// reached the first time.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/imageproc"
)

// pageJPEG draws a page that compresses like line art rather than like noise,
// so the per-page byte budget is not what these tests end up measuring.
func pageJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{235, 235, 235, 255}
			switch {
			case y%400 < 14: // a horizontal gutter every 400 rows
			case (x/17+y/23)%5 == 0:
				c = color.RGBA{40, 40, 40, 255}
			case (x+y)%64 < 8:
				c = color.RGBA{120, 120, 120, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// configOf reads an output page's dimensions without decoding it.
func configOf(t *testing.T, path string) image.Config {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return cfg
}

// mapFetcher serves a different body per URL, which is what a chapter of mixed
// geometry needs and the shared stubFetcher cannot do.
type mapFetcher struct {
	mu     sync.Mutex
	bodies map[string][]byte
	calls  map[string]int
}

func newMapFetcher() *mapFetcher {
	return &mapFetcher{bodies: map[string][]byte{}, calls: map[string]int{}}
}

func (f *mapFetcher) add(url string, body []byte) { f.bodies[url] = body }

func (f *mapFetcher) Get(ctx context.Context, url, referer string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.bodies[url]
	if !ok {
		return nil, fmt.Errorf("no body for %s", url)
	}
	f.calls[url]++
	return io.NopCloser(strings.NewReader(string(body))), nil
}

// chapterOf builds a chapter whose page i has the geometry sizes[i].
func chapterOf(t *testing.T, f *mapFetcher, id string, sizes [][2]int) download.Chapter {
	t.Helper()
	ch := download.Chapter{ID: id, Title: "Chapter " + id, Number: id}
	for i, s := range sizes {
		u := fmt.Sprintf("https://example.invalid/%s/%d.jpg", id, i)
		f.add(u, pageJPEG(t, s[0], s[1]))
		ch.PageURLs = append(ch.PageURLs, u)
	}
	return ch
}

func runQueue(t *testing.T, dir string, f *mapFetcher, mode imageproc.SplitMode, chs ...download.Chapter) ([]assemble.Chapter, download.Stats) {
	t.Helper()
	q := download.New(f, download.Options{
		Concurrency:   2,
		EncodeWorkers: 2,
		SplitStrips:   mode,
		NoMemoryLimit: true,
	})
	out, stats, err := q.Run(context.Background(), dir, chs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out, stats
}

// checkIndices is the constraint assemble enforces and PLAN §12.3 calls out:
// a chapter's pages start at 0 and run in order, however many source images
// they came from.
func checkIndices(t *testing.T, chs []assemble.Chapter) {
	t.Helper()
	for _, ch := range chs {
		for i, p := range ch.Pages {
			if p.Index != i {
				t.Errorf("chapter %s: page %d has Index %d; assemble rejects that", ch.ID, i, p.Index)
			}
			if st, err := os.Stat(p.Path); err != nil || st.Size() == 0 {
				t.Errorf("chapter %s: page %d (%s) is missing or empty", ch.ID, i, p.Path)
			}
		}
	}
}

// TestStripChapterIsSplitAndRenumbered is the feature working: a chapter of
// webtoon strips comes back as many more pages than it had source images, in
// order and starting at 0.
func TestStripChapterIsSplitAndRenumbered(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	// 800 × 8000 is the measured webtoon shape: h:w 10, past the extreme.
	ch := chapterOf(t, f, "ep1", [][2]int{{800, 8000}, {800, 8000}, {800, 6000}})

	out, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, out)

	if stats.PagesSplit != 3 {
		t.Errorf("PagesSplit = %d, want 3; every image in this chapter is a strip", stats.PagesSplit)
	}
	if len(out[0].Pages) != stats.PagesFromSplit {
		t.Errorf("chapter has %d pages but PagesFromSplit = %d", len(out[0].Pages), stats.PagesFromSplit)
	}
	if len(out[0].Pages) < 15 {
		t.Errorf("three strips totalling 22000 rows became %d pages; expected around 21", len(out[0].Pages))
	}
	if stats.PagesTotal != 3 {
		t.Errorf("PagesTotal = %d; it counts source images, not output pages", stats.PagesTotal)
	}
	t.Logf("3 strips (8000+8000+6000 rows) → %d pages", len(out[0].Pages))
}

// TestLoneTallPageInAnOrdinaryChapterIsNotSplit is the regression the user is
// worried about, end to end: one author's-note-shaped image among ordinary
// manga pages must come through whole.
func TestLoneTallPageInAnOrdinaryChapterIsNotSplit(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	sizes := [][2]int{}
	for range 9 {
		sizes = append(sizes, [2]int{1200, 1700})
	}
	// h:w 4.5 — past the candidate threshold, short of the extreme.
	sizes = append(sizes, [2]int{800, 3600})
	ch := chapterOf(t, f, "c12", sizes)

	out, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, out)

	if stats.PagesSplit != 0 {
		t.Errorf("PagesSplit = %d: a lone tall image in a chapter of nine ordinary pages was split", stats.PagesSplit)
	}
	if len(out[0].Pages) != 10 {
		t.Errorf("10 source images became %d pages; nothing should have been cut", len(out[0].Pages))
	}
	for _, p := range out[0].Pages {
		if strings.Contains(filepath.Base(p.Path), "-of-") {
			t.Errorf("%s is a split piece", p.Path)
		}
	}
}

// TestOrdinaryChapterIsUntouched is the blunt version of the same worry: a
// whole chapter of real page geometry, and not a single cut.
func TestOrdinaryChapterIsUntouched(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "c1", [][2]int{
		{1200, 1700}, // typical page
		{2480, 3508}, // A4 scan
		{2550, 3300}, // letter scan
		{3200, 2200}, // double-page spread
		{1200, 3400}, // two pages stacked vertically
	})

	out, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, out)
	if stats.PagesSplit != 0 || stats.PagesFromSplit != 0 {
		t.Errorf("PagesSplit=%d PagesFromSplit=%d: real page geometry was split", stats.PagesSplit, stats.PagesFromSplit)
	}
	if len(out[0].Pages) != 5 {
		t.Errorf("5 pages in, %d out", len(out[0].Pages))
	}
}

// TestCorroborationSplitsAChapterThatIsMostlyTall exercises the deferral path:
// every image is in the band that needs the chapter's agreement, and gets it.
func TestCorroborationSplitsAChapterThatIsMostlyTall(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	sizes := [][2]int{}
	for range 5 {
		sizes = append(sizes, [2]int{800, 3600}) // h:w 4.5, candidate not extreme
	}
	ch := chapterOf(t, f, "ep7", sizes)

	out, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, out)
	if stats.PagesSplit != 5 {
		t.Fatalf("PagesSplit = %d; five tall images corroborate each other and should all be split", stats.PagesSplit)
	}
	if len(out[0].Pages) <= 5 {
		t.Errorf("five 3600-row strips became %d pages", len(out[0].Pages))
	}

	// And the parked raw bytes are gone.
	assertNoParkedFiles(t, dir)
}

// TestOverrideNeverKeepsAStripWhole — the escape hatch, in the direction that
// matters most: the user sees a bad result and stops it without waiting for us.
func TestOverrideNeverKeepsAStripWhole(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "ep1", [][2]int{{800, 8000}, {800, 8000}})

	out, stats := runQueue(t, dir, f, imageproc.SplitNever, ch)
	checkIndices(t, out)
	if stats.PagesSplit != 0 {
		t.Errorf("never split %d images anyway", stats.PagesSplit)
	}
	if len(out[0].Pages) != 2 {
		t.Errorf("never produced %d pages from 2 images", len(out[0].Pages))
	}
}

// TestOverrideAlwaysSplitsTheBandDetectionRefuses — and, crucially, still
// leaves an ordinary page as one page.
func TestOverrideAlwaysSplitsTheBandDetectionRefuses(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "c3", [][2]int{
		{1200, 1700}, // ordinary: one page even under always
		{800, 3600},  // the band auto leaves alone without corroboration
	})

	out, stats := runQueue(t, dir, f, imageproc.SplitAlways, ch)
	checkIndices(t, out)
	if stats.PagesSplit != 1 {
		t.Errorf("PagesSplit = %d, want 1: the tall image and only the tall image", stats.PagesSplit)
	}
	if got := filepath.Base(out[0].Pages[0].Path); strings.Contains(got, "-of-") {
		t.Errorf("under always, the 1200×1700 page was cut up (%s)", got)
	}
}

// TestSplitPageOffsetsFollowTheNewPageCount is PLAN §12.3's first constraint,
// and the one that breaks "Read" silently if it is missed. The offsets are
// derived by assemble from the page counts download hands it, so this checks
// the whole chain rather than the arithmetic in isolation.
func TestSplitPageOffsetsFollowTheNewPageCount(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	strips := chapterOf(t, f, "ep1", [][2]int{{800, 8000}})
	ordinary := chapterOf(t, f, "ep2", [][2]int{{1200, 1700}, {1200, 1700}, {1200, 1700}})

	out, _ := runQueue(t, dir, f, imageproc.SplitAuto, strips, ordinary)
	checkIndices(t, out)

	first := len(out[0].Pages)
	if first < 5 {
		t.Fatalf("the strip became %d pages; the test needs it to be several", first)
	}

	vol := assemble.Volume{Series: "Test", Label: "1", Title: "Test v1", Chapters: out}
	m, err := assemble.Assemble(context.Background(), t.TempDir(), vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if m.PageCount != first+3 {
		t.Errorf("manifest PageCount = %d, want %d", m.PageCount, first+3)
	}
	off, ok := m.PageFor("ep2")
	if !ok {
		t.Fatal("ep2 has no offset in the manifest")
	}
	if off != first {
		t.Errorf("ep2 opens at page %d, want %d: the offset did not follow the split", off, first)
	}
}

// TestResumeKeepsTheSplitDecision — the naming on disk records what was
// decided, so a second run neither refetches nor re-judges.
func TestResumeKeepsTheSplitDecision(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "ep1", [][2]int{{800, 8000}, {1200, 1700}})

	first, _ := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	second, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, second)

	if stats.PagesFetched != 0 {
		t.Errorf("a resumed run refetched %d pages", stats.PagesFetched)
	}
	if len(first[0].Pages) != len(second[0].Pages) {
		t.Fatalf("resume produced %d pages, first run %d", len(second[0].Pages), len(first[0].Pages))
	}
	for i := range first[0].Pages {
		if first[0].Pages[i].Path != second[0].Pages[i].Path {
			t.Errorf("page %d: %s then %s", i, first[0].Pages[i].Path, second[0].Pages[i].Path)
		}
	}
}

// TestPartialSplitIsRedoneRatherThanTrusted — a crash between the third and
// fourth piece of a strip must not leave a page set that looks complete. The
// count is in the filename precisely so this is detectable.
func TestPartialSplitIsRedoneRatherThanTrusted(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "ep1", [][2]int{{800, 8000}})

	first, _ := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	want := len(first[0].Pages)
	if want < 4 {
		t.Fatalf("the strip became %d pages; the test needs several", want)
	}
	if err := os.Remove(first[0].Pages[2].Path); err != nil {
		t.Fatal(err)
	}

	second, stats := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	checkIndices(t, second)
	if stats.PagesFetched != 1 {
		t.Errorf("a strip missing a piece was not refetched (PagesFetched=%d)", stats.PagesFetched)
	}
	if len(second[0].Pages) != want {
		t.Errorf("after the redo the chapter has %d pages, want %d", len(second[0].Pages), want)
	}
}

// TestSplitPiecesFillThePanelRatherThanSliver is the point of the whole
// feature: before it, an 800 × 8000 strip normalised to a sliver a few dozen
// pixels wide inside a white 3:4 page.
func TestSplitPiecesFillThePanelRatherThanSliver(t *testing.T) {
	dir := t.TempDir()
	f := newMapFetcher()
	ch := chapterOf(t, f, "ep1", [][2]int{{800, 8000}})

	out, _ := runQueue(t, dir, f, imageproc.SplitAuto, ch)
	for i, p := range out[0].Pages {
		cfg := configOf(t, p.Path)
		ratio := float64(cfg.Height) / float64(cfg.Width)
		if ratio > 1.45 || ratio < 1.25 {
			t.Errorf("page %d is %d×%d (h:w %.2f); a split piece should be about 4:3", i, cfg.Width, cfg.Height, ratio)
		}
		if cfg.Width < 600 {
			t.Errorf("page %d is only %d px wide — that is the sliver this feature exists to remove", i, cfg.Width)
		}
	}
}

func assertNoParkedFiles(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(d.Name(), ".strip-") || strings.HasSuffix(d.Name(), ".tmp") {
			t.Errorf("scratch file left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
