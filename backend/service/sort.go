package service

import (
	"context"
	"strings"

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
		_ = send(out, appload.MessageSortDocuments, req)
		return
	}

	req["createUnder"] = comics.FolderID

	if id := s.recordedFolder(src, seriesID); id != "" {
		if _, ok, err := s.library.ChildByID(ctx, comics.FolderID, id); err != nil {
			s.log.Warn("could not check the recorded folder", "folder", id, "err", err)
		} else if ok {
			req["folderId"] = id
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
