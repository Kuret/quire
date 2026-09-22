package shelf_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/shelf"
)

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
