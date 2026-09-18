package service

import (
	"context"

	"github.com/rickl/quire/backend/appload"
)

// checkedRequest is the MessageDocumentsChecked payload.
type checkedRequest struct {
	// Checked says the frontend actually looked. **Everything here turns on
	// it.** A frontend with no bridge answers false with an empty Missing, and
	// that must never be read as "none of them are missing" — see
	// documentsChecked.
	Checked bool `json:"checked"`

	// DocumentUUIDs is what it was asked about, echoed back.
	DocumentUUIDs []string `json:"documentUuids"`

	// Missing is the subset that no longer resolves on the tablet.
	Missing []string `json:"missing"`

	// Detail is why the frontend could not check, when it could not. It exists
	// so that a failure on the device reaches this log instead of the QML
	// console nobody is reading on a tablet — an exception in the bridge was
	// silence until 2026-09-18 (see ui/Answers.js).
	Detail string `json:"detail,omitempty"`
}

// maxTotalWipe is the largest number of records Quire will drop in one
// reconcile when *every* one of them came back missing.
//
// A user who deletes their entire library between two launches is possible; a
// bug that reports everything missing is likelier, and the two are
// indistinguishable from here. Above this, the answer is refused and the records
// stay — which costs the user nothing they will notice, because tapping Read on
// any of them still cleans that one up through the §6 M6 path.
//
// Three, because at that size "I deleted the couple of things I had" is an
// ordinary afternoon and the blast radius of being wrong is small. At thirty it
// is neither.
const maxTotalWipe = 3

// askToCheck asks the frontend which recorded documents are still on the tablet.
//
// The user can delete a download in xochitl, and until this existed Quire only
// noticed when they tapped Read — and even then it kept the pages. See
// documentsChecked for what is done with the answer, and why the answer's shape
// matters more than the feature.
func (s *Service) askToCheck(ctx context.Context, out Sender) {
	if s.libStore == nil {
		return
	}
	var uuids []string
	for _, rec := range s.libStore.List() {
		if rec.DocumentUUID != "" {
			uuids = append(uuids, rec.DocumentUUID)
		}
	}
	if len(uuids) == 0 {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	_ = send(out, appload.MessageCheckDocuments, map[string]any{"documentUuids": uuids})
}

// documentsChecked drops the records of documents the tablet no longer has, and
// reclaims their pages.
//
// # The guard, stated bluntly because the failure is not survivable
//
// **An answer that could not be checked deletes nothing.** Not "deletes nothing
// this time" as a nicety — the whole path is refused. A frontend whose bridge
// failed to load cannot ask xochitl anything, and its empty Missing list means
// "I do not know", not "none of them exist". Confusing those two drops every
// record and deletes the entire page cache on the user's device, silently,
// because a QML file did not resolve.
//
// Three rules follow, and each has a test that fails when it is removed:
//
//  1. **Act only on `checked: true`.** The flag is data, not an absence.
//  2. **Act only on ids we asked about.** A reply naming something else is not
//     an answer to the question that was asked.
//  3. **Refuse a total wipe** of more than maxTotalWipe records. Everything
//     missing at once is far likelier to be a bug than a user who emptied their
//     library between launches, and the recovery from refusing is a tap.
func (s *Service) documentsChecked(req checkedRequest) error {
	if !req.Checked {
		// The important line in this file.
		s.log.Info("the frontend could not check the library; nothing was removed",
			"asked", len(req.DocumentUUIDs), "detail", req.Detail)
		return nil
	}
	if s.libStore == nil || len(req.Missing) == 0 {
		return nil
	}

	asked := make(map[string]bool, len(req.DocumentUUIDs))
	for _, id := range req.DocumentUUIDs {
		asked[id] = true
	}
	missing := make(map[string]bool, len(req.Missing))
	for _, id := range req.Missing {
		if !asked[id] {
			// Not an answer to the question asked. Ignored rather than
			// obeyed: the backend chose the list, and a reply that widens it
			// is a reply to something else.
			s.log.Warn("the frontend reported a document that was not asked about", "document", id)
			continue
		}
		missing[id] = true
	}
	if len(missing) == 0 {
		return nil
	}
	if len(missing) == len(asked) && len(asked) > maxTotalWipe {
		s.log.Warn("every recorded document came back missing; refusing to act on it",
			"records", len(asked), "limit", maxTotalWipe)
		return nil
	}

	var freed int64
	dropped := 0
	for _, rec := range s.libStore.List() {
		if !missing[rec.DocumentUUID] {
			continue
		}
		if err := s.libStore.Remove(rec.Key); err != nil {
			s.log.Error("could not forget a document the tablet no longer has",
				"document", rec.DocumentUUID, "err", err)
			continue
		}
		// After the removal and against what remains, so a chapter another
		// record still holds survives — the same ordering as every other
		// reclaim.
		freed += s.reclaimPages(rec, s.libStore.List())
		dropped++
	}
	if dropped > 0 {
		s.log.Info("forgot documents the tablet no longer has",
			"dropped", dropped, "of", len(asked),
			"freedBytes", freed, "freedMiB", freed>>20)
	}
	return nil
}
