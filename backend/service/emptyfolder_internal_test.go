package service

import (
	"errors"
	"testing"

	"github.com/rickl/quire/backend/library"
)

// The feature: a folder that was listed and came back empty may go.
func TestAnEmptyFolderMayBeRemoved(t *testing.T) {
	ok, why := folderVerdict("lantern", "comics", nil, nil)
	if !ok {
		t.Errorf("an empty series folder was refused: %s", why)
	}
}

// **A listing that failed is not an empty folder.** The same rule as `checked`
// on the reconcile pass: an answer that could not be obtained must never be
// read as "nothing there".
func TestAFolderThatCouldNotBeListedIsLeftAlone(t *testing.T) {
	ok, why := folderVerdict("lantern", "comics", nil, errors.New("the web interface did not answer"))
	if ok {
		t.Error("a folder was deleted on the strength of a failed listing")
	}
	if why == "" {
		t.Error("no reason was given for leaving it")
	}
}

// A folder with something in it stays, whatever Quire deleted out of it. The
// user may have put something of their own there, and it is theirs.
func TestAFolderWithSomethingInItIsLeftAlone(t *testing.T) {
	entries := []library.Entry{{ID: "something", Parent: "lantern", VisibleName: "A note of mine"}}
	if ok, _ := folderVerdict("lantern", "comics", entries, nil); ok {
		t.Error("a folder with something in it was deleted")
	}
}

// **Never Comics.** A user who deletes their only series should still have the
// folder every future download goes into.
func TestComicsItselfIsNeverRemoved(t *testing.T) {
	if ok, _ := folderVerdict("comics", "comics", nil, nil); ok {
		t.Error("Comics was deleted")
	}
}

// And never the top level, which is what a record from before series folders
// existed points at.
func TestTheTopLevelIsNeverRemoved(t *testing.T) {
	if ok, _ := folderVerdict(library.RootID, "comics", nil, nil); ok {
		t.Error("the top level was deleted")
	}
}

// Without knowing which folder is Comics, the guard above cannot be applied at
// all, so nothing may be deleted.
func TestAFolderIsLeftAloneWhenComicsCannotBeResolved(t *testing.T) {
	if ok, _ := folderVerdict("lantern", "", nil, nil); ok {
		t.Error("a folder was deleted without establishing which folder is Comics")
	}
}

// Only a folder Quire recorded. Documents filed by hand are filed where the
// user wanted them.
func TestAnUnrecordedFolderIsLeftAlone(t *testing.T) {
	if ok, _ := folderVerdict("", "comics", nil, nil); ok {
		t.Error("an unrecorded folder was deleted")
	}
}

// The note claims the tidy-up only in the words of what happened, and names the
// folder when there is a name to use.
func TestTheFolderNoteNamesTheFolder(t *testing.T) {
	if got := SeriesFolderRemovedNote("The Lantern Keeper"); got == "" ||
		!contains(got, "The Lantern Keeper") || !contains(got, "empty") {
		t.Errorf("note %q", got)
	}
	if got := SeriesFolderRemovedNote(""); got == "" || contains(got, "“”") {
		t.Errorf("an unnamed folder produced %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
