package theme

import "testing"

func TestGenreListingRoundTrips(t *testing.T) {
	l := GenreListing("slice-of-life", "  Slice of Life ")
	if l.Group != ListingGroupGenre || l.Label != "Slice of Life" {
		t.Fatalf("GenreListing = %+v", l)
	}
	id, ok := GenreID(l.ID)
	if !ok || id != "slice-of-life" {
		t.Fatalf("GenreID(%q) = %q, %v", l.ID, id, ok)
	}
	for _, notGenre := range []string{ListingPopular, "genre:", ""} {
		if _, ok := GenreID(notGenre); ok {
			t.Errorf("GenreID(%q) claimed a genre", notGenre)
		}
	}
}

func TestEveryWellKnownListingHasALabel(t *testing.T) {
	for _, id := range []string{ListingLatest, ListingPopular, ListingNew, ListingRating, ListingCompleted} {
		if ListingLabel(id) == "" {
			t.Errorf("no label for %q", id)
		}
	}
	if ListingLabel("genre:action") != "" {
		t.Error("a genre ID got a well-known label")
	}
}
