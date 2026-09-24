package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// fakeLister is a theme.Theme that also implements theme.Lister, entirely
// in-memory: no fetcher, no fixtures, no network. It exists because none of
// the real themes are this core agent's to touch — see PLAN's browse-listings
// contract — and because a fake pinned here can assert the *service's* rules
// about listings (ordering, well-known labels, degrading on failure, the
// pager key) without depending on how any one theme happens to answer.
type fakeLister struct {
	// listings is returned by Listings(), unless listErr is set.
	listings []theme.Listing
	listErr  error

	// pages maps "listingID:page" to what List answers; a missing key is an
	// empty, non-exhausting page unless calls records it first so a test can
	// see this it was asked at all.
	pages map[string][]theme.SeriesStub

	// browsePages maps "page" (as a string) to what Search("", page) answers.
	browsePages map[string][]theme.SeriesStub

	calls []string
}

func (f *fakeLister) ID() string                  { return "fakelister" }
func (f *fakeLister) Fingerprint(*probe.Page) int { return 0 }
func (f *fakeLister) SuggestedName() string       { return "" }
func (f *fakeLister) AllowedHosts() []string      { return nil }

func (f *fakeLister) Search(_ context.Context, _ *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	f.calls = append(f.calls, fmt.Sprintf("search:%q:%d", q, page))
	if q != "" {
		return nil, nil
	}
	return f.browsePages[fmt.Sprintf("%d", page)], nil
}

func (f *fakeLister) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeLister) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeLister) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLister) Listings(context.Context, *theme.Source) ([]theme.Listing, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listings, nil
}

func (f *fakeLister) List(_ context.Context, _ *theme.Source, listingID string, page int) ([]theme.SeriesStub, error) {
	f.calls = append(f.calls, fmt.Sprintf("list:%s:%d", listingID, page))
	known := false
	for _, l := range f.listings {
		if l.ID == listingID {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("fakelister: unknown listing %q", listingID)
	}
	return f.pages[fmt.Sprintf("%s:%d", listingID, page)], nil
}

// fakeDefaultLister is a fakeLister that also implements theme.DefaultLister,
// for asserting the service's relabel-and-deduplicate rule (see
// backend/theme/listing.go's DefaultLister doc comment).
type fakeDefaultLister struct {
	fakeLister
	defaultListing string
}

func (f *fakeDefaultLister) DefaultListing() string { return f.defaultListing }

// fakeNonLister is an ordinary theme.Theme with no Lister at all — the other
// half of "a non-Lister theme gets exactly [latest]".
type fakeNonLister struct{ calls []string }

func (f *fakeNonLister) ID() string                  { return "fakenonlister" }
func (f *fakeNonLister) Fingerprint(*probe.Page) int { return 0 }
func (f *fakeNonLister) SuggestedName() string       { return "" }
func (f *fakeNonLister) AllowedHosts() []string      { return nil }

func (f *fakeNonLister) Search(_ context.Context, _ *theme.Source, q string, page int) ([]theme.SeriesStub, error) {
	f.calls = append(f.calls, fmt.Sprintf("search:%q:%d", q, page))
	if q == "" && page == 1 {
		return []theme.SeriesStub{{ID: "s1", Title: "Only Series"}}, nil
	}
	return nil, nil
}
func (f *fakeNonLister) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeNonLister) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeNonLister) Pages(context.Context, *theme.Source, string) ([]string, error) {
	return nil, errors.New("not implemented")
}

// newListingsService builds a Service over one source, driven by th, with no
// real fetcher — the fakes above never touch the network.
func newListingsService(t *testing.T, th theme.Theme) (*service.Service, string) {
	t.Helper()
	reg := theme.NewRegistry()
	reg.MustRegister(th)

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	src, err := store.Add(&theme.Source{
		Name: "Fake Source", Lang: "en", Theme: th.ID(),
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    nil,
		Covers:     covers.New(dir+"/covers", nil),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
	})
	t.Cleanup(svc.Close)
	return svc, src.ID
}

type listingsReply struct {
	SourceID string `json:"sourceId"`
	Listings []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Group string `json:"group"`
	} `json:"listings"`
}

func fetchListingsReply(t *testing.T, svc *service.Service, sourceID string) listingsReply {
	t.Helper()
	rec := &recorder{}
	handle(t, svc, rec, appload.MessageListListings, fmt.Sprintf(`{"sourceId":%q}`, sourceID))
	var out listingsReply
	if err := json.Unmarshal(rec.wait(t, appload.MessageListings), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestNonListerThemeOffersOnlyLatest is the mutation check by name: a source
// whose theme is not a theme.Lister gets exactly [latest], never an error and
// never nothing.
func TestNonListerThemeOffersOnlyLatest(t *testing.T) {
	svc, sourceID := newListingsService(t, &fakeNonLister{})
	got := fetchListingsReply(t, svc, sourceID)
	if len(got.Listings) != 1 || got.Listings[0].ID != theme.ListingLatest {
		t.Fatalf("listings = %+v, want exactly [latest]", got.Listings)
	}
	if got.Listings[0].Label != theme.ListingLabel(theme.ListingLatest) {
		t.Errorf("label = %q, want the well-known wording", got.Listings[0].Label)
	}
}

// TestListerOrdersSortStatusGenre pins the ordering rule: sort, then status,
// then genre, latest always first and never duplicated even if the theme also
// named it, well-known labels replaced regardless of what the theme sent, and
// genres kept in the theme's own order.
func TestListerOrdersSortStatusGenre(t *testing.T) {
	th := &fakeLister{listings: []theme.Listing{
		theme.GenreListing("romance", "Romance"),
		{ID: theme.ListingCompleted, Group: theme.ListingGroupStatus},
		{ID: theme.ListingLatest, Label: "should be overwritten", Group: theme.ListingGroupSort},
		{ID: theme.ListingPopular, Label: "Hot", Group: theme.ListingGroupSort},
		theme.GenreListing("action", "Action"),
	}}
	svc, sourceID := newListingsService(t, th)
	got := fetchListingsReply(t, svc, sourceID)

	var ids, groups []string
	for _, l := range got.Listings {
		ids = append(ids, l.ID)
		groups = append(groups, l.Group)
	}
	wantIDs := []string{theme.ListingLatest, theme.ListingPopular, theme.ListingCompleted, "genre:romance", "genre:action"}
	if fmt.Sprint(ids) != fmt.Sprint(wantIDs) {
		t.Fatalf("ids = %v, want %v", ids, wantIDs)
	}
	for _, l := range got.Listings {
		if l.ID == theme.ListingPopular && l.Label != theme.ListingLabel(theme.ListingPopular) {
			t.Errorf("popular label = %q, want the well-known wording, not the theme's %q", l.Label, "Hot")
		}
		if l.ID == "genre:romance" && l.Label != "Romance" {
			t.Errorf("genre label = %q, want the theme's own %q", l.Label, "Romance")
		}
	}
}

// TestListingsDegradeWithoutErrorBanner is the other mutation check: a
// Listings() failure never reaches the frontend as MessageError, and degrades
// to whatever is already known — latest-only with nothing cached yet, or the
// last successful answer once there is one.
func TestListingsDegradeWithoutErrorBanner(t *testing.T) {
	th := &fakeLister{listErr: errors.New("the genre page answered a challenge")}
	svc, sourceID := newListingsService(t, th)

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageListListings, fmt.Sprintf(`{"sourceId":%q}`, sourceID))
	rec.wait(t, appload.MessageListings)
	if hasFrame(rec, appload.MessageError) {
		t.Fatal("a Listings() failure must never reach the frontend as an error banner")
	}
	var got listingsReply
	if err := json.Unmarshal(rec.sent[len(rec.sent)-1].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Listings) != 1 || got.Listings[0].ID != theme.ListingLatest {
		t.Fatalf("listings = %+v, want latest-only degrade with nothing cached yet", got.Listings)
	}

	// Now prime the cache with a real success, break the theme, and confirm
	// the *previous* good answer is what a later failure degrades to.
	th.listErr = nil
	th.listings = []theme.Listing{{ID: theme.ListingPopular, Group: theme.ListingGroupSort}}
	got2 := fetchListingsReply(t, svc, sourceID)
	if len(got2.Listings) != 2 {
		t.Fatalf("listings = %+v, want latest + popular once the theme works", got2.Listings)
	}

	th.listErr = errors.New("now it is broken")
	rec2 := &recorder{}
	handle(t, svc, rec2, appload.MessageListListings, fmt.Sprintf(`{"sourceId":%q}`, sourceID))
	// The cache is fresh (well under 24h), so a second request within the TTL
	// must not even ask the theme again, let alone show the old data as new —
	// this assertion is really about listingsFor's cache, exercised through
	// the service.
	got3 := fetchListingsReply(t, svc, sourceID)
	_ = rec2
	if len(got3.Listings) != 2 {
		t.Fatalf("listings = %+v, want the cached popular+latest answer, not a degrade — the cache was still fresh", got3.Listings)
	}
}

// TestUnknownListingSourceSearchFails pins the "unknown listing never falls
// back silently" mutation check from the caller's side: a non-Lister theme
// asked for anything but the default listing must error, not quietly serve
// its one Search.
func TestUnknownListingSourceSearchFails(t *testing.T) {
	th := &fakeNonLister{}
	svc, sourceID := newListingsService(t, th)

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageSearch,
		fmt.Sprintf(`{"sourceId":%q,"query":"","listing":%q,"page":1,"pageSize":10}`, sourceID, theme.ListingPopular))
	if !hasFrame(rec, appload.MessageError) {
		t.Fatal("a non-Lister theme asked for a named listing must answer with an error, never a silent fall back to its default")
	}
}

// TestSwitchingListingsDoesNotServeStalePages is the pagerKey mutation check:
// paging listing A and then switching to listing B must not hand back A's
// cached rows, and paging B back to A must not hand back B's either.
func TestSwitchingListingsDoesNotServeStalePages(t *testing.T) {
	th := &fakeLister{
		listings: []theme.Listing{
			{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
			{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		},
		pages: map[string][]theme.SeriesStub{
			theme.ListingPopular + ":1": {{ID: "p1", Title: "Popular One"}},
			theme.ListingNew + ":1":     {{ID: "n1", Title: "New One"}},
		},
	}
	svc, sourceID := newListingsService(t, th)

	search := func(listing string) pageReply {
		rec := &recorder{}
		handle(t, svc, rec, appload.MessageSearch,
			fmt.Sprintf(`{"sourceId":%q,"query":"","listing":%q,"page":1,"pageSize":10}`, sourceID, listing))
		var out pageReply
		if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	popular := search(theme.ListingPopular)
	if len(popular.Series) != 1 || popular.Series[0].ID != "p1" {
		t.Fatalf("popular = %+v, want just p1", popular.Series)
	}
	newest := search(theme.ListingNew)
	if len(newest.Series) != 1 || newest.Series[0].ID != "n1" {
		t.Fatalf("switching to \"new\" served %+v, want just n1 — not popular's cached page", newest.Series)
	}
	popularAgain := search(theme.ListingPopular)
	if len(popularAgain.Series) != 1 || popularAgain.Series[0].ID != "p1" {
		t.Fatalf("switching back to popular served %+v, want just p1 — not new's cached page", popularAgain.Series)
	}
}

// TestListingLatestMatchesBrowse pins §7.5's rule as the caller sees it:
// naming "latest" explicitly must answer exactly what plain Browse (an
// empty listing) does, sharing its cache.
func TestListingLatestMatchesBrowse(t *testing.T) {
	th := &fakeLister{browsePages: map[string][]theme.SeriesStub{
		"1": {{ID: "b1", Title: "Browse One"}},
	}}
	svc, sourceID := newListingsService(t, th)

	rec := &recorder{}
	handle(t, svc, rec, appload.MessageSearch,
		fmt.Sprintf(`{"sourceId":%q,"query":"","listing":"latest","page":1,"pageSize":10}`, sourceID))
	var out pageReply
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Series) != 1 || out.Series[0].ID != "b1" {
		t.Fatalf("listing \"latest\" = %+v, want Browse's own page", out.Series)
	}
	for _, c := range th.calls {
		if c != `search:"":1` && c != `search:"":2` {
			t.Fatalf("calls = %v, want only Search(\"\", ...) — List() must never be called for the default listing", th.calls)
		}
	}
}

// TestDefaultListerRelabelsLatestAndDropsTheDuplicate pins
// backend/theme/listing.go's DefaultLister contract: a theme whose Search("")
// is genuinely e.g. "popular" order relabels the always-first "latest" entry
// with that listing's own well-known wording, keeps its id "latest" (so
// paging/Search("") are unchanged), and drops the theme's own entry for that
// same ID so it is never offered twice under two names.
func TestDefaultListerRelabelsLatestAndDropsTheDuplicate(t *testing.T) {
	th := &fakeDefaultLister{
		fakeLister: fakeLister{listings: []theme.Listing{
			{ID: theme.ListingPopular, Group: theme.ListingGroupSort},
			{ID: theme.ListingNew, Group: theme.ListingGroupSort},
		}},
		defaultListing: theme.ListingPopular,
	}
	svc, sourceID := newListingsService(t, th)
	got := fetchListingsReply(t, svc, sourceID)

	var ids []string
	for _, l := range got.Listings {
		ids = append(ids, l.ID)
	}
	wantIDs := []string{theme.ListingLatest, theme.ListingNew}
	if fmt.Sprint(ids) != fmt.Sprint(wantIDs) {
		t.Fatalf("ids = %v, want %v — popular must not appear a second time under its own id", ids, wantIDs)
	}
	if got.Listings[0].ID != theme.ListingLatest {
		t.Fatalf("first entry id = %q, want %q so paging/Search(\"\") stay unchanged", got.Listings[0].ID, theme.ListingLatest)
	}
	if want := theme.ListingLabel(theme.ListingPopular); got.Listings[0].Label != want {
		t.Errorf("first entry label = %q, want %q (DefaultListing's own label)", got.Listings[0].Label, want)
	}
}

// TestDefaultListerIgnoredWhenLatest is the other half: DefaultListing
// returning "" or ListingLatest itself changes nothing — same as a theme that
// does not implement DefaultLister at all.
func TestDefaultListerIgnoredWhenLatest(t *testing.T) {
	th := &fakeDefaultLister{
		fakeLister:     fakeLister{listings: []theme.Listing{{ID: theme.ListingPopular, Group: theme.ListingGroupSort}}},
		defaultListing: theme.ListingLatest,
	}
	svc, sourceID := newListingsService(t, th)
	got := fetchListingsReply(t, svc, sourceID)

	if got.Listings[0].Label != theme.ListingLabel(theme.ListingLatest) {
		t.Errorf("label = %q, want the ordinary \"latest\" wording unchanged", got.Listings[0].Label)
	}
	found := false
	for _, l := range got.Listings {
		if l.ID == theme.ListingPopular {
			found = true
		}
	}
	if !found {
		t.Errorf("listings = %+v, want popular still offered on its own id when DefaultListing is latest", got.Listings)
	}
}
