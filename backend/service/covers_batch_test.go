package service

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/covers"
)

// A page turn changes which tiles are on screen. PLAN §12.1 says the covers
// still being fetched for the tiles that went are work nobody will see — and,
// behind PLAN §7.4's serialising limiter, work standing in front of the covers
// that *are* on screen. So a batch supersedes the last one outright.
func TestCoverBatchCancelsThePreviousOne(t *testing.T) {
	// covers is only checked for nil here; nothing is fetched, because the
	// batches below are empty. What is under test is the handover.
	s := &Service{covers: &covers.Cache{}}

	s.runCoverBatch(context.Background(), nil, nil)
	first := s.coverBatch
	if first == nil {
		t.Fatal("the first batch left no context to cancel")
	}
	select {
	case <-first.Done():
		t.Fatal("the first batch was cancelled before a second one arrived")
	default:
	}

	s.runCoverBatch(context.Background(), nil, nil)
	select {
	case <-first.Done():
	default:
		t.Fatal("a new batch did not cancel the covers still in flight for the old page")
	}

	if s.coverBatch == first {
		t.Fatal("the second batch reused the cancelled context")
	}
}

// Without a cover cache there is nothing to fetch and nothing to cancel; a
// frontend that asks anyway must not be a nil dereference.
func TestCoverBatchWithoutACacheDoesNothing(t *testing.T) {
	s := &Service{}
	s.runCoverBatch(context.Background(), nil, []coverWant{{SourceID: "src", SeriesID: "a", URL: "https://example.invalid/a.jpg"}})
	if s.coverBatch != nil {
		t.Fatal("a service with no cover cache started a batch")
	}
}

// PLAN §7.6: the `Referer` a cover host asks for must name the page the URL
// was actually extracted from. The URL round-trips through the frontend, so
// the page it came from is remembered here when the theme hands it over and
// looked up again when the request comes back. These pin that it is a lookup
// and not a reconstruction.
func TestCoverReferrerIsRememberedPerSourceAndURL(t *testing.T) {
	s := &Service{}
	const url = "https://cdn.example.invalid/a.webp"
	s.rememberCoverReferrer("comick", url, "https://example.invalid/comic/lantern")

	if got := s.coverReferrerFor("comick", url); got != "https://example.invalid/comic/lantern" {
		t.Errorf("referrer = %q, want the page it was recorded against", got)
	}
	// Two sources can run the same software on two hosts, and one's referrer is
	// not a truthful one for the other.
	if got := s.coverReferrerFor("other", url); got != "" {
		t.Errorf("a referrer recorded for one source leaked to another: %q", got)
	}
	// A URL with no note yields "", and therefore no header, which §7.6 says is
	// the right answer when we do not know.
	if got := s.coverReferrerFor("comick", "https://cdn.example.invalid/b.webp"); got != "" {
		t.Errorf("an unknown cover URL invented a referrer: %q", got)
	}
}

// A theme with no page to name — mangadex, whose cover host asks for nothing —
// records nothing, so nothing is sent.
func TestNoReferrerIsRecordedForAThemeThatNamesNone(t *testing.T) {
	s := &Service{}
	s.rememberCoverReferrer("mangadex", "https://uploads.example.invalid/a.jpg", "")
	if got := s.coverReferrerFor("mangadex", "https://uploads.example.invalid/a.jpg"); got != "" {
		t.Errorf("referrer = %q, want none", got)
	}
}

// A screen whose rows come from several sources asks for all of them in one
// message, and each tile is fetched from its own source.
//
// This is the regression test for a real bug. The frontend first sent one
// RequestCover per source, which looks harmless until you read runCoverBatch:
// it cancels the batch before it, on the stated grounds that the new batch is
// the complete visible set. Split across three messages, each one cancelled the
// last, so a three-source Downloaded screen fetched one source's covers and
// silently abandoned the other two — timing-dependent, so it would have read as
// flaky rather than broken.
func TestAVisibleSetMaySpanSources(t *testing.T) {
	req := coverRequest{SourceID: "fallback"}
	req.Covers = append(req.Covers,
		struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			URL      string `json:"url"`
		}{SourceID: "mangadex", SeriesID: "a", URL: "https://a.invalid/a.jpg"},
		struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			URL      string `json:"url"`
		}{SourceID: "comick", SeriesID: "b", URL: "https://b.invalid/b.jpg"},
		struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			URL      string `json:"url"`
		}{SeriesID: "c", URL: "https://c.invalid/c.jpg"},
	)

	got := coverWants(req)
	if len(got) != 3 {
		t.Fatalf("got %d wants, want 3", len(got))
	}
	if got[0].SourceID != "mangadex" || got[1].SourceID != "comick" {
		t.Errorf("a tile was not attributed to its own source: %+v", got)
	}
	// An entry that names no source belongs to the request's, which is what the
	// single-source screens rely on: they send sourceId once and nothing else.
	if got[2].SourceID != "fallback" {
		t.Errorf("an entry with no source of its own did not fall back: %+v", got[2])
	}
}

// The single {seriesId,url} form still means a set of one, attributed to the
// request's source.
func TestASingleCoverIsStillASetOfOne(t *testing.T) {
	got := coverWants(coverRequest{SourceID: "mangadex", SeriesID: "a", URL: "https://a.invalid/a.jpg"})
	if len(got) != 1 {
		t.Fatalf("got %d wants, want 1", len(got))
	}
	if got[0].SourceID != "mangadex" || got[0].SeriesID != "a" {
		t.Errorf("the single form lost something: %+v", got[0])
	}
}
