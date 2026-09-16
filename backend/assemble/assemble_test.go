package assemble_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/imageproc"
)

// All page images in this package are synthesised here. PLAN §1.3/§1.4: no
// network in tests, and no image sourced from a real site in the repository.

// writePage writes one normalised page carrying a visible page number, so
// page *order* can be checked by reading the PDF back rather than trusted.
func writePage(t *testing.T, dir string, n int, w, h int) string {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{235, 235, 235, 255}
			if x < 6 || y < 6 || x >= w-6 || y >= h-6 {
				c = color.RGBA{20, 20, 20, 255}
			}
			src.Set(x, y, c)
		}
	}
	// A bar chart of n blocks along the top edge: survives JPEG and any
	// rescale, and is readable straight out of the decoded pixels.
	stamp(src, n)

	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, src, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatalf("encode source: %v", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("p%04d.jpg", n))
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	defer f.Close()
	if _, err := imageproc.Normalise(f, bytes.NewReader(raw.Bytes()), imageproc.DefaultOptions()); err != nil {
		t.Fatalf("normalise page %d: %v", n, err)
	}
	return path
}

// stamp draws n black blocks in a row near the top of img, each block 1/64 of
// the width, on a white strip.
func stamp(img *image.RGBA, n int) {
	b := img.Bounds()
	unit := b.Dx() / 64
	if unit < 1 {
		unit = 1
	}
	top, bottom := b.Dy()/8, b.Dy()/8+unit*2
	for y := top; y < bottom && y < b.Dy(); y++ {
		for x := unit; x < b.Dx()-unit; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for i := range n {
		x0 := unit + i*unit*2
		for y := top + unit/2; y < bottom-unit/2 && y < b.Dy(); y++ {
			for x := x0; x < x0+unit && x < b.Dx(); x++ {
				img.Set(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
}

// readStamp counts the blocks stamp drew, from a rendered page image.
func readStamp(img image.Image) int {
	b := img.Bounds()
	y := b.Min.Y + b.Dy()/8 + b.Dy()/64
	// Skip a margin at each edge: the page's own dark border would otherwise
	// count as two extra runs.
	margin := max(b.Dx()/80, 8)
	runs, inRun := 0, false
	for x := b.Min.X + margin; x < b.Max.X-margin; x++ {
		r, g, bl, _ := img.At(x, y).RGBA()
		dark := (r>>8)+(g>>8)+(bl>>8) < 3*110
		if dark && !inRun {
			runs++
		}
		inRun = dark
	}
	return runs
}

// buildVolume writes pageCount pages spread over chapters and returns the
// Volume describing them.
func buildVolume(t *testing.T, dir string, chapters, pagesPerChapter int) assemble.Volume {
	t.Helper()
	pages := filepath.Join(dir, "pages")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	n := 0
	var chs []assemble.Chapter
	for c := range chapters {
		ch := assemble.Chapter{
			ID:     fmt.Sprintf("ch-%d", c+1),
			Title:  fmt.Sprintf("Chapter %d", c+1),
			Number: strconv.Itoa(c + 1),
		}
		for p := range pagesPerChapter {
			n++
			ch.Pages = append(ch.Pages, assemble.Page{Path: writePage(t, pages, n, 1620, 2160), Index: p})
		}
		chs = append(chs, ch)
	}
	return assemble.Volume{Series: "Test Series", Label: "1", Title: "Test Series — Volume 1", Chapters: chs}
}

// pdfPageBoxes reads a PDF back and returns each page's MediaBox dimensions.
func pdfPageBoxes(t *testing.T, path string) []struct{ W, H float64 } {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer f.Close()
	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	dims, err := ctx.PageDims()
	if err != nil {
		t.Fatalf("page dims: %v", err)
	}
	out := make([]struct{ W, H float64 }, len(dims))
	for i, d := range dims {
		out[i] = struct{ W, H float64 }{d.Width, d.Height}
	}
	return out
}

func TestAssembleVolume(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 3, 4)
	out := filepath.Join(dir, "out")

	m, err := assemble.Assemble(t.Context(), out, vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if m.PageCount != 12 {
		t.Errorf("PageCount = %d, want 12", m.PageCount)
	}
	pdf := assemble.PDFPath(out, vol.Slug())
	st, err := os.Stat(pdf)
	if err != nil {
		t.Fatalf("stat pdf: %v", err)
	}
	if st.Size() != m.Bytes {
		t.Errorf("manifest Bytes = %d, file is %d", m.Bytes, st.Size())
	}

	// Page count and per-page MediaBox, read back out of the PDF.
	boxes := pdfPageBoxes(t, pdf)
	if len(boxes) != 12 {
		t.Fatalf("pdf has %d pages, want 12", len(boxes))
	}
	for i, b := range boxes {
		if math.Abs(b.W-assemble.MediaBoxWidthPt) > 0.5 || math.Abs(b.H-assemble.MediaBoxHeightPt) > 0.5 {
			t.Errorf("page %d MediaBox %.1fx%.1f, want %.0fx%.0f",
				i+1, b.W, b.H, assemble.MediaBoxWidthPt, assemble.MediaBoxHeightPt)
		}
	}

	// Chapter offsets.
	want := []int{0, 4, 8}
	if len(m.Chapters) != 3 {
		t.Fatalf("manifest has %d chapters, want 3", len(m.Chapters))
	}
	for i, c := range m.Chapters {
		if c.PageOffset != want[i] {
			t.Errorf("chapter %s offset = %d, want %d", c.ID, c.PageOffset, want[i])
		}
		if c.PageCount != 4 {
			t.Errorf("chapter %s pageCount = %d, want 4", c.ID, c.PageCount)
		}
	}
	if off, ok := m.PageFor("ch-2"); !ok || off != 4 {
		t.Errorf("PageFor(ch-2) = %d, %v; want 4, true", off, ok)
	}

	// The manifest round-trips from disk.
	loaded, err := assemble.LoadManifest(out, vol.Slug())
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.PageCount != m.PageCount || len(loaded.Chapters) != len(m.Chapters) {
		t.Errorf("loaded manifest disagrees: %+v", loaded)
	}
	if !assemble.IsAssembled(out, vol.Slug()) {
		t.Error("IsAssembled = false for a freshly assembled volume")
	}

	t.Logf("12 pages, %d bytes total, %.0f KiB/page", m.Bytes, float64(m.Bytes)/12/1024)
}

// Page order is checked by extracting each page's embedded image and reading
// the stamp back, so a reversed or shuffled volume fails here.
func TestAssemblePreservesPageOrder(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 2, 3)
	out := filepath.Join(dir, "out")

	if _, err := assemble.Assemble(t.Context(), out, vol, assemble.DefaultOptions()); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	extracted := filepath.Join(dir, "extract")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := api.ExtractImagesFile(assemble.PDFPath(out, vol.Slug()), extracted, nil, nil); err != nil {
		t.Fatalf("extract images: %v", err)
	}

	entries, err := os.ReadDir(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("extracted %d images, want 6", len(entries))
	}

	// pdfcpu names extracted images "<file>_<page>_<id>.<ext>"; index by page.
	byPage := map[int]string{}
	for _, e := range entries {
		parts := strings.Split(e.Name(), "_")
		if len(parts) < 3 {
			t.Fatalf("unexpected extracted name %q", e.Name())
		}
		p, err := strconv.Atoi(parts[len(parts)-2])
		if err != nil {
			t.Fatalf("unexpected extracted name %q: %v", e.Name(), err)
		}
		byPage[p] = filepath.Join(extracted, e.Name())
	}

	for page := 1; page <= 6; page++ {
		path, ok := byPage[page]
		if !ok {
			t.Fatalf("no image extracted for page %d", page)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}
		if got := readStamp(img); got != page {
			t.Errorf("page %d carries stamp %d: pages are out of order", page, got)
		}
	}
}

func TestAssembleRejectsOversizedPage(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 1, 2)
	out := filepath.Join(dir, "out")

	opts := assemble.DefaultOptions()
	opts.MaxPageBytes = 1024

	_, err := assemble.Assemble(t.Context(), out, vol, opts)
	if !errors.Is(err, assemble.ErrPageTooLarge) {
		t.Fatalf("err = %v, want ErrPageTooLarge", err)
	}
	assertNoOutput(t, out, vol.Slug())
}

func TestAssembleRejectsEmptyVolume(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	vol := assemble.Volume{Series: "S", Label: "1"}
	if _, err := assemble.Assemble(t.Context(), out, vol, assemble.DefaultOptions()); !errors.Is(err, assemble.ErrNoPages) {
		t.Fatalf("err = %v, want ErrNoPages", err)
	}
}

func TestAssembleRejectsMissingPage(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 1, 2)
	vol.Chapters[0].Pages[1].Path = filepath.Join(dir, "does-not-exist.jpg")
	out := filepath.Join(dir, "out")

	if _, err := assemble.Assemble(t.Context(), out, vol, assemble.DefaultOptions()); err == nil {
		t.Fatal("expected an error for a missing page")
	}
	assertNoOutput(t, out, vol.Slug())
}

func TestAssembleHonoursCancellation(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 2, 4)
	out := filepath.Join(dir, "out")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := assemble.Assemble(ctx, out, vol, assemble.DefaultOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	assertNoOutput(t, out, vol.Slug())
}

// assertNoOutput is the "a partial download must never produce a PDF"
// invariant: nothing at the destination, and no stray .pdf anywhere the
// library could see — leftovers are only allowed under .partial.
func assertNoOutput(t *testing.T, dir, slug string) {
	t.Helper()
	if _, err := os.Stat(assemble.PDFPath(dir, slug)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a PDF exists at %s after a failed assembly", assemble.PDFPath(dir, slug))
	}
	if _, err := os.Stat(assemble.ManifestPath(dir, slug)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a manifest exists at %s after a failed assembly", assemble.ManifestPath(dir, slug))
	}
	if assemble.IsAssembled(dir, slug) {
		t.Error("IsAssembled = true after a failed assembly")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == assemble.PartialDirName {
			continue
		}
		if strings.HasSuffix(e.Name(), ".pdf") {
			t.Errorf("stray %s left in the destination directory", e.Name())
		}
	}
}

// TestAssembleSurvivesKill runs assembly in a subprocess and SIGKILLs it
// mid-write, then asserts the destination is untouched. This is the real
// version of the atomicity claim: a cancelled context is cooperative, a
// SIGKILL is not.
func TestAssembleSurvivesKill(t *testing.T) {
	dir := t.TempDir()
	// Enough pages that assembly is still running when the kill lands.
	vol := buildVolume(t, dir, 6, 10)
	out := filepath.Join(dir, "out")

	specPath := filepath.Join(dir, "spec.txt")
	var spec strings.Builder
	for _, ch := range vol.Chapters {
		for _, p := range ch.Pages {
			spec.WriteString(p.Path + "\n")
		}
	}
	if err := os.WriteFile(specPath, []byte(spec.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAssembleHelper")
	cmd.Env = append(os.Environ(), "QUIRE_ASSEMBLE_HELPER=1", "QUIRE_HELPER_OUT="+out, "QUIRE_HELPER_SPEC="+specPath)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Kill while the helper is partway through writing the PDF: wait until the
	// temp file under .partial has grown, then SIGKILL.
	partial := filepath.Join(out, assemble.PartialDirName)
	killed := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if partialBytes(partial) > 0 {
			_ = cmd.Process.Signal(syscall.SIGKILL)
			killed = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	err := cmd.Wait()
	if !killed {
		t.Fatalf("helper finished before it could be killed (err %v); the test cannot prove anything", err)
	}
	if err == nil {
		t.Fatal("helper exited cleanly despite SIGKILL")
	}

	assertNoOutput(t, out, "Test-Series-v1")

	// And the leftover is confined to .partial, which CleanPartial removes.
	if err := assemble.CleanPartial(out); err != nil {
		t.Fatalf("CleanPartial: %v", err)
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s survived CleanPartial", partial)
	}
}

func partialBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var n int64
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			n += info.Size()
		}
	}
	return n
}

// TestAssembleHelper is the subprocess half of TestAssembleSurvivesKill. It is
// a no-op unless the environment marks it as the helper.
func TestAssembleHelper(t *testing.T) {
	if os.Getenv("QUIRE_ASSEMBLE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	spec, err := os.ReadFile(os.Getenv("QUIRE_HELPER_SPEC"))
	if err != nil {
		t.Fatal(err)
	}
	var ch assemble.Chapter
	ch.ID, ch.Title = "ch-1", "Chapter 1"
	for i, line := range strings.Split(strings.TrimSpace(string(spec)), "\n") {
		ch.Pages = append(ch.Pages, assemble.Page{Path: line, Index: i})
	}
	vol := assemble.Volume{Series: "Test Series", Label: "1", Chapters: []assemble.Chapter{ch}}
	if _, err := assemble.Assemble(context.Background(), os.Getenv("QUIRE_HELPER_OUT"), vol, assemble.DefaultOptions()); err != nil {
		t.Fatalf("helper assemble: %v", err)
	}
}

// PLAN §6 M4's reversal, consequence 2: with one PDF per chapter the
// chapter→(PDF, page offset) map becomes trivial — offset 0 — and a trivial
// case is exactly the kind that gets quietly dropped. M6's "Read" reads this
// map, so an empty Chapters list here means a document that opens at page one
// by luck rather than by design, and a PageFor that answers false means no
// answer at all.
func TestASingleChapterVolumeStillCarriesItsOffset(t *testing.T) {
	dir := t.TempDir()
	vol := buildVolume(t, dir, 1, 4)
	out := filepath.Join(dir, "out")

	m, err := assemble.Assemble(t.Context(), out, vol, assemble.DefaultOptions())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if len(m.Chapters) != 1 {
		t.Fatalf("manifest holds %d chapters, want 1; the map must exist even when it is trivial", len(m.Chapters))
	}
	if m.Chapters[0].PageOffset != 0 {
		t.Errorf("offset = %d, want 0", m.Chapters[0].PageOffset)
	}
	if m.Chapters[0].PageCount != 4 {
		t.Errorf("pageCount = %d, want 4", m.Chapters[0].PageCount)
	}
	off, ok := m.PageFor("ch-1")
	if !ok {
		t.Fatal(`PageFor("ch-1") says it does not know; "Read" has nothing to open with`)
	}
	if off != 0 {
		t.Errorf(`PageFor("ch-1") = %d, want 0`, off)
	}
	// A chapter that is genuinely not in this document still answers no. The
	// trivial case must not become "every chapter is page 0".
	if _, ok := m.PageFor("ch-2"); ok {
		t.Error(`PageFor("ch-2") claims a chapter this document does not hold`)
	}
}
