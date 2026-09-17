package service

import (
	"context"
	"strings"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/library"
)

// sortedRequest is the MessageDocumentsSorted payload: what the frontend did
// with a finished download.
type sortedRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`

	DocumentUUIDs []string `json:"documentUuids"`

	// Moved is the documents whose parent really changed, read back by the
	// frontend rather than assumed from a call returning.
	Moved []string `json:"moved"`

	FolderID   string `json:"folderId"`
	FolderName string `json:"folderName"`
	Created    bool   `json:"created"`
	Detail     string `json:"detail"`
}

// maxFolderName caps the series title used as a folder name.
//
// The user reads this name on the tablet, so it is the series' own name rather
// than a scheme. The same cap is applied in Sorting.js, because the frontend is
// the side that creates the folder; this one is here so the *lookup* asks about
// the same name the create would have made.
const maxFolderName = 60

// markSorting registers a sort as in flight and hands back the release.
//
// It is what keeps the two paths that can sort a document from sorting it at
// once: a download finishing sends its own request, and an attach arriving a
// moment later must not send a second one for the same documents. The re-sort
// pass skips anything registered here.
//
// The release also wakes whoever is waiting for the answer, which is how the
// re-sort does one group at a time.
func (s *Service) markSorting(uuids []string) chan struct{} {
	done := make(chan struct{})
	s.sortMu.Lock()
	if s.sorting == nil {
		s.sorting = map[string]chan struct{}{}
	}
	for _, id := range uuids {
		s.sorting[id] = done
	}
	s.sortMu.Unlock()
	return done
}

// finishSorting clears the in-flight marks for these documents and wakes the
// waiter. Calling it twice is harmless: a closed channel stays closed, and a
// document that is not marked is simply not there.
func (s *Service) finishSorting(uuids []string) {
	s.sortMu.Lock()
	defer s.sortMu.Unlock()
	for _, id := range uuids {
		done, ok := s.sorting[id]
		if !ok {
			continue
		}
		delete(s.sorting, id)
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

// sortInFlight reports whether a sort has already been asked for this document.
func (s *Service) sortInFlight(uuid string) bool {
	s.sortMu.Lock()
	defer s.sortMu.Unlock()
	_, ok := s.sorting[uuid]
	return ok
}

// askToSort asks the frontend to put a finished volume in its series folder.
//
// # Why this is a conversation rather than a function call
//
// Neither side can do it alone (PLAN §6 M5, corrected 2026-09-17). Creating a
// folder and moving a document into one are xochitl QML calls, which only the
// frontend can reach; nothing in that QML enumerates a folder's children, so
// "is there already a folder for this series?" can only be answered over HTTP,
// which only this side speaks. So the backend decides *where*, and the frontend
// does it and reports back.
//
// The order of preference for the target is the order of how much it is worth
// trusting:
//
//  1. **The folder id already recorded for this (source, series)**, if it still
//     resolves. Recording the id rather than matching on the name is what keeps
//     a renamed folder working, and what keeps two series of the same title
//     from different sources apart.
//  2. **A folder of that name inside Comics.** The user may have made one, or
//     an older download may have used one before ids were recorded.
//  3. **Nothing** — and the frontend is asked to create it.
//
// Every one of these can end with the documents staying in Comics, which is
// where every download went before this existed. That is a worse-looking
// library, not a failed download, and it is never reported as one.
func (s *Service) askToSort(ctx context.Context, out Sender, src, seriesID, seriesTitle string,
	uuids []string, place library.Placement) {

	if s.library == nil || len(uuids) == 0 {
		return
	}
	name := folderName(seriesTitle)
	if name == "" {
		// Nothing to name a folder after. A folder called "" would be worse
		// than a flat library.
		return
	}

	// Already in the right place: Place() prefers Comics/<series> when it
	// exists, so a series that has been downloaded before lands there without
	// anything being moved. Recording it is all that is left to do.
	if len(place.Path) == 2 && strings.EqualFold(place.Path[1], name) {
		s.rememberFolder(uuids, place.FolderID, place.Path)
		return
	}

	comics, err := s.library.Resolve(ctx, library.ComicsFolder)
	if err != nil {
		s.log.Warn("could not look for the Comics folder", "err", err)
		return
	}

	req := map[string]any{
		"documentUuids": uuids,
		"folderName":    name,
		"sourceId":      src,
		"seriesId":      seriesID,
	}

	if !comics.Complete() {
		// Comics itself is missing. Quire makes it now — the same call, and the
		// manual setup step only ever existed because folder creation was
		// thought impossible. ComicsRemedy still covers the case where creating
		// it fails.
		req["createComics"] = true
		req["comicsName"] = library.ComicsFolder
		req["createUnder"] = ""
		s.log.Info("asking the frontend to make the Comics folder", "series", seriesID)
		s.markSorting(uuids)
		_ = send(out, appload.MessageSortDocuments, req)
		return
	}

	req["createUnder"] = comics.FolderID

	if id := s.recordedFolder(src, seriesID); id != "" {
		if _, ok, err := s.library.ChildByID(ctx, comics.FolderID, id); err != nil {
			s.log.Warn("could not check the recorded folder", "folder", id, "err", err)
		} else if ok {
			req["folderId"] = id
			s.markSorting(uuids)
			_ = send(out, appload.MessageSortDocuments, req)
			return
		} else {
			// The user deleted or moved it. Falling back to the name, and then
			// to creating, is what keeps a deleted folder from orphaning the
			// series.
			s.log.Info("the recorded series folder has gone", "folder", id, "series", seriesID)
		}
	}

	if child, ok, err := s.library.Child(ctx, comics.FolderID, name); err != nil {
		s.log.Warn("could not look for the series folder", "name", name, "err", err)
	} else if ok {
		req["folderId"] = child.ID
	}

	s.markSorting(uuids)
	_ = send(out, appload.MessageSortDocuments, req)
}

// documentsSorted records what the frontend managed to do.
//
// Only documents whose parent actually changed are recorded as being in the
// folder: the frontend reads each one back, and a call that returned without
// moving anything must not leave the store claiming otherwise. A record that
// says the wrong folder is worse than one that says Comics, because the next
// download believes it.
func (s *Service) documentsSorted(req sortedRequest) error {
	// Whatever the answer says, these documents are no longer being sorted.
	defer s.finishSorting(req.DocumentUUIDs)

	switch {
	case len(req.Moved) == 0:
		s.log.Info("a download was left in the Comics folder",
			"series", req.SeriesID, "folder", req.FolderName, "why", req.Detail,
			"documents", len(req.DocumentUUIDs))
		return nil
	case req.FolderID == "":
		// Moved somewhere the frontend cannot name. Nothing to record, and
		// nothing to be done about it from here.
		s.log.Warn("documents moved into an unnamed folder", "series", req.SeriesID)
		return nil
	}

	s.rememberFolder(req.Moved, req.FolderID, []string{library.ComicsFolder, req.FolderName})
	s.log.Info("a download was sorted into its series folder",
		"series", req.SeriesID, "folder", req.FolderName, "id", req.FolderID,
		"moved", len(req.Moved), "of", len(req.DocumentUUIDs), "created", req.Created,
		"detail", req.Detail)
	return nil
}

// rememberFolder writes the folder onto every record naming one of these
// documents.
//
// Per document rather than per series: a volume too big for the upload cap is
// several documents with several records, and each one holds its own copy of
// where it is.
func (s *Service) rememberFolder(uuids []string, folderID string, path []string) {
	if s.libStore == nil || folderID == "" {
		return
	}
	want := make(map[string]bool, len(uuids))
	for _, id := range uuids {
		want[id] = true
	}

	for _, rec := range s.libStore.List() {
		if !want[rec.DocumentUUID] {
			continue
		}
		if rec.FolderUUID == folderID && len(rec.FolderPath) == len(path) {
			continue
		}
		rec.FolderUUID = folderID
		rec.FolderPath = append([]string(nil), path...)
		if err := s.libStore.Put(rec); err != nil {
			s.log.Warn("could not record where a volume ended up",
				"document", rec.DocumentUUID, "err", err)
		}
	}
}

// recordedFolder is the folder id this (source, series) was last put in, or "".
//
// Keyed by source *and* series on purpose: two series with the same title from
// different sources would share a folder if they were matched on the name
// alone, and the id is what keeps them apart once each has one.
func (s *Service) recordedFolder(sourceID, seriesID string) string {
	if s.libStore == nil {
		return ""
	}
	for _, rec := range s.libStore.List() {
		if rec.Source != sourceID || rec.Series != seriesID {
			continue
		}
		// Only a folder *inside* Comics counts. A record from before this
		// existed points straight at Comics, which is not a series folder.
		if rec.FolderUUID != "" && len(rec.FolderPath) == 2 {
			return rec.FolderUUID
		}
	}
	return ""
}

// folderName is the series title as the folder will be called.
//
// It mirrors Sorting.js's folderName, because the backend looks a folder up by
// the name the frontend would create. Two implementations of one rule is a
// thing to be uncomfortable about; the alternative is the frontend asking the
// backend what to call a folder it is about to create, which is a round trip
// for a string.
func folderName(title string) string {
	s := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '\n', '\r', '\t':
			return ' '
		}
		return r
	}, title)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxFolderName {
		s = strings.TrimSpace(s[:maxFolderName])
	}
	return s
}

// resortDelay is how long one group's answer is waited for before the pass
// moves on.
//
// Long enough for a create and a move on a busy device, short enough that a
// frontend which has gone away without saying so does not hold the pass open.
// Nothing is lost by giving up early: the record stays unsorted and the next
// attach tries again, which is the whole design.
const resortDelay = 30 * time.Second

// resortOnAttach files the downloads that finished while nobody was listening.
//
// # Why this exists at all
//
// Sorting is done by the frontend, so a download that finishes while the
// frontend is away is never sorted: the volume stays in Comics and no later
// event goes looking for it. Attach is the moment the capability comes back,
// and it is the only moment that reliably coincides with that having happened.
//
// **No timer, and no polling.** The same reasoning as the cache control having
// no schedule: this is tidying, and a background task that rearranges the
// user's library on a clock is a thing that moves their documents while they
// are reading. The app being opened is the event.
//
// # What qualifies, and why both conditions are needed
//
//  1. **Never sorted.** A record with a series folder recorded against it is
//     off limits for good. A document we once filed that is sitting in Comics
//     now is a document the *user* moved back, and filing it again on every
//     attach would be Quire overruling them — quietly, repeatedly, and in a way
//     they cannot switch off.
//  2. **Still in Comics right now**, read from the library rather than from the
//     record. The record says where the volume landed at upload time, not where
//     it is; the user may have put it anywhere since.
//
// # The subtle way this could eat itself
//
// A frontend whose bridge failed to load answers with `moved: []`, and
// documentsSorted records nothing for that — deliberately. If it *did* record a
// folder, condition 1 would disqualify the record for good on the strength of a
// failure, and the volume would sit in Comics forever with Quire believing it
// had been filed. That is why the not-moved case writes nothing, and why there
// is a test holding it there.
//
// It runs on the background tracker, so it is cancellable and Close waits for
// it (PLAN §12.4's shutdown path), and one group at a time so a library that
// has been offline for a week does not open with forty folder creations at
// once.
func (s *Service) resortOnAttach(ctx context.Context, out Sender) {
	if s.library == nil || s.libStore == nil {
		return
	}

	comics, err := s.library.Resolve(ctx, library.ComicsFolder)
	if err != nil || !comics.Complete() {
		// No Comics folder, or no way to ask. Nothing here is urgent enough to
		// be worth saying to anyone: the next download creates the folder, and
		// the next attach tries this again.
		return
	}

	// One listing for the whole pass, not one per record.
	entries, err := s.library.List(ctx, comics.FolderID)
	if err != nil {
		s.log.Info("could not list the Comics folder on attach", "err", err)
		return
	}
	inComics := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Type == library.Document {
			inComics[e.ID] = true
		}
	}

	// Grouped by (source, series): one folder serves all of a series'
	// documents, and one selection moves them, exactly as a fresh download
	// does.
	type group struct {
		source, series, title string
		uuids                 []string
	}
	var order []string
	groups := map[string]*group{}
	for _, rec := range s.libStore.List() {
		if sorted(rec) || !inComics[rec.DocumentUUID] {
			continue
		}
		if s.sortInFlight(rec.DocumentUUID) {
			// A download that has just finished is already having this done.
			// Both paths moving the same documents is the one way this pass
			// could make things worse rather than tidier.
			continue
		}
		key := rec.Source + "\x00" + rec.Series
		g, ok := groups[key]
		if !ok {
			g = &group{source: rec.Source, series: rec.Series}
			groups[key], order = g, append(order, key)
		}
		g.uuids = append(g.uuids, rec.DocumentUUID)
		if g.title == "" {
			g.title = seriesTitleOf(rec)
		}
	}
	if len(order) == 0 {
		return
	}

	s.log.Info("filing downloads that were never sorted", "series", len(order))

	for _, key := range order {
		if ctx.Err() != nil {
			// The frontend went away. A request nobody is there to answer is
			// the exact failure this pass exists to fix, so it stops rather
			// than shouting into a closed socket.
			s.log.Info("stopped filing early; the frontend went away")
			return
		}
		g := groups[key]
		s.askToSort(ctx, out, g.source, g.series, g.title, g.uuids, library.Placement{})

		// Read *after* the ask: the mark is made by the ask, and asking is also
		// where a group can be declined — no folder name, no library — in which
		// case there is no answer to wait for. Reading it first waits for
		// nothing and sends the whole library's worth of groups at once.
		done := s.pendingFor(g.uuids)
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return
		case <-time.After(resortDelay):
			s.log.Info("no answer about a filing", "series", g.series)
		}
	}
}

// pendingFor is the channel that closes when the answer to a sort arrives, or
// nil when there is nothing outstanding — because the group was declined, or
// because the answer got back before this was asked.
func (s *Service) pendingFor(uuids []string) chan struct{} {
	if len(uuids) == 0 {
		return nil
	}
	s.sortMu.Lock()
	defer s.sortMu.Unlock()
	return s.sorting[uuids[0]]
}

// sorted reports whether a record has a series folder recorded against it.
//
// A record from before sorting existed points at Comics itself, with a path of
// one name, and is not sorted. Two names mean Comics/<series>.
func sorted(rec library.Record) bool {
	return rec.FolderUUID != "" && len(rec.FolderPath) == 2
}

// seriesTitleOf is the series name for a record, or "" when it cannot be known.
//
// **The recorded title first.** The backend had it in hand at download time and
// now keeps it (library.Record.SeriesTitle), so the folder a re-sort asks for is
// named from the same string the fresh download would have used — which is what
// stops the two paths creating two folders for one series.
//
// **The filename, for records written before that field existed.** assemble
// composes "<Series> — Ch 0001", so the part before the em dash is the title.
// It is a parse of a filename and it is wrong for a series whose title contains
// an em dash of its own; that is exactly why the field exists, and why this is
// only the fallback.
//
// **No dash and no title, no folder.** Naming a folder after the whole document
// would put "Wandance — Ch 0001.pdf" on screen as a folder name, which is worse
// than leaving the volume in Comics: askToSort refuses an empty name, so
// returning one here is how this pass declines to guess.
func seriesTitleOf(rec library.Record) string {
	if title := folderName(rec.SeriesTitle); title != "" {
		return title
	}
	i := strings.Index(rec.VisibleName, " \u2014 ")
	if i <= 0 {
		return ""
	}
	return folderName(rec.VisibleName[:i])
}
