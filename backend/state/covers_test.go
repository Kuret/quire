package state_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/state"
)

const lanternSeries = "/manga/the-lantern-keeper/"
const lanternCover = "https://cdn.example.invalid/covers/lantern.webp"

func newCoverStore(t *testing.T) (*state.Store, string) {
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

// The whole point of the cache: a URL learnt from a listing today is still
// there after the backend has been killed for memory and started again, which
// is what lets the Downloaded and Watching screens draw a cover for a series
// nobody has browsed to since.
func TestCoverURLSurvivesAReopen(t *testing.T) {
	s, dir := newCoverStore(t)
	if err := s.RememberCovers("example-reader", []state.CoverRef{
		{SeriesID: lanternSeries, URL: lanternCover},
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.CoverURL("example-reader", lanternSeries); got != lanternCover {
		t.Fatalf("cover URL = %q, want %q", got, lanternCover)
	}

	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.CoverURL("example-reader", lanternSeries); got != lanternCover {
		t.Errorf("after a restart the cover URL is %q, want %q", got, lanternCover)
	}
}

// A series Quire has never listed is the ordinary case for anything downloaded
// before this cache existed. It is "" and it is not an error: the row draws
// without a tile and says everything else it has to say.
func TestAnUnknownSeriesHasNoCoverURL(t *testing.T) {
	s, _ := newCoverStore(t)
	if err := s.RememberCovers("example-reader", []state.CoverRef{
		{SeriesID: lanternSeries, URL: lanternCover},
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.CoverURL("example-reader", "/manga/somebody-else/"); got != "" {
		t.Errorf("an unlisted series has cover URL %q, want none", got)
	}
	// Per source, because two sources can carry the same series id and one's
	// cover is not a picture of the other's series.
	if got := s.CoverURL("other-reader", lanternSeries); got != "" {
		t.Errorf("another source's series has cover URL %q, want none", got)
	}
}

// The cache is not part of the setup a user exports and imports (PLAN §7.2).
// A few hundred KB of somebody else's CDN URLs travelling with a dozen lines of
// sources is not an export anybody asked for.
func TestCoverURLsStayOutOfTheExportedEnvelope(t *testing.T) {
	s, dir := newCoverStore(t)
	if err := s.RememberCovers("example-reader", []state.CoverRef{
		{SeriesID: lanternSeries, URL: lanternCover},
	}); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), lanternCover) {
		t.Errorf("%s carries a cached cover URL:\n%s", state.FileName, b)
	}
	if _, err := os.Stat(filepath.Join(dir, state.CoverFileName)); err != nil {
		t.Errorf("the cache is not in its own file either: %v", err)
	}
}

// A cache that can stop the app from starting is not a cache. The file is
// dropped and re-learnt from the next listing.
func TestAnUnreadableCoverCacheDoesNotStopTheStoreOpening(t *testing.T) {
	s, dir := newCoverStore(t)
	if err := s.RememberCovers("example-reader", []state.CoverRef{
		{SeriesID: lanternSeries, URL: lanternCover},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, state.CoverFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatalf("a corrupt cover cache stopped the store opening: %v", err)
	}
	if got := reopened.CoverURL("example-reader", lanternSeries); got != "" {
		t.Errorf("cover URL = %q, want none from a cache that could not be read", got)
	}
	// And it is usable again straight away.
	if err := reopened.RememberCovers("example-reader", []state.CoverRef{
		{SeriesID: lanternSeries, URL: lanternCover},
	}); err != nil {
		t.Fatal(err)
	}
	if got := reopened.CoverURL("example-reader", lanternSeries); got != lanternCover {
		t.Errorf("cover URL = %q after re-learning it, want %q", got, lanternCover)
	}
}

// The cap is what keeps the file a small write rather than an unbounded one.
// The newest entries are the ones kept: they are the listings the user is
// actually looking at.
func TestTheCoverCacheIsBounded(t *testing.T) {
	s, dir := newCoverStore(t)

	// Twelve listings of a hundred rows, oldest first, so eviction has an order
	// to respect. 1200 is comfortably over the 1024 cap.
	const total, perListing = 1200, 100
	for i := 0; i < total; i += perListing {
		refs := make([]state.CoverRef, 0, perListing)
		for j := i; j < i+perListing; j++ {
			refs = append(refs, state.CoverRef{
				SeriesID: fmt.Sprintf("/manga/series-%04d/", j),
				URL:      fmt.Sprintf("https://cdn.example.invalid/%04d.webp", j),
			})
		}
		if err := s.RememberCovers("example-reader", refs); err != nil {
			t.Fatal(err)
		}
	}

	b, err := os.ReadFile(filepath.Join(dir, state.CoverFileName))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Covers []struct {
			SeriesID string `json:"seriesId"`
		} `json:"covers"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Covers) > 1024 {
		t.Errorf("%d entries cached, want no more than the 1024 cap", len(file.Covers))
	}
	if len(file.Covers) == 0 {
		t.Fatalf("the cache is empty; eviction is not meant to drop everything")
	}
	// The most recent listing is still there, and the oldest is not.
	if got := s.CoverURL("example-reader", fmt.Sprintf("/manga/series-%04d/", total-1)); got == "" {
		t.Error("the newest entry was evicted")
	}
	if got := s.CoverURL("example-reader", "/manga/series-0000/"); got != "" {
		t.Errorf("the oldest entry survived a full cache: %q", got)
	}
}

// Removing a source forgets its covers — see dropCoversFor. The cap would have
// evicted them eventually, and "eventually" is not what removing a source means.
func TestRemovingASourceForgetsItsCovers(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.Add(source("Kept", "https://kept.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.Add(source("Gone", "https://gone.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RememberCovers(kept.ID, []state.CoverRef{{SeriesID: "a", URL: "https://kept.invalid/a.jpg"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberCovers(gone.ID, []state.CoverRef{{SeriesID: "b", URL: "https://gone.invalid/b.jpg"}}); err != nil {
		t.Fatal(err)
	}

	if err := s.Remove(gone.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.CoverURL(gone.ID, "b"); got != "" {
		t.Errorf("the removed source's cover survived: %q", got)
	}
	if got := s.CoverURL(kept.ID, "a"); got != "https://kept.invalid/a.jpg" {
		t.Errorf("removing one source took another's cover: %q", got)
	}

	// And it stayed forgotten: the point is the file, not just the map.
	reopened, err := state.Open(dir, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.CoverURL(gone.ID, "b"); got != "" {
		t.Errorf("the removed source's cover came back from disk: %q", got)
	}
	if got := reopened.CoverURL(kept.ID, "a"); got != "https://kept.invalid/a.jpg" {
		t.Errorf("the kept source's cover did not survive the reopen: %q", got)
	}
}
