package madara_test

// PLAN §7.6: the referrer must be the page the cover URL was genuinely
// extracted from. TestSearch and TestSeries already assert the *exact* URL
// fetched rather than merely something plausible; this file adds the
// remaining PLAN §7.6 guarantees that those two do not: that the header is
// one fetch.PageReferrer will actually accept.

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// The referrer must be a URL fetch.PageReferrer accepts, or it is a theme bug
// that sends no header at all rather than a broken one.
func TestCoverReferrerIsUsable(t *testing.T) {
	f := themetest.New(t, map[string]themetest.Route{
		"GET /manga/the-lantern-keeper/": {File: "series.html"},
	})
	th := madara.NewWithClock(f, clock)

	got, err := th.Series(context.Background(), siteA(), "/manga/the-lantern-keeper/")
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
