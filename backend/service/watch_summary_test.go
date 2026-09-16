package service

import "testing"

// The counts are arithmetic; the wording is where this breaks. Every case here
// is one a user can reach in an evening.
func TestSummariseWording(t *testing.T) {
	newRow := func(n int) watchView {
		return watchView{State: watchStateNew, NewChapters: n, Badge: plural(n)}
	}
	okRow := func() watchView { return watchView{State: watchStateOK} }
	failedRow := func(n int) watchView {
		return watchView{State: watchStateFailed, NewChapters: n, Badge: plural(n)}
	}

	tests := []struct {
		name   string
		rows   []watchView
		want   watchSummary
		reason string
	}{
		{
			name:   "nothing is watched",
			rows:   nil,
			want:   watchSummary{},
			reason: "a user who has never watched anything is shown nothing",
		},
		{
			name:   "watched, and nothing has happened",
			rows:   []watchView{okRow(), okRow(), okRow()},
			want:   watchSummary{},
			reason: "silence, not a cheerful zero",
		},
		{
			name: "exactly one series, exactly one chapter",
			rows: []watchView{newRow(1), okRow()},
			want: watchSummary{
				SeriesWithNew: 1, NewChapters: 1,
				Short: "1 new", Phrase: "1 series has new chapters",
			},
			reason: "singular verb; 'series' is its own plural",
		},
		{
			name: "one series, several chapters",
			rows: []watchView{newRow(7), okRow()},
			want: watchSummary{
				SeriesWithNew: 1, NewChapters: 7,
				Short: "1 new", Phrase: "1 series has new chapters",
			},
			reason: "the short label counts series, not chapters",
		},
		{
			name: "several series",
			rows: []watchView{newRow(3), newRow(1), newRow(7), okRow()},
			want: watchSummary{
				SeriesWithNew: 3, NewChapters: 11,
				Short: "3 new", Phrase: "3 series have new chapters",
			},
		},
		{
			name: "one check failed and nothing is new",
			rows: []watchView{okRow(), failedRow(0), okRow()},
			want: watchSummary{
				Failed: 1,
				Phrase: "1 series couldn’t be checked",
			},
			reason: "the entry point stays quiet about a blip; the screen says it plainly",
		},
		{
			name: "everything failed",
			rows: []watchView{failedRow(0), failedRow(0), failedRow(0)},
			want: watchSummary{
				Failed: 3,
				Phrase: "3 series couldn’t be checked",
			},
			reason: "still no entry-point badge: nothing is known, and '' claims nothing",
		},
		{
			name: "some new, one failed",
			rows: []watchView{newRow(2), newRow(1), failedRow(0), okRow()},
			want: watchSummary{
				SeriesWithNew: 2, NewChapters: 3, Failed: 1,
				Short: "2 new", Phrase: "2 series have new chapters, and 1 couldn’t be checked",
			},
			reason: "the failure is said once, without repeating the word 'series'",
		},
		{
			name: "a failed check that still carries what an earlier one found",
			rows: []watchView{failedRow(3), okRow()},
			want: watchSummary{
				SeriesWithNew: 1, NewChapters: 3, Failed: 1,
				Short: "1 new", Phrase: "1 series has new chapters, and 1 couldn’t be checked",
			},
			reason: "the row still shows a badge, so the summary must agree with it",
		},
		{
			name: "a round is in progress",
			rows: []watchView{{State: watchStateChecking, NewChapters: 3}, newRow(1), okRow()},
			want: watchSummary{
				SeriesWithNew: 1, NewChapters: 1,
				Short: "1 new", Phrase: "1 series has new chapters",
			},
			reason: "an in-flight row must not make the count tick up and back down",
		},
		{
			name:   "watched but never checked",
			rows:   []watchView{{State: watchStateUnchecked}, {State: watchStateUnchecked}},
			want:   watchSummary{},
			reason: "nothing is known yet, and nothing is claimed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summarise(tc.rows)
			if got != tc.want {
				t.Errorf("summarise() = %+v\nwant %+v\n(%s)", got, tc.want, tc.reason)
			}
			// The two nothing-to-report rules, asserted on every case rather
			// than only where they bite.
			if got.SeriesWithNew == 0 && got.Short != "" {
				t.Errorf("short = %q with nothing new", got.Short)
			}
			if got.SeriesWithNew == 0 && got.Failed == 0 && got.Phrase != "" {
				t.Errorf("phrase = %q with nothing to report", got.Phrase)
			}
		})
	}
}
