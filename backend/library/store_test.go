package library_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/library"
)

func TestStoreRoundTripsADocumentUUID(t *testing.T) {
	dir := t.TempDir()
	s, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := library.Key{Source: "mangadex", Series: "abc", Volume: "1"}
	rec := library.Record{
		Key:          key,
		DocumentUUID: "950d535b-a5f7-4b26-9a78-c788f95a5c8c",
		FolderUUID:   "a93dfa8f-8e58-4792-82d3-81a00ca982f4",
		FolderPath:   []string{"Comics", "Snotgirl"},
		VisibleName:  "Snotgirl Vol. 1.pdf",
		Pages:        212,
	}
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	reopened, err := library.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(key)
	if !ok {
		t.Fatal("the volume was not remembered")
	}
	if got.DocumentUUID != rec.DocumentUUID {
		t.Errorf("uuid %q, want %q", got.DocumentUUID, rec.DocumentUUID)
	}
	if got.StoredAt.IsZero() {
		t.Error("StoredAt was not filled in")
	}
	if len(got.FolderPath) != 2 {
		t.Errorf("folder path %v", got.FolderPath)
	}
}

// Re-downloading gives xochitl a reason to allocate a second UUID, and the old
// one no longer opens anything the user recognises. The record must move on.
func TestPutReplacesTheUUIDForTheSameVolume(t *testing.T) {
	s, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := library.Key{Source: "s", Series: "series", Volume: "1"}
	if err := s.Put(library.Record{Key: key, DocumentUUID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(library.Record{Key: key, DocumentUUID: "second"}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("%d records, want 1", n)
	}
	if got, _ := s.Get(key); got.DocumentUUID != "second" {
		t.Errorf("uuid %q, want second", got.DocumentUUID)
	}
}

func TestPutRefusesAnIncompleteRecord(t *testing.T) {
	s, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []library.Record{
		{Key: library.Key{Source: "s", Series: "x", Volume: "1"}},
		{DocumentUUID: "u"},
		{Key: library.Key{Source: "s", Series: "x"}, DocumentUUID: "u"},
	} {
		if err := s.Put(rec); err == nil {
			t.Errorf("stored %+v", rec)
		}
	}
}

func TestRemoveIsSafeToRepeat(t *testing.T) {
	s, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := library.Key{Source: "s", Series: "x", Volume: "1"}
	if err := s.Put(library.Record{Key: key, DocumentUUID: "u"}); err != nil {
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
	s, err := library.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Put(library.Record{Key: library.Key{Source: "s", Series: "x", Volume: "1"}, DocumentUUID: "old", StoredAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(library.Record{Key: library.Key{Source: "s", Series: "x", Volume: "2"}, DocumentUUID: "new", StoredAt: old.AddDate(1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); got[0].DocumentUUID != "new" {
		t.Errorf("first is %q, want new", got[0].DocumentUUID)
	}
}

func TestOpenStoreRejectsAGarbledFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, library.StoreFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.OpenStore(dir); err == nil {
		t.Fatal("want an error rather than a silently empty library")
	} else if !strings.Contains(err.Error(), library.StoreFileName) {
		t.Errorf("error %q does not name the file", err)
	}
}

// SeriesTitle was added on 2026-09-17, after people already had a library.json.
// A new optional field has to read back from a file written without it — no
// version bump, no migration — and this asserts that rather than assuming it.
func TestAStoreWrittenBeforeSeriesTitleStillLoads(t *testing.T) {
	dir := t.TempDir()
	old := `{
  "version": 1,
  "volumes": [
    {
      "source": "example-reader",
      "series": "/manga/the-lantern-keeper/",
      "volume": "1",
      "documentUuid": "d1",
      "folderUuid": "comics",
      "folderPath": ["Comics"],
      "visibleName": "The Lantern Keeper \u2014 Ch 0001.pdf"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, library.StoreFileName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := library.OpenStore(dir)
	if err != nil {
		t.Fatalf("a library.json written before the field would not load: %v", err)
	}
	recs := s.List()
	if len(recs) != 1 {
		t.Fatalf("%d records, want the one that was there", len(recs))
	}
	if recs[0].DocumentUUID != "d1" || recs[0].VisibleName == "" {
		t.Errorf("the record came back wrong: %+v", recs[0])
	}
	if recs[0].SeriesTitle != "" {
		t.Errorf("SeriesTitle %q, want empty so the filename fallback is used", recs[0].SeriesTitle)
	}

	// And writing it back does not invent a value for the missing field.
	recs[0].Pages = 12
	if err := s.Put(recs[0]); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, library.StoreFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "seriesTitle") {
		t.Errorf("an empty SeriesTitle was written out:\n%s", b)
	}
}
