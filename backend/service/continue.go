// Continue reading (PLAN §12.9): working out, for one (source, series) row on
// the Downloaded overview or the Watching list, what tapping it should open —
// without ever going to the network. A tap on either screen used to open the
// series' own results list, which fetches from the web first and is slow;
// this is what lets it jump straight to the next thing to read instead.
package service

import (
	"sort"

	"github.com/rickl/quire/backend/shelf"
)

// The three kinds a continueTarget can be. continueKindNone ("") means there
// is nothing to read yet — the row falls back to browsing the series, the
// same as it always did.
const (
	continueKindSaved   = "saved"
	continueKindLibrary = "library"
	continueKindNone    = ""
)

// continueTarget is what tapping a row should open. It rides on both the
// downloaded overview's rows and the watch list's rows as "continue".
type continueTarget struct {
	Kind         string `json:"kind"`
	ChapterID    string `json:"chapterId,omitempty"`
	DocumentUUID string `json:"documentUuid,omitempty"`
}

// computeContinue works out one series' continueTarget from its saved shelf
// records and the newest library document uuid for it — latestUUID's own
// answer, so this rule and "Read latest" never disagree about which document
// is the library's newest.
//
// The rule (PLAN §12.9):
//
//  1. If any record has been read (LastReadAt set), take the most recently
//     read one. If it is finished — see recordFinished — and there is a
//     *next* saved record in reading order, continue there instead, from
//     its own stored position (normally the start). Otherwise continue at
//     the most recently read record itself.
//  2. Else, if there are any saved records at all, continue at the first one
//     in reading order — nothing has been read yet, so start at the start.
//  3. Else, if the series has a library document, continue there — the
//     newest one, exactly what "Read latest" already opens.
//  4. Else there is nothing to read.
func computeContinue(recs []shelf.Record, latestLibraryUUID string) continueTarget {
	if len(recs) == 0 {
		if latestLibraryUUID != "" {
			return continueTarget{Kind: continueKindLibrary, DocumentUUID: latestLibraryUUID}
		}
		return continueTarget{}
	}

	ordered := continueOrder(recs)

	mostRecent := -1
	for i, r := range ordered {
		if r.LastReadAt.IsZero() {
			continue
		}
		if mostRecent < 0 || r.LastReadAt.After(ordered[mostRecent].LastReadAt) {
			mostRecent = i
		}
	}

	if mostRecent < 0 {
		// Never read: the first chapter in reading order.
		return continueTarget{Kind: continueKindSaved, ChapterID: ordered[0].Chapter}
	}

	current := ordered[mostRecent]
	if recordFinished(current) && mostRecent+1 < len(ordered) {
		return continueTarget{Kind: continueKindSaved, ChapterID: ordered[mostRecent+1].Chapter}
	}
	return continueTarget{Kind: continueKindSaved, ChapterID: current.Chapter}
}

// continueOrder is a series' saved records in reading order: by Number
// ascending, then by SavedAt, then by the chapter id itself — the order named
// in PLAN §12.9, and the order "the next saved chapter" is defined against.
func continueOrder(recs []shelf.Record) []shelf.Record {
	ordered := append([]shelf.Record(nil), recs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Number != ordered[j].Number {
			return ordered[i].Number < ordered[j].Number
		}
		if !ordered[i].SavedAt.Equal(ordered[j].SavedAt) {
			return ordered[i].SavedAt.Before(ordered[j].SavedAt)
		}
		return ordered[i].Chapter < ordered[j].Chapter
	})
	return ordered
}

// recordFinished reports whether a saved record has been read to its end.
//
// A chapter of pages is finished once its last page has been shown; a book
// is finished by the fraction read through it rather than by a page number,
// because a book's own page numbering changes with the reader's settings
// (books-contract.md §B) and a page index alone would not mean the same
// thing on two different reopenings.
func recordFinished(r shelf.Record) bool {
	if r.IsBook() {
		return r.PositionFraction >= 0.98
	}
	return r.Position >= len(r.Pages)-1
}
