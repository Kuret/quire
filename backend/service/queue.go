package service

import (
	"context"
	"fmt"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/assemble"
)

// enqueueManyRequest is the MessageEnqueueDownloads payload: one selection of
// rows, queued together.
type enqueueManyRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`

	// Grouping says which list the selection came from — the chapter rows or
	// the volume rows — exactly as it does on a single download.
	Grouping string `json:"grouping,omitempty"`

	// ChapterIDs are the selected rows, in the order the list showed them. A
	// volume row's id is its first chapter's, which is what the single-row
	// path uses too.
	ChapterIDs []string `json:"chapterIds"`
}

func (r enqueueManyRequest) grouping() string {
	if r.Grouping == assemble.GroupingVolume {
		return assemble.GroupingVolume
	}
	return assemble.GroupingChapter
}

// enqueueMany queues a selection of rows, immediately.
//
// # No question
//
// A selection used to ask before it queued. The user asked for that to go —
// "just download and queue instantly when i click download" — and they are
// right about their own screen: picking the rows *is* the deliberate step, and
// a confirmation behind a deliberate step is a tap people learn to make without
// reading. A single row's own Download button still asks where it always did
// (see askToConfirm); this is the selection path only.
//
// # Why this is not a loop over enqueueDownload
//
// Two things about a selection are still different from the same taps made one
// at a time:
//
//   - **No per-row question.** Each row is queued as already confirmed, so a
//     selection of volumes does not turn into a stack of individual
//     confirmations. Without a question at the selection level, this is the
//     only thing holding that back.
//   - **One answer about the queue.** The queue is sixteen deep and stays that
//     way: a deeper one on a 2 GB device is how /home fills while the user is
//     not looking (docs/DEVICE-NOTES.md §10.3). A selection longer than that is
//     ordinary, not an error, and answering it with fourteen copies of "Quire
//     is already busy" is noise standing in for information. With nothing asked
//     up front, this reply is now the only way the user learns what did not
//     fit, which makes it more load-bearing than it was, not less.
//
// The downloads themselves are the same downloads: the same queue, the same
// single worker, the same per-row progress.
func (s *Service) enqueueMany(ctx context.Context, out Sender, req enqueueManyRequest) error {
	if s.library == nil || s.libStore == nil {
		return s.sendError(out, "unavailable",
			"This build of Quire cannot save to the reMarkable's library.")
	}
	if req.SourceID == "" || req.SeriesID == "" {
		return s.sendError(out, "bad_request", "Quire needs a source and a series to download from.")
	}

	ids := dedupe(req.ChapterIDs)
	if len(ids) == 0 {
		return s.sendError(out, "bad_request", "Nothing was selected, so there is nothing to queue.")
	}

	queued, skipped := 0, 0
	for _, id := range ids {
		one := downloadRequest{
			SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: id,
			Grouping: req.Grouping, Confirmed: true,
		}
		// Confirmed, so no per-row question: the selection was confirmed as a
		// whole, and asking again for each row would be asking the same
		// question ten times.
		if s.offerToQueue(ctx, out, one) {
			queued++
			continue
		}
		// The queue is full. The rest will not fit either — nothing leaves it
		// between two sends of the same burst — but the loop runs on rather
		// than breaking, because a worker that finishes mid-burst makes room
		// and a break would refuse a row the queue would have taken.
		skipped++
	}

	s.log.Info("queued a selection", "source", req.SourceID, "series", req.SeriesID,
		"queued", queued, "skipped", skipped, "grouping", req.grouping())

	return send(out, appload.MessageQueueResult, map[string]any{
		"queued":  queued,
		"skipped": skipped,
		"message": queueOutcome(queued, skipped, req.grouping()),
	})
}

// queueOutcome is what the backend says after the fact, and it is empty when
// everything fitted.
//
// Silence is right in that case: every row that was queued says "Queued." for
// itself, and a summary repeating what the list already shows is a sentence
// that trains people to ignore sentences.
func queueOutcome(queued, skipped int, grouping string) string {
	if skipped == 0 {
		return ""
	}

	what := "chapters"
	if grouping == assemble.GroupingVolume {
		what = "volumes"
	}
	one := what[:len(what)-1]

	switch {
	case queued == 0:
		return fmt.Sprintf("Quire is already holding as many downloads as it will queue, so none of those "+
			"%d %s went on. Ask again once some have finished.", skipped, what)
	case queued == 1:
		return fmt.Sprintf("Queued 1 %s. The other %d didn’t fit — Quire holds %d at a time, so ask "+
			"again once some have finished.", one, skipped, downloadQueueDepth)
	default:
		return fmt.Sprintf("Queued %d %s. The other %d didn’t fit — Quire holds %d at a time, so ask "+
			"again once some have finished.", queued, what, skipped, downloadQueueDepth)
	}
}

// dedupe keeps the first occurrence of each id, in order.
//
// The order is the list's own, which is the order the user is reading in, and
// it is what the queue will work through.
func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
