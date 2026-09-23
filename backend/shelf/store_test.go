package shelf_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/shelf"
)

// TestOldRecordReadsAsPages is a backward-compatibility check: a saved.json
// written before books existed has no "kind" field at all, and must still
// read as an ordinary chapter of page images rather than as some new,
// unrecognised kind.
func TestOldRecordReadsAsPages(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"records":[{"source":"mangadex","series":"abc","chapter":"ch1",
		"seriesTitle":"Snotgirl","chapterTitle":"Chapter 1","pages":["abc/ch1/0001.jpg"],
		"savedAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, shelf.StoreFileName), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := shelf.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := s.Get(shelf.Key{Source: "mangadex", Series: "abc", Chapter: "ch1"})
	if !ok {
		t.Fatal("the old record was not read")
	}
	if rec.IsBook() {
		t.Error("a record with no kind field must not read as a book")
	}
	if rec.Kind != shelf.KindPages {
		t.Errorf("Kind = %q, want the zero value", rec.Kind)
	}
}

func TestStoreRoundTripsABook(t *testing.T) {
	dir := t.TempDir()
	s, err := shelf.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := shelf.Key{Source: "shelfmark", Series: "dark-disciple", Chapter: "release-1"}
	rec := shelf.Record{
		Key:              key,
		Kind:             shelf.KindBook,
		SeriesTitle:      "Dark Disciple",
		ChapterTitle:     "Dark Disciple",
		File:             "books/shelfmark/dark-disciple/release-1.epub",
		Format:           "epub",
		Bytes:            553004,
		Position:         12,
		PositionFraction: 0.05,
		PositionSnippet:  "For as long as I live",
		PositionLayout:   "abc123:509x679",
	}
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	reopened, err := shelf.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(key)
	if !ok {
		t.Fatal("the book was not remembered")
	}
	if !got.IsBook() {
		t.Error("IsBook() = false for a Kind: book record")
	}
	if got.File != rec.File || got.Format != rec.Format {
		t.Errorf("File/Format = %q/%q, want %q/%q", got.File, got.Format, rec.File, rec.Format)
	}
	if got.PositionFraction != rec.PositionFraction || got.PositionSnippet != rec.PositionSnippet ||
		got.PositionLayout != rec.PositionLayout {
		t.Errorf("position fields did not round-trip: %+v", got)
	}
	if len(got.Pages) != 0 {
		t.Errorf("a book record should have no Pages, got %v", got.Pages)
	}
}

func TestStoreRoundTripsAChapter(t *testing.T) {
	dir := t.TempDir()
	s, err := shelf.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := shelf.Key{Source: "mangadex", Series: "abc", Chapter: "ch1"}
	rec := shelf.Record{
		Key:          key,
		SeriesTitle:  "Snotgirl",
		ChapterTitle: "Chapter 1",
		Number:       1,
		Pages:        []string{"mangadex/abc/ch1-abcd1234/0001.jpg", "mangadex/abc/ch1-abcd1234/0002.jpg"},
		Bytes:        4096,
	}
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	reopened, err := shelf.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(key)
	if !ok {
		t.Fatal("the chapter was not remembered")
	}
	if got.SavedAt.IsZero() {
		t.Error("SavedAt was not filled in")
	}
	if len(got.Pages) != 2 {
		t.Errorf("pages %v", got.Pages)
	}
	if got.Position != 0 {
		t.Errorf("position %d, want 0 by default", got.Position)
	}
}

// Re-saving the same chapter replaces the record rather than duplicating it —
// the same rule library.Store applies to a re-downloaded volume.
func TestPutReplacesTheRecordForTheSameChapter(t *testing.T) {
	s, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := shelf.Key{Source: "s", Series: "series", Chapter: "1"}
	if err := s.Put(shelf.Record{Key: key, Position: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(shelf.Record{Key: key, Position: 5}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("%d records, want 1", n)
	}
	if got, _ := s.Get(key); got.Position != 5 {
		t.Errorf("position %d, want 5", got.Position)
	}
}

func TestPutRefusesAnIncompleteRecord(t *testing.T) {
	s, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []shelf.Record{
		{Key: shelf.Key{Source: "s", Series: "x"}},
		{Key: shelf.Key{Chapter: "1"}},
		{Key: shelf.Key{Source: "s", Chapter: "1"}},
	} {
		if err := s.Put(rec); err == nil {
			t.Errorf("stored %+v", rec)
		}
	}
}

func TestRemoveIsSafeToRepeat(t *testing.T) {
	s, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := shelf.Key{Source: "s", Series: "x", Chapter: "1"}
	if err := s.Put(shelf.Record{Key: key}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.Remove(key); err != nil {
			t.Fatalf("remove %d: %v", i, err)
		}
	}
	if _, ok := s.Get(key); ok {
		t.Error("still there")
	}
}

func TestListIsNewestFirst(t *testing.T) {
	s, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Put(shelf.Record{Key: shelf.Key{Source: "s", Series: "x", Chapter: "1"}, SavedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(shelf.Record{Key: shelf.Key{Source: "s", Series: "x", Chapter: "2"}, SavedAt: old.AddDate(1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); got[0].Chapter != "2" {
		t.Errorf("first is %q, want 2", got[0].Chapter)
	}
}

func TestForSeriesFiltersByKey(t *testing.T) {
	s, err := shelf.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(shelf.Record{Key: shelf.Key{Source: "s", Series: "x", Chapter: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(shelf.Record{Key: shelf.Key{Source: "s", Series: "y", Chapter: "1"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.ForSeries("s", "x"); len(got) != 1 {
		t.Fatalf("%d records, want 1", len(got))
	}
}

func TestOpenStoreRejectsAGarbledFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, shelf.StoreFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := shelf.OpenStore(dir); err == nil {
		t.Fatal("want an error rather than a silently empty shelf")
	} else if !strings.Contains(err.Error(), shelf.StoreFileName) {
		t.Errorf("error %q does not name the file", err)
	}
}
