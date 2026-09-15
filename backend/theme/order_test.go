package theme_test

import (
	"strings"
	"testing"

	"github.com/rickl/quire/backend/theme"
)

// PLAN §7.2, decided 2026-09-15: Chapters returns ascending reading order.
//
// The bug this replaced was silent. madara lists chapters newest-first,
// MangaDex oldest-first, and nothing said which was right; §6 M4 groups a run
// of chapters into one volume PDF, so a descending source produced a PDF that
// read backwards inside a file whose reading position xochitl then owns. M5
// surfaced it as a volume titled "The Lantern Keeper — 4–1".
//
// These tests are over theme.SortAscending directly. The per-theme tests pin
// the same contract against each theme's own fixtures; this pins the decision
// procedure, including the cases no current fixture happens to contain.

func chapters(spec string) []theme.Chapter {
	var out []theme.Chapter
	for _, f := range strings.Fields(spec) {
		ch := theme.Chapter{ID: "/c/" + f, Title: f, Number: theme.ChapterNumber(f)}
		out = append(out, ch)
	}
	return out
}

func ids(chs []theme.Chapter) string {
	var parts []string
	for _, c := range chs {
		parts = append(parts, c.Title)
	}
	return strings.Join(parts, " ")
}

func TestSortAscending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		in          string
		want        string
		wantOrdered bool
		why         string
	}{
		{
			name: "already ascending", in: "1 2 3", want: "1 2 3", wantOrdered: true,
			why: "MangaDex's feed sorts server-side; normalising must not disturb it",
		},
		{
			name: "descending is reversed", in: "4 3 2 1", want: "1 2 3 4", wantOrdered: true,
			why: "madara and mangathemesia both render newest-first",
		},
		{
			name: "decimal chapters sort as numbers", in: "4 3.5 3 1", want: "1 3 3.5 4", wantOrdered: true,
			why: "half-chapters are routine and are not integers",
		},
		{
			name: "ten sorts after nine", in: "11 10 9", want: "9 10 11", wantOrdered: true,
			why: "the string comparison a generic caller would reach for puts 10 before 9",
		},
		{
			name: "single chapter", in: "1", want: "1", wantOrdered: true,
			why: "one chapter has no order to get wrong",
		},
		{
			name: "unnumbered extras keep their neighbours", in: "5 4 Extra 3 2 1", want: "1 2 3 Extra 4 5", wantOrdered: true,
			why: "direction is detected from the numbered chapters and the whole list " +
				"reversed, so Extra stays between 3 and 4 where the site put it — " +
				"sorting by number would fling it to one end, belonging to nobody",
		},
		{
			name: "an unnumbered extra in an ascending list", in: "1 2 Omake 3", want: "1 2 Omake 3", wantOrdered: true,
			why: "already ascending: nothing to do, and nothing to guess",
		},
		{
			name: "unordered but fully numbered", in: "3 1 4 2", want: "1 2 3 4", wantOrdered: true,
			why: "the site's order means nothing, but every chapter is numbered, so a sort is safe",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chs := chapters(tc.in)
			ordered := theme.SortAscending(chs)
			if got := ids(chs); got != tc.want {
				t.Errorf("SortAscending(%q) = %q, want %q\n%s", tc.in, got, tc.want, tc.why)
			}
			if ordered != tc.wantOrdered {
				t.Errorf("ordered = %v, want %v\n%s", ordered, tc.wantOrdered, tc.why)
			}
		})
	}
}

// The honest failures. A wrong order is worse than an admitted one, so these
// keep the site's order and say they do not know what it means.
func TestSortAscendingAdmitsWhatItCannotOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		why  string
	}{
		{
			name: "nothing is numbered", in: "Prologue Extra Omake Side-Story",
			why: "no numbers means no direction; there is nothing to detect",
		},
		{
			name: "unordered, and not everything is numbered", in: "3 Extra 1 4 2",
			why: "the numbered chapters establish no direction, so the unnumbered ones " +
				"cannot be placed without guessing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chs := chapters(tc.in)
			before := ids(chs)
			if theme.SortAscending(chs) {
				t.Fatalf("SortAscending(%q) claimed an order it cannot have\n%s", tc.in, tc.why)
			}
			if got := ids(chs); got != before {
				t.Errorf("source order was disturbed: %q -> %q; an unknown order must be "+
					"left as the site gave it", before, got)
			}
		})
	}
}

// SortAndMark is what themes call, and the flag is how the failure reaches a
// caller. A theme cannot sort without reporting or report without sorting.
func TestSortAndMarkFlagsAnUnknownOrder(t *testing.T) {
	t.Parallel()

	t.Run("orderable", func(t *testing.T) {
		chs := theme.SortAndMark(chapters("4 3 2 1"))
		if !theme.OrderIsKnown(chs) {
			t.Error("a list that was ordered is flagged unknown")
		}
		for _, c := range chs {
			if c.OrderUnknown {
				t.Errorf("%s carries OrderUnknown", c.Title)
			}
		}
	})

	t.Run("not orderable", func(t *testing.T) {
		chs := theme.SortAndMark(chapters("Prologue Extra Omake"))
		if theme.OrderIsKnown(chs) {
			t.Fatal("an unorderable list reported a known order; PLAN §6 M4 would assemble " +
				"a volume PDF from it and nothing downstream would notice")
		}
		// The flag describes the list, so every element carries it — a caller
		// looking at any one chapter gets the same true answer.
		for _, c := range chs {
			if !c.OrderUnknown {
				t.Errorf("%s does not carry OrderUnknown", c.Title)
			}
		}
	})

	t.Run("empty and single lists", func(t *testing.T) {
		if !theme.OrderIsKnown(theme.SortAndMark(nil)) {
			t.Error("an empty list has no order to be unsure about")
		}
		if !theme.OrderIsKnown(theme.SortAndMark(chapters("1"))) {
			t.Error("a single-chapter list has no order to be unsure about")
		}
	})
}

// Volume is a tie-break, not a prefix. Chapter numbers run continuously across
// volumes on every source seen so far, so keying on volume first would reorder
// a correct list the moment one label was missing.
func TestVolumeIsOnlyATieBreak(t *testing.T) {
	t.Parallel()

	t.Run("a missing volume label does not move a chapter", func(t *testing.T) {
		chs := []theme.Chapter{
			{Title: "3", Number: 3, Volume: "2"},
			{Title: "2", Number: 2, Volume: ""},
			{Title: "1", Number: 1, Volume: "1"},
		}
		if !theme.SortAscending(chs) {
			t.Fatal("not ordered")
		}
		if got := ids(chs); got != "1 2 3" {
			t.Errorf("got %q, want \"1 2 3\"; the unlabelled chapter was moved", got)
		}
	})

	t.Run("volume separates a restarted numbering", func(t *testing.T) {
		// What a re-release in volumes looks like: chapter 1 twice.
		chs := []theme.Chapter{
			{Title: "v2c1", Number: 1, Volume: "2"},
			{Title: "v1c1", Number: 1, Volume: "1"},
		}
		if !theme.SortAscending(chs) {
			t.Fatal("not ordered")
		}
		if got := ids(chs); got != "v1c1 v2c1" {
			t.Errorf("got %q, want \"v1c1 v2c1\"", got)
		}
	})
}
