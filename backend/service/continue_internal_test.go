package service

import (
	"testing"
	"time"

	"github.com/rickl/quire/backend/shelf"
)

func chapterRec(chapter string, number float64, savedAt time.Time) shelf.Record {
	return shelf.Record{
		Key:     shelf.Key{Source: "src", Series: "series", Chapter: chapter},
		Number:  number,
		SavedAt: savedAt,
		Pages:   []string{"0001.jpg", "0002.jpg"},
	}
}

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestComputeContinueNothingAtAll(t *testing.T) {
	got := computeContinue(nil, "")
	if got.Kind != continueKindNone {
		t.Errorf("kind = %q, want none", got.Kind)
	}
}

func TestComputeContinueLibraryOnly(t *testing.T) {
	got := computeContinue(nil, "doc-1")
	if got.Kind != continueKindLibrary || got.DocumentUUID != "doc-1" {
		t.Errorf("got %+v, want library/doc-1", got)
	}
}

// Never read: the first chapter in reading order, not the most recently
// saved one — saving order and reading order can differ.
func TestComputeContinueNeverRead(t *testing.T) {
	recs := []shelf.Record{
		chapterRec("ch2", 2, t0.Add(1*time.Hour)),
		chapterRec("ch1", 1, t0),
		chapterRec("ch3", 3, t0.Add(2*time.Hour)),
	}
	got := computeContinue(recs, "")
	if got.Kind != continueKindSaved || got.ChapterID != "ch1" {
		t.Errorf("got %+v, want saved/ch1", got)
	}
}

// Read but not finished: continue at the same chapter.
func TestComputeContinueUnfinishedContinuesTheSameChapter(t *testing.T) {
	ch1 := chapterRec("ch1", 1, t0)
	ch1.Position = 0 // not the last of 2 pages
	ch1.LastReadAt = t0.Add(time.Hour)
	ch2 := chapterRec("ch2", 2, t0)

	got := computeContinue([]shelf.Record{ch1, ch2}, "")
	if got.Kind != continueKindSaved || got.ChapterID != "ch1" {
		t.Errorf("got %+v, want saved/ch1", got)
	}
}

// Finished, with a next chapter saved: continue there.
func TestComputeContinueFinishedGoesToTheNextChapter(t *testing.T) {
	ch1 := chapterRec("ch1", 1, t0)
	ch1.Position = 1 // last of 2 pages: finished
	ch1.LastReadAt = t0.Add(time.Hour)
	ch2 := chapterRec("ch2", 2, t0)

	got := computeContinue([]shelf.Record{ch1, ch2}, "")
	if got.Kind != continueKindSaved || got.ChapterID != "ch2" {
		t.Errorf("got %+v, want saved/ch2 (the next chapter)", got)
	}
}

// Finished, with no next chapter: continue at the same (finished) one — there
// is nothing else to offer.
func TestComputeContinueFinishedWithNoNextStaysPut(t *testing.T) {
	ch1 := chapterRec("ch1", 1, t0)
	ch1.Position = 1
	ch1.LastReadAt = t0.Add(time.Hour)

	got := computeContinue([]shelf.Record{ch1}, "")
	if got.Kind != continueKindSaved || got.ChapterID != "ch1" {
		t.Errorf("got %+v, want saved/ch1", got)
	}
}

// A finished book (fraction-based) also advances to its next chapter.
func TestComputeContinueFinishedBookGoesToTheNextChapter(t *testing.T) {
	book := shelf.Record{
		Key:  shelf.Key{Source: "src", Series: "series", Chapter: "vol1"},
		Kind: shelf.KindBook, Number: 1, SavedAt: t0,
		PositionFraction: 0.99, LastReadAt: t0.Add(time.Hour),
	}
	next := chapterRec("vol2", 2, t0)

	got := computeContinue([]shelf.Record{book, next}, "")
	if got.Kind != continueKindSaved || got.ChapterID != "vol2" {
		t.Errorf("got %+v, want saved/vol2", got)
	}
}

// The most recently read record wins, even if an earlier-numbered chapter was
// read more recently than a later one — "most recent" is about LastReadAt,
// not about reading order.
func TestComputeContinuePicksTheMostRecentlyRead(t *testing.T) {
	ch1 := chapterRec("ch1", 1, t0)
	ch1.Position = 1
	ch1.LastReadAt = t0.Add(2 * time.Hour) // read most recently
	ch2 := chapterRec("ch2", 2, t0)
	ch2.Position = 0
	ch2.LastReadAt = t0.Add(time.Hour)

	got := computeContinue([]shelf.Record{ch1, ch2}, "")
	// ch1 is the most recently read and is finished, with ch2 as its next.
	if got.Kind != continueKindSaved || got.ChapterID != "ch2" {
		t.Errorf("got %+v, want saved/ch2", got)
	}
}

// Ordering ties: same Number, break by SavedAt, then by chapter id.
func TestContinueOrderBreaksTiesBySavedAtThenChapterID(t *testing.T) {
	a := chapterRec("b", 1, t0.Add(time.Hour))
	b := chapterRec("a", 1, t0)
	c := chapterRec("c", 1, t0) // same Number and SavedAt as b: chapter id breaks it

	ordered := continueOrder([]shelf.Record{a, b, c})
	got := []string{ordered[0].Chapter, ordered[1].Chapter, ordered[2].Chapter}
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A saved record with a library document also available: the saved rule
// wins outright — the library uuid is only consulted when there are no
// saved records at all.
func TestComputeContinuePrefersSavedOverLibrary(t *testing.T) {
	ch1 := chapterRec("ch1", 1, t0)
	got := computeContinue([]shelf.Record{ch1}, "doc-1")
	if got.Kind != continueKindSaved || got.ChapterID != "ch1" {
		t.Errorf("got %+v, want saved/ch1 even though a library document exists", got)
	}
}
