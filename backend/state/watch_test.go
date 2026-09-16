package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/state"
)

var watchNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

// chapterIDs are deliberately not a tidy run of "chapter-1".."chapter-5".
//
// Every field bug this project has had was green in CI because the fixtures
// were uniform: a real source mixes half-chapters, specials with no number at
// all, percent-encoded titles, a group's name in the path, and the occasional
// ID that differs from its neighbour only in the last character. If the set
// logic has an edge, it is here.
var chapterIDs = []string{
	"/manga/the-lantern-keeper/chapter-1/",
	"/manga/the-lantern-keeper/chapter-1-5/",
	"/manga/the-lantern-keeper/chapter-2/",
	"/manga/the-lantern-keeper/chapter-2/?group=second-wind",
	"/manga/the-lantern-keeper/omake-the-forty-second-lamp/",
	"/manga/the-lantern-keeper/ch%C3%A2pter-3/",
	"/manga/the-lantern-keeper/CHAPTER-3/",
}

func newWatchStore(t *testing.T) (*state.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(source("Example Reader", "https://example.invalid")); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// The heart of it: "new" is a set difference, and the cases that a
// count-went-up check gets wrong are the ones that actually happen.
func TestNewChaptersAgainstARewrittenHistory(t *testing.T) {
	seen := chapterIDs

	tests := []struct {
		name string
		now  []string
		want int
	}{
		{
			name: "nothing changed",
			now:  seen,
			want: 0,
		},
		{
			name: "two appended",
			now:  append(append([]string(nil), seen...), "/manga/the-lantern-keeper/chapter-4/", "/manga/the-lantern-keeper/chapter-5/"),
			want: 2,
		},
		{
			name: "the site reordered its list",
			now:  []string{seen[6], seen[0], seen[4], seen[2], seen[1], seen[5], seen[3]},
			want: 0,
		},
		{
			name: "three removed for a takedown",
			now:  seen[:4],
			want: 0,
		},
		// The case a "the count went up" check gets silently wrong: the total
		// is unchanged, and two chapters nobody has read have appeared.
		{
			name: "two removed and two added, so the count is identical",
			now: append(append([]string(nil), seen[:5]...),
				"/manga/the-lantern-keeper/chapter-4/", "/manga/the-lantern-keeper/chapter-5/"),
			want: 2,
		},
		// The case it gets wrong in the other direction: a source relabels its
		// chapters wholesale — "12" becomes "11.5" — without publishing
		// anything. The IDs are site-relative paths and do not move, so this is
		// correctly nothing.
		{
			name: "renumbered, same URLs",
			now:  seen,
			want: 0,
		},
		{
			name: "a merge shrinks the list below the seen count",
			now:  []string{seen[0], seen[2], "/manga/the-lantern-keeper/chapters-3-4/"},
			want: 1,
		},
		{
			name: "the same chapter listed twice is one chapter",
			now: append(append([]string(nil), seen...),
				"/manga/the-lantern-keeper/chapter-4/", "/manga/the-lantern-keeper/chapter-4/"),
			want: 1,
		},
		{
			name: "blank IDs are not chapters",
			now:  append(append([]string(nil), seen...), "", "   "),
			want: 0,
		},
		{
			name: "case matters, because a URL is not case-folded",
			now:  append(append([]string(nil), seen...), "/manga/the-lantern-keeper/Chapter-1/"),
			want: 1,
		},
		{
			name: "an empty list is not a hundred removals, and not new",
			now:  nil,
			want: 0,
		},
	}

	digests := make([]string, 0, len(seen))
	for _, id := range seen {
		digests = append(digests, state.ChapterDigest(id))
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := state.NewChapters(digests, tc.now); got != tc.want {
				t.Errorf("NewChapters = %d, want %d", got, tc.want)
			}
		})
	}
}

// The plain case, end to end through the store: a check finds new chapters,
// and looking at the series clears them.
func TestACheckFindsNewChaptersAndSeeingTheSeriesClearsThem(t *testing.T) {
	s, _ := newWatchStore(t)

	if _, err := s.Watch("example-reader", "/manga/the-lantern-keeper/", "The Lantern Keeper", chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}

	// Nothing has happened at the source yet.
	n, err := s.RecordCheck("example-reader", "/manga/the-lantern-keeper/", chapterIDs, watchNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("an unchanged series reported %d new chapters", n)
	}

	grown := append(append([]string(nil), chapterIDs...),
		"/manga/the-lantern-keeper/chapter-4/",
		"/manga/the-lantern-keeper/chapter-5/",
		"/manga/the-lantern-keeper/chapter-6/")
	n, err = s.RecordCheck("example-reader", "/manga/the-lantern-keeper/", grown, watchNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("new = %d, want 3", n)
	}
	if got := s.Watches()[0].NewCount; got != 3 {
		t.Errorf("stored NewCount = %d, want 3", got)
	}

	// The user opens the series. That, and only that, clears it.
	if err := s.MarkSeen("example-reader", "/manga/the-lantern-keeper/", grown, watchNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	w := s.Watches()[0]
	if w.NewCount != 0 {
		t.Errorf("NewCount after looking = %d, want 0", w.NewCount)
	}
	if w.SeenAt.IsZero() {
		t.Error("SeenAt was not recorded")
	}
	// And a check straight afterwards agrees, rather than re-announcing.
	if n, err := s.RecordCheck("example-reader", "/manga/the-lantern-keeper/", grown, watchNow.Add(4*time.Hour)); err != nil || n != 0 {
		t.Errorf("after looking: new = %d, err = %v; want 0", n, err)
	}
}

// A series watched with no baseline must not announce its back catalogue: the
// first check seeds instead. This is the fixture nobody writes — a 400-chapter
// series watched from a search result, where the user never opened the detail.
func TestTheFirstCheckSeedsTheBaselineInsteadOfAnnouncingEverything(t *testing.T) {
	s, _ := newWatchStore(t)

	big := make([]string, 0, 400)
	for i := range 400 {
		big = append(big, "/manga/long-running/chapter-"+itoa(i)+"/")
	}

	if _, err := s.Watch("example-reader", "/manga/long-running/", "Long Running", nil, watchNow); err != nil {
		t.Fatal(err)
	}
	n, err := s.RecordCheck("example-reader", "/manga/long-running/", big, watchNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the first check announced %d new chapters, want 0", n)
	}

	// The next one is real.
	n, err = s.RecordCheck("example-reader", "/manga/long-running/", append(big, "/manga/long-running/chapter-400/"), watchNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("new = %d, want 1", n)
	}
}

// PLAN §12.2: failure is not "new", and it is not zero either.
func TestAFailedCheckKeepsTheCountAndSaysWhatWentWrong(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.Watch("example-reader", "/manga/the-lantern-keeper/", "The Lantern Keeper", chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCheck("example-reader", "/manga/the-lantern-keeper/",
		append(append([]string(nil), chapterIDs...), "/manga/the-lantern-keeper/chapter-4/"),
		watchNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordCheckFailure("example-reader", "/manga/the-lantern-keeper/",
		"The site asked for a browser challenge.", watchNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	w := s.Watches()[0]
	if w.Failure == "" {
		t.Error("a failed check left no reason on the series")
	}
	if w.NewCount != 1 {
		t.Errorf("NewCount = %d after a failure, want the 1 it legitimately had", w.NewCount)
	}
	if w.CheckedAt.IsZero() {
		t.Error("a failed check is still a completed check and must set CheckedAt")
	}

	// A failure with nothing to say still says something.
	if err := s.RecordCheckFailure("example-reader", "/manga/the-lantern-keeper/", "  ", watchNow); err != nil {
		t.Fatal(err)
	}
	if got := s.Watches()[0].Failure; strings.TrimSpace(got) == "" {
		t.Error("an empty reason must still leave a sentence")
	}

	// And a later success clears it.
	if _, err := s.RecordCheck("example-reader", "/manga/the-lantern-keeper/", chapterIDs, watchNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := s.Watches()[0].Failure; got != "" {
		t.Errorf("failure %q survived a successful check", got)
	}
}

// PLAN §12.2: a watched series whose source is removed is dropped with it.
func TestRemovingASourceDropsItsWatches(t *testing.T) {
	s, dir := newWatchStore(t)
	if _, err := s.Add(source("Other Reader", "https://other.invalid")); err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct{ src, series, title string }{
		{"example-reader", "/manga/a/", "A"},
		{"example-reader", "/manga/b/", "B"},
		{"other-reader", "/manga/c/", "C"},
	} {
		if _, err := s.Watch(w.src, w.series, w.title, chapterIDs, watchNow); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Remove("example-reader"); err != nil {
		t.Fatal(err)
	}
	got := s.Watches()
	if len(got) != 1 || got[0].SourceID != "other-reader" {
		t.Fatalf("watches after removing a source = %+v, want only other-reader's", got)
	}

	// And it stayed dropped: an orphan that only exists in memory would come
	// back on the next launch.
	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Watches(); len(got) != 1 || got[0].SourceID != "other-reader" {
		t.Fatalf("after reload: %+v", got)
	}
}

// A watch on a source that is not there is refused rather than written.
func TestWatchingAnUnknownSourceIsRefused(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.Watch("no-such-source", "/manga/a/", "A", nil, watchNow); err == nil {
		t.Fatal("a watch on a missing source must be refused")
	}
	if got := s.Watches(); len(got) != 0 {
		t.Fatalf("it was written anyway: %+v", got)
	}
}

// "New" has to survive a restart, or the indicator is a lie by the next
// morning — this backend is killed for memory routinely (PLAN §6 M7).
func TestNewSurvivesARestart(t *testing.T) {
	s, dir := newWatchStore(t)
	if _, err := s.Watch("example-reader", "/manga/the-lantern-keeper/", "The Lantern Keeper", chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	grown := append(append([]string(nil), chapterIDs...),
		"/manga/the-lantern-keeper/chapter-4/", "/manga/the-lantern-keeper/chapter-5/")
	if _, err := s.RecordCheck("example-reader", "/manga/the-lantern-keeper/", grown, watchNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	again, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	got := again.Watches()
	if len(got) != 1 {
		t.Fatalf("watches after reload = %d", len(got))
	}
	if got[0].NewCount != 2 {
		t.Errorf("NewCount after reload = %d, want 2", got[0].NewCount)
	}
	if !got[0].CheckedAt.Equal(watchNow.Add(time.Hour)) {
		t.Errorf("CheckedAt after reload = %v", got[0].CheckedAt)
	}
	// The baseline survived too, so a check after the restart is still right.
	if n, err := again.RecordCheck("example-reader", "/manga/the-lantern-keeper/", grown, watchNow.Add(2*time.Hour)); err != nil || n != 2 {
		t.Errorf("after reload: new = %d, err = %v; want 2", n, err)
	}
}

// The cooldown clock is read from the store, so it survives a restart too — on
// this device a restart happens about as often as the app is opened, and an
// in-memory cooldown would therefore never apply.
func TestLastCheckedSourceIsTheNewestAcrossTheSourcesWatches(t *testing.T) {
	s, _ := newWatchStore(t)
	for _, series := range []string{"/manga/a/", "/manga/b/"} {
		if _, err := s.Watch("example-reader", series, series, chapterIDs, watchNow); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.LastCheckedSource("example-reader"); !got.IsZero() {
		t.Errorf("an unchecked source reports %v, want the zero time", got)
	}
	if _, err := s.RecordCheck("example-reader", "/manga/a/", chapterIDs, watchNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCheckFailure("example-reader", "/manga/b/", "no", watchNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := s.LastCheckedSource("example-reader"); !got.Equal(watchNow.Add(2 * time.Hour)) {
		t.Errorf("LastCheckedSource = %v, want the newest of the two", got)
	}
	// A failed check still counts: it cost a request, which is what the
	// cooldown is rationing.
	if got := s.LastCheckedSource("no-such-source"); !got.IsZero() {
		t.Errorf("an unknown source reports %v", got)
	}
}

// Watching twice is not an error, and a second watch with a fresh baseline
// re-seeds rather than duplicating the row.
func TestWatchingTwiceUpdatesRatherThanDuplicates(t *testing.T) {
	s, _ := newWatchStore(t)
	if _, err := s.Watch("example-reader", "/manga/a/", "A", chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Watch("example-reader", "/manga/a/", "A, Revised", nil, watchNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := s.Watches()
	if len(got) != 1 {
		t.Fatalf("watches = %d, want 1", len(got))
	}
	if got[0].Title != "A, Revised" {
		t.Errorf("title = %q", got[0].Title)
	}
	// A nil baseline means "nothing to offer", not "forget what you knew".
	if got[0].SeenAt.IsZero() {
		t.Error("the existing baseline was thrown away by a watch with nothing to seed from")
	}

	if err := s.Unwatch("example-reader", "/manga/a/"); err != nil {
		t.Fatal(err)
	}
	if got := s.Watches(); len(got) != 0 {
		t.Fatalf("unwatch left %+v", got)
	}
	// Unwatching what is not watched is what the user asked for already.
	if err := s.Unwatch("example-reader", "/manga/a/"); err != nil {
		t.Errorf("unwatching twice: %v", err)
	}
}

// MarkSeen and RecordCheckFailure on an unwatched series are no-ops: the user
// reads plenty of series they have not asked to be told about.
func TestSeeingAnUnwatchedSeriesDoesNothing(t *testing.T) {
	s, _ := newWatchStore(t)
	if err := s.MarkSeen("example-reader", "/manga/not-watched/", chapterIDs, watchNow); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCheckFailure("example-reader", "/manga/not-watched/", "boom", watchNow); err != nil {
		t.Fatal(err)
	}
	if got := s.Watches(); len(got) != 0 {
		t.Fatalf("a watch appeared from nowhere: %+v", got)
	}
	if _, err := s.RecordCheck("example-reader", "/manga/not-watched/", chapterIDs, watchNow); err == nil {
		t.Error("recording a check against an unwatched series must be an error")
	}
}

// Migration #2: an imported file naming a source that was not imported with it.
func TestMigrationDropsWatchesWithNoSource(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, `{
	  "version": 2,
	  "sources": [{"id":"example-reader","name":"Example Reader","lang":"en","theme":"madara",
	               "baseUrl":"https://example.invalid","addedAt":"2026-03-04T12:00:00Z",
	               "allowedHosts":[]}],
	  "watched": [
	    {"sourceId":"example-reader","seriesId":"/manga/a/","title":"A","addedAt":"2026-03-04T12:00:00Z"},
	    {"sourceId":"gone-away","seriesId":"/manga/b/","title":"B","addedAt":"2026-03-04T12:00:00Z"}
	  ]
	}`)

	s, err := state.Open(dir, migrationRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Watches()
	if len(got) != 1 || got[0].SourceID != "example-reader" {
		t.Fatalf("watches = %+v, want only the one with a source", got)
	}

	on := readStore(t, path)
	if on["version"] != float64(state.CurrentVersion) {
		t.Errorf("file version %v, want %d", on["version"], state.CurrentVersion)
	}
	if raw, ok := on["watched"].([]any); !ok || len(raw) != 1 {
		t.Errorf("on disk: watched = %v", on["watched"])
	}
	// And it is gone for good, not just filtered on the way past.
	body, err := os.ReadFile(filepath.Join(dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "gone-away") {
		t.Error("the orphaned watch is still in the file")
	}
}
