package service_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// fakeLibrary is the part of xochitl's web interface the download path uses:
// a folder listing, and an upload that lands in whichever folder was listed
// last. That selection is server state and survives the connection, which is
// the whole reason Upload has to select the folder itself.
type fakeLibrary struct {
	mu       sync.Mutex
	entries  []library.Entry
	selected string
	uploaded [][]byte
	names    []string
	n        int
}

func (f *fakeLibrary) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.URL.Path == "/upload" {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			http.Error(w, "bad type", http.StatusBadRequest)
			return
		}
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(part)
		name := part.FileName()
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			name += ".pdf"
		}
		f.n++
		f.uploaded = append(f.uploaded, body)
		f.names = append(f.names, name)
		f.entries = append(f.entries, library.Entry{
			ID:          "uploaded-" + string(rune('0'+f.n)),
			Parent:      f.selected,
			Type:        library.Document,
			VisibleName: name,
			FileType:    "pdf",
		})
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"Upload successful"}`)
		return
	}

	f.selected = strings.TrimPrefix(r.URL.Path, "/documents/")
	out := []library.Entry{}
	for _, e := range f.entries {
		if e.Parent == f.selected {
			out = append(out, e)
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// jpegPage is a page image the real imageproc pipeline can decode. Fixtures
// that hand back HTML where a JPEG is expected prove nothing about the path
// that matters here.
func jpegPage(t *testing.T) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// downloadRoutes is the M3 fixture set plus the three page images the reader
// page points at.
func downloadRoutes(t *testing.T) map[string]themetest.Route {
	r := routes()

	// Every chapter of the volume, not just the one the user tapped: PLAN §6
	// M4 makes the volume the unit, so a download of chapter 4 fetches the
	// other three as well. A fixture that only served one would hide that.
	for _, ch := range []string{"chapter-1", "chapter-3", "chapter-3-5", "chapter-4"} {
		r["GET /manga/the-lantern-keeper/"+ch+"/"] = themetest.Route{File: "reader.html"}
	}

	page := jpegPage(t)
	jpg := themetest.Route{Body: page, Header: http.Header{"Content-Type": []string{"image/jpeg"}}}
	for _, n := range []string{"001", "002", "003", "004"} {
		r["GET /pages/lantern-keeper/4/"+n+".jpg"] = jpg
	}
	r["GET /wp-content/uploads/pages/lantern-keeper/4/005.jpg"] = jpg
	return r
}

func newDownloadService(t *testing.T) (*service.Service, *state.Store, *library.Store, *fakeLibrary, *recorder) {
	t.Helper()

	f := themetest.New(t, downloadRoutes(t))
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	libStore, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeLibrary{entries: []library.Entry{
		{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"},
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	conf := filepath.Join(dir, "xochitl.conf")
	if err := os.WriteFile(conf, []byte("WebInterfaceEnabled=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
		Library: library.New(library.Options{
			BaseURL:  srv.URL,
			ConfPath: conf,
			AddAlias: func() error { return nil },
		}),
		LibraryStore: libStore,
		DownloadDir:  filepath.Join(dir, "downloads"),
	})
	return svc, store, libStore, fake, &recorder{}
}

func addSource(t *testing.T, store *state.Store) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

// seriesID and chapterID walk the same path the frontend does — search, then
// series detail — rather than hard-coding IDs the theme derives.
func firstChapter(t *testing.T, svc *service.Service, rec *recorder) (seriesID, chapterID string) {
	t.Helper()
	handle(t, svc, rec, appload.MessageSearch, `{"sourceId":"example-reader","query":"lantern"}`)
	var results struct {
		Series []struct {
			ID string `json:"id"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Series) == 0 {
		t.Fatal("search found nothing to download")
	}
	seriesID = results.Series[0].ID

	handle(t, svc, rec, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			ID string `json:"id"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Chapters) == 0 {
		t.Fatal("no chapters to download")
	}
	return seriesID, detail.Chapters[0].ID
}

// progressOf collects every DownloadProgress frame sent so far.
func progressOf(t *testing.T, rec *recorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, f := range rec.sent {
		if f.Type != appload.MessageDownloadProgress {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(f.Payload, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

// waitForPhase blocks until a DownloadProgress with the given phase arrives.
func waitForPhase(t *testing.T, rec *recorder, phase string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range progressOf(t, rec) {
			if m["phase"] == phase {
				return m
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	var seen []string
	for _, m := range progressOf(t, rec) {
		seen = append(seen, m["phase"].(string)+": "+m["message"].(string))
	}
	t.Fatalf("no %q phase arrived; got:\n  %s", phase, strings.Join(seen, "\n  "))
	return nil
}

// The M5 acceptance path, end to end in process: tap Download, and a PDF of
// the whole volume ends up in the library with its UUID remembered.
func TestDownloadEndsWithAStoredDocumentUUID(t *testing.T) {
	svc, store, libStore, fake, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)

	done := waitForPhase(t, rec, "done")

	uuid, _ := done["documentUuid"].(string)
	if uuid == "" {
		t.Fatal("the done message carried no document UUID")
	}

	fake.mu.Lock()
	uploads := len(fake.uploaded)
	name := ""
	parent := ""
	if uploads > 0 {
		name = fake.names[0]
		for _, e := range fake.entries {
			if e.ID == uuid {
				parent = e.Parent
			}
		}
	}
	pdf := []byte(nil)
	if uploads > 0 {
		pdf = fake.uploaded[0]
	}
	fake.mu.Unlock()

	if uploads != 1 {
		t.Fatalf("%d uploads, want 1", uploads)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("what was uploaded is not a PDF: %q", pdf[:min(8, len(pdf))])
	}
	if !strings.HasSuffix(name, ".pdf") {
		t.Errorf("uploaded name %q", name)
	}
	if parent != "comics" {
		t.Errorf("landed in %q, want the Comics folder", parent)
	}

	// The UUID is M6's handle, so it has to survive a restart of the backend.
	reopened, err := library.OpenStore(filepath.Dir(libStore.Path()))
	if err != nil {
		t.Fatal(err)
	}
	recs := reopened.List()
	if len(recs) != 1 {
		t.Fatalf("%d stored volumes, want 1", len(recs))
	}
	if recs[0].DocumentUUID != uuid {
		t.Errorf("stored %q, reported %q", recs[0].DocumentUUID, uuid)
	}
	if recs[0].Source != "example-reader" || recs[0].Series == "" {
		t.Errorf("stored under %+v", recs[0].Key)
	}
	if recs[0].Pages == 0 {
		t.Error("stored no page count")
	}
}

// The series folder does not exist, so the volume goes in the nearest folder
// that does — and the user is told where to make the one that is missing,
// because xochitl's web interface cannot make it for them.
func TestDownloadReportsAMissingSeriesFolder(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)

	done := waitForPhase(t, rec, "done")
	note, _ := done["note"].(string)
	if !strings.Contains(note, "Comics") {
		t.Errorf("note %q does not tell the user where to make the folder", note)
	}
}

// Progress is streamed, not delivered once at the end: a volume is minutes of
// work and a frozen screen is indistinguishable from a crash.
func TestDownloadStreamsProgress(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "done")

	seen := map[string]bool{}
	for _, m := range progressOf(t, rec) {
		seen[m["phase"].(string)] = true
		if m["message"] == "" {
			t.Errorf("phase %q arrived with no message for the user", m["phase"])
		}
	}
	for _, want := range []string{"queued", "fetching", "assembling", "storing", "done"} {
		if !seen[want] {
			t.Errorf("no %q progress was sent; saw %v", want, seen)
		}
	}
}

func TestDownloadWithNoLibraryIsRefusedInWords(t *testing.T) {
	svc, _, rec := newService(t, routes())
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"s","volumeId":"c"}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Message == "" || strings.Contains(e.Message, "not implemented") {
		t.Errorf("error %+v", e)
	}
}

func TestDownloadNeedsAllThreeIdentifiers(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	handle(t, svc, rec, appload.MessageEnqueueDownload, `{"sourceId":"example-reader"}`)
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "bad_request" {
		t.Errorf("code %q", e.Code)
	}
}
