package library_test

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/rickl/quire/backend/library"
)

// fakeXochitl is the web interface's observed behaviour, not a convenient
// stand-in for it. The two things it reproduces on purpose:
//
//   - POST /upload takes no folder. The document lands in whichever folder was
//     last fetched with GET /documents/<guid>, and that selection is server
//     state that outlives the connection (docs/DEVICE-NOTES.md §5, Q1c).
//   - xochitl appends ".pdf" to the uploaded filename when it is missing.
type fakeXochitl struct {
	mu sync.Mutex

	entries  []library.Entry
	selected string
	next     int

	uploads    []string
	uploadCode int

	// requests records the method+path of every request, so a test can assert
	// the GET-before-POST ordering rather than assume it.
	requests []string
}

func newFake(entries ...library.Entry) *fakeXochitl {
	return &fakeXochitl{entries: entries, uploadCode: http.StatusCreated}
}

func (f *fakeXochitl) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return s
}

func (f *fakeXochitl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	switch {
	case r.URL.Path == "/upload":
		f.upload(w, r)
	case strings.HasPrefix(r.URL.Path, "/documents/"):
		f.selected = strings.TrimPrefix(r.URL.Path, "/documents/")
		out := []library.Entry{}
		for _, e := range f.entries {
			if e.Parent == f.selected {
				out = append(out, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeXochitl) upload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "" || r.Header.Get("Referer") == "" {
		http.Error(w, `{"error":"bad origin"}`, http.StatusForbidden)
		return
	}
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, `{"error":"bad content type"}`, http.StatusBadRequest)
		return
	}
	part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
	if err != nil || part.FormName() != "file" {
		http.Error(w, `{"error":"No file sent"}`, http.StatusBadRequest)
		return
	}
	_, _ = io.Copy(io.Discard, part)

	if f.uploadCode != http.StatusCreated {
		w.WriteHeader(f.uploadCode)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
		return
	}

	name := part.FileName()
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	f.next++
	f.uploads = append(f.uploads, name)
	f.entries = append(f.entries, library.Entry{
		ID:           "doc-" + string(rune('a'+f.next-1)),
		Parent:       f.selected,
		Type:         library.Document,
		VisibleName:  name,
		VissibleName: name,
		FileType:     "pdf",
	})
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, `{"status":"Upload successful"}`)
}

func confWith(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xochitl.conf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newLibrary(t *testing.T, f *fakeXochitl, conf string) *library.Library {
	t.Helper()
	return library.New(library.Options{
		BaseURL:  f.server(t).URL,
		ConfPath: conf,
		AddAlias: func() error { return nil },
	})
}

const enabledConf = "[General]\nWebInterfaceEnabled=true\nDeveloperMode=true\n"

func TestUploadLandsInTheResolvedFolder(t *testing.T) {
	f := newFake(
		library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"},
		library.Entry{ID: "snot", Parent: "comics", Type: library.Collection, VisibleName: "Snotgirl"},
	)
	lib := newLibrary(t, f, confWith(t, enabledConf))
	ctx := context.Background()

	place, err := lib.Resolve(ctx, "Comics", "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if !place.Complete() {
		t.Fatalf("missing folders %v, want none", place.Missing)
	}
	if place.FolderID != "snot" {
		t.Fatalf("folder %q, want snot", place.FolderID)
	}
	if place.Remedy() != "" {
		t.Fatalf("remedy %q, want none", place.Remedy())
	}

	res, err := lib.Upload(ctx, place.FolderID, "Snotgirl Vol. 1.pdf", strings.NewReader("%PDF-1.7\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.FolderUUID != "snot" {
		t.Errorf("landed in %q, want snot", res.FolderUUID)
	}
	if res.DocumentUUID == "" {
		t.Error("no document UUID")
	}
	if res.VisibleName != "Snotgirl Vol. 1.pdf" {
		t.Errorf("visible name %q", res.VisibleName)
	}
}

// The upload target is global server state, so Upload must select the folder
// itself immediately beforehand — it may not rely on a listing the caller did
// earlier, however recently.
func TestUploadSelectsTheFolderItself(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))
	ctx := context.Background()

	if _, err := lib.Upload(ctx, "comics", "v1.pdf", strings.NewReader("%PDF")); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	last := -1
	for i, r := range f.requests {
		if r == "POST /upload" {
			last = i
		}
	}
	if last <= 0 {
		t.Fatalf("no upload in %v", f.requests)
	}
	if got := f.requests[last-1]; got != "GET /documents/comics" {
		t.Errorf("request before the upload was %q, want GET /documents/comics (%v)", got, f.requests)
	}
}

func TestResolveReportsMissingFolders(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))

	place, err := lib.Resolve(context.Background(), "Comics", "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if place.Complete() {
		t.Fatal("want Snotgirl reported missing")
	}
	if place.FolderID != "comics" {
		t.Errorf("fell back to %q, want comics", place.FolderID)
	}
	if got := place.Missing; len(got) != 1 || got[0] != "Snotgirl" {
		t.Errorf("missing %v, want [Snotgirl]", got)
	}
	remedy := place.Remedy()
	for _, want := range []string{"Snotgirl", "My Files → Comics"} {
		if !strings.Contains(remedy, want) {
			t.Errorf("remedy %q does not mention %q", remedy, want)
		}
	}
}

func TestResolveFallsBackToTheRoot(t *testing.T) {
	lib := newLibrary(t, newFake(), confWith(t, enabledConf))
	place, err := lib.Resolve(context.Background(), "Comics", "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if place.FolderID != library.RootID {
		t.Errorf("folder %q, want the root", place.FolderID)
	}
	if len(place.Missing) != 2 {
		t.Errorf("missing %v, want both", place.Missing)
	}
	if !strings.Contains(place.Remedy(), "My Files") {
		t.Errorf("remedy %q", place.Remedy())
	}
}

func TestResolveMatchesFolderNamesCaseInsensitively(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))
	place, err := lib.Resolve(context.Background(), "Comics")
	if err != nil {
		t.Fatal(err)
	}
	if place.FolderID != "comics" {
		t.Errorf("folder %q, want comics", place.FolderID)
	}
}

// A document must never be confused with a folder of the same name: uploading
// into a DocumentType guid would silently leave the target wherever it was.
func TestResolveIgnoresDocumentsWithTheFolderName(t *testing.T) {
	f := newFake(library.Entry{ID: "doc", Parent: "", Type: library.Document, VisibleName: "Comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))
	place, err := lib.Resolve(context.Background(), "Comics")
	if err != nil {
		t.Fatal(err)
	}
	if place.FolderID != library.RootID {
		t.Errorf("folder %q, want the root", place.FolderID)
	}
}

func TestDisabledWebInterfaceSaysWhatToDo(t *testing.T) {
	f := newFake()
	lib := newLibrary(t, f, confWith(t, "[General]\nWebInterfaceEnabled=false\n"))

	if e := lib.EnsureReachable(context.Background()); e == nil {
		t.Fatal("want an error")
	} else if e.Error() != library.WebInterfaceRemedy {
		t.Fatalf("error %q, want the plain-language remedy", e)
	}

	if _, err := lib.Upload(context.Background(), "", "v.pdf", strings.NewReader("%PDF")); err == nil {
		t.Fatal("want the upload refused too")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.uploads) != 0 {
		t.Errorf("uploaded %v with the interface off", f.uploads)
	}
}

func TestAMissingConfIsNotAnError(t *testing.T) {
	f := newFake()
	lib := newLibrary(t, f, filepath.Join(t.TempDir(), "absent.conf"))
	if err := lib.EnsureReachable(context.Background()); err != nil {
		t.Fatalf("want no error off the device, got %v", err)
	}
}

func TestAKeylessConfIsTreatedAsOff(t *testing.T) {
	enabled, err := library.WebInterfaceEnabled(confWith(t, "[General]\nDeveloperMode=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Error("a conf with no WebInterfaceEnabled key must count as off")
	}
}

func TestUploadFailureIsReported(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"})
	f.uploadCode = http.StatusInternalServerError
	lib := newLibrary(t, f, confWith(t, enabledConf))

	if _, err := lib.Upload(context.Background(), "comics", "v.pdf", strings.NewReader("%PDF")); err == nil {
		t.Fatal("want an error")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not carry the status", err)
	}
}

// A re-download of the same volume produces a second document with the same
// name. The new one must be identified by ID, not by name.
func TestUploadDistinguishesADuplicateName(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))
	ctx := context.Background()

	first, err := lib.Upload(ctx, "comics", "Vol 1.pdf", strings.NewReader("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := lib.Upload(ctx, "comics", "Vol 1.pdf", strings.NewReader("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if first.DocumentUUID == second.DocumentUUID {
		t.Fatalf("both uploads reported %s", first.DocumentUUID)
	}
}

func TestUploadAppendsThePDFSuffixLikeXochitlDoes(t *testing.T) {
	f := newFake()
	lib := newLibrary(t, f, confWith(t, enabledConf))
	res, err := lib.Upload(context.Background(), library.RootID, "Vol 1", strings.NewReader("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if res.VisibleName != "Vol 1.pdf" {
		t.Errorf("visible name %q, want the suffix xochitl adds", res.VisibleName)
	}
}

func TestAliasFailureStopsTheUpload(t *testing.T) {
	f := newFake()
	lib := library.New(library.Options{
		BaseURL:  f.server(t).URL,
		ConfPath: confWith(t, enabledConf),
		AddAlias: func() error { return library.ErrAliasForbidden },
	})
	if err := lib.EnsureReachable(context.Background()); err == nil {
		t.Fatal("want the alias failure surfaced")
	}
}

// The library is flat by design: a volume goes straight into Comics, with its
// series in the name.
func TestPlaceIsFlatIntoComics(t *testing.T) {
	f := newFake(library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"})
	lib := newLibrary(t, f, confWith(t, enabledConf))

	place, err := lib.Place(context.Background(), "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if !place.Complete() {
		t.Fatalf("missing %v; a series subfolder must not be required", place.Missing)
	}
	if place.FolderID != "comics" {
		t.Errorf("folder %q, want comics", place.FolderID)
	}
	if place.Remedy() != "" {
		t.Errorf("remedy %q, want none", place.Remedy())
	}
}

// A per-series subfolder the user made is honoured, because someone who has
// tidied their library should not have Quire untidy it.
func TestPlaceUsesASeriesFolderIfTheUserMadeOne(t *testing.T) {
	f := newFake(
		library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"},
		library.Entry{ID: "snot", Parent: "comics", Type: library.Collection, VisibleName: "Snotgirl"},
	)
	lib := newLibrary(t, f, confWith(t, enabledConf))

	place, err := lib.Place(context.Background(), "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if place.FolderID != "snot" {
		t.Errorf("folder %q, want snot", place.FolderID)
	}
	if len(place.Path) != 2 {
		t.Errorf("path %v", place.Path)
	}
}

// No Comics folder is the one-time setup step, not a failure: the volume still
// lands, at the top of My Files, and the user is told how to fix it.
func TestPlaceWithoutComicsFallsBackAndSaysSo(t *testing.T) {
	lib := newLibrary(t, newFake(), confWith(t, enabledConf))

	place, err := lib.Place(context.Background(), "Snotgirl")
	if err != nil {
		t.Fatal(err)
	}
	if place.FolderID != library.RootID {
		t.Errorf("folder %q, want the root", place.FolderID)
	}
	if got := place.Missing; len(got) != 1 || got[0] != library.ComicsFolder {
		t.Fatalf("missing %v, want just Comics — the series folder is optional", got)
	}
	if place.Remedy() != library.ComicsRemedy {
		t.Errorf("remedy %q, want the one-time setup instruction", place.Remedy())
	}
}

// The cap is on the multipart body and xochitl usually enforces it by resetting
// the connection mid-transfer, so a body over it must never leave the process:
// the alternative is wasting a 90 MB upload to learn what we already knew.
func TestAnOversizedUploadIsRefusedBeforeItIsSent(t *testing.T) {
	f := newFake()
	lib := newLibrary(t, f, confWith(t, enabledConf))

	big := strings.NewReader(strings.Repeat("A", library.MaxUploadBytes))
	_, err := lib.Upload(context.Background(), library.RootID, "huge.pdf", big)
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !errors.Is(err, library.ErrTooLarge) {
		t.Errorf("error %v, want ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %q is not plain enough to show anyone", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r == "POST /upload" {
			t.Fatal("the body was sent anyway")
		}
	}
}

// The budget has to leave room under the cap for multipart framing, or a volume
// assembled exactly to budget still fails.
func TestTheUploadBudgetLeavesMarginUnderTheCap(t *testing.T) {
	if library.UploadBudgetBytes >= library.MaxUploadBytes {
		t.Fatalf("budget %d is not below the cap %d", library.UploadBudgetBytes, library.MaxUploadBytes)
	}
	if library.MaxUploadBytes != 100_000_000 {
		t.Errorf("cap is %d; it was measured at exactly 100,000,000 on the device",
			library.MaxUploadBytes)
	}
}

// Child answers the question the frontend cannot ask for itself: xochitl's QML
// creates folders and moves documents but enumerates nothing, so "is there
// already a folder for this series?" is asked over HTTP and handed over.
func TestChildFindsAFolderInsideAnother(t *testing.T) {
	f := newFake(
		library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"},
		library.Entry{ID: "wandance", Parent: "comics", Type: library.Collection, VisibleName: "Wandance"},
		library.Entry{ID: "adoc", Parent: "comics", Type: library.Document, VisibleName: "Snotgirl"},
	)
	lib := newLibrary(t, f, confWith(t, enabledConf))

	got, ok, err := lib.Child(context.Background(), "comics", "Wandance")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ID != "wandance" {
		t.Errorf("Child found %q (%v), want wandance", got.ID, ok)
	}

	// A *document* of that name is not a folder to upload into.
	if _, ok, err := lib.Child(context.Background(), "comics", "Snotgirl"); err != nil || ok {
		t.Errorf("Child matched a document: ok=%v err=%v", ok, err)
	}

	if _, ok, err := lib.Child(context.Background(), "comics", "Nothing Here"); err != nil || ok {
		t.Errorf("Child invented a folder: ok=%v err=%v", ok, err)
	}
}

// A recorded folder id is only worth anything while it resolves. The user can
// delete the folder Quire made, and the next download must go back to looking
// it up by name rather than at an id that is not there.
func TestChildByIDReportsAFolderThatHasGone(t *testing.T) {
	f := newFake(
		library.Entry{ID: "comics", Parent: "", Type: library.Collection, VisibleName: "Comics"},
		library.Entry{ID: "wandance", Parent: "comics", Type: library.Collection, VisibleName: "Wandance"},
	)
	lib := newLibrary(t, f, confWith(t, enabledConf))

	if _, ok, err := lib.ChildByID(context.Background(), "comics", "wandance"); err != nil || !ok {
		t.Errorf("ChildByID lost a folder that is there: ok=%v err=%v", ok, err)
	}
	if _, ok, err := lib.ChildByID(context.Background(), "comics", "deleted-by-the-user"); err != nil || ok {
		t.Errorf("ChildByID kept a folder that has gone: ok=%v err=%v", ok, err)
	}
	if _, ok, err := lib.ChildByID(context.Background(), "comics", ""); err != nil || ok {
		t.Errorf("ChildByID answered for an empty id: ok=%v err=%v", ok, err)
	}
}
