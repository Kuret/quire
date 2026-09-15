package service_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// pageReply is what MessageSearchResults now carries: the page actually served,
// how many there are *if the source has said*, and whether there is more.
type pageReply struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"pageSize"`
	TotalPages int  `json:"totalPages"`
	HasMore    bool `json:"hasMore"`
	Series     []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"series"`
}

// newPagingService is newService with the fetcher handed back, so a test can
// count what actually went over the wire.
func newPagingService(t *testing.T) (*service.Service, *themetest.Fetcher) {
	t.Helper()
	f := themetest.New(t, routes())
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
	})
	return svc, f
}

// listingRequests counts the requests that went to the site's listing, which is
// the only kind a page turn could wrongly cause.
func listingRequests(f *themetest.Fetcher) int {
	n := 0
	for _, c := range f.Calls() {
		if strings.Contains(c.URL, "post_type=wp-manga") {
			n++
		}
	}
	return n
}

// browse asks for one display page. Each ask gets a fresh recorder, so a reply
// can never be confused with the previous one's.
func browse(t *testing.T, svc *service.Service, page, size int) pageReply {
	t.Helper()
	rec := &recorder{}
	handle(t, svc, rec, appload.MessageBrowse,
		fmt.Sprintf(`{"sourceId":"example-reader","page":%d,"pageSize":%d}`, page, size))
	var out pageReply
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPageTurnsAreNotHTTPRequests is PLAN §12.1's central rule at the level the
// frontend sees it: the display page and the source page are different things,
// and turning from one display page to the next out of a listing already
// fetched must not touch the network.
func TestPageTurnsAreNotHTTPRequests(t *testing.T) {
	svc, f := newPagingService(t)

	first := browse(t, svc, 1, 1)
	if len(first.Series) != 1 {
		t.Fatalf("page 1 held %d series, want 1 — the display page size was not honoured", len(first.Series))
	}
	if first.Page != 1 || first.PageSize != 1 {
		t.Fatalf("page/pageSize = %d/%d, want 1/1", first.Page, first.PageSize)
	}
	if !first.HasMore {
		t.Fatal("hasMore = false on page 1 of a listing with more in it")
	}
	afterFirst := listingRequests(f)
	if afterFirst == 0 {
		t.Fatal("page 1 made no request at all")
	}

	second := browse(t, svc, 2, 1)
	if len(second.Series) != 1 {
		t.Fatalf("page 2 held %d series, want 1", len(second.Series))
	}
	if second.Series[0].ID == first.Series[0].ID {
		t.Fatal("page 2 showed the same series as page 1")
	}
	if got := listingRequests(f); got != afterFirst {
		t.Fatalf("turning the page cost %d listing requests, want 0 (%d -> %d)",
			got-afterFirst, afterFirst, got)
	}

	// And back again, still free.
	back := browse(t, svc, 1, 1)
	if back.Series[0].ID != first.Series[0].ID {
		t.Fatal("going back did not land on the same page")
	}
	if got := listingRequests(f); got != afterFirst {
		t.Fatalf("going back cost %d listing requests, want 0", got-afterFirst)
	}
}

// TestTheLastPageIsAHardStopWithAnHonestTotal: the total appears only once the
// source has run out, and Next is not offered past the end (PLAN §12.1 — no
// wrap, and no invented denominator).
func TestTheLastPageIsAHardStopWithAnHonestTotal(t *testing.T) {
	svc, _ := newPagingService(t)

	// A page big enough to swallow the whole fixture listing. The source's
	// second page is empty, so the pager learns the total on the first ask.
	all := browse(t, svc, 1, 9)
	if all.TotalPages != 1 {
		t.Fatalf("totalPages = %d, want 1 once the source has run out", all.TotalPages)
	}
	if all.HasMore {
		t.Fatal("hasMore = true on the only page there is")
	}

	// Asking past the end reports the page actually served rather than an
	// empty screen labelled with a page that does not exist.
	past := browse(t, svc, 7, 9)
	if past.Page != 1 {
		t.Fatalf("page = %d after asking for 7, want the clamp to 1", past.Page)
	}
	if len(past.Series) != len(all.Series) {
		t.Fatalf("the clamped page held %d series, want %d", len(past.Series), len(all.Series))
	}
}
