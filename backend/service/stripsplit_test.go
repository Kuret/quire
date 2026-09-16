package service_test

// The per-source strip-splitting override, end to end — PLAN §12.3.
//
// imageproc proves the cuts land in gutters and download proves the queue
// renumbers around them. What is left, and what these pin, is that the schema
// field is actually *honoured*: a `splitStrips` value that the user sets, that
// validates, and that is then ignored is the failure mode worth a test of its
// own, because nothing else in the system would notice it.

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// stripWidth and stripHeight are a vertical-scroll page in miniature: h:w 4.25,
// past imageproc's candidate threshold and short of the extreme ratio, so the
// chapter-wide corroboration path is the one under test rather than the
// "nothing else explains this" shortcut. Small on purpose — this test is about
// the wiring, and eighty full-size encodes would make it a benchmark.
const (
	stripWidth  = 400
	stripHeight = 1700
)

// stripJPEG draws a strip with authored horizontal gutters, the thing the
// splitter exists to find.
func stripJPEG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8(245) // gutter / background
			if y%380 > 24 { // panel content
				v = uint8(60 + (x*3+y*7)%150)
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// stripRoutes is downloadRoutes with every page image replaced by a strip, so
// the fixture is a vertical-scroll source rather than a manga one.
func stripRoutes(t *testing.T) map[string]themetest.Route {
	t.Helper()
	r := downloadRoutes(t)
	strip := themetest.Route{
		Body:   stripJPEG(t, stripWidth, stripHeight),
		Header: http.Header{"Content-Type": []string{"image/jpeg"}},
	}
	for _, n := range []string{"001", "002", "003", "004"} {
		r["GET /pages/lantern-keeper/4/"+n+".jpg"] = strip
	}
	r["GET /wp-content/uploads/pages/lantern-keeper/4/005.jpg"] = strip
	return r
}

// addSourceWithSplitStrips adds the fixture source carrying an explicit
// override, through the same store the app uses — so the value passes
// Registry.Validate on the way in, exactly as a user-added source would.
func addSourceWithSplitStrips(t *testing.T, store *state.Store, mode string) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
		SplitStrips: mode,
	}); err != nil {
		t.Fatalf("adding a source with splitStrips=%q: %v", mode, err)
	}
}

// downloadStripVolume runs a whole download of the strip fixture and returns
// the page count of the PDF that reached the library.
func downloadStripVolume(t *testing.T, mode string) (pages, sourceImages int) {
	t.Helper()
	svc, store, _, fake, rec := newDownloadServiceWith(t, stripRoutes(t))
	addSourceWithSplitStrips(t, store, mode)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	done := waitForPhase(t, rec, "done")

	// pagesTotal counts source images, not output pages (download.Progress),
	// so it is the denominator the split is measured against.
	sourceImages = fetchingPagesTotal(t, rec)
	if sourceImages == 0 {
		t.Fatalf("no pagesTotal was ever reported; done was %v", done)
	}

	fake.mu.Lock()
	uploads := len(fake.uploaded)
	var pdf []byte
	if uploads > 0 {
		pdf = fake.uploaded[0]
	}
	fake.mu.Unlock()

	if uploads != 1 {
		t.Fatalf("%d uploads, want 1", uploads)
	}
	return pdfPageCount(t, pdf), sourceImages
}

// fetchingPagesTotal reads pagesTotal from the *fetching* phase only.
//
// The field means two different things on the stream and the distinction is
// exactly the one this file is about: while fetching it is download.Progress's
// count of source images, and once a part is assembled the service overwrites
// it with the assembled PDF's page count. Taking the largest value seen would
// silently read the post-split number and make the comparison vacuous.
func fetchingPagesTotal(t *testing.T, rec *recorder) int {
	t.Helper()
	best := 0
	for _, m := range progressOf(t, rec) {
		if m["phase"] != "fetching" {
			continue
		}
		if v, ok := m["pagesTotal"].(float64); ok && int(v) > best {
			best = int(v)
		}
	}
	return best
}

func pdfPageCount(t *testing.T, b []byte) int {
	t.Helper()
	ctx, err := api.ReadValidateAndOptimize(bytes.NewReader(b), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	return ctx.PageCount
}

// TestSplitStripsAutoSplitsAStripSource is the baseline: with the default, a
// source whose pages are strips produces more PDF pages than it had images.
func TestSplitStripsAutoSplitsAStripSource(t *testing.T) {
	pages, images := downloadStripVolume(t, "auto")
	if pages <= images {
		t.Fatalf("%d source strips produced %d PDF pages; auto should have cut them up", images, pages)
	}
	t.Logf("auto: %d source strips → %d PDF pages", images, pages)
}

// TestSplitStripsNeverLeavesAStripWhole is the escape hatch doing its job on
// the same input. This is the test that fails if the schema field is stored,
// validated and then dropped on the floor.
func TestSplitStripsNeverLeavesAStripWhole(t *testing.T) {
	pages, images := downloadStripVolume(t, "never")
	if pages != images {
		t.Fatalf("with splitStrips=never, %d source strips produced %d PDF pages; want one page each",
			images, pages)
	}
	t.Logf("never: %d source strips → %d PDF pages", images, pages)
}

// TestSplitStripsOverrideChangesTheOutcome states the two results against each
// other. Either test above could pass on a broken build if the fixture stopped
// being strip-shaped; only the comparison shows the override is what moved the
// outcome.
func TestSplitStripsOverrideChangesTheOutcome(t *testing.T) {
	auto, images := downloadStripVolume(t, "auto")
	never, _ := downloadStripVolume(t, "never")
	if auto == never {
		t.Fatalf("auto and never both produced %d pages from %d strips; the override is not reaching the queue",
			auto, images)
	}
	t.Logf("%d source strips → %d pages under auto, %d under never", images, auto, never)
}

// TestSplitStripsEmptyMeansAuto pins the stored form of "unset". A source added
// before this feature existed has no splitStrips field at all, and must behave
// as the schema default rather than as "never" by accident.
func TestSplitStripsEmptyMeansAuto(t *testing.T) {
	unset, images := downloadStripVolume(t, "")
	auto, _ := downloadStripVolume(t, "auto")
	if unset != auto {
		t.Errorf("an unset splitStrips produced %d pages, auto %d; absent must mean the default", unset, auto)
	}
	if mode, err := imageproc.ParseSplitMode(""); err != nil || mode != imageproc.SplitAuto {
		t.Errorf(`ParseSplitMode("") = %v, %v`, mode, err)
	}
	t.Logf("unset: %d source strips → %d PDF pages, same as auto", images, unset)
}

// TestSplitStripsRejectsAnInvalidValueAtTheStore keeps the bad value from ever
// reaching a download: it is refused when the source is added, which is where a
// user can still do something about it.
func TestSplitStripsRejectsAnInvalidValueAtTheStore(t *testing.T) {
	_, store, _, _, _ := newDownloadServiceWith(t, stripRoutes(t))
	_, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
		SplitStrips: "sometimes",
	})
	if err == nil {
		t.Fatal("the store accepted splitStrips=sometimes")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("splitStrips")) {
		t.Errorf("the refusal does not name the field the user has to fix: %v", err)
	}
}
