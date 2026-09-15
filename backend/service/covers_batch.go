package service

// Covers for the tiles on screen — PLAN §6 M3's grid and PLAN §12.1's paging.
//
// Nothing image-shaped crosses the socket (PLAN §7.1): the backend writes a
// downscaled copy to disk and sends its path.

import (
	"context"

	"github.com/rickl/quire/backend/appload"
)

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
		go s.runCover(batchCtx, out, sourceID, w.SeriesID, w.URL)
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
	path, err := s.covers.Path(ctx, src, url)
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
