package theme

import (
	"sort"
	"strings"
)

// Chapter ordering.
//
// # The contract
//
// PLAN §7.2: **Chapters returns ascending reading order — earliest chapter
// first.** Every theme normalises to it before returning.
//
// # Why this is not left to the caller
//
// It was, until 2026-09-15, and M5 assembled a volume titled "The Lantern
// Keeper — 4–1". madara's markup lists chapters newest-first; MangaDex's feed
// sorts oldest-first; nothing said which was right. §6 M4 groups a run of
// chapters into one volume PDF, so a descending source produces a PDF whose
// pages run backwards — inside a file whose reading position xochitl then
// owns. Silent, and ruinous.
//
// The fix belongs in the theme rather than in a sort at the call site, because
// only the theme knows what its identifiers mean. A generic sort is how this
// goes wrong a second time: "10" sorts before "9" as a string, "12.5" is not
// an integer, and "Extra", "Omake" and "Side Story" are not numbers at all.
//
// # How SortAscending decides
//
// Direction first, sorting second, and that order matters.
//
// Sites list chapters in a consistent direction; what they do *not* do is
// interleave unnumbered extras predictably. Given "5, 4, Extra, 3, 2, 1", a
// sort by number puts Extra at one end, where it belongs to nobody. Detecting
// that the numbered chapters run downwards and reversing the whole list keeps
// Extra between 3 and 4, which is where the site put it and where the reader
// expects it.
//
// So:
//
//  1. No chapter carries a number at all → nothing is known. Source order is
//     kept and ordered=false.
//  2. The numbered chapters already ascend → keep the list as it is.
//  3. The numbered chapters descend → reverse the whole list, unnumbered
//     entries included, preserving their neighbours.
//  4. Neither, and every chapter is numbered → the site's order means nothing,
//     so sort by number. Stable, so equal numbers keep their relative order.
//  5. Neither, and some chapters are unnumbered → the unnumbered ones cannot
//     be placed without guessing. Source order is kept and ordered=false.
//
// Cases 1 and 5 are the honest failures. A wrong order is worse than an
// admitted one, so they are surfaced rather than papered over: see
// MarkOrderUnknown and Chapter.OrderUnknown.

// SortAscending normalises chs to ascending reading order in place and reports
// whether it could establish one.
//
// A false return does not mean the slice is unusable — it means the slice is
// in the *site's* order and Quire does not know what that order is. The caller
// decides what to do about that; what it must not do is assume.
func SortAscending(chs []Chapter) (ordered bool) {
	if len(chs) < 2 {
		return true
	}

	// The indices of the chapters that carry a number, in source order.
	var numbered []int
	for i := range chs {
		if chs[i].Number >= 0 {
			numbered = append(numbered, i)
		}
	}
	if len(numbered) == 0 {
		return false
	}
	if len(numbered) == 1 {
		// One number establishes no direction. If it is the only chapter that
		// matters — a one-chapter list padded with extras — there is nothing
		// to get wrong either way, so keep source order and say so.
		return len(chs) == 1
	}

	ascending, descending := true, true
	for k := 1; k < len(numbered); k++ {
		prev, cur := chs[numbered[k-1]], chs[numbered[k]]
		switch {
		case less(cur, prev):
			ascending = false
		case less(prev, cur):
			descending = false
		}
	}

	switch {
	case ascending:
		return true
	case descending:
		reverse(chs)
		return true
	case len(numbered) == len(chs):
		// Unordered, but everything is numbered, so a sort is safe.
		sort.SliceStable(chs, func(i, j int) bool { return less(chs[i], chs[j]) })
		return true
	default:
		return false
	}
}

// less is the reading-order comparison, and the only one.
//
// Chapter number is primary. Volume is a *tie-break*, not a prefix, which is
// deliberate: on every source seen so far chapter numbers run continuously
// across volumes, so keying on volume first would reorder a correct list the
// moment one chapter's volume label was missing or wrong. Volume only speaks
// up when two chapters claim the same number, which is what happens when a
// series is re-released in volumes and the numbering restarts.
func less(a, b Chapter) bool {
	if a.Number != b.Number {
		return a.Number < b.Number
	}
	av, bv := volumeNumber(a.Volume), volumeNumber(b.Volume)
	if av != bv && av >= 0 && bv >= 0 {
		return av < bv
	}
	return false
}

func reverse(chs []Chapter) {
	for i, j := 0, len(chs)-1; i < j; i, j = i+1, j-1 {
		chs[i], chs[j] = chs[j], chs[i]
	}
}

// volumeNumber parses a volume label, or returns -1. Labels are as varied as
// chapter titles ("3", "Vol. 3", "Volume 3", "TBD"), so it reuses the same
// number extraction rather than inventing a second one.
func volumeNumber(v string) float64 {
	if strings.TrimSpace(v) == "" {
		return -1
	}
	return ChapterNumber(v)
}

// MarkOrderUnknown sets OrderUnknown on every chapter in the list.
//
// The flag is a property of the *list*, not of any one chapter — Quire either
// knows the reading order of a series or it does not. It lives on the element
// because Theme.Chapters returns a plain slice (PLAN §7.2), and inventing a
// wrapper type to carry one boolean would change the interface for every
// caller in order to say something they can already be told. Setting it on
// every element keeps the claim true whichever chapter a caller happens to
// look at, and OrderIsKnown reads it back at the list level.
func MarkOrderUnknown(chs []Chapter) {
	for i := range chs {
		chs[i].OrderUnknown = true
	}
}

// OrderIsKnown reports whether the reading order of this list is trustworthy.
//
// A caller assembling several chapters into one volume PDF (PLAN §6 M4) should
// consult it: producing a single file whose pages run backwards is the failure
// this whole mechanism exists to prevent, and an empty or single-chapter list
// has no order to get wrong.
func OrderIsKnown(chs []Chapter) bool {
	for i := range chs {
		if chs[i].OrderUnknown {
			return false
		}
	}
	return true
}

// SortAndMark is what a theme calls: it normalises the order and records the
// answer on the chapters themselves, so a theme cannot accidentally sort
// without reporting, or report without sorting.
func SortAndMark(chs []Chapter) []Chapter {
	if !SortAscending(chs) {
		MarkOrderUnknown(chs)
	}
	return chs
}
