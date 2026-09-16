package service

// Covers for the tiles on screen — PLAN §6 M3's grid and PLAN §12.1's paging.
//
// Nothing image-shaped crosses the socket (PLAN §7.1): the backend writes a
// downscaled copy to disk and sends its path.

import (
	"context"

	"github.com/rickl/quire/backend/appload"
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

// coverWant is one tile the frontend has on screen.
type coverWant struct {
	SeriesID string
	URL      string
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
func (s *Service) runCoverBatch(ctx context.Context, out Sender, sourceID string, want []coverWant) {
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
		if w.SeriesID == "" || w.URL == "" {
			continue
		}
		s.goBackground(batchCtx, func(ctx context.Context) { s.runCover(ctx, out, sourceID, w.SeriesID, w.URL) })
	}
}

func (s *Service) runCover(ctx context.Context, out Sender, sourceID, seriesID, url string) {
	if s.covers == nil {
		return
	}
	src, ok := s.store.Get(sourceID)
	if !ok {
		return
	}
	from, err := theme.CoverRefererFrom(s.coverReferrerFor(sourceID, url))
	if err != nil {
		// A theme named something it cannot have fetched. PLAN §7.6: do not
		// invent a replacement — say so and send no header, which is what the
		// zero Referrer CoverRefererFrom hands back does.
		s.log.Warn("cover referrer unusable", "source", sourceID, "series", seriesID, "err", err)
	}
	path, err := s.covers.Path(ctx, src, url, from)
	if err != nil {
		// A missing cover is a blank tile, not an error dialogue: the grid is
		// still usable and the titles are still readable.
		s.log.Debug("cover unavailable", "source", sourceID, "series", seriesID, "err", err)
		return
	}
	_ = send(out, appload.MessageCoverReady, map[string]any{
		"sourceId": sourceID,
		"seriesId": seriesID,
		"path":     path,
	})
}
