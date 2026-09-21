package service

import (
	"testing"

	"github.com/rickl/quire/backend/library"
)

// sorted is the predicate resortOnAttach uses to skip a record that never
// needs filing again. A book's finished path is one name, Books — the same
// length as an unfiled comic's, which is why the name and not just the length
// has to be checked (PLAN's Books design, 2026-09-21).

// A filed book is sorted: nothing under Books to add, and resortOnAttach must
// never treat it as a straggler still waiting to be filed.
func TestSortedRecognisesAFiledBook(t *testing.T) {
	rec := library.Record{FolderUUID: "books", FolderPath: []string{library.BooksFolder}}
	if !sorted(rec) {
		t.Error("a book filed into Books was not recognised as sorted")
	}
}

// An unfiled comic — one name, Comics itself — must not be mistaken for a
// filed book just because both paths are one element long.
func TestSortedDoesNotConfuseAnUnfiledComicWithAFiledBook(t *testing.T) {
	rec := library.Record{FolderUUID: "comics", FolderPath: []string{library.ComicsFolder}}
	if sorted(rec) {
		t.Error("an unfiled comic, sitting in Comics itself, was read as sorted")
	}
}

// A comic in its series folder is sorted, exactly as before this existed.
func TestSortedRecognisesAFiledComic(t *testing.T) {
	rec := library.Record{FolderUUID: "lantern", FolderPath: []string{library.ComicsFolder, "The Lantern Keeper"}}
	if !sorted(rec) {
		t.Error("a comic filed into its series folder was not recognised as sorted")
	}
}

// A book record with no folder recorded yet is not sorted — there is nothing
// to skip it for.
func TestSortedIsFalseWithNoFolderRecorded(t *testing.T) {
	rec := library.Record{FolderPath: []string{library.BooksFolder}}
	if sorted(rec) {
		t.Error("a record with no folder id recorded was read as sorted")
	}
}

// seriesFolderOf must never name the Books folder itself: a book has no
// per-title subfolder to ever be found empty, so the empty-folder sweep must
// have nothing to check Books against.
func TestSeriesFolderOfIgnoresABook(t *testing.T) {
	rec := library.Record{FolderUUID: "books", FolderPath: []string{library.BooksFolder}}
	if got := seriesFolderOf(rec); got != "" {
		t.Errorf("seriesFolderOf(a filed book) = %q, want \"\" — Books must never be swept", got)
	}
}

// The comic case is unchanged: a series folder is still findable this way.
func TestSeriesFolderOfFindsAComicsSeriesFolder(t *testing.T) {
	rec := library.Record{FolderUUID: "lantern", FolderPath: []string{library.ComicsFolder, "The Lantern Keeper"}}
	if got := seriesFolderOf(rec); got != "lantern" {
		t.Errorf("seriesFolderOf = %q, want lantern", got)
	}
}
