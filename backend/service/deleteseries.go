package service

import (
	"context"
	"fmt"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// Deleting every download of one series, from the downloaded overview.
//
// PLAN §12.5. The same two-step shape as the single delete in delete.go — the
// backend composes the question, the frontend acts, the backend is told what
// actually happened — because the frontend is the only side that can reach
// xochitl's library and the backend is the only side that writes sentences
// (PLAN §2).
//
// **What is different is that this is several documents and each of them fails
// on its own.** A run that deletes five of seven must say five of seven: a
// clean "done" hides two documents the user thinks are gone, and a flat failure
// hides five that really went and whose records are already dropped.

// deleteSeriesRequest is the MessageDeleteSeries payload.
type deleteSeriesRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`

	// Confirmed is false on the first message, which is the frontend asking
	// what to put in front of the user.
	Confirmed bool `json:"confirmed,omitempty"`

	// Results is one entry per document the frontend tried, and it is what was
	// observed rather than what was intended.
	Results []deleteResult `json:"results,omitempty"`

	// Detail is why the frontend could not delete them, when it could not.
	Detail string `json:"detail,omitempty"`
}

// deleteResult is one document's outcome, in the frontend's words.
type deleteResult struct {
	DocumentUUID string `json:"documentUuid"`

	// Trashed means the document is out of the user's library. Removed means
	// it was then taken out of the Trash as well. They are separate because
	// they fail separately — see deleteRequest in delete.go.
	Trashed bool `json:"trashed,omitempty"`
	Removed bool `json:"removed,omitempty"`
}

// DeleteSeriesUnknownRemedy answers a delete for a series with nothing in it.
//
// It can happen honestly: a row on screen older than the store, or a last
// download deleted from the series screen a moment ago.
const DeleteSeriesUnknownRemedy = "Quire has no downloads for that series any more, so there is nothing " +
	"for it to delete."

// DeleteSeriesFailedRemedy is said when not one document could be removed.
//
// It says everything is untouched, because that is the state the user is in and
// it is the one that decides what to do next: nothing was lost, and the row is
// still on the screen saying what it said before.
const DeleteSeriesFailedRemedy = "Quire could not remove any of those downloads from your reMarkable, so it " +
	"has left everything as it was."

// deleteSeriesQuestion is the sentence the confirm strip asks.
//
// It names the series and the number, because those are the two facts that
// decide whether this is the row the user meant: a title on its own does not
// distinguish "this deletes the one chapter you just read" from "this deletes
// forty".
//
// **And it says what is not touched.** The same promise the single delete now
// makes (see deleteQuestion): this removes these documents and nothing else,
// and the rest of the Trash is left alone. A user who has read the older
// warning about the Trash being emptied will otherwise assume it still applies.
func deleteSeriesQuestion(title string, n int) string {
	what := fmt.Sprintf("all %d downloads", n)
	if n == 1 {
		what = "the one download"
	}
	return "Delete " + what + " of “" + title + "” from your reMarkable for good? Nothing else is " +
		"touched, and the rest of your Trash is left alone."
}

// deleteSeriesPartly is the sentence for a run where some documents survived.
//
// It leads with what went, because that part is done and irreversible, and then
// says plainly how many are still there. The user's next move — try again, or
// go and look — depends on that number, so it is never rounded into "some".
func deleteSeriesPartly(deleted, failed int) string {
	return fmt.Sprintf("Quire deleted %d of those downloads, but %s still on your reMarkable.",
		deleted, countOf(failed, "1 is", "%d are"))
}

// deleteSeriesLeftInTrash is said when every document went, but some of them
// only as far as the Trash.
//
// Not a failure: the downloads are out of the library and the records are gone.
// What it corrects is the promise that they were gone for good, because they
// are recoverable instead — a difference the user can act on, and the tablet is
// where they act on it.
func deleteSeriesLeftInTrash(kept int) string {
	return fmt.Sprintf("Those downloads are deleted and out of your library, but %s still in your "+
		"reMarkable’s Trash. Emptying the Trash on the tablet will finish the job.",
		countOf(kept, "1 is", "%d are"))
}

// countOf picks between a written-out singular and a formatted plural, so the
// sentences above read as English rather than as "1 downloads".
//
// Not named `plural`: watch.go already has one of those, and it is about new
// chapters. Two helpers with one name would be read as one helper.
func countOf(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}

// deleteSeries asks the question, or acts on the answer.
func (s *Service) deleteSeries(ctx context.Context, out Sender, req deleteSeriesRequest) error {
	if req.SeriesID == "" {
		return s.sendError(out, "bad_request", "Quire was asked to delete nothing.")
	}

	if !req.Confirmed {
		// Step one: hand back the question and the documents it is about.
		// Nothing is touched, and an accidental tap gets no further.
		recs := s.recordsFor(req.SourceID, req.SeriesID)
		if len(recs) == 0 {
			return s.sendError(out, "not_found", DeleteSeriesUnknownRemedy)
		}
		uuids := make([]string, 0, len(recs))
		for _, rec := range recs {
			uuids = append(uuids, rec.DocumentUUID)
		}
		return send(out, appload.MessageDeleteSeriesConfirm, map[string]any{
			"sourceId":      req.SourceID,
			"seriesId":      req.SeriesID,
			"documentUuids": uuids,
			"message":       deleteSeriesQuestion(downloadedTitle(recs), len(recs)),
		})
	}

	// Both read before anything is forgotten: the folder id and the title come
	// off the records, and the records are about to go.
	folderID := s.recordedFolder(req.SourceID, req.SeriesID)
	folderName := folderName(downloadedTitle(s.recordsFor(req.SourceID, req.SeriesID)))

	var deleted, kept, failed, freed int64
	for _, res := range req.Results {
		if res.DocumentUUID == "" {
			continue
		}
		if !res.Trashed {
			// The frontend tried and could not. The record stays, the PDF
			// stays, and the row keeps saying what it said — all of which is
			// true, and none of which is true if the record is dropped here.
			failed++
			continue
		}
		deleted++
		if !res.Removed {
			kept++
		}
		freed += s.forgetDocument(res.DocumentUUID)
	}

	s.log.Info("downloads for a series were deleted from the reMarkable",
		"source", req.SourceID, "series", req.SeriesID,
		"deleted", deleted, "leftInTrash", kept, "failed", failed,
		"pagesFreedBytes", freed, "pagesFreedMiB", freed>>20)

	// The list first, whatever else happened: the rows on screen are now wrong
	// about several series at once — a row can lose a download, lose all of
	// them, or be unaffected — and the backend is the side that works that out
	// from the records (PLAN §2). Sent before any note, so the screen shows the
	// finished state with a note about it rather than a note over a stale list.
	if err := s.sendDownloaded(out); err != nil {
		return err
	}

	// The empty series folder, but only when the whole series went. **A partial
	// delete leaves the folder**, which also falls out of the listing — the
	// surviving documents are in it — but it is asserted here rather than left
	// to fall out, because "it would have been caught later" is not a guard.
	if failed == 0 && deleted > 0 {
		s.tidySeriesFolder(ctx, out, folderID, folderName)
	}

	switch {
	case deleted == 0 && failed > 0:
		s.log.Warn("no download of a series could be deleted",
			"series", req.SeriesID, "detail", req.Detail)
		return s.sendError(out, "not_deleted", DeleteSeriesFailedRemedy)
	case failed > 0:
		return s.sendError(out, "not_all_deleted", deleteSeriesPartly(int(deleted), int(failed)))
	case kept > 0:
		return s.sendError(out, "left_in_trash", deleteSeriesLeftInTrash(int(kept)))
	}
	return nil
}

// forgetDocument drops one record and reclaims the pages behind it, returning
// the bytes freed.
//
// The remaining-records check is read *after* the record is removed, so "is any
// other record still using this chapter?" is asked of the records that are
// staying. Asking before would find this one and keep everything it named —
// which is the same ordering the single delete depends on, and the reason this
// loops one document at a time rather than dropping them all and reclaiming
// once: a series' records commonly share a chapter with each other.
func (s *Service) forgetDocument(uuid string) int64 {
	if s.libStore == nil {
		return 0
	}
	for _, rec := range s.libStore.List() {
		if rec.DocumentUUID != uuid {
			continue
		}
		if err := s.libStore.Remove(rec.Key); err != nil {
			s.log.Error("could not forget a deleted volume", "uuid", uuid, "err", err)
			return 0
		}
		return s.reclaimPages(rec, s.libStore.List())
	}
	return 0
}

// recordsFor is every record for one (source, series) pair.
func (s *Service) recordsFor(source, series string) []library.Record {
	if s.libStore == nil {
		return nil
	}
	var out []library.Record
	for _, rec := range s.libStore.List() {
		if rec.Source == source && rec.Series == series && rec.DocumentUUID != "" {
			out = append(out, rec)
		}
	}
	return out
}
