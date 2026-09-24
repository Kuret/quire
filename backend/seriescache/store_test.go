package seriescache_test

import (
	"testing"
	"time"

	"github.com/rickl/quire/backend/seriescache"
	"github.com/rickl/quire/backend/theme"
)

func entry(source, series string, at time.Time) seriescache.Entry {
	return seriescache.Entry{
		SourceID:  source,
		SeriesID:  series,
		Series:    theme.Series{ID: series, Title: "Title of " + series},
		Chapters:  []theme.Chapter{{ID: series + "-ch1", Title: "Chapter 1", Number: 1}},
		FetchedAt: at,
		OpenedAt:  at,
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.Put(entry("src", "series-1", now), nil); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get("src", "series-1")
	if !ok {
		t.Fatal("expected a cached entry")
	}
	if got.Series.Title != "Title of series-1" || len(got.Chapters) != 1 {
		t.Fatalf("round trip lost data: %+v", got)
	}

	if _, ok := store.Get("src", "series-missing"); ok {
		t.Fatal("expected no entry for a series never put")
	}

	// Reopening from disk must see the same entry.
	store2, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got2, ok := store2.Get("src", "series-1")
	if !ok || got2.Series.Title != got.Series.Title {
		t.Fatalf("entry did not survive reopening the store: %+v ok=%v", got2, ok)
	}
}

func TestTouchUpdatesOpenedAtOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	if err := store.Put(entry("src", "series-1", base), nil); err != nil {
		t.Fatal(err)
	}
	later := time.Now()
	store.Touch("src", "series-1", later)

	got, ok := store.Get("src", "series-1")
	if !ok {
		t.Fatal("expected the entry to still be there")
	}
	if !got.FetchedAt.Equal(base) {
		t.Errorf("Touch changed FetchedAt: got %v, want %v", got.FetchedAt, base)
	}
	if !got.OpenedAt.Equal(later) {
		t.Errorf("Touch did not bump OpenedAt: got %v, want %v", got.OpenedAt, later)
	}
}

func TestRemoveAndRemoveSource(t *testing.T) {
	dir := t.TempDir()
	store, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.Put(entry("src-a", "s1", now), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(entry("src-a", "s2", now), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(entry("src-b", "s1", now), nil); err != nil {
		t.Fatal(err)
	}

	if err := store.Remove("src-a", "s1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("src-a", "s1"); ok {
		t.Error("Remove left the entry behind")
	}
	// Removing again must not error.
	if err := store.Remove("src-a", "s1"); err != nil {
		t.Fatalf("removing twice errored: %v", err)
	}

	if err := store.RemoveSource("src-a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("src-a", "s2"); ok {
		t.Error("RemoveSource left an entry behind for that source")
	}
	if _, ok := store.Get("src-b", "s1"); !ok {
		t.Error("RemoveSource touched a different source's entry")
	}
}

func TestEvictionKeepsProtectedEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-24 * time.Hour)

	// One protected series, opened longest ago — it must survive eviction
	// despite being the oldest entry, because eviction order alone would
	// pick it first; the unprotected entries added after it should be what
	// gets trimmed to make room instead.
	if err := store.Put(entry("src", "protected", base), nil); err != nil {
		t.Fatal(err)
	}

	protect := func(source, series string) bool { return series == "protected" }

	for i := 0; i < seriescache.MaxSeries; i++ {
		at := base.Add(time.Duration(i+1) * time.Minute)
		seriesID := "series-" + time.Duration(i).String()
		if err := store.Put(entry("src", seriesID, at), protect); err != nil {
			t.Fatal(err)
		}
	}

	if _, ok := store.Get("src", "protected"); !ok {
		t.Fatal("eviction dropped a protected entry")
	}
	if got := len(store.List()); got != seriescache.MaxSeries {
		t.Errorf("expected the cap to hold with the protected entry kept and others trimmed, got %d entries", got)
	}

	// Everything protected: the cache must be allowed to sit over the cap
	// rather than break the "never evict a protected series" promise.
	dir2 := t.TempDir()
	store2, err := seriescache.OpenStore(dir2)
	if err != nil {
		t.Fatal(err)
	}
	protectAll := func(string, string) bool { return true }
	for i := 0; i < seriescache.MaxSeries+5; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		seriesID := "series-" + time.Duration(i).String()
		if err := store2.Put(entry("src", seriesID, at), protectAll); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store2.List()); got != seriescache.MaxSeries+5 {
		t.Errorf("expected every protected entry to survive over the cap, got %d entries (want %d)",
			got, seriescache.MaxSeries+5)
	}
}

func TestEvictionTrimsUnprotectedEntriesToTheCap(t *testing.T) {
	dir := t.TempDir()
	store, err := seriescache.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < seriescache.MaxSeries+10; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		seriesID := "series-" + time.Duration(i).String()
		if err := store.Put(entry("src", seriesID, at), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.List()); got != seriescache.MaxSeries {
		t.Errorf("expected eviction to trim to %d entries, got %d", seriescache.MaxSeries, got)
	}
	// The earliest-opened entries are the ones that should be gone.
	if _, ok := store.Get("src", "series-"+time.Duration(0).String()); ok {
		t.Error("the oldest unprotected entry should have been evicted")
	}
}
