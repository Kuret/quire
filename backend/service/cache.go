package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/download"
)

// clearCacheRequest is the MessageClearCache payload.
type clearCacheRequest struct {
	// Confirmed is set on the second send. The first one asks: this is hundreds
	// of megabytes, and the only way back is to download it all again.
	Confirmed bool `json:"confirmed,omitempty"`
}

// cacheSweep is what one pass over the download cache did, or would do.
type cacheSweep struct {
	Freed int64
	Kept  int64
	// KeptDirs is how many chapter directories were left because a download is
	// writing into them.
	KeptDirs int
}

// sendCacheStatus answers with the size of the cache and a sentence for it.
//
// bytes and message stay exactly what they always were — the download
// cache's own size and a sentence about it alone — so a frontend built before
// the storage summary existed still reads a cache status the way it always
// has. savedBytes, freeBytes and storage ride alongside for the one that
// wants the fuller picture (PLAN §12.6).
func (s *Service) sendCacheStatus(out Sender, message string) error {
	size := s.cacheSize()
	if message == "" {
		message = cacheSizeSentence(size)
	}
	st := s.storageStatus()
	return send(out, appload.MessageCacheStatus, map[string]any{
		"bytes":      size,
		"message":    message,
		"savedBytes": st.SavedBytes,
		"freeBytes":  st.FreeBytes,
		"storage":    st.Message,
	})
}

// storageStatus is what Quire's own storage is holding, across everything —
// saved chapters and the download cache, for every source including private
// ones. PLAN §12.6: there is no private/ordinary split here, because this is
// a single number about disk, not a list of series.
type storageStatus struct {
	SavedBytes int64
	CacheBytes int64
	FreeBytes  int64
	// FreeKnown is false on a platform with no statfs, or when there is no
	// saved or download directory to ask about. The composed sentence omits
	// the free-space clause entirely rather than claim a number invented for
	// the occasion.
	FreeKnown bool
	// Message is the one backend-composed sentence (PLAN §2): what is using
	// the space, and — when it is known — how much is free.
	Message string
}

// storageStatus computes the current storageStatus. It is cheap enough to
// call on every request that wants it — a saved-chapter tree and a cache
// directory are the same walk sweepCache and treeSize already do, not a
// second index kept in memory to go stale.
func (s *Service) storageStatus() storageStatus {
	saved := s.savedSize()
	cache := s.cacheSize()

	free, freeErr := download.FreeSpace(s.freeSpaceRoot())
	st := storageStatus{
		SavedBytes: saved,
		CacheBytes: cache,
		FreeKnown:  freeErr == nil,
	}
	if st.FreeKnown {
		st.FreeBytes = free
	}
	st.Message = storageSentence(saved, cache, free, st.FreeKnown)
	return st
}

// savedSize is what Quire's own saved-chapter storage is holding right now,
// across every source — private ones included, since this is a disk figure
// and not a list a private source needs keeping off (PLAN §12.6).
func (s *Service) savedSize() int64 {
	if s.savedDir == "" {
		return 0
	}
	return treeSize(s.savedDir)
}

// freeSpaceRoot is the directory whose filesystem the free-space figure is
// measured on: the saved root when this build has one — /home on the device
// — falling back to the download cache's directory for a build with no
// "Saved in Quire" of its own. Empty when neither exists, which
// download.FreeSpace turns into "unknown" the same way a statfs failure
// would.
func (s *Service) freeSpaceRoot() string {
	if s.savedDir != "" {
		return s.savedDir
	}
	return s.downloadDir
}

// clearCache empties the download cache, after asking.
//
// # What it is for
//
// Page images outlive the download that fetched them, on purpose: PLAN §6 M4's
// resume skips files already on disk. Deleting a document reclaims the pages
// behind it (PLAN §12.4), which covers the ordinary case — but not every case,
// and the gaps are permanent:
//
//   - **Pages a delete had to skip.** A delete landing while that chapter is
//     downloading leaves its pages alone, and the record it would have been
//     found by is gone by then. No later delete will ever name them again.
//   - **Pages no record ever pointed at.** A download that failed, or was
//     cancelled before its document existed, leaves what it had fetched.
//
// Without a control, both sit there for good. The user asked for one — "will
// these files just stay in limbo indefinitely? Maybe we can add a clear cache
// option in the main menu?" — and this is it. There is no automatic clearing
// and no timer: a cache that empties itself is a download that vanished the
// night before a flight.
//
// # What it clears
//
// Everything in the download cache that nothing is writing to, not only the
// orphans. "Clear cache" should mean what it says, and a rule the user can
// predict beats a clever one that reclaims slightly more. It is safe in a way
// worth stating: the documents are on the tablet, in xochitl's library, wholly
// independent of these pages. The cost of clearing is a refetch, and only if
// the user deletes a download and wants it again.
//
// Nothing outside the download directory is touched — not the library record,
// not the source list, not the logs, not the covers.
func (s *Service) clearCache(out Sender, req clearCacheRequest) error {
	if s.downloadDir == "" {
		return s.sendCacheStatus(out, "This build of Quire keeps no download cache.")
	}

	if !req.Confirmed {
		size := s.cacheSize()
		if size == 0 {
			// Nothing to ask about. Answering with the state of the world is
			// more use than a question whose answer changes nothing.
			return s.sendCacheStatus(out, cacheSizeSentence(0))
		}
		return send(out, appload.MessageCacheConfirm, map[string]any{
			"bytes":   size,
			"message": clearCacheQuestion(size),
		})
	}

	swept := s.sweepCache()
	s.log.Info("cleared the download cache",
		"freedBytes", swept.Freed, "freedMiB", swept.Freed>>20,
		"keptBytes", swept.Kept, "keptMiB", swept.Kept>>20, "keptChapters", swept.KeptDirs)

	return s.sendCacheStatus(out, clearCacheOutcome(swept))
}

// cacheSize is what the download cache is holding right now.
func (s *Service) cacheSize() int64 {
	if s.downloadDir == "" {
		return 0
	}
	return treeSize(s.downloadDir)
}

// sweepCache removes every cached chapter directory and assembled file that no
// download is writing, and tidies the tree it leaves behind.
//
// It walks the cache rather than the library, because what it is for is exactly
// the files the library no longer knows about.
func (s *Service) sweepCache() cacheSweep {
	var swept cacheSweep

	root, err := filepath.Abs(filepath.Clean(s.downloadDir))
	if err != nil {
		s.log.Warn("could not resolve the downloads directory", "dir", s.downloadDir, "err", err)
		return swept
	}

	sources, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("could not read the download cache", "dir", root, "err", err)
		}
		return swept
	}

	for _, source := range sources {
		sourceDir := filepath.Join(root, source.Name())
		if !source.IsDir() {
			swept.Freed += s.removeUnderRoot(root, sourceDir)
			continue
		}

		series, err := os.ReadDir(sourceDir)
		if err != nil {
			s.log.Warn("could not read a cached source", "dir", sourceDir, "err", err)
			continue
		}
		for _, one := range series {
			seriesDir := filepath.Join(sourceDir, one.Name())
			if !one.IsDir() {
				swept.Freed += s.removeUnderRoot(root, seriesDir)
				continue
			}
			s.sweepSeries(root, seriesDir, &swept)
			pruneEmpty(seriesDir, root)
		}
		pruneEmpty(sourceDir, root)
	}
	return swept
}

// sweepSeries clears one series directory: its chapter directories, and the
// assembled PDFs and manifests sitting beside them.
func (s *Service) sweepSeries(root, seriesDir string, swept *cacheSweep) {
	entries, err := os.ReadDir(seriesDir)
	if err != nil {
		s.log.Warn("could not read a cached series", "dir", seriesDir, "err", err)
		return
	}

	for _, e := range entries {
		path := filepath.Join(seriesDir, e.Name())
		if e.IsDir() && s.chapterDirIsClaimed(path) {
			// A download is writing here. Counted rather than skipped silently,
			// because "cleared 592 MB" while quietly leaving 40 MB behind is the
			// kind of small lie that becomes a bug report.
			kept := treeSize(path)
			swept.Kept += kept
			swept.KeptDirs++
			s.log.Info("keeping a chapter a download is still writing", "dir", path, "bytes", kept)
			continue
		}
		swept.Freed += s.removeUnderRoot(root, path)
	}
}

// ---- the sentences ---------------------------------------------------------
//
// All of them are the backend's (PLAN §2). The view renders a size it is given
// and never formats one of its own, so "636 MB" is spelled one way in the whole
// application.

func cacheSizeSentence(size int64) string {
	if size == 0 {
		return "The download cache is empty."
	}
	return fmt.Sprintf("The download cache is holding %s of page images.", humanBytes(size))
}

func clearCacheQuestion(size int64) string {
	return fmt.Sprintf("Clear %s of cached page images? Nothing in your reMarkable’s library changes — "+
		"these are only the pages Quire kept so a repeated download could skip them.", humanBytes(size))
}

// storageSentence is the one sentence PLAN §12.6 asks for: what Quire's own
// storage is holding, and — when it is known — how much room is left.
//
//   - Both saved and cache non-zero: "Quire is using 1.4 GB — 1.1 GB of saved
//     chapters and 300 MB of download cache."
//   - A zero part is dropped rather than said as "0 bytes": "Quire is using
//     1.1 GB of saved chapters."
//   - Neither: "Quire isn’t using any storage yet."
//
// The free clause is appended only when it is known (see storageStatus), and
// omitted rather than guessed at when it is not — a platform with no statfs,
// or a build with neither a saved nor a download directory to ask about.
func storageSentence(saved, cache, free int64, freeKnown bool) string {
	var sentence string
	switch {
	case saved == 0 && cache == 0:
		sentence = "Quire isn’t using any storage yet."
	case saved > 0 && cache > 0:
		sentence = fmt.Sprintf("Quire is using %s — %s of saved chapters and %s of download cache.",
			humanBytes(saved+cache), humanBytes(saved), humanBytes(cache))
	case saved > 0:
		sentence = fmt.Sprintf("Quire is using %s of saved chapters.", humanBytes(saved))
	default:
		sentence = fmt.Sprintf("Quire is using %s of download cache.", humanBytes(cache))
	}
	if freeKnown {
		sentence += fmt.Sprintf(" %s free on this reMarkable.", humanBytes(free))
	}
	return sentence
}

func clearCacheOutcome(swept cacheSweep) string {
	switch {
	case swept.Freed == 0 && swept.KeptDirs == 0:
		return "There was nothing to clear."
	case swept.KeptDirs == 1:
		return fmt.Sprintf("Cleared %s. %s was left, because a download is still using it.",
			humanBytes(swept.Freed), humanBytes(swept.Kept))
	case swept.KeptDirs > 1:
		return fmt.Sprintf("Cleared %s. %s was left across %d chapters a download is still using.",
			humanBytes(swept.Freed), humanBytes(swept.Kept), swept.KeptDirs)
	default:
		return fmt.Sprintf("Cleared %s.", humanBytes(swept.Freed))
	}
}

// humanBytes is a size in the units a person uses, at one decimal place from a
// megabyte up. Bytes below a kilobyte are said exactly: "0.0 kB" for a stray
// 200-byte file reads like a rounding error rather than a fact.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0f kB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}
