package library

import (
	"context"
	"fmt"
	"strings"
)

// ComicsFolder is the folder PLAN §6 M5 puts downloads in.
//
// A volume is uploaded into Comics and then *moved* into a per-series folder by
// the frontend (PLAN §6 M5, corrected 2026-09-17: xochitl's QML creates folders
// and moves documents, even though its web interface cannot — see Resolve).
// Uploading flat and sorting afterwards keeps this path exactly as it was, so a
// future OS that closes the QML door leaves downloads landing in Comics rather
// than failing.
//
// A per-series subfolder that already exists is used directly by Place, which
// is both the user-made case this was written for and, now, the second and
// every later download of a series Quire has sorted before.
const ComicsFolder = "Comics"

// ComicsRemedy is what the user is told when there is no Comics folder and
// Quire could not make one.
//
// It is no longer the first thing they see: since 2026-09-17 Quire creates the
// folder itself, and this is the fallback for a creation that failed. The words
// are unchanged because they are still the right words — this is still
// something the user can do on the tablet in a few seconds — and because the
// path that reaches them is still a path where nothing else will.
const ComicsRemedy = "Quire keeps downloaded comics in a folder called " +
	"“Comics” on your reMarkable, and there isn’t one yet. On the tablet, in " +
	"My Files, make a folder called Comics and Quire will use it from then on. " +
	"Until then downloads go to the top of My Files."

// BooksFolder is where a downloaded book goes, flat — never a per-title
// subfolder.
//
// A comic gets a per-series subfolder because one series can be dozens of
// chapters; a book is one document, and a folder holding one document each is
// not tidiness, it is a folder list as long as the library. So PlaceBook
// resolves exactly this one level and nothing under it.
const BooksFolder = "Books"

// BooksRemedy is what the user is told when there is no Books folder and Quire
// could not make one.
//
// Same shape as ComicsRemedy and for the same reason: it is no longer the
// first thing they see, since Quire creates the folder itself, but it is still
// the fallback for a creation that failed, and it is still something the user
// can do on the tablet in a few seconds.
const BooksRemedy = "Quire keeps downloaded books in a folder called " +
	"“Books” on your reMarkable, and there isn’t one yet. On the tablet, in " +
	"My Files, make a folder called Books and Quire will use it from then on. " +
	"Until then downloads go to the top of My Files."

// Placement is the answer to "where does this volume go".
type Placement struct {
	// FolderID is the deepest folder on the requested path that exists. It is
	// where the upload will actually land.
	FolderID string

	// Path is the names of the folders that were found, outermost first.
	Path []string

	// Missing is the names that do not exist, outermost first. It is empty
	// when the whole path resolved.
	Missing []string
}

// Complete reports whether the whole requested path exists.
func (p Placement) Complete() bool { return len(p.Missing) == 0 }

// Remedy is the plain-language instruction for the folders that are missing,
// or "" when nothing is.
//
// The wording lives here rather than in the UI for the same reason every other
// piece of user-facing text does: PLAN §2 makes the frontend a dumb view.
func (p Placement) Remedy() string {
	if p.Complete() {
		return ""
	}
	full := append(append([]string{}, p.Path...), p.Missing...)
	where := "My Files"
	if len(p.Path) > 0 {
		where = "My Files → " + strings.Join(p.Path, " → ")
	}
	// The one case the user actually meets: they have not made Comics, or
	// Books, yet. Place never reports anything else missing for a comic,
	// because the series subfolder is optional by design, and PlaceBook never
	// reports anything else at all, because a book has no second level.
	if len(p.Path) == 0 && len(p.Missing) == 1 {
		switch p.Missing[0] {
		case ComicsFolder:
			return ComicsRemedy
		case BooksFolder:
			return BooksRemedy
		}
	}
	return fmt.Sprintf("The reMarkable has no folder %s yet. "+
		"Create it on the tablet under %s and the next download will go there. "+
		"Until then Quire saves into %s.",
		strings.Join(full, " → "), where, where)
}

// Place decides where a volume of a series goes, and is the shape PLAN §6 M5
// settled on: flat into Comics, with a per-series subfolder used only if the
// user happens to have made one.
//
// If Comics itself is missing the volume goes to the top of My Files and
// Remedy says how to fix that for next time. It is never a refusal: by the time
// this is called the pages are fetched and the PDF is built, and a file in the
// wrong folder can be dragged into the right one — a refused download cannot.
func (l *Library) Place(ctx context.Context, series string) (Placement, error) {
	comics, err := l.Resolve(ctx, ComicsFolder)
	if err != nil {
		return Placement{}, err
	}
	if !comics.Complete() {
		return comics, nil
	}
	if strings.TrimSpace(series) == "" {
		return comics, nil
	}
	nested, err := l.Resolve(ctx, ComicsFolder, series)
	if err != nil {
		return Placement{}, err
	}
	if nested.Complete() {
		return nested, nil
	}
	return comics, nil
}

// PlaceBook decides where a downloaded book goes: flat into Books, and never
// into a per-title subfolder — a book has no chapters to collect, so there is
// nothing a subfolder would be for.
//
// It is Resolve of one name, not a parallel walk: Place's nesting (a second
// Resolve for the series, falling back to the first) exists only because a
// comic's subfolder is optional and worth preferring when it is there. A book
// has no second level to prefer, so there is nothing here to duplicate.
//
// If Books itself is missing the book goes to the top of My Files and Remedy
// says how to fix that for next time — the same non-refusal Place makes for a
// comic, for the same reason: the file is already fetched, and a file in the
// wrong place can be dragged into the right one.
func (l *Library) PlaceBook(ctx context.Context) (Placement, error) {
	return l.Resolve(ctx, BooksFolder)
}

// Resolve walks a folder path from the top level down, and reports how far it
// got.
//
// **It does not create anything, and that is still true of this package.**
// xochitl's web interface has no folder-create route — the entire surface is
// /documents/, /download/ and /upload, confirmed by reading the routes out of
// the binary and by probing POST, PUT, PATCH and MKCOL on /documents/, all of
// which simply return the listing. A folder written straight to disk as a
// CollectionType record is not noticed without a restart (see the package
// comment).
//
// **What changed on 2026-09-17 is who else can create one.** xochitl's own QML
// can: `Library.createCollectionWrapper(parentId, name)`, reachable from the
// frontend, proven on hardware (PLAN §6 M5). So a missing folder is still
// reported rather than invented *here*, and the frontend is asked — which is
// also why this file gained Child and ChildByID, since that QML enumerates
// nothing.
//
// Until the answer comes back, the volume goes into the deepest folder that
// does exist.
func (l *Library) Resolve(ctx context.Context, path ...string) (Placement, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	p := Placement{FolderID: RootID}
	for i, name := range path {
		entries, err := l.listLocked(ctx, p.FolderID)
		if err != nil {
			return Placement{}, err
		}
		child, ok := findFolder(entries, name)
		if !ok {
			p.Missing = append(p.Missing, path[i:]...)
			return p, nil
		}
		p.FolderID = child.ID
		p.Path = append(p.Path, child.VisibleName)
	}
	return p, nil
}

// Child finds the folder called name inside parentID.
//
// It exists because the *frontend* cannot: xochitl's QML can create a folder
// and move documents into one (PLAN §6 M5, corrected 2026-09-17) but nothing on
// it enumerates a folder's children — ten candidates were tried and none
// worked. Listing is the half only this side can do, so "is there already a
// folder for this series?" is asked here and the answer is handed over.
func (l *Library) Child(ctx context.Context, parentID, name string) (Entry, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := l.listLocked(ctx, parentID)
	if err != nil {
		return Entry{}, false, err
	}
	child, ok := findFolder(entries, name)
	return child, ok, nil
}

// ChildByID reports whether id is still a folder inside parentID.
//
// A recorded folder id is only useful while it resolves: the user can delete
// or move the folder Quire made, and a stale id must send the next download
// back to looking the folder up by name rather than to a folder that is not
// there.
func (l *Library) ChildByID(ctx context.Context, parentID, id string) (Entry, bool, error) {
	if id == "" {
		return Entry{}, false, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := l.listLocked(ctx, parentID)
	if err != nil {
		return Entry{}, false, err
	}
	for _, e := range entries {
		if e.Type == Collection && e.ID == id {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}

// findFolder matches a folder by visible name, case-insensitively.
//
// Case-insensitive because the user types the folder name on a tablet
// keyboard and "comics" is not a different folder from "Comics" to anyone but
// a computer. An exact match still wins, so a library holding both is
// resolved the way it looks.
func findFolder(entries []Entry, name string) (Entry, bool) {
	var fallback Entry
	var haveFallback bool
	for _, e := range entries {
		if e.Type != Collection {
			continue
		}
		if e.VisibleName == name {
			return e, true
		}
		if !haveFallback && strings.EqualFold(e.VisibleName, name) {
			fallback, haveFallback = e, true
		}
	}
	return fallback, haveFallback
}
