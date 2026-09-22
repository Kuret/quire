package doujinreader_test

// PLAN §7.6: the referrer must be the page the cover URL was genuinely
// extracted from. TestSearchWithNoQueryBrowsesTheHomeListing,
// TestSearchWithAQuery and TestSeries already assert the *exact* URL fetched;
// this adds the remaining guarantee: that the header is one
// fetch.PageReferrer will actually accept.

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/doujinreader"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestCoverReferrerIsUsable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /g/42/": {File: "series.html"},
	})
	th := doujinreader.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), site(), "/g/42")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := theme.CoverRefererFrom(got.CoverReferrer)
	if err != nil {
		t.Fatalf("the theme named a referrer the fetch layer refuses: %v", err)
	}
	if ref.IsZero() {
		t.Fatal("a cover with a page behind it must yield a referrer, not the zero value")
	}
}
