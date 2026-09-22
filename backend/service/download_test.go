package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/rickl/quire/backend/shelf"
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

	// onUpload, if set, is called after a successful upload with the lock
	// released. It lets a test act at the exact moment a part lands.
	onUpload func()
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
		// What xochitl does with the name, measured on the device: it appends
		// ".pdf" only when the filename has **no extension at all**. An
		// ".epub" is kept verbatim and lands with fileType "epub", which is
		// what makes the stock reader paginate it (2026-09-20). A fake that
		// appended ".pdf" to every non-pdf name would hide exactly the bug the
		// book path has to avoid.
		fileType := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if fileType == "" {
			name += ".pdf"
			fileType = "pdf"
		}
		f.n++
		f.uploaded = append(f.uploaded, body)
		f.names = append(f.names, name)
		f.entries = append(f.entries, library.Entry{
			ID:          "uploaded-" + string(rune('0'+f.n)),
			Parent:      f.selected,
			Type:        library.Document,
			VisibleName: name,
			FileType:    fileType,
		})
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"Upload successful"}`)
		if f.onUpload != nil {
			hook := f.onUpload
			f.mu.Unlock()
			hook()
			f.mu.Lock()
		}
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

// downloadHarness is everything buildDownloadHarness assembles, for the tests
// that need more of it than the (svc, store, libStore, fake, rec) tuple most
// of this file was written around — chiefly the "Saved in Quire" tests, which
// need the shelf store and the saved-files root the tuple has no room for.
type downloadHarness struct {
	svc         *service.Service
	store       *state.Store
	libStore    *library.Store
	shelfStore  *shelf.Store
	fake        *fakeLibrary
	fetcher     *themetest.Fetcher
	savedDir    string
	downloadDir string
}

// fetcherCalls is every request the theme made through the fake network, so a
// test can prove a "Send to library" seeded from a saved chapter made no new
// ones.
func (h *downloadHarness) fetcherCalls() []themetest.Request {
	return h.fetcher.Calls()
}

// buildDownloadHarness wires a Service with both backing stores a comic
// download can land in: the reMarkable library (via fakeLibrary, an
// in-process stand-in for xochitl's web interface) and Quire's own saved
// storage. Both are always wired, even for a test only interested in one of
// them, because the *first* message of a two-step download (the unconfirmed
// "what does this cover?" ask) resolves a destination and is refused if that
// destination's store does not exist — see enqueueDownload — and the default
// destination is "quire".
func buildDownloadHarness(t *testing.T, routes map[string]themetest.Route,
	tweaks ...func(*service.Options)) *downloadHarness {
	t.Helper()

	f := themetest.New(t, routes)
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))
	reg.MustRegister(volumeTheme{madara.NewWithClock(f, func() time.Time { return fixedNow })})

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	libStore, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	shelfStore, err := shelf.OpenStore(dir)
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

	downloadDir := filepath.Join(dir, "downloads")
	savedDir := filepath.Join(dir, "saved")
	opts := service.Options{
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
		DownloadDir:  downloadDir,
		SavedDir:     savedDir,
		ShelfStore:   shelfStore,
	}
	for _, tweak := range tweaks {
		tweak(&opts)
	}
	svc := service.New(opts)
	t.Cleanup(svc.Close)
	return &downloadHarness{
		svc: svc, store: store, libStore: libStore, shelfStore: shelfStore, fake: fake, fetcher: f,
		savedDir: savedDir, downloadDir: downloadDir,
	}
}

func newDownloadService(t *testing.T) (*service.Service, *state.Store, *library.Store, *fakeLibrary, *recorder) {
	t.Helper()
	return newDownloadServiceWith(t, downloadRoutes(t))
}

func newDownloadServiceWith(t *testing.T, routes map[string]themetest.Route,
	tweaks ...func(*service.Options)) (
	*service.Service, *state.Store, *library.Store, *fakeLibrary, *recorder) {
	t.Helper()
	h := buildDownloadHarness(t, routes, tweaks...)
	return h.svc, h.store, h.libStore, h.fake, &recorder{}
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
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)

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

// A per-series subfolder is optional by design (PLAN §6 M5): the library is
// flat, so its absence is not worth a word to the user.
func TestAMissingSeriesFolderIsNotWorthMentioning(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)

	done := waitForPhase(t, rec, "done")
	if note, _ := done["note"].(string); note != "" {
		t.Errorf("note %q; a flat library has nothing to complain about", note)
	}
	if message, _ := done["message"].(string); !strings.Contains(message, "Comics") {
		t.Errorf("done message %q does not say where the volume went", message)
	}
}

// No Comics folder is the one-time setup step. The volume still lands — the
// bytes are already fetched — and the user is told how to fix it for next time.
func TestDownloadWithoutComicsStillLandsAndSaysSo(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	addSource(t, store)

	fake.mu.Lock()
	fake.entries = nil
	fake.mu.Unlock()

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)

	done := waitForPhase(t, rec, "done")
	if uuid, _ := done["documentUuid"].(string); uuid == "" {
		t.Fatal("the download was refused for want of a folder")
	}
	note, _ := done["note"].(string)
	if note != library.ComicsRemedy {
		t.Errorf("note %q, want the one-time setup instruction", note)
	}
}

// Progress is streamed, not delivered once at the end: a volume is minutes of
// work and a frozen screen is indistinguishable from a crash.
func TestDownloadStreamsProgress(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
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

// A tap on Download does not start a multi-chapter download. PLAN §6 M3 wants
// the UI to say what it is doing, and queueing ten chapters and a few hundred
// megabytes silently is not that.
func TestFirstTapAsksBeforeDownloadingAVolume(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)

	ask := waitForPhase(t, rec, "confirm")

	count, _ := ask["chapterCount"].(float64)
	if count < 2 {
		t.Fatalf("chapterCount %v, want the whole volume", ask["chapterCount"])
	}
	message, _ := ask["message"].(string)
	for _, want := range []string{"chapters", "Download all"} {
		if !strings.Contains(message, want) {
			t.Errorf("question %q does not mention %q", message, want)
		}
	}
	if ask["firstChapter"] == "" || ask["lastChapter"] == "" {
		t.Errorf("the question does not say which chapters: %+v", ask)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.uploaded) != 0 {
		t.Errorf("%d uploads happened before the user confirmed", len(fake.uploaded))
	}
}

// Confirming is one step, not a wizard: the same message with confirmed:true
// runs the download.
func TestConfirmingRunsTheDownload(t *testing.T) {
	svc, store, _, _, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+`"}`)
	waitForPhase(t, rec, "confirm")

	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")
}

// The volume is named for the flat library it lands in: Comics holds every
// series side by side, so the series has to be in the document name.
func TestTheDocumentIsNamedSeriesAndVolume(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.names) != 1 {
		t.Fatalf("names %v", fake.names)
	}
	name := fake.names[0]
	if !strings.Contains(name, "Lantern Keeper") || !strings.Contains(name, "Vol ") {
		t.Errorf("document name %q, want \"<Series> — Vol N.pdf\"", name)
	}
}

// A series whose reading order Quire could not establish gets one PDF per
// chapter — and is told why. Assembling a volume from a list we admit we could
// not order would produce a silently scrambled book, which is the failure the
// ordering contract exists to prevent, reintroduced one layer up.
func TestAnUnorderedSeriesFallsBackToOnePDFPerChapter(t *testing.T) {
	r := downloadRoutes(t)
	r["POST /manga/the-lantern-keeper/ajax/chapters/"] = themetest.Route{File: "chapters-unordered.html"}
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, r)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)

	// One chapter is not a volume, so there is nothing to confirm: the
	// download starts on the first tap.
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","destination":"library"}`)

	done := waitForPhase(t, rec, "done")
	note, _ := done["note"].(string)
	if !strings.Contains(note, "order") {
		t.Errorf("note %q does not explain why this is one chapter on its own", note)
	}

	fake.mu.Lock()
	names := append([]string(nil), fake.names...)
	fake.mu.Unlock()
	if len(names) != 1 {
		t.Fatalf("uploaded %v, want exactly the one chapter", names)
	}
	if strings.Contains(names[0], "Vol ") {
		t.Errorf("name %q calls a lone chapter a volume", names[0])
	}
	if n := len(libStore.List()); n != 1 {
		t.Errorf("%d records stored", n)
	}
}

// The whole reason splitToBudget exists: xochitl refuses a body of 100 MB or
// more, usually by resetting the connection most of the way through. A volume
// over budget must arrive as several documents rather than as one failure.
func TestAnOversizedVolumeArrivesAsParts(t *testing.T) {
	svc, store, libStore, fake, rec := newDownloadServiceWith(t, downloadRoutes(t),
		func(o *service.Options) {
			// The fixture volume is 20 small pages; a budget below that
			// forces the split without downloading anything large.
			o.UploadBudgetBytes = 4096
		})
	addVolumeSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"grouping":"volume","sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	done := waitForPhase(t, rec, "done")

	fake.mu.Lock()
	names := append([]string(nil), fake.names...)
	fake.mu.Unlock()

	if len(names) < 2 {
		t.Fatalf("uploaded %v, want the volume split into parts", names)
	}
	for i, name := range names {
		want := fmt.Sprintf("(part %d of %d)", i+1, len(names))
		if !strings.Contains(name, want) {
			t.Errorf("part %d is called %q, want it to carry %q", i+1, name, want)
		}
	}
	if note, _ := done["note"].(string); !strings.Contains(note, "parts") {
		t.Errorf("note %q does not explain why there are several files", note)
	}

	// Every part is remembered separately, and between them they account for
	// every chapter — the chapter map is M6's input and a split must not lose
	// any of it.
	recs := libStore.List()
	if len(recs) != len(names) {
		t.Fatalf("%d records for %d documents", len(recs), len(names))
	}
	chapters := map[string]bool{}
	for _, r := range recs {
		if r.Parts != len(names) {
			t.Errorf("record %q says %d parts", r.Volume, r.Parts)
		}
		if len(r.Chapters) == 0 {
			t.Errorf("record %q recorded no chapters", r.Volume)
		}
		for _, id := range r.Chapters {
			chapters[id] = true
		}
	}
	if len(chapters) != 4 {
		t.Errorf("the parts between them hold %d chapters, want all 4", len(chapters))
	}

	// And the chapter list still offers Read on every one of them.
	fresh := &recorder{}
	handle(t, svc, fresh, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`"}`)
	var detail struct {
		Chapters []struct {
			ID           string `json:"id"`
			DocumentUUID string `json:"documentUuid"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(fresh.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	for _, c := range detail.Chapters {
		if c.DocumentUUID == "" {
			t.Errorf("chapter %s lost its document when the volume was split", c.ID)
		}
	}
}

// volumeTheme is the fixture theme with volume labels on its chapter list.
//
// It exists because PLAN §6 M4 was revised on 2026-09-16: a volume view is
// offered only where the *source* publishes real volume labels, so a theme that
// publishes none — which the Madara fixture, marked `no-volumn`, deliberately
// does not — now has no volume to download and no view to reach one from. The
// tests that are about volumes need a source that has some.
//
// It wraps the real Madara theme rather than faking one, so everything else
// those tests lean on — the referer policy, the page parser, the clock — is the
// theme's own behaviour and not a stub's.
type volumeTheme struct{ *madara.Theme }

const volumeThemeID = "madara-volumes"

func (volumeTheme) ID() string { return volumeThemeID }

// Chapters puts every chapter of the fixture series in volume 1. One volume of
// four is the same shape the tests had before the revision, when a source with
// no labels was grouped into runs of ten.
func (v volumeTheme) Chapters(ctx context.Context, s *theme.Source, id string) ([]theme.Chapter, error) {
	chs, err := v.Theme.Chapters(ctx, s, id)
	for i := range chs {
		chs[i].Volume = "1"
	}
	return chs, err
}

// addVolumeSource adds a source whose theme publishes volume labels, which is
// what makes a volume download available at all.
//
// The grouping itself is not here: it moved off the source and onto the
// download request, so the tests that want a volume ask for it in the message
// they send, with `"grouping":"volume"`.
func addVolumeSource(t *testing.T, store *state.Store) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: volumeThemeID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

// PLAN §6 M4's reversal, consequence 1: no confirm step for a single chapter.
//
// The step already skipped a one-chapter volume, and per chapter being the
// default makes that the common case rather than an edge one — which is
// exactly why it has to keep holding. A prompt that always says the same thing
// is one people learn to tap through without reading, and then the one time it
// says something else they tap through that too.
func TestTheFirstTapDownloadsAChapterWithoutAsking(t *testing.T) {
	svc, store, _, fake, rec := newDownloadService(t)
	addSource(t, store) // the default: one PDF per chapter

	seriesID, chapterID := firstChapter(t, svc, rec)
	// No "confirmed":true. This is the first tap.
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","destination":"library"}`)

	done := waitForPhase(t, rec, "done")
	for _, m := range progressOf(t, rec) {
		if m["phase"] == "confirm" {
			t.Errorf("a one-chapter download asked first: %v", m["message"])
		}
	}
	if len(fake.uploaded) != 1 {
		t.Errorf("%d documents uploaded, want 1", len(fake.uploaded))
	}
	if name, _ := done["title"].(string); !strings.Contains(name, "Ch ") {
		t.Errorf("the finished document is %q, which does not name a chapter", name)
	}
}
