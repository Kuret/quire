package service

// Covers for the tiles on screen — PLAN §6 M3's grid and PLAN §12.1's paging.
//
// Nothing image-shaped crosses the socket (PLAN §7.1): the backend writes a
// downscaled copy to disk and sends its path.

import (
	"context"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// maxCoverRefs bounds the referrer memo. A screenful is single digits and a
// listing page is tens, so this is many screens' worth; the whole map is
// dropped when it is reached rather than evicted entry by entry, because the
// only cost of forgetting is a cover fetched without a header, and the listing
// that names it again is one page turn away.
const maxCoverRefs = 4096

// rememberCoverReferrer records the page a cover URL was parsed out of.
//
// PLAN §7.6 requires the `Referer` to be **the page we actually fetched**, and
// forbids reconstructing one later. The cover URL round-trips through the
// frontend — the grid asks for the tiles it has on screen, by URL — so by the
// time the request comes back the response it came from is long gone. The
// value is therefore kept beside the URL at the moment the theme hands it over
// and looked up again by that URL, which is remembering rather than guessing.
//
// A URL we have no note for yields "", and therefore no header at all, which
// PLAN §7.6 is explicit is the correct answer when we do not know.
func (s *Service) rememberCoverReferrer(sourceID, coverURL, referrer string) {
	if coverURL == "" || referrer == "" {
		return
	}
	s.coverRefMu.Lock()
	defer s.coverRefMu.Unlock()
	if s.coverRefs == nil || len(s.coverRefs) >= maxCoverRefs {
		s.coverRefs = map[string]string{}
	}
	s.coverRefs[coverRefKey(sourceID, coverURL)] = referrer
}

// rememberCoverURLs records which series each cover URL belongs to, for the
// screens that will never see this listing.
//
// It is called at exactly the points rememberCoverReferrer is, and for the same
// underlying reason — this is the only moment anything knows the pairing — but
// it answers a different question and outlives the session. The referrer memo
// is "what page was this URL on", is needed for one fetch, and is in memory;
// this is "which series is this a picture of", is needed by the Downloaded and
// Watching screens days later, and is on disk (see state/covers.go).
//
// A failed write is logged and nothing else. The cost is a blank tile on a
// screen that is otherwise complete, and refusing the user's search because a
// cover cache could not be written would be the tail wagging the dog.
func (s *Service) rememberCoverURLs(sourceID string, refs []state.CoverRef) {
	if s.store == nil || len(refs) == 0 {
		return
	}
	if err := s.store.RememberCovers(sourceID, refs); err != nil {
		s.log.Warn("could not remember which series a cover belongs to",
			"source", sourceID, "err", err)
	}
}

func (s *Service) coverReferrerFor(sourceID, coverURL string) string {
	s.coverRefMu.Lock()
	defer s.coverRefMu.Unlock()
	return s.coverRefs[coverRefKey(sourceID, coverURL)]
}

// coverRefKey is per-source, because two sources can run the same software on
// two hosts and a referrer from one is not a truthful one for the other.
func coverRefKey(sourceID, coverURL string) string {
	return sourceID + "\x00" + coverURL
}

// coverWant is one tile the frontend has on screen, and the source it belongs
// to: a visible set can span sources (the Downloaded and Watching screens), so
// the source is a property of the tile rather than of the batch.
type coverWant struct {
	SourceID string
	SeriesID string
	URL      string
}

// coverRequest is the RequestCover payload. It lives here rather than inline in
// the message switch so that coverWants can be tested: which source each tile
// is attributed to is exactly the kind of decision PLAN §2 wants out of a place
// nothing can reach.
type coverRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`
	URL      string `json:"url"`
	Covers   []struct {
		SourceID string `json:"sourceId"`
		SeriesID string `json:"seriesId"`
		URL      string `json:"url"`
	} `json:"covers"`
}

// coverWants turns one request into the visible set.
//
// An entry may name its own source and falls back to the request's when it does
// not. That is what lets a single message carry a screen whose rows come from
// several sources — which runCoverBatch requires, because it cancels everything
// before it on the grounds that what it was handed is the *whole* visible set.
func coverWants(req coverRequest) []coverWant {
	want := make([]coverWant, 0, len(req.Covers)+1)
	for _, c := range req.Covers {
		src := c.SourceID
		if src == "" {
			src = req.SourceID
		}
		want = append(want, coverWant{SourceID: src, SeriesID: c.SeriesID, URL: c.URL})
	}
	if len(want) == 0 && req.SeriesID != "" {
		want = append(want, coverWant{SourceID: req.SourceID, SeriesID: req.SeriesID, URL: req.URL})
	}
	return want
}

// runCoverBatch fetches the covers for the tiles now on screen and abandons the
// previous batch.
//
// PLAN §12.1: a page turn changes which tiles are visible, and a cover still
// being fetched for a tile that has gone is work nobody will see — on a device
// where the politeness limiter (PLAN §7.4) serialises requests, it is also work
// standing in front of the covers that *are* on screen. The new batch is the
// complete visible set, so cancelling everything before it loses nothing: a
// cover that survives the turn is in this batch too, and by then it is on disk.
func (s *Service) runCoverBatch(ctx context.Context, out Sender, want []coverWant) {
	if s.covers == nil {
		return
	}

	s.coverMu.Lock()
	if s.coverCancel != nil {
		s.coverCancel()
	}
	batchCtx, cancel := context.WithCancel(ctx)
	s.coverBatch, s.coverCancel = batchCtx, cancel
	s.coverMu.Unlock()

	if len(want) == 0 {
		return
	}
	for _, w := range want {
		if w.SourceID == "" || w.SeriesID == "" || w.URL == "" {
			continue
		}
		s.goBackground(batchCtx, func(ctx context.Context) { s.runCover(ctx, out, w.SourceID, w.SeriesID, w.URL) })
	}
}

func (s *Service) runCover(ctx context.Context, out Sender, sourceID, seriesID, url string) {
	if s.covers == nil {
		return
	}
	th, src, err := s.themeFor(sourceID)
	if err != nil {
		return
	}
	from, refErr := theme.CoverRefererFrom(s.coverReferrerFor(sourceID, url))
	if refErr != nil {
		// A theme named something it cannot have fetched. PLAN §7.6: do not
		// invent a replacement — say so and send no header, which is what the
		// zero Referrer CoverRefererFrom hands back does.
		s.log.Warn("cover referrer unusable", "source", sourceID, "series", seriesID, "err", refErr)
	}
	path, err := s.covers.Path(ctx, th, src, url, from)
	if err != nil {
		// A missing cover is a blank tile, not an error dialogue: the grid is
		// still usable and the titles are still readable.
		s.log.Debug("cover unavailable", "source", sourceID, "series", seriesID, "err", err)
		return
	}
	// Logged because a cover that is on disk and not on screen is otherwise
	// indistinguishable from one that was never fetched: the failure is silent
	// by design (a blank tile, not a dialogue), so the only way to tell the two
	// apart afterwards is to say which series a delivered cover was for.
	s.log.Debug("cover ready", "source", sourceID, "series", seriesID, "path", path)
	_ = send(out, appload.MessageCoverReady, map[string]any{
		"sourceId": sourceID,
		"seriesId": seriesID,
		"path":     path,
	})
}
