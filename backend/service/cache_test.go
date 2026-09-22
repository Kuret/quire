package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/service"
)

// cacheWith builds a download cache of its own and returns the service and the
// root it was built under.
//
// Every test here builds its own: a clear that leaves something behind is only
// a finding if nothing else could have put it there.
func cacheWith(t *testing.T, chapters ...string) (*service.Service, string, string) {
	t.Helper()
	svc, store, _, root := serviceWithDownloadRoot(t)
	addSource(t, store)
	seriesDir := pageCache(t, root, "example-reader", "the-lantern-keeper", chapters...)
	return svc, root, seriesDir
}

// The size is what makes the button pressable: nobody clears something they
// cannot see the size of, and it is how they check it worked.
func TestCacheSizeIsReportedInWords(t *testing.T) {
	svc, _, _ := cacheWith(t, "/manga/the-lantern-keeper/chapter-1/")

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageGetCacheSize, `{}`)

	var st struct {
		Bytes   int64  `json:"bytes"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheStatus), &st); err != nil {
		t.Fatal(err)
	}
	if st.Bytes == 0 {
		t.Fatal("the cache reported nothing, though pages were written")
	}
	if !strings.Contains(st.Message, "holding") {
		t.Errorf("message %q does not say what the cache is holding", st.Message)
	}
}

// TestCacheStatusCarriesTheStorageSummary covers PLAN §12.6's addition to
// MessageCacheStatus: savedBytes, freeBytes and a composed storage sentence
// ride alongside the cache's own bytes/message, which stay exactly what they
// were.
func TestCacheStatusCarriesTheStorageSummary(t *testing.T) {
	svc, _, _ := cacheWith(t, "/manga/the-lantern-keeper/chapter-1/")

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageGetCacheSize, `{}`)

	var st struct {
		Bytes      int64  `json:"bytes"`
		Message    string `json:"message"`
		SavedBytes int64  `json:"savedBytes"`
		FreeBytes  int64  `json:"freeBytes"`
		Storage    string `json:"storage"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheStatus), &st); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.Message, "holding") {
		t.Errorf("message %q changed shape; it must stay the cache-only sentence", st.Message)
	}
	if st.Storage == "" {
		t.Error("no storage sentence")
	}
	if !strings.Contains(st.Storage, "download cache") {
		t.Errorf("storage %q does not mention the download cache", st.Storage)
	}
}

// Clearing asks first. It is hundreds of megabytes and the only way back is to
// fetch it all again.
func TestClearingTheCacheAsksFirst(t *testing.T) {
	svc, _, seriesDir := cacheWith(t, "/manga/the-lantern-keeper/chapter-1/")
	chapter := download.ChapterDir(seriesDir, "/manga/the-lantern-keeper/chapter-1/")

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageClearCache, `{}`)

	var q struct {
		Bytes   int64  `json:"bytes"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheConfirm), &q); err != nil {
		t.Fatal(err)
	}
	if q.Bytes == 0 {
		t.Error("the question is about nothing")
	}
	if !strings.Contains(q.Message, "library") {
		t.Errorf("question %q does not say the library is untouched", q.Message)
	}
	if !exists(t, chapter) {
		t.Error("asking cleared the cache")
	}
}

// The confirmed clear empties the cache and tidies the tree behind it.
func TestClearingTheCacheRemovesEverythingAndPrunes(t *testing.T) {
	svc, _, seriesDir := cacheWith(t,
		"/manga/the-lantern-keeper/chapter-1/",
		"/manga/the-lantern-keeper/chapter-2/")

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageClearCache, `{"confirmed":true}`)

	var st struct {
		Bytes   int64  `json:"bytes"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheStatus), &st); err != nil {
		t.Fatal(err)
	}
	if st.Bytes != 0 {
		t.Errorf("%d bytes left after a clear", st.Bytes)
	}
	if !strings.Contains(st.Message, "Cleared") {
		t.Errorf("message %q does not say what happened", st.Message)
	}
	if exists(t, seriesDir) {
		t.Error("the series directory was left behind empty")
	}
	if exists(t, filepath.Dir(seriesDir)) {
		t.Error("the source directory was left behind empty")
	}
}

// Nothing to clear is said plainly rather than reported as a clear of nothing.
func TestClearingAnEmptyCacheSaysSo(t *testing.T) {
	svc, _, _ := cacheWith(t)

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageClearCache, `{}`)

	var st struct {
		Bytes   int64  `json:"bytes"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheStatus), &st); err != nil {
		t.Fatal(err)
	}
	if st.Bytes != 0 {
		t.Errorf("%d bytes in a cache nothing was written to", st.Bytes)
	}
	if !strings.Contains(st.Message, "empty") {
		t.Errorf("message %q does not say the cache is empty", st.Message)
	}
}

// The library, the sources and the logs are not the cache. A clear that reached
// them would be losing the user's own records to reclaim page images.
func TestClearingTheCacheLeavesEverythingElseAlone(t *testing.T) {
	svc, root, _ := cacheWith(t, "/manga/the-lantern-keeper/chapter-1/")

	outside := filepath.Join(filepath.Dir(root), "state")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "library.json")
	if err := os.WriteFile(keep, []byte(`{"records":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageClearCache, `{"confirmed":true}`)
	rec.wait(t, appload.MessageCacheStatus)

	if !exists(t, keep) {
		t.Fatal("a cache clear removed something outside the download directory")
	}
}
