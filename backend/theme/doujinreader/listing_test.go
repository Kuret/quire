package doujinreader_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/doujinreader"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Ranked by the count published next to each tag, descending, capped at 30
// (maxGenreListings): "tag-00" (count 32) first, "tag-29" (count 3) last,
// "tag-30" and "tag-31" excluded. The duplicate "tag-05" entry the fixture
// carries a second time (different label, higher count) must not appear
// twice or displace the label first seen for it.
func TestListingsRanksGenresByPublishedCountAndCaps(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tags/": {File: "tags.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Fatalf("got %d listings, want the cap of 30: %+v", len(got), got)
	}
	for _, l := range got {
		if l.Group != theme.ListingGroupGenre {
			t.Errorf("listing %+v has group %q, want every entry to be a genre", l, l.Group)
		}
	}
	if want := theme.GenreListing("tag-00", "Placeholder Tag 00"); got[0] != want {
		t.Errorf("first listing = %+v, want the highest-count tag %+v", got[0], want)
	}
	if want := theme.GenreListing("tag-29", "Placeholder Tag 29"); got[29] != want {
		t.Errorf("last listing = %+v, want the 30th-ranked tag %+v", got[29], want)
	}
	for _, l := range got {
		if id, _ := theme.GenreID(l.ID); id == "tag-30" || id == "tag-31" {
			t.Errorf("listing %+v: a tag past the cap was offered", l)
		}
	}
	seen := map[string]int{}
	for _, l := range got {
		seen[l.ID]++
	}
	if n := seen[theme.GenreListing("tag-05", "").ID]; n != 1 {
		t.Errorf("tag-05 appears %d times, want exactly 1", n)
	}
}

// No sort or status group exists on this family (see listing.go's package
// comment): Listings must never offer one.
func TestListingsOffersNoSortOrStatusGroup(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tags/": {File: "tags.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got {
		if l.Group == theme.ListingGroupSort || l.Group == theme.ListingGroupStatus {
			t.Errorf("listing %+v: this family has no site-wide sort or status listing to offer", l)
		}
	}
}

func TestListingLatestDelegatesToEmptyQuerySearch(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /": {File: "home.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), theme.ListingLatest, 1)
	if err != nil {
		t.Fatal(err)
	}
	want, err := th.Search(context.Background(), site(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List(ListingLatest) = %+v, want the same as Search(\"\") = %+v", got, want)
	}
}

func TestListingGenreReadsTheTagListingFirstPage(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tag/sample-tag/": {File: "tag.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), "genre:sample-tag", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "/g/42" {
		t.Fatalf("got %+v, want the one gallery on the fixture's first page", got)
	}
}

// Page 2 is discovered off page 1's own pagination widget, not assembled from
// an assumed URL shape: tag.html's own page-2 link is a query-string URL
// (?page=2), the *other* pagination scheme from tags.html's path-style one,
// and listGenre must still reach it.
func TestListingGenrePage2IsDiscoveredFromThePaginationWidget(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tag/sample-tag/":        {File: "tag.html"},
		"GET /tag/sample-tag/?page=2": {File: "tag-page2.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), "genre:sample-tag", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "/g/43" {
		t.Fatalf("got %+v, want the one gallery on the fixture's second page", got)
	}
}

// A page number the pagination widget does not offer (past the last page)
// reads as "no more results", not an error.
func TestListingGenrePastTheLastPageIsEmptyNotAnError(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tag/sample-tag/": {File: "tag.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.List(context.Background(), site(), "genre:sample-tag", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want no results past the last page the widget offers", got)
	}
}

func TestListingUnknownIDErrors(t *testing.T) {
	th := doujinreader.NewWithClock(themetest.New(t, nil), clock)
	if _, err := th.List(context.Background(), site(), "not-a-real-listing", 1); err == nil {
		t.Fatal("List returned no error for an unknown listing id")
	}
}

// nhentai.xxx's own tags-index skin: ".tag_name"/".tag_count" siblings inside
// the anchor, the count abbreviated ("2K"). tagIndexLabel and parseCount must
// both read this correctly, exercised here without a whole-file fixture.
func TestListingsReadsTheOtherSitesTagsIndexSkin(t *testing.T) {
	body := `<!DOCTYPE html><html><body><div class="tags_list">` +
		`<a class="tag_btn" href="/tag/ahegao/"><span class="tag_name">ahegao</span><span class="tag_count">63K</span></a>` +
		`<a class="tag_btn" href="/tag/rare-thing/"><span class="tag_name">rare thing</span><span class="tag_count">17</span></a>` +
		`</div></body></html>`
	f := themetest.New(t, map[string]themetest.Route{
		"GET /tags/": {Body: body},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Listings(context.Background(), site())
	if err != nil {
		t.Fatal(err)
	}
	want := []theme.Listing{
		theme.GenreListing("ahegao", "ahegao"),
		theme.GenreListing("rare-thing", "rare thing"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Listings() = %+v, want %+v (63K must outrank 17)", got, want)
	}
}
