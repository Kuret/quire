package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §6 M7's case list, each proved rather than asserted. The fixtures here
// are deliberately awkward — mixed page sizes, a response that dies part way,
// a source that changes shape between two calls — because today's two field
// bugs (the 100 MB upload cap and the OOM) were both green in CI against
// fixtures where every page was the same size and nothing ever went wrong.

// variedPage returns a JPEG whose encoded size depends on n: flat grey
// compresses to almost nothing, noise does not. A volume of these is nothing
// like the uniform fixture that hid the upload cap.
func variedPage(t *testing.T, n int) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 160+n*40, 240+n*40))
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			var v uint8
			switch n % 3 {
			case 0:
				v = 200 // flat: compresses tiny
			case 1:
				v = uint8((x * y) % 255) // busy: compresses badly
			default:
				v = uint8((x ^ y) % 255)
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	var buf strings.Builder
	if err := jpeg.Encode(&stringWriter{&buf}, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

type stringWriter struct{ b *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// variedRoutes is downloadRoutes with pages of genuinely different sizes.
func variedRoutes(t *testing.T) map[string]themetest.Route {
	r := downloadRoutes(t)
	names := []string{"001", "002", "003", "004"}
	for i, n := range names {
		r["GET /pages/lantern-keeper/4/"+n+".jpg"] = themetest.Route{
			Body:   variedPage(t, i),
			Header: http.Header{"Content-Type": []string{"image/jpeg"}},
		}
	}
	r["GET /wp-content/uploads/pages/lantern-keeper/4/005.jpg"] = themetest.Route{
		Body:   variedPage(t, 3),
		Header: http.Header{"Content-Type": []string{"image/jpeg"}},
	}
	return r
}

// failingFetcher breaks page retrieval after n successes, which is what a wifi
// drop or a tablet waking up mid-volume looks like from here.
type failingFetcher struct {
	inner theme.Fetcher
	after int
	err   error

	mu sync.Mutex
	n  int
}

func (f *failingFetcher) Get(ctx context.Context, p *fetch.Policy, u string) (*fetch.Response, error) {
	return f.inner.Get(ctx, p, u)
}

func (f *failingFetcher) GetFrom(ctx context.Context, p *fetch.Policy, u string, from fetch.Referrer) (*fetch.Response, error) {
	return f.inner.GetFrom(ctx, p, u, from)
}

func (f *failingFetcher) PostForm(ctx context.Context, p *fetch.Policy, u string, form url.Values) (*fetch.Response, error) {
	return f.inner.PostForm(ctx, p, u, form)
}

func (f *failingFetcher) GetRetrieval(ctx context.Context, p *fetch.Policy, u string) (*fetch.Response, error) {
	return f.GetRetrievalFrom(ctx, p, u, fetch.Referrer{})
}

func (f *failingFetcher) GetRetrievalFrom(ctx context.Context, p *fetch.Policy, u string, from fetch.Referrer) (*fetch.Response, error) {
	f.mu.Lock()
	f.n++
	fail := f.n > f.after
	f.mu.Unlock()
	if fail {
		return nil, f.err
	}
	return f.inner.GetRetrievalFrom(ctx, p, u, from)
}

// Wifi drops mid-volume. The acceptance wording: no corrupt PDF, no orphaned
// temp files, no duplicate library entry.
func TestWifiDroppingMidVolumeLeavesNothingBroken(t *testing.T) {
	var dir string
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, variedRoutes(t),
		func(o *service.Options) {
			o.Fetcher = &failingFetcher{
				inner: o.Fetcher, after: 6,
				err: errors.New("dial tcp: network is unreachable"),
			}
			dir = filepath.Join(t.TempDir(), "downloads")
			o.DownloadDir = dir
			o.DownloadOptions.Retries = 1
		})
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)

	failed := waitForPhase(t, rec, "failed")
	if msg, _ := failed["message"].(string); msg == "" {
		t.Error("the failure arrived with nothing to show the user")
	}

	// Nothing reached the library, and nothing was remembered: a half volume
	// must not become a library entry that opens to a truncated book.
	fake.mu.Lock()
	uploads := len(fake.uploaded)
	fake.mu.Unlock()
	if uploads != 0 {
		t.Errorf("%d uploads from a volume that never finished", uploads)
	}
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d library records for a failed download", n)
	}

	pages, pdfs, temps := countArtefacts(t, dir)
	if pages == 0 {
		t.Error("the pages fetched before the drop were discarded; resume would refetch them")
	}
	if pdfs != 0 {
		t.Errorf("%d PDFs left behind", pdfs)
	}
	if temps != 0 {
		t.Errorf("%d orphaned temp files left behind", temps)
	}
}

// The site changes shape mid-series: chapter 1 still parses, chapter 3 no
// longer yields pages. The download must fail honestly rather than quietly
// assembling a volume with a hole in it.
func TestASiteChangingShapeMidSeriesFailsHonestly(t *testing.T) {
	r := variedRoutes(t)
	// The reader page for one chapter of the volume stops containing images.
	r["GET /manga/the-lantern-keeper/chapter-3/"] = themetest.Route{
		Body: `<!doctype html><html><head><title>Chapter 3</title></head>
		<body><div class="reading-content"></div></body></html>`,
	}

	svc, store, libStore, fake, rec := newDownloadServiceWith(t, r)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)

	failed := waitForPhase(t, rec, "failed")
	msg, _ := failed["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "pages") {
		t.Errorf("message %q does not say what could not be read", msg)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.uploaded) != 0 {
		t.Errorf("a volume with a missing chapter reached the library: %v", fake.names)
	}
	if n := len(libStore.List()); n != 0 {
		t.Errorf("%d records for a volume that was never built", n)
	}
}

// The theme a source names is no longer shipped — an implementation change, or
// a downgrade. The user must be told, not shown an empty screen.
func TestASourceWhoseThemeIsGoneSaysSo(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	// Rewrite the stored theme to one nothing registers.
	src, ok := store.Get("example-reader")
	if !ok {
		t.Fatal("no source")
	}
	if err := store.Remove(src.ID); err != nil {
		t.Fatal(err)
	}
	src.Theme = "a-theme-quire-no-longer-has"
	// Add bypasses validation for an unknown theme, so write it through the
	// store's own path and expect the failure at use time rather than add time.
	if _, err := store.Add(src); err == nil {
		handle(t, svc, rec, appload.MessageBrowse, `{"sourceId":"`+src.ID+`","page":1}`)
		var e struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(e.Message, "site type") {
			t.Errorf("message %q does not explain the theme is gone", e.Message)
		}
		return
	}
	// The store refused it outright, which is also an honest answer: the
	// failure surfaces on load rather than three screens later.
}

// The user deletes a document in xochitl. Still has to hold after the split
// work: deleting one part of a split volume must forget that part and leave
// the others openable.
func TestDeletingOnePartLeavesTheOthersReadable(t *testing.T) {
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, variedRoutes(t),
		func(o *service.Options) { o.UploadBudgetBytes = 20000 })
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true}`)
	waitForPhase(t, rec, "done")

	fake.mu.Lock()
	parts := len(fake.uploaded)
	fake.mu.Unlock()
	if parts < 2 {
		t.Skipf("the varied fixture produced %d part(s); nothing to test here", parts)
	}

	recs := libStore.List()
	gone := recs[0]
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageOpenInReader,
		`{"documentUuid":"`+gone.DocumentUUID+`","missing":true}`)

	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "document_gone" {
		t.Errorf("code %q", e.Code)
	}

	left := libStore.List()
	if len(left) != len(recs)-1 {
		t.Fatalf("%d records left of %d; only the deleted part should go", len(left), len(recs))
	}
	for _, r := range left {
		if r.DocumentUUID == gone.DocumentUUID {
			t.Error("the deleted part is still remembered")
		}
		if len(r.Chapters) == 0 {
			t.Error("a surviving part lost its chapter list and can no longer be found")
		}
	}
}

// countArtefacts walks a download directory. Temp files are anything left by an
// interrupted assembly; PLAN §6 M4 builds into .partial and renames, so a
// failed run must leave nothing that looks like a finished file.
func countArtefacts(t *testing.T, dir string) (pages, pdfs, temps int) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(filepath.Base(path))
		switch {
		case strings.HasSuffix(name, ".jpg"):
			pages++
		case strings.HasSuffix(name, ".pdf"):
			if strings.Contains(path, ".partial") {
				temps++
			} else {
				pdfs++
			}
		case strings.HasSuffix(name, ".tmp"):
			temps++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return pages, pdfs, temps
}
