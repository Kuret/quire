package library

import (
	"context"
	"fmt"
	"strings"
)

// ComicsFolder is the top-level folder PLAN §6 M5 puts downloads in.
const ComicsFolder = "Comics"

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
	return fmt.Sprintf("The reMarkable has no folder %s yet. "+
		"Create it on the tablet under %s and the next download will go there. "+
		"Until then Quire saves into %s.",
		strings.Join(full, " → "), where, where)
}

// Resolve walks a folder path from the top level down, and reports how far it
// got.
//
// It does not create anything, because it cannot. xochitl's web interface has
// no folder-create route — the entire surface is /documents/, /download/ and
// /upload, confirmed by reading the routes out of the binary and by probing
// POST, PUT, PATCH and MKCOL on /documents/, all of which simply return the
// listing. A folder can only be made by writing a CollectionType record to
// disk, and xochitl will not notice that without a restart (see the package
// comment). So a missing folder is reported, not invented, and the volume goes
// into the deepest folder that does exist.
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
