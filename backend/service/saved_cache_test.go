package service_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/shelf"
)

// Clear cache only ever walks the download cache (backend/service/cache.go),
// never the saved root — by construction, since sweepCache is handed
// s.downloadDir and nothing else. This proves the guarantee end to end: a
// chapter saved in Quire survives a full cache clear untouched.
func TestClearCacheLeavesASavedChapterIntact(t *testing.T) {
	h := buildDownloadHarness(t, downloadRoutes(t))
	addSource(t, h.store)
	rec := &recorder{}
	seriesID, chapterID := saveOne(t, h, rec)

	before, ok := h.shelfStore.Get(shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID})
	if !ok || len(before.Pages) == 0 {
		t.Fatal("chapter was not saved")
	}
	firstPage := filepath.Join(h.savedDir, before.Pages[0])
	if _, err := os.Stat(firstPage); err != nil {
		t.Fatalf("the saved page does not exist before clearing: %v", err)
	}

	// Some unrelated junk in the download cache, so there is something for
	// Clear cache to actually clear.
	pageCache(t, h.downloadDir, "other-source", "other-series", "chapter-1")

	handle(t, h.svc, rec, appload.MessageClearCache, `{}`)
	var confirm struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageCacheConfirm), &confirm); err != nil {
		t.Fatal(err)
	}
	handle(t, h.svc, rec, appload.MessageClearCache, `{"confirmed":true}`)
	rec.wait(t, appload.MessageCacheStatus)

	if _, err := os.Stat(firstPage); err != nil {
		t.Errorf("the saved page was removed by clearing the cache: %v", err)
	}
	after, ok := h.shelfStore.Get(shelf.Key{Source: "example-reader", Series: seriesID, Chapter: chapterID})
	if !ok {
		t.Error("the shelf record was dropped by clearing the cache")
	}
	if len(after.Pages) != len(before.Pages) {
		t.Errorf("pages %v, want unchanged %v", after.Pages, before.Pages)
	}
}
