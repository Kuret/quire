package service

import (
	"testing"
	"time"

	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/shelf"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
)

// humanAge, PLAN §12.12's humanised chapter-list age.
func TestHumanAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "a moment ago"},
		{59 * time.Second, "a moment ago"},
		{2 * time.Minute, "2 minutes ago"},
		{75 * time.Second, "1 minute ago"},
		{45 * time.Minute, "45 minutes ago"},
		{2 * time.Hour, "2 hours ago"},
		{75 * time.Minute, "1 hour ago"},
		{23 * time.Hour, "23 hours ago"},
		{3 * 24 * time.Hour, "3 days ago"},
		{25 * time.Hour, "1 day ago"},
	}
	for _, c := range cases {
		if got := humanAge(c.d); got != c.want {
			t.Errorf("humanAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func newSourceStore(t *testing.T) (*state.Store, string) {
	t.Helper()
	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(nil))
	store, err := state.Open(t.TempDir(), reg)
	if err != nil {
		t.Fatal(err)
	}
	src, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, src.ID
}

// A series with a chapter saved in Quire must survive eviction: forgetting its
// chapter list would leave the saved chapter with nothing to show it saved
// against on the series screen.
func TestSeriesCacheProtectKeepsSavedChapters(t *testing.T) {
	store, sourceID := newSourceStore(t)
	shelfStore, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := shelfStore.Put(shelf.Record{
		Key: shelf.Key{Source: sourceID, Series: "the-series", Chapter: "ch-1"},
	}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Store: store, ShelfStore: shelfStore})
	protect := s.seriesCacheProtect()

	if !protect(sourceID, "the-series") {
		t.Error("a series with a saved chapter must be protected from eviction")
	}
	if protect(sourceID, "some-other-series") {
		t.Error("an unrelated series must not be protected")
	}
}

// A series with a document already in the reMarkable library must likewise
// survive: the "Read" button on that row depends on the chapter list knowing
// which document holds which chapter.
func TestSeriesCacheProtectKeepsLibraryRecords(t *testing.T) {
	store, sourceID := newSourceStore(t)
	libStore, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: sourceID, Series: "the-series", Volume: "1"},
		DocumentUUID: "doc-1",
		Chapters:     []string{"ch-1"},
	}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Store: store, LibraryStore: libStore})
	protect := s.seriesCacheProtect()

	if !protect(sourceID, "the-series") {
		t.Error("a series with a library record must be protected from eviction")
	}
	if protect(sourceID, "some-other-series") {
		t.Error("an unrelated series must not be protected")
	}
}

// A watched series must survive too: a watch with nothing to check its
// chapters against on reopen would either lose its "new since you last
// looked" baseline or have to refetch just to re-establish it.
func TestSeriesCacheProtectKeepsWatchedSeries(t *testing.T) {
	store, sourceID := newSourceStore(t)
	if _, err := store.Watch(sourceID, "the-series", "The Series", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Store: store})
	protect := s.seriesCacheProtect()

	if !protect(sourceID, "the-series") {
		t.Error("a watched series must be protected from eviction")
	}
	if protect(sourceID, "some-other-series") {
		t.Error("an unrelated series must not be protected")
	}
}

// synthesizeSeriesDetail builds a listing from saved chapters and library
// records alone, for a series never cached and unreachable right now.
func TestSynthesizeSeriesDetailFromSavedChapters(t *testing.T) {
	store, sourceID := newSourceStore(t)
	shelfStore, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := shelfStore.Put(shelf.Record{
		Key:          shelf.Key{Source: sourceID, Series: "the-series", Chapter: "ch-1"},
		SeriesTitle:  "The Series",
		ChapterTitle: "Chapter One",
		Number:       1,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Store: store, ShelfStore: shelfStore})

	series, chapters, ok := s.synthesizeSeriesDetail(sourceID, "the-series")
	if !ok {
		t.Fatal("expected a synthesised result from the saved chapter")
	}
	if series.Title != "The Series" {
		t.Errorf("title = %q, want the saved chapter's series title", series.Title)
	}
	if len(chapters) != 1 || chapters[0].ID != "ch-1" || chapters[0].Title != "Chapter One" {
		t.Fatalf("unexpected chapters: %+v", chapters)
	}
}

func TestSynthesizeSeriesDetailFromLibraryOnly(t *testing.T) {
	store, sourceID := newSourceStore(t)
	libStore, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := libStore.Put(library.Record{
		Key:          library.Key{Source: sourceID, Series: "the-series", Volume: "1"},
		DocumentUUID: "doc-1",
		SeriesTitle:  "The Series",
		Chapters:     []string{"ch-1", "ch-2"},
	}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Store: store, LibraryStore: libStore})

	series, chapters, ok := s.synthesizeSeriesDetail(sourceID, "the-series")
	if !ok {
		t.Fatal("expected a synthesised result from the library record")
	}
	if series.Title != "The Series" {
		t.Errorf("title = %q, want the library record's series title", series.Title)
	}
	if len(chapters) != 2 {
		t.Fatalf("expected both library-only chapters, got %+v", chapters)
	}
	for _, c := range chapters {
		if c.Number != -1 {
			t.Errorf("chapter %q: number = %v, want -1 (unknown) for a library-only fallback", c.ID, c.Number)
		}
		if c.Title == "" {
			t.Errorf("chapter %q has no fallback title", c.ID)
		}
	}
}

func TestSynthesizeSeriesDetailNothingOnTablet(t *testing.T) {
	store, sourceID := newSourceStore(t)
	s := New(Options{Store: store})
	if _, _, ok := s.synthesizeSeriesDetail(sourceID, "the-series"); ok {
		t.Error("expected no synthesis when nothing is saved or in the library")
	}
}
