package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

// addSecondSource is a second source, so the same series title can exist twice
// and the row's (source, series) pairing has something to be right about.
func addSecondSource(t *testing.T, store *state.Store) {
	t.Helper()
	if _, err := store.Add(&theme.Source{
		Name: "Other Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://other.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
}

type downloadedList struct {
	Series []struct {
		SourceID   string `json:"sourceId"`
		SourceName string `json:"sourceName"`
		SeriesID   string `json:"seriesId"`
		Title      string `json:"title"`
		Detail     string `json:"detail"`
		CoverURL   string `json:"coverUrl"`
		LatestUUID string `json:"latestUuid"`
		Openable   bool   `json:"openable"`
		Note       string `json:"note"`
	} `json:"series"`
	Empty string `json:"empty"`
}

// askDownloaded fetches the overview.
func askDownloaded(t *testing.T, svc *service.Service) downloadedList {
	t.Helper()
	rec := &recorder{}
	handle(t, svc, rec, appload.MessageListDownloaded, `{}`)
	var got downloadedList
	if err := json.Unmarshal(rec.wait(t, appload.MessageDownloadedList), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// storedVolume is one download, as the library store holds it.
func storedVolume(source, series, title, volume, uuid string) library.Record {
	return library.Record{
		Key:          library.Key{Source: source, Series: series, Volume: volume},
		DocumentUUID: uuid,
		SeriesTitle:  title,
		VisibleName:  title + " — Ch " + volume + ".pdf",
	}
}

// One row per series with a download, naming its source, and nothing for a
// series that has none.
func TestDownloadedListsOnlySeriesWithDownloads(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	if err := libStore.Put(storedVolume("example-reader", "/manga/the-lantern-keeper/",
		"The Lantern Keeper", "1", "doc-a")); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows, want 1", len(got.Series))
	}
	row := got.Series[0]
	if row.Title != "The Lantern Keeper" {
		t.Errorf("title %q", row.Title)
	}
	if row.SeriesID != "/manga/the-lantern-keeper/" || row.SourceID != "example-reader" {
		t.Errorf("row points at %q on %q", row.SeriesID, row.SourceID)
	}
	if row.SourceName != "Example Reader" {
		t.Errorf("source name %q; every row names its source", row.SourceName)
	}
	if !row.Openable {
		t.Error("a row whose source is present must be openable")
	}
	if row.Detail != "1 download" {
		t.Errorf("detail %q", row.Detail)
	}
}

// The navigational reason the row is a pair: two sources holding the same
// series are two rows, each carrying its own source, so a tap knows where to go.
func TestDownloadedSeparatesTheSameSeriesFromTwoSources(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	addSecondSource(t, store)

	for _, rec := range []library.Record{
		storedVolume("example-reader", "/manga/the-lantern-keeper/", "The Lantern Keeper", "1", "doc-a"),
		storedVolume("other-reader", "/series/lantern/", "The Lantern Keeper", "1", "doc-b"),
	} {
		if err := libStore.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 2 {
		t.Fatalf("%d rows, want one per source", len(got.Series))
	}
	seen := map[string]string{}
	for _, row := range got.Series {
		if row.Title != "The Lantern Keeper" {
			t.Errorf("title %q", row.Title)
		}
		seen[row.SourceID] = row.SeriesID
	}
	if seen["example-reader"] != "/manga/the-lantern-keeper/" || seen["other-reader"] != "/series/lantern/" {
		t.Errorf("rows point at %v; each must carry its own source's series id", seen)
	}
}

// Every volume of a series is one row, counted — and the count is of downloads,
// which is true for a volume record and for a legacy one alike.
func TestDownloadedCountsTheDownloadsOfASeries(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	for i, uuid := range []string{"doc-a", "doc-b", "doc-c"} {
		if err := libStore.Put(storedVolume("example-reader", "/manga/the-lantern-keeper/",
			"The Lantern Keeper", string(rune('1'+i)), uuid)); err != nil {
			t.Fatal(err)
		}
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows, want the series once", len(got.Series))
	}
	if got.Series[0].Detail != "3 downloads" {
		t.Errorf("detail %q, want a count of downloads", got.Series[0].Detail)
	}
}

// A source the user removed leaves its downloads behind. The row is listed
// because the files are theirs, and it says why it cannot be opened rather than
// failing on a tap.
func TestDownloadedListsASeriesWhoseSourceIsGone(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	if err := libStore.Put(storedVolume("deleted-source", "/manga/whatever/",
		"Some Series", "1", "doc-a")); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows; downloads survive their source being removed", len(got.Series))
	}
	row := got.Series[0]
	if row.Openable {
		t.Error("a row whose source has gone must not offer to open it")
	}
	if !strings.Contains(row.Note, "removed") {
		t.Errorf("note %q does not say why", row.Note)
	}
	if !strings.Contains(row.Note, "still on your reMarkable") {
		t.Errorf("note %q does not say the downloads are still there", row.Note)
	}
}

// A record written before SeriesTitle existed still names itself, by the same
// em-dash rule the filing pass uses.
func TestDownloadedNamesALegacyRecordFromItsFilename(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	legacy := storedVolume("example-reader", "/manga/the-lantern-keeper/", "", "1", "doc-a")
	legacy.SeriesTitle = ""
	legacy.VisibleName = "The Lantern Keeper — Ch 0001.pdf"
	if err := libStore.Put(legacy); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows, want the legacy record listed", len(got.Series))
	}
	if got.Series[0].Title != "The Lantern Keeper" {
		t.Errorf("title %q, want the name split off the filename", got.Series[0].Title)
	}
}

// And a record that yields no name at all is still listed, by something
// honest — never skipped, because it is a download the user made.
func TestDownloadedStillListsARecordWithNoNameToFind(t *testing.T) {
	svc, store, libStore, _, _ := newDownloadService(t)
	addSource(t, store)
	odd := storedVolume("example-reader", "/manga/mystery/", "", "1", "doc-a")
	odd.SeriesTitle = ""
	odd.VisibleName = "no-series-here.pdf"
	if err := libStore.Put(odd); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 1 {
		t.Fatalf("%d rows; a nameless download is still a download", len(got.Series))
	}
	if got.Series[0].Title != "/manga/mystery/" {
		t.Errorf("title %q, want the series id as the honest fallback", got.Series[0].Title)
	}
}

// Newest first: what the user did last is what they are looking for.
func TestDownloadedPutsTheNewestSeriesFirst(t *testing.T) {
	svc, store, libStore, _, rec := newDownloadService(t)
	addSource(t, store)

	seriesID, chapterID := firstChapter(t, svc, rec)
	handle(t, svc, rec, appload.MessageEnqueueDownload,
		`{"sourceId":"example-reader","seriesId":"`+seriesID+`","volumeId":"`+chapterID+
			`","confirmed":true,"destination":"library"}`)
	waitForPhase(t, rec, "done")

	// An older download of a different series.
	older := storedVolume("example-reader", "/manga/older/", "An Older Series", "1", "doc-old")
	older.StoredAt = fixedNow.Add(-72 * 60 * 60 * 1000000000)
	if err := libStore.Put(older); err != nil {
		t.Fatal(err)
	}

	got := askDownloaded(t, svc)
	if len(got.Series) != 2 {
		t.Fatalf("%d rows, want 2", len(got.Series))
	}
	if got.Series[1].Title != "An Older Series" {
		t.Errorf("the older series is at %d, want last", 0)
	}
}

// An empty library says so, in the backend's words.
func TestDownloadedSaysSoWhenThereIsNothing(t *testing.T) {
	svc, store, _, _, _ := newDownloadService(t)
	addSource(t, store)

	got := askDownloaded(t, svc)
	if len(got.Series) != 0 {
		t.Fatalf("%d rows, want none", len(got.Series))
	}
	if !strings.Contains(got.Empty, "Nothing downloaded yet") {
		t.Errorf("empty state %q", got.Empty)
	}
}
