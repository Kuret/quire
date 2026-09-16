package service

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/library"
)

// reclaimPages removes the cached page images behind a deleted document, and
// reports how many bytes came back.
//
// # Why a delete has to do this at all
//
// The pages a download fetched stay on disk after the PDF is built, and that is
// deliberate: PLAN §6 M4's resume works by skipping page files that are already
// there, so cancelling at page 300 of 325 and starting again costs 25 pages
// rather than 325. The cost is that the cache outlives the document. On the
// user's device it had reached 636 MB, and a chapter deleted and downloaded
// again came back in 25 seconds without touching the network — which looks like
// a fast download and is really a delete that did not delete.
//
// Asked, the user chose this: "delete should mean gone, and the space should
// come back", accepting that a re-download refetches.
//
// # The three things that make it more than a RemoveAll
//
//  1. **The directory name is not reinvented.** download.ChapterDir is the
//     function that created it. A slug computed a second way removes nothing,
//     or removes another chapter's pages; only the first is a quiet failure.
//  2. **A chapter can belong to more than one record.** A volume document and a
//     per-chapter document can cover the same ground, and so can two parts of a
//     split. Pages still named by a record that is staying are left alone.
//  3. **Nothing outside the downloads root is touched.** Every path is built
//     from the root, the source id and the record's own chapters — never from a
//     title or a visible name, which the *source* controls. Anything that
//     resolves outside the root is refused and logged rather than removed.
func (s *Service) reclaimPages(rec library.Record, remaining []library.Record) int64 {
	if s.downloadDir == "" {
		return 0
	}

	root, err := filepath.Abs(filepath.Clean(s.downloadDir))
	if err != nil {
		s.log.Warn("could not resolve the downloads directory", "dir", s.downloadDir, "err", err)
		return 0
	}

	seriesDir := filepath.Join(root, safeSegment(rec.Source), safeSegment(rec.Series))

	// Chapters still spoken for by a record that is staying.
	keep := map[string]bool{}
	for _, other := range remaining {
		if other.Source != rec.Source || other.Series != rec.Series {
			continue
		}
		for _, id := range other.Chapters {
			keep[id] = true
		}
	}

	var freed int64
	for _, id := range rec.Chapters {
		if id == "" {
			continue
		}
		if keep[id] {
			s.log.Debug("keeping cached pages another download still covers",
				"source", rec.Source, "chapter", id)
			continue
		}
		dir := download.ChapterDir(seriesDir, id)
		if s.chapterDirIsClaimed(dir) {
			// A running download is writing into this directory. Taking it away
			// mid-write leaves that download assembling a volume out of the
			// pages that happened to survive, which is a worse outcome than the
			// space staying used until the next delete.
			s.log.Info("leaving cached pages a download is still writing",
				"source", rec.Source, "chapter", id)
			continue
		}

		freed += s.removeUnderRoot(root, dir)
	}

	// The assembled PDF, and the manifest that is its sidecar: same directory,
	// same slug, written by assemble after the PDF was renamed into place. The
	// manifest path is derived from the record's own PDF path rather than
	// rebuilt from the volume's title, which is the source's to choose.
	//
	// Both go through the same root check as the pages. The record is Quire's
	// own, so a PDF outside the downloads directory should be impossible — and
	// "should be impossible" is exactly the kind of path worth refusing to
	// delete rather than trusting.
	if rec.PDF != "" {
		freed += s.removeUnderRoot(root, rec.PDF)
	}
	if manifest := manifestFor(rec.PDF); manifest != "" {
		freed += s.removeUnderRoot(root, manifest)
	}

	// An empty tree that never shrinks is the same complaint one directory up,
	// so the series and its source go when nothing is left in them.
	pruneEmpty(seriesDir, root)
	pruneEmpty(filepath.Dir(seriesDir), root)

	return freed
}

// manifestFor is the sidecar beside an assembled PDF, or "" when there is no
// PDF path to derive one from.
func manifestFor(pdf string) string {
	if pdf == "" || !strings.EqualFold(filepath.Ext(pdf), ".pdf") {
		return ""
	}
	return strings.TrimSuffix(pdf, filepath.Ext(pdf)) + assemble.ManifestSuffix
}

// removeUnderRoot deletes path and reports the bytes it held, refusing anything
// that does not resolve inside root.
//
// The check is on the resolved path, not on the string it was built from: a
// chapter id is a source's to choose, and "under the downloads directory" is
// the one property that has to survive whatever it chose.
func (s *Service) removeUnderRoot(root, path string) int64 {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		s.log.Warn("refusing to remove an unresolvable path", "path", path, "err", err)
		return 0
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		s.log.Error("refusing to remove a path outside the downloads directory",
			"path", abs, "root", root)
		return 0
	}

	size := treeSize(abs)
	if err := os.RemoveAll(abs); err != nil {
		s.log.Warn("could not remove cached pages", "path", abs, "err", err)
		return 0
	}
	return size
}

// treeSize adds up the files under path. A path that is already gone is zero,
// not an error: the cache is allowed to have been cleared by other means.
func treeSize(path string) int64 {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return total
	}
	return total
}

// pruneEmpty removes dir when it is empty and inside root. It is deliberately
// one level at a time and never recursive upward past the root.
func pruneEmpty(dir, root string) {
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(abs)
}
