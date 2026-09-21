package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/theme"
)

// Downloading from a theme.FileTheme — a source whose "chapters" are finished
// files rather than page images (PLAN §7.2's interface, extended 2026-09-20).
//
// # Why this is a separate path and not a flag on the old one
//
// runDownload is the page pipeline: page URLs per chapter, the image queue,
// imageproc's resize and strip splitting, assemble's PDF, and a split to fit
// xochitl's upload cap. Every one of those steps exists to turn a set of images
// into a document. A book is a document already.
//
// **Verified on hardware 2026-09-20:** an epub POSTed to the device's /upload
// endpoint answers HTTP 201 and lands in the library with fileType "epub", and
// the stock reader paginates it natively. So there is nothing for the image
// pipeline to do here, and putting a book through it would at best waste
// minutes and at worst produce a PDF of nothing.
//
// What is *not* different, and must not become different: the bytes are fetched
// through the same guarded client as a page image, so PLAN §7.4's SSRF guard,
// rate limits, honest User-Agent, byte budget and response size cap apply to the
// transfer — the cap raised to fetch.FileRetrievalMaxResponseBytes by a named
// call, never removed;
// the document is placed and sorted the same way; the library record is the same
// record; cancellation is honoured throughout; and the downloaded overview lists
// it like anything else. A book is a document like any other once it has landed.

// runFileDownload is the whole file path: ask the source for the file, fetch it,
// put it in the library, remember it.
//
// It takes both th and ft because the two are used for different things: ft is
// the retrieval, th is everything theme.Theme already answers (the book's
// title, which names the document's folder).
func (s *Service) runFileDownload(ctx, parent context.Context, out Sender, req downloadRequest,
	th theme.Theme, ft theme.FileTheme, src *theme.Source) {

	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
		Grouping: req.grouping()}

	// The same helpers runDownload has, with one difference that matters: the
	// cancelled wording. "The pages already downloaded are kept, so starting
	// again will carry on from here" is true of a chapter of images and false of
	// a book — there is no partial file, nothing was written, and promising a
	// resume that cannot happen is worse than saying nothing.
	//
	// cancelled makes the same test runDownload does: a cancellation is the
	// user's Stop only while the parent is still alive; if it is gone the whole
	// backend is shutting down and there is nobody to tell.
	cancelled := func(err error) bool {
		return errors.Is(err, context.Canceled) && parent.Err() == nil
	}
	stopped := func() {
		p.Phase = phaseCancelled
		p.Message = "Stopped. Nothing was added to your library."
		s.log.Info("book download cancelled", "source", req.SourceID, "series", req.SeriesID,
			"release", req.VolumeID)
		_ = send(out, appload.MessageDownloadProgress, p)
	}
	fail := func(format string, args ...any) {
		p.Phase = phaseFailed
		p.Message = fmt.Sprintf(format, args...)
		s.log.Warn("book download failed", "source", req.SourceID, "series", req.SeriesID,
			"release", req.VolumeID, "message", p.Message)
		_ = send(out, appload.MessageDownloadProgress, p)
	}
	step := func(phase, message string) {
		p.Phase, p.Message = phase, message
		_ = send(out, appload.MessageDownloadProgress, p)
	}

	step(phasePreparing, "Looking this book up…")

	// The book's own metadata, for the document's series folder and for the
	// library record's title. Chapters is deliberately *not* called: for a file
	// theme it is the slow release search (36 seconds, measured 2026-09-20) and
	// Retrieve runs it again itself, fresh, because the release object it posts
	// back cannot be reconstructed from a cached copy.
	series, err := th.Series(ctx, src, req.SeriesID)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not read that book: %s", plain(err))
		return
	}
	p.Series, p.Title = series.Title, series.Title

	// Retrieve can legitimately take minutes: the instance searches its sources
	// in turn and its own release_search_timeout is 300 seconds. Its notes are
	// the only thing standing between the user and a bar that looks frozen, so
	// they are forwarded as they arrive rather than summarised at the end.
	step(phaseFetching, "Asking "+sourceLabel(src)+" for this book…")
	fileURL, name, err := ft.Retrieve(ctx, src, req.VolumeID, func(note string) {
		step(phaseFetching, progressSentence(note))
	})
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not get this book: %s", plain(err))
		return
	}

	// The transfer, through the guarded client — the whole reason FileTheme
	// hands back a URL instead of bytes (see its comment). It is a retrieval:
	// one file the user named, not a crawl. See fetchFile for the two ways it
	// differs from a page image, both of them about size and time.
	step(phaseFetching, fmt.Sprintf("Downloading %s…", name))
	file, err := s.fetchFile(ctx, src, fileURL)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not download the file: %s", plain(err))
		return
	}

	step(phaseStoring, fmt.Sprintf("Putting %s in your reMarkable library…", name))
	res, place, err := s.storeFile(ctx, name, file)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("%s", plain(err))
		return
	}

	rec := library.Record{
		// The release id is the volume key. It is what makes the record
		// findable, and unlike a chapter number it is unique among the releases
		// of one book — two epubs of the same book from two sources are two
		// downloads, and keying them both as "1" would make the second overwrite
		// the first's UUID.
		Key:          library.Key{Source: src.ID, Series: req.SeriesID, Volume: safeSegment(req.VolumeID)},
		DocumentUUID: res.DocumentUUID,
		FolderUUID:   res.FolderUUID,
		FolderPath:   place.Path,
		VisibleName:  res.VisibleName,
		SeriesTitle:  series.Title,
		// PDF is left empty: there is no file of Quire's on disk to reclaim.
		// The document is on the tablet and the bytes never touched the download
		// directory, which is why nothing here claims space back later.
		//
		// Pages likewise: an epub reflows, so it has no page count that would
		// still be true at the next font size. 0 reads as "not known", which is
		// the truth, and omitempty keeps it off the wire.
		Bytes: int64(len(file)),
		// The release this document is, so M6's "Read" lands on the row the user
		// tapped — storedVolumes maps chapter ids to records through this field.
		Chapters: []string{req.VolumeID},
		StoredAt: time.Now(),
	}
	if err := s.libStore.Put(rec); err != nil {
		s.log.Error("could not remember the document UUID", "uuid", res.DocumentUUID, "err", err)
		p.Note = strings.TrimSpace(p.Note +
			" Quire could not remember this book, so opening it from Quire may not work.")
	}
	s.log.Info("book stored", "document", res.DocumentUUID, "folder", res.FolderUUID,
		"name", res.VisibleName, "bytes", len(file))

	// A book has no per-title subfolder — see kindOf and library.PlaceBook —
	// so the only thing left to ask the frontend for is the Books folder
	// itself, and only when the upload could not land in it directly.
	s.askToFileBook(ctx, out, src.ID, req.SeriesID, []string{res.DocumentUUID}, place)

	p.DocumentUUID = res.DocumentUUID
	p.FolderPath = place.Path
	p.Note = strings.TrimSpace(p.Note + " " + place.Remedy())

	where := "My Files"
	if len(place.Path) > 0 {
		where += " → " + strings.Join(place.Path, " → ")
	}
	p.Phase = phaseDone
	p.Message = fmt.Sprintf("%s is in %s.", res.VisibleName, where)
	_ = send(out, appload.MessageDownloadProgress, p)
}

// fileRetriever is the one capability this path needs that theme.Fetcher does
// not offer: a retrieval with the long timeout and the file-sized response cap
// (fetch.Client.GetFileRetrieval).
//
// It is asserted rather than added to theme.Fetcher for the same reason
// shelfmark asserts jsonPoster: that interface is implemented by the real
// client and half a dozen test doubles, and widening it would make every one of
// them carry a method only this call will ever make.
//
// **There is no fallback to the ordinary retrieval.** Falling back would cap a
// book at DefaultMaxResponseBytes — 8 MiB — which is the exact bug this exists
// to fix, and it would do it silently, at the end of a download the user has
// waited minutes for. A fetcher that cannot do this gets a sentence saying so.
type fileRetriever interface {
	GetFileRetrieval(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error)
}

// fetchFile downloads the file the theme named, through the source's own policy.
//
// No Referer: the URL was handed over by the theme rather than parsed out of a
// page, so there is no page to name and PLAN §7.6's answer is to send no header
// at all.
//
// The size cap still applies — it is raised, not removed. A file past
// fetch.FileRetrievalMaxResponseBytes is refused here, in one sentence, rather
// than at the end of a 90 MB transfer the device would have reset anyway (see
// that constant).
func (s *Service) fetchFile(ctx context.Context, src *theme.Source, rawurl string) ([]byte, error) {
	r, ok := s.fetch.(fileRetriever)
	if !ok {
		return nil, fmt.Errorf("this build cannot download a whole file, only page images")
	}
	pol, err := src.Policy()
	if err != nil {
		return nil, err
	}
	resp, err := r.GetFileRetrieval(ctx, pol, rawurl, fetch.Referrer{})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s answered %d", rawurl, resp.StatusCode)
	}
	return resp.Body, nil
}

// storeFile uploads a finished file and reports where it landed.
//
// It is storeVolume for a document Quire did not build, and it is deliberately
// the same in every respect that is not about the PDF: the same
// not-cancellable-once-posted rule (see storeVolume for why abandoning the
// read-back orphans a document), the same timeout — except the folder
// placement, which is PlaceBook rather than Place, because a book has no
// per-title subfolder to prefer (see kindOf and library.PlaceBook).
//
// **The name is passed through verbatim.** library.Upload's own comment records
// that xochitl appends ".pdf" only when the uploaded filename has no extension,
// so "An Example Book (1970).epub" stays an epub — which is what makes the
// device's reader paginate it (measured 2026-09-20). Anything that "tidied" the
// extension here would turn a working book into a PDF that is not one.
func (s *Service) storeFile(ctx context.Context, name string, file []byte) (
	library.Result, library.Placement, error) {

	ctx, release := context.WithTimeout(context.WithoutCancel(ctx), 3*library.DefaultTimeout)
	defer release()

	if err := s.library.EnsureReachable(ctx); err != nil {
		return library.Result{}, library.Placement{}, err
	}
	place, err := s.library.PlaceBook(ctx)
	if err != nil {
		return library.Result{}, library.Placement{}, err
	}
	if !place.Complete() {
		s.log.Info("library folder missing", "missing", place.Missing, "using", place.FolderID)
	}

	res, err := s.library.Upload(ctx, place.FolderID, name, bytes.NewReader(file))
	if err != nil {
		return library.Result{}, place, err
	}
	return res, place, nil
}

// progressSentence turns a theme's progress note into a line to show.
//
// The notes are written as fragments — "looking for a source", "downloading —
// 31%" — because a theme should not have to guess how its caller presents them.
// This is the one place they become sentences, so the wording rule of PLAN §2
// is applied once rather than in every theme.
func progressSentence(note string) string {
	n := strings.TrimSpace(note)
	if n == "" {
		return "Working…"
	}
	r := []rune(n)
	r[0] = unicode.ToUpper(r[0])
	// Compared as a rune, not a byte: "…" is three bytes and its last one is
	// not what anybody would write in this set.
	if strings.ContainsRune(".!?…", r[len(r)-1]) {
		return string(r)
	}
	return string(r) + "…"
}

// sourceLabel is the source's name for a sentence, falling back to "the source"
// rather than to an id nobody typed.
func sourceLabel(src *theme.Source) string {
	if name := strings.TrimSpace(src.Name); name != "" {
		return name
	}
	return "the source"
}
