package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/assemble"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/theme"
)

// The phases a download passes through. They are sent as data rather than as
// sentences the frontend has to recognise, but every one of them arrives with
// a Message that is already the sentence to show: PLAN §2 keeps the wording
// here.
const (
	phaseConfirm    = "confirm"
	phaseCancelled  = "cancelled"
	phaseQueued     = "queued"
	phasePreparing  = "preparing"
	phaseFetching   = "fetching"
	phaseAssembling = "assembling"
	phaseStoring    = "storing"
	phaseDone       = "done"
	phaseFailed     = "failed"
)

// downloadRequest is the MessageEnqueueDownload payload.
//
// VolumeID is the chapter the user tapped. It names the *document* that
// chapter belongs to, which by default (PLAN §6 M4, reversed 2026-09-16) is
// that chapter on its own, and is a whole volume when the request asks for one.
// The field keeps its name because it keys library.Record and renaming it would
// orphan every download already on the tablet.
type downloadRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`
	VolumeID string `json:"volumeId"`

	// Grouping is what this download wants: assemble.GroupingChapter, the
	// default, or assemble.GroupingVolume when the user tapped a row in the
	// volume view (PLAN §6 M4, revised 2026-09-16).
	//
	// It rides on the request rather than on the source because the question
	// it answers is situational — whether the user is about to be without a
	// connection — and not a property of a website. Empty means chapter, so an
	// older frontend that does not send it still gets the default.
	Grouping string `json:"grouping,omitempty"`

	// Confirmed is set on the second send, after the user has been told what
	// the volume covers. A tap that quietly queues ten chapters and a few
	// hundred megabytes is not the UI saying what it is doing (PLAN §6 M3), so
	// the first send answers with a question instead of starting work.
	Confirmed bool `json:"confirmed"`
}

// grouping is the request's grouping with the default filled in.
//
// Anything unrecognised resolves to one PDF per chapter rather than being
// refused: a bad value costs the user a download they asked for, and the
// default is the safe answer in every case.
func (r downloadRequest) grouping() string {
	if r.Grouping == assemble.GroupingVolume {
		return assemble.GroupingVolume
	}
	return assemble.GroupingChapter
}

// downloadProgress is the MessageDownloadProgress payload.
type downloadProgress struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`
	VolumeID string `json:"volumeId"`

	// Grouping echoes what the request asked for, so the frontend can put the
	// frame on the row that asked. A volume's ID is its first chapter's, so
	// without it a chapter download and a volume download starting at the same
	// chapter are indistinguishable — and a chapter finishing would light up
	// "Read" on a volume row that is not on the tablet.
	Grouping string `json:"grouping,omitempty"`

	Phase string `json:"phase"`

	// Message is ready to display, in plain language.
	Message string `json:"message"`

	Series string `json:"series,omitempty"`
	Title  string `json:"title,omitempty"`

	PagesDone   int   `json:"pagesDone,omitempty"`
	PagesTotal  int   `json:"pagesTotal,omitempty"`
	BytesStored int64 `json:"bytesStored,omitempty"`

	// PagesSplit and PagesFromSplit report vertical-scroll strip splitting
	// (PLAN §12.3): how many source images were cut up, and how many pages they
	// became. They are sent on the done phase, and only when splitting actually
	// happened — omitempty, so an ordinary manga download carries neither.
	//
	// This is the user's way of checking the guarantee we made them. The worry
	// that prompted the feature was "I'd hate for an actual manga to be
	// recognised as a webtoon and get weirdly split", and splitting that happens
	// silently makes that worry unanswerable: the only way to find out would be
	// to notice the reader looks wrong and guess at the cause.
	PagesSplit     int `json:"pagesSplit,omitempty"`
	PagesFromSplit int `json:"pagesFromSplit,omitempty"`

	// DocumentUUID is xochitl's handle for the finished document, on the done
	// phase. It is what M6 opens the stock reader with.
	DocumentUUID string   `json:"documentUuid,omitempty"`
	FolderPath   []string `json:"folderPath,omitempty"`

	// Note carries something the user should know but that did not stop the
	// download — a missing library folder, most of all.
	Note string `json:"note,omitempty"`

	// ChapterCount, FirstChapter and LastChapter describe what a volume covers.
	// They are sent with the confirm phase so the UI can show the shape of the
	// job without parsing Message.
	ChapterCount int    `json:"chapterCount,omitempty"`
	FirstChapter string `json:"firstChapter,omitempty"`
	LastChapter  string `json:"lastChapter,omitempty"`

	// VolumeLabel is the source's own volume label when it has one, so the UI
	// can say "Volume 3" where the site does.
	VolumeLabel string `json:"volumeLabel,omitempty"`
}

// downloadJob is one queued request and the connection that asked for it.
type downloadJob struct {
	out Sender
	req downloadRequest
}

// downloadKey identifies a download for cancelling. It is the request's three
// IDs and not the volume label, because the label is only known after the
// chapter list has been fetched — and the user may well want out before then.
type downloadKey struct {
	Source string
	Series string
	Volume string
}

func (r downloadRequest) key() downloadKey {
	return downloadKey{Source: r.SourceID, Series: r.SeriesID, Volume: r.VolumeID}
}

// cancelDownload stops a download, whether it is running or still queued.
//
// Cancellation is prompt because it cancels a context that is threaded all the
// way to the HTTP request for the current page. Polling a flag between pages
// would make cancel feel broken in exactly the case it matters — a page fetch
// that has stalled — since that is when the gap between pages is longest.
func (s *Service) cancelDownload(out Sender, req downloadRequest) error {
	key := req.key()

	s.dlMu.Lock()
	cancel, running := s.dlActive[key]
	if !running {
		// Still in the queue, or the user tapped Stop between the worker
		// picking it up and it registering. Mark it; the worker checks.
		if s.dlCancelled == nil {
			s.dlCancelled = map[downloadKey]bool{}
		}
		s.dlCancelled[key] = true
	}
	s.dlMu.Unlock()

	if running {
		cancel()
		// The download's own goroutine sends the cancelled message once it has
		// actually stopped, so the UI never shows "stopped" while work
		// continues.
		return nil
	}

	return send(out, appload.MessageDownloadProgress, downloadProgress{
		SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
		Grouping: req.grouping(),
		Phase:    phaseCancelled,
		Message:  "Stopped.",
	})
}

// takeCancelled reports whether this download was cancelled before it started,
// clearing the mark.
func (s *Service) takeCancelled(key downloadKey) bool {
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	if s.dlCancelled[key] {
		delete(s.dlCancelled, key)
		return true
	}
	return false
}

// beginDownload registers a cancellable context for a running download.
func (s *Service) beginDownload(ctx context.Context, key downloadKey) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)

	s.dlMu.Lock()
	if s.dlActive == nil {
		s.dlActive = map[downloadKey]context.CancelFunc{}
	}
	s.dlActive[key] = cancel
	s.dlMu.Unlock()

	return ctx, func() {
		s.dlMu.Lock()
		delete(s.dlActive, key)
		delete(s.dlCancelled, key)
		s.dlMu.Unlock()
		cancel()
	}
}

// claimChapters marks the page directories a running download is writing, and
// returns the release.
//
// Only a *running* download claims. A queued one has written nothing yet, so
// removing its pages first costs it a refetch and nothing else — resume simply
// finds them absent (PLAN §6 M4). A running one is the case that matters:
// taking the directory out from under a writer leaves it filling files that are
// no longer anywhere, and a volume assembled from whatever survived.
//
// **Claimed by directory, not by chapter id.** Deleting a download knows the
// chapter ids it is about; clearing the whole cache knows only what is on disk,
// and a slug cannot be turned back into the id it came from. The directory is
// the thing both of them can compare, so it is the thing that is claimed.
//
// Counted rather than flagged, because the same chapter can be claimed by the
// volume that contains it and by itself in the same session.
func (s *Service) claimChapters(seriesDir string, chapters []download.Chapter) func() {
	dirs := make([]string, 0, len(chapters))
	s.dlMu.Lock()
	if s.dlChapters == nil {
		s.dlChapters = map[string]int{}
	}
	for _, ch := range chapters {
		if ch.ID == "" {
			continue
		}
		dir := claimKey(download.ChapterDir(seriesDir, ch.ID))
		s.dlChapters[dir]++
		dirs = append(dirs, dir)
	}
	s.dlMu.Unlock()

	return func() {
		s.dlMu.Lock()
		defer s.dlMu.Unlock()
		for _, dir := range dirs {
			if s.dlChapters[dir] <= 1 {
				delete(s.dlChapters, dir)
				continue
			}
			s.dlChapters[dir]--
		}
	}
}

// chapterDirIsClaimed reports whether a download is writing into this directory
// right now.
func (s *Service) chapterDirIsClaimed(dir string) bool {
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	return s.dlChapters[claimKey(dir)] > 0
}

// claimKey is the comparable form of a path: absolute where it can be, cleaned
// either way, so two spellings of one directory are one claim.
func claimKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// enqueueDownload accepts a request and returns immediately.
//
// Downloads run one at a time. Two reasons, and they are independent: the
// device has ~2 GB of RAM shared with xochitl and assembly holds a volume in
// memory (docs/DEVICE-NOTES.md §10.3), and the upload endpoint's target folder
// is global mutable server state, so two uploads interleaving would put a
// volume in the wrong folder. The queue is what makes "Enqueue" in the message
// name true.
func (s *Service) enqueueDownload(ctx context.Context, out Sender, req downloadRequest) error {
	if s.library == nil || s.libStore == nil {
		return s.sendError(out, "unavailable",
			"This build of Quire cannot save to the reMarkable's library.")
	}
	if req.SourceID == "" || req.SeriesID == "" || req.VolumeID == "" {
		return s.sendError(out, "bad_request", "Quire needs a source, a series and a chapter to download.")
	}

	if !req.Confirmed {
		// Read-only, and off the download queue on purpose: asking what a
		// volume covers must not wait behind somebody else's download.
		s.goBackground(ctx, func(ctx context.Context) { s.askToConfirm(ctx, out, req) })
		return nil
	}

	if s.offerToQueue(ctx, out, req) {
		return nil
	}
	return s.sendError(out, "busy",
		"Quire is already busy with as many downloads as it will queue. Try again when one has finished.")
}

// offerToQueue puts one confirmed request on the queue and reports whether it
// fitted, telling the row it is queued when it did.
//
// It is separate from enqueueDownload's refusal because a *selection* refuses
// differently: one tap that does not fit is worth a sentence, but a selection
// of thirty against a queue of sixteen would send that same sentence fourteen
// times. The caller decides how to say no; this only decides whether there was
// room. See enqueueMany.
func (s *Service) offerToQueue(ctx context.Context, out Sender, req downloadRequest) bool {
	s.dlOnce.Do(func() {
		s.dlQueue = make(chan downloadJob, downloadQueueDepth)
		s.goBackground(ctx, s.downloadWorker)
	})

	select {
	case s.dlQueue <- downloadJob{out: out, req: req}:
		_ = send(out, appload.MessageDownloadProgress, downloadProgress{
			SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
			Grouping: req.grouping(),
			Phase:    phaseQueued,
			Message:  "Queued.",
		})
		return true
	default:
		return false
	}
}

// askToConfirm answers the first tap with what the volume actually is.
//
// A single-chapter volume is enqueued without asking: there is nothing to
// warn about, and a confirm step that always says "download this one chapter?"
// is a step the user learns to tap through without reading.
//
// Since PLAN §6 M4 was reversed on 2026-09-16 that is the *common* case, not
// an edge one — one PDF per chapter is the default — so this path is now
// mostly the early return below. It stays for the volume view: a tap there
// queues ten chapters and a few hundred megabytes, which is exactly what it
// was written for.
func (s *Service) askToConfirm(ctx context.Context, out Sender, req downloadRequest) {
	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
		Grouping: req.grouping()}

	th, src, err := s.themeFor(req.SourceID)
	if err != nil {
		_ = s.sendError(out, "not_found", plain(err))
		return
	}
	series, err := th.Series(ctx, src, req.SeriesID)
	if err != nil {
		_ = s.sendError(out, "series_failed", plain(err))
		return
	}
	chapters, err := th.Chapters(ctx, src, req.SeriesID)
	if err != nil {
		_ = s.sendError(out, "chapters_failed", plain(err))
		return
	}
	vol, ok := volumeContaining(series.Title, chapters, req.VolumeID, req.grouping())
	if !ok {
		_ = s.sendError(out, "not_found",
			fmt.Sprintf("That chapter is no longer in %s's chapter list.", series.Title))
		return
	}

	if len(vol.Chapters) <= 1 {
		req.Confirmed = true
		// Detached from *this* goroutine's context on purpose: the queue and
		// the download outlive the question that decided not to ask. Tying
		// them to the confirm step would cancel the download the moment the
		// check that started it returned. Close still stops them — the tracked
		// worker's context is a child of the service's (background.go).
		if err := s.enqueueDownload(context.WithoutCancel(ctx), out, req); err != nil {
			s.log.Warn("could not enqueue a single-chapter volume", "err", err)
		}
		return
	}

	first := chapterLabel(vol.Chapters[0])
	last := chapterLabel(vol.Chapters[len(vol.Chapters)-1])
	n := len(vol.Chapters)

	// Name the source's own volume when there is one: "Volume 3" is something
	// the reader recognises from the site. The chapters past the last label —
	// the ones no print edition has reached yet — are grouped by counting
	// instead, and saying so is better than calling them a volume number the
	// site has never used.
	var what string
	if vol.SourceLabelled {
		what = fmt.Sprintf("Volume %s of %s is %d chapters, %s to %s.", vol.Label, vol.Series, n, first, last)
	} else {
		what = fmt.Sprintf("%s hasn’t given these chapters a volume number yet, so Quire groups "+
			"them into runs of %d. This one is %s to %s.",
			vol.Series, assemble.DefaultChaptersPerVolume, first, last)
	}

	p.Phase = phaseConfirm
	p.Series, p.Title = vol.Series, vol.Title
	p.ChapterCount = n
	p.FirstChapter, p.LastChapter = first, last
	p.VolumeLabel = vol.Label
	p.Message = fmt.Sprintf("%s It becomes one file, so your place in the reader carries "+
		"across chapters. Download all %d?", what, n)
	_ = send(out, appload.MessageDownloadProgress, p)
}

// chapterLabel is how a chapter is named in a sentence to the user.
func chapterLabel(c assemble.Chapter) string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	if n := strings.TrimSpace(c.Number); n != "" {
		return "chapter " + n
	}
	return c.ID
}

// downloadQueueDepth is how many requests wait behind the one running. It is
// small on purpose: a deep queue on a tablet is a way to fill /home while the
// user is not looking.
const downloadQueueDepth = 16

func (s *Service) downloadWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.dlQueue:
			if s.takeCancelled(job.req.key()) {
				_ = send(job.out, appload.MessageDownloadProgress, downloadProgress{
					SourceID: job.req.SourceID, SeriesID: job.req.SeriesID,
					VolumeID: job.req.VolumeID, Grouping: job.req.grouping(),
					Phase:   phaseCancelled,
					Message: "Stopped before it started.",
				})
				continue
			}
			s.runDownload(ctx, job.out, job.req)
		}
	}
}

// runDownload is the whole path: chapter list → pages → images on disk → one
// PDF → the reMarkable library → a remembered UUID.
func (s *Service) runDownload(parent context.Context, out Sender, req downloadRequest) {
	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
		Grouping: req.grouping()}

	ctx, done := s.beginDownload(parent, req.key())
	defer done()

	// uploaded counts the parts of a split volume already in the library when a
	// cancel lands. They stay: each is a complete, correctly indexed document
	// with its own thumbnail, and xochitl's web interface has no delete route
	// to take one back with even if we wanted to (M5). Dropping the record
	// while leaving the document would be strictly worse — an orphan in the
	// user's library that Quire cannot account for.
	uploaded := 0

	stopped := func() {
		p.Phase = phaseCancelled
		switch {
		case uploaded == 1:
			p.Message = "Stopped. The part already saved is in your library."
		case uploaded > 1:
			p.Message = fmt.Sprintf("Stopped. The %d parts already saved are in your library.", uploaded)
		default:
			p.Message = "Stopped. The pages already downloaded are kept, so starting again will carry on from here."
		}
		s.log.Info("download cancelled", "source", req.SourceID, "series", req.SeriesID,
			"volume", req.VolumeID, "partsUploaded", uploaded)
		_ = send(out, appload.MessageDownloadProgress, p)
	}

	// cancelled distinguishes "the user pressed Stop" from "something broke".
	// parent.Err() being nil is the test: if the parent is gone the whole
	// backend is shutting down and there is nobody to tell.
	cancelled := func(err error) bool {
		return errors.Is(err, context.Canceled) && parent.Err() == nil
	}

	fail := func(format string, args ...any) {
		p.Phase = phaseFailed
		p.Message = fmt.Sprintf(format, args...)
		s.log.Warn("download failed", "source", req.SourceID, "series", req.SeriesID,
			"volume", req.VolumeID, "message", p.Message)
		_ = send(out, appload.MessageDownloadProgress, p)
	}
	step := func(phase, message string) {
		p.Phase, p.Message = phase, message
		_ = send(out, appload.MessageDownloadProgress, p)
	}

	step(phasePreparing, "Looking up the chapters…")

	th, src, err := s.themeFor(req.SourceID)
	if err != nil {
		fail("%s", plain(err))
		return
	}
	series, err := th.Series(ctx, src, req.SeriesID)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not read that series: %s", plain(err))
		return
	}
	chapters, err := th.Chapters(ctx, src, req.SeriesID)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not read the chapter list: %s", plain(err))
		return
	}

	vol, ok := volumeContaining(series.Title, chapters, req.VolumeID, req.grouping())
	if !ok {
		fail("That chapter is no longer in %s's chapter list.", series.Title)
		return
	}
	p.Series, p.Title = vol.Series, vol.Title
	if vol.OrderUnknown {
		// Say it before the work starts, not only at the end: the user is
		// about to watch a download they did not quite ask for.
		p.Note = UnorderedSeriesNote
	}

	// Page URLs come one chapter at a time, through the source's own policy —
	// so its allowedHosts apply, which is what lets a source whose images live
	// on a CDN (MangaDex serves from *.mangadex.network) work at all.
	step(phasePreparing, fmt.Sprintf("Finding the pages of %s…", vol.Title))
	chs := make([]download.Chapter, 0, len(vol.Chapters))
	for _, ch := range vol.Chapters {
		urls, err := th.Pages(ctx, src, ch.ID)
		if err != nil {
			if cancelled(err) {
				stopped()
				return
			}
			fail("Quire could not find the pages of %s: %s", ch.Title, plain(err))
			return
		}
		if len(urls) == 0 {
			fail("%s has no pages Quire can read.", ch.Title)
			return
		}
		// The Referer these page images will be fetched with, resolved here
		// because here is the only place the chapter is still in hand: the
		// queue fetches a whole volume and could only guess (PLAN §7.6). A
		// theme with nothing to name yields "", and the request then carries
		// no header at all.
		ref, err := theme.PageRefererFor(th, src, ch.ID)
		if err != nil {
			// A theme naming a page address Quire cannot use is a theme bug.
			// Log it and send nothing, which is the honest header for "we do
			// not know" — inventing a substitute is what §7.6 forbids.
			s.log.Warn("theme named an unusable page referer",
				"source", src.ID, "theme", src.Theme, "chapter", ch.ID, "err", err)
		}
		chs = append(chs, download.Chapter{
			ID: ch.ID, Title: ch.Title, Number: ch.Number, Volume: ch.Volume, PageURLs: urls,
			Referer: ref.String(),
		})
	}

	dir := filepath.Join(s.downloadDir, safeSegment(src.ID), safeSegment(req.SeriesID))

	// Held for the rest of the run, so a delete or a cache clear landing
	// mid-download leaves these pages alone and takes the rest.
	defer s.claimChapters(dir, chs)()

	// The pages already on disk are left exactly where they are. Resume works
	// by skipping files that exist (PLAN §6 M4), so cancelling at page 300 of
	// 325 and starting again must not refetch 300 pages. Cancel means stop,
	// not discard.
	pages, stats, err := s.downloadPages(ctx, out, src, dir, chs, &p)
	if err != nil {
		if cancelled(err) {
			stopped()
			return
		}
		fail("Quire could not download the pages: %s", plain(err))
		return
	}
	vol.Chapters = pages

	// xochitl refuses a multipart body of 100 MB or more, and usually by
	// resetting the connection most of the way through rather than answering
	// (PLAN §6 M4). So the budget decides the final shape of the volume, after
	// the pages are on disk and their real sizes are known.
	parts, err := splitToBudget(vol, s.uploadBudget())
	if err != nil {
		fail("Quire could not work out how large %s is: %s", vol.Title, plain(err))
		return
	}
	if len(parts) > 1 {
		p.Note = strings.TrimSpace(p.Note + " " + fmt.Sprintf(
			"%s is too big for the reMarkable to accept in one file, so Quire saved it as %d parts.",
			vol.Title, len(parts)))
	}

	var (
		lastResult library.Result
		lastPlace  library.Placement
		lastName   string
		totalPages int
		stored     []string
	)
	for _, part := range parts {
		if err := ctx.Err(); err != nil && cancelled(err) {
			stopped()
			return
		}
		label := part.Title
		if len(parts) > 1 {
			label = fmt.Sprintf("%s (%d of %d)", part.Title, part.Part, part.Parts)
		}

		step(phaseAssembling, fmt.Sprintf("Building the PDF of %s…", label))
		// assemble builds into a temp file and renames it into place only once
		// it is whole, so a cancellation here leaves no half-written PDF for
		// the library to find.
		manifest, err := assemble.Assemble(ctx, dir, part.Volume, assemble.DefaultOptions())
		if err != nil {
			if cancelled(err) {
				stopped()
				return
			}
			fail("Quire could not build the PDF: %s", plain(err))
			return
		}
		totalPages += manifest.PageCount
		p.PagesDone, p.PagesTotal = totalPages, totalPages
		p.BytesStored = stats.BytesStored

		step(phaseStoring, fmt.Sprintf("Putting %s in your reMarkable library…", label))
		res, place, err := s.storeVolume(ctx, part, dir, manifest)
		if err != nil {
			if cancelled(err) {
				stopped()
				return
			}
			fail("%s", plain(err))
			return
		}
		uploaded++
		lastResult, lastPlace, lastName = res, place, res.VisibleName

		rec := library.Record{
			Key:          library.Key{Source: src.ID, Series: req.SeriesID, Volume: part.Label},
			DocumentUUID: res.DocumentUUID,
			FolderUUID:   res.FolderUUID,
			FolderPath:   place.Path,
			VisibleName:  res.VisibleName,
			PDF:          assemble.PDFPath(dir, part.Slug()),
			Pages:        manifest.PageCount,
			Bytes:        manifest.Bytes,
			Chapters:     chapterIDs(part.Volume),
			Part:         part.Part,
			Parts:        part.Parts,
			StoredAt:     time.Now(),
		}
		if err := s.libStore.Put(rec); err != nil {
			// The document is on the tablet either way. Losing the UUID only
			// costs M6's "Read" button, so say so rather than calling the
			// whole download a failure.
			s.log.Error("could not remember the document UUID", "uuid", res.DocumentUUID, "err", err)
			p.Note = strings.TrimSpace(p.Note +
				" Quire could not remember this volume, so opening it from Quire may not work.")
		}
		s.log.Info("volume stored", "document", res.DocumentUUID, "folder", res.FolderUUID,
			"name", res.VisibleName, "pages", manifest.PageCount,
			"part", part.Part, "parts", part.Parts)
		stored = append(stored, res.DocumentUUID)
	}

	// Every part of a split volume is its own document, and they all belong in
	// the same folder — so they are sorted together, in one selection, after
	// the last of them is in the library (PLAN §6 M5, corrected 2026-09-17).
	// The download is already a success by this point; sorting can only make
	// the library tidier or leave it exactly as it has always been.
	s.askToSort(ctx, out, src.ID, req.SeriesID, series.Title, stored, lastPlace)

	p.DocumentUUID = lastResult.DocumentUUID
	p.FolderPath = lastPlace.Path
	p.Note = strings.TrimSpace(p.Note + " " + lastPlace.Remedy())

	where := "My Files"
	if len(lastPlace.Path) > 0 {
		where += " → " + strings.Join(lastPlace.Path, " → ")
	}
	p.Phase = phaseDone
	if len(parts) > 1 {
		p.Message = fmt.Sprintf("%s is in %s, in %d parts.", vol.Title, where, len(parts))
	} else {
		p.Message = fmt.Sprintf("%s is in %s.", lastName, where)
	}
	if line := splitSentence(stats); line != "" {
		p.PagesSplit, p.PagesFromSplit = stats.PagesSplit, stats.PagesFromSplit
		p.Message += " " + line
	}
	_ = send(out, appload.MessageDownloadProgress, p)
}

// downloadPages runs the page queue, streaming progress to the frontend.
func (s *Service) downloadPages(ctx context.Context, out Sender, src *theme.Source, dir string,
	chs []download.Chapter, p *downloadProgress) ([]assemble.Chapter, download.Stats, error) {

	opts := s.downloadOptions

	// The per-source strip-splitting override (PLAN §12.3). An unparseable
	// value cannot reach here — Registry.Validate refuses it when the source is
	// added or imported — so a bad one falls back to the default rather than
	// failing a download the user has already confirmed.
	if mode, err := imageproc.ParseSplitMode(src.SplitStrips); err != nil {
		s.log.Warn("ignoring an invalid splitStrips value", "source", src.ID, "value", src.SplitStrips)
	} else {
		opts.SplitStrips = mode
	}
	opts.OnSplit = func(url string, pages int) {
		s.log.Info("split a vertical-scroll strip", "source", src.ID, "url", url, "pages", pages)
	}

	opts.OnProgress = func(pr download.Progress) {
		p.Phase = phaseFetching
		p.PagesDone, p.PagesTotal = pr.PagesDone, pr.PagesTotal
		p.BytesStored = pr.BytesStored
		p.Message = fmt.Sprintf("Downloading page %d of %d…", pr.PagesDone, pr.PagesTotal)
		_ = send(out, appload.MessageDownloadProgress, *p)
	}
	opts.OnWarn = func(stored, threshold int64) {
		s.log.Warn("download is large", "bytes", stored, "threshold", threshold, "dir", dir)
	}

	q := download.New(&sourceFetcher{fetch: s.fetch, src: src}, opts)
	return q.Run(ctx, dir, chs)
}

// storeVolume uploads the assembled PDF and reports where it landed.
func (s *Service) storeVolume(ctx context.Context, vol volumePlan, dir string,
	manifest *assemble.Manifest) (library.Result, library.Placement, error) {

	// Uploading is deliberately not cancellable.
	//
	// Once the POST is on the wire the document either exists on the tablet or
	// does not, and the UUID is only knowable by reading the folder back
	// afterwards. Abandoning that read-back on a cancel leaves a document in
	// the user's library that Quire has no record of — and xochitl's web
	// interface has no delete route to take it back with. So a Stop that
	// arrives mid-upload is honoured at the *next part boundary* instead,
	// which costs a few seconds over loopback and cannot orphan anything.
	ctx, release := context.WithTimeout(context.WithoutCancel(ctx), 3*library.DefaultTimeout)
	defer release()

	if err := s.library.EnsureReachable(ctx); err != nil {
		return library.Result{}, library.Placement{}, err
	}
	// Flat into Comics, with the series in the document name. A per-series
	// subfolder is used when the user has made one, and never required: Quire
	// cannot create folders, so requiring one would be requiring the user to
	// do housekeeping before every new series.
	place, err := s.library.Place(ctx, vol.Series)
	if err != nil {
		return library.Result{}, library.Placement{}, err
	}
	if !place.Complete() {
		// Not a failure. The pages are fetched and the PDF is built; the
		// volume goes to the top of My Files and Placement.Remedy tells the
		// user how to make the folder for next time.
		s.log.Info("library folder missing", "missing", place.Missing, "using", place.FolderID)
	}

	f, err := os.Open(assemble.PDFPath(dir, vol.Slug()))
	if err != nil {
		return library.Result{}, place, fmt.Errorf("opening the assembled volume: %w", err)
	}
	defer f.Close()

	res, err := s.library.Upload(ctx, place.FolderID, documentName(vol, manifest), f)
	if err != nil {
		return library.Result{}, place, err
	}
	return res, place, nil
}

// documentName is what the document is called on the tablet:
// "<Series> — Ch 0012" per chapter, or "<Series> — Vol N" when the user has
// asked for volumes.
//
// The series is in the name because the library is flat (PLAN §6 M5): Comics
// holds documents from every series side by side, so a name that does not say
// which series it belongs to is useless the moment there are two. With one PDF
// per chapter as the default (§6 M4, reversed 2026-09-16) there are roughly ten
// times as many of them, which is why the chapter number is zero-padded — see
// assemble.ChapterDocumentLabel.
//
// xochitl appends ".pdf" when the uploaded filename does not end in it, so the
// suffix is here rather than left to chance: a name that already carries it is
// one the user can predict.
func documentName(vol volumePlan, manifest *assemble.Manifest) string {
	series := strings.TrimSpace(vol.Series)
	label := strings.TrimSpace(vol.Label)

	var name string
	switch {
	case vol.PerChapter:
		// Not a volume, and calling it "Vol 12" would say it was. The chapter
		// names itself, padded so the flat Comics folder sorts.
		name = strings.TrimSpace(vol.Title)
	case series != "" && label != "":
		name = fmt.Sprintf("%s — Vol %s", series, label)
	case series != "":
		name = series
	default:
		name = strings.TrimSpace(manifest.Title)
	}
	if name == "" {
		name = vol.Slug()
	}
	// A slash in a title would be read as a path separator by the multipart
	// filename, and the result is a document called whatever came after it.
	name = strings.NewReplacer("/", "-", "\\", "-").Replace(name)
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

// volumeContaining groups a series' chapters into volumes and returns the one
// holding chapterID, with no pages yet.
//
// The grouping is assemble.GroupIntoVolumes', unchanged, and it is applied to
// the chapter list in the order the theme gave it — which is the order the
// user is looking at in the chapter list, so the volume they get is the one
// the chapter they tapped appears to be in.
func volumeContaining(seriesTitle string, chapters []theme.Chapter, chapterID string,
	mode string) (volumePlan, bool) {

	for _, v := range groupVolumes(seriesTitle, chapters, mode) {
		for _, c := range v.Chapters {
			if c.ID == chapterID {
				return v, true
			}
		}
	}
	return volumePlan{}, false
}

// volumePlan is a volume plus how Quire arrived at it, which is what decides
// the wording the user sees.
type volumePlan struct {
	assemble.Volume

	// SourceLabelled is true when the volume carries the source's own label
	// ("3") rather than a number Quire made up by counting chapters. It is the
	// difference between "Volume 3", which the reader recognises from the
	// site, and "the second group of ten", which means nothing to anyone.
	SourceLabelled bool

	// PerChapter is set when this document holds exactly one chapter and is
	// named after it rather than after a volume. Since 2026-09-16 that is the
	// default (PLAN §6 M4), so it is no longer a sign that anything went
	// wrong — see OrderUnknown for the case that is.
	PerChapter bool

	// OrderUnknown is set when the theme could not establish a reading order.
	// It is separate from PerChapter because the two used to be the same
	// thing and are not any more: per chapter is now what the user asked for,
	// while an unknowable order is something we have to tell them about.
	// Conflating them would put the "we couldn't order this" note on every
	// ordinary download, which is a note nobody would read.
	OrderUnknown bool

	// Part and Parts are 1-based and both 0 when the volume was not split.
	// A volume splits when it would exceed xochitl's upload cap; see
	// splitToBudget.
	Part  int
	Parts int
}

// UnorderedSeriesNote explains a one-PDF-per-chapter download.
//
// A volume is a *run* of chapters. Building one from a list the theme admits it
// could not order produces a silently scrambled book — the exact failure PLAN
// §7.2's ordering contract exists to prevent, reintroduced one layer up. One
// file per chapter clutters the library, which is why §6 M4 argues against it
// in the normal case, but page order *within* a chapter comes from Pages() and
// is authoritative, so every file is at least correct. Refusing outright would
// be worse: the user would get nothing where we could have given them
// something true.
const UnorderedSeriesNote = "Quire couldn’t work out what order this series’ chapters go in, " +
	"so it saved this one as its own file rather than build a volume that might read back to " +
	"front. The pages inside it are in the right order."

// groupVolumes is the single place a series' chapter list becomes volumes.
//
// There is exactly one because the volume *label* is what a stored document is
// keyed by: if the chapter list were grouped one way when downloading and
// another when deciding which rows already have a document, the Read button
// would appear on the wrong chapters.
//
// The chapter list is used in the order the theme returned it, which PLAN §7.2
// requires to be ascending reading order. This deliberately does not sort: two
// places normalising order is how order drifts.
//
// mode is what the *download request* asked for — assemble.GroupingChapter,
// the default, or assemble.GroupingVolume. PLAN §6 M4 was revised on
// 2026-09-16: the source's volume labels are information, not an instruction,
// so a label decides nothing until the user picks the volume view.
//
// Two things override the request, both of them silently, because both are
// cases where the volume view was never offered in the first place and the
// request could only have come from an older frontend or a replayed message:
// an unknowable reading order, and a source that publishes no labels.
func groupVolumes(seriesTitle string, chapters []theme.Chapter, mode string) []volumePlan {
	if seriesTitle == "" {
		seriesTitle = "Series"
	}
	flat := flattenChapters(chapters)

	// An unknowable reading order overrides whatever was asked for. A volume
	// is a *run* of chapters, so building one from a list the theme admits it
	// could not order produces a silently scrambled book.
	if !theme.OrderIsKnown(chapters) {
		return perChapterVolumes(seriesTitle, flat, true)
	}
	if mode != assemble.GroupingVolume || !volumesAvailable(chapters) {
		return perChapterVolumes(seriesTitle, flat, false)
	}

	return labelledVolumes(seriesTitle, flat)
}

// labelledVolumes groups on the source's own volume labels, with runs of
// DefaultChaptersPerVolume across the stretch it has not labelled. It asks no
// questions: the caller has already decided this series has volumes worth
// building.
func labelledVolumes(seriesTitle string, flat []assemble.Chapter) []volumePlan {
	grouped := assemble.GroupIntoVolumes(seriesTitle, flat, assemble.DefaultChaptersPerVolume)
	plans := make([]volumePlan, 0, len(grouped))
	for _, v := range grouped {
		plans = append(plans, volumePlan{
			Volume:         v,
			SourceLabelled: len(v.Chapters) > 0 && v.Chapters[0].Volume != "",
		})
	}
	return plans
}

// volumeRow is one row of the chapter screen's volume view.
//
// Every string on it is finished (PLAN §2). What a volume *contains* is the
// whole reason the view is worth having — seven chapters is what tells the user
// whether it is worth taking before a journey — so Detail says so rather than
// leaving the frontend to pluralise a count.
type volumeRow struct {
	// ID is the chapter a download request should name. A request carries the
	// chapter the user tapped, and the backend works out the volume around it,
	// so any chapter in the volume would do; the first is the stable choice.
	ID string `json:"id"`

	Label  string `json:"label"`
	Title  string `json:"title"`
	Detail string `json:"detail"`

	ChapterCount int `json:"chapterCount"`

	// DocumentUUID is set when this whole volume is already one document on the
	// tablet, which is what turns the row's button into "Read". A volume whose
	// chapters are on the tablet as separate per-chapter files is *not* that
	// document, and the row goes on offering the volume.
	DocumentUUID string `json:"documentUuid,omitempty"`
}

// volumeRows is the chapter screen's second view, or nil when there is none.
//
// nil rather than an empty slice, and decided here rather than in QML, because
// the rule is about the data: see volumesAvailable.
func volumeRows(seriesTitle string, chapters []theme.Chapter,
	stored map[string]library.Record) []volumeRow {

	if !volumesAvailable(chapters) {
		return nil
	}

	plans := groupVolumes(seriesTitle, chapters, assemble.GroupingVolume)
	rows := make([]volumeRow, 0, len(plans))
	for _, p := range plans {
		if len(p.Chapters) == 0 {
			continue
		}
		first := chapterLabel(p.Chapters[0])
		last := chapterLabel(p.Chapters[len(p.Chapters)-1])
		n := len(p.Chapters)

		r := volumeRow{
			ID:           p.Chapters[0].ID,
			Label:        p.Label,
			ChapterCount: n,
		}
		if p.SourceLabelled {
			r.Title = fmt.Sprintf("Volume %s", p.Label)
		} else {
			// Quire counted these; calling them "Volume 4" would claim a number
			// the site has never used. They are the chapters past the last
			// label, which is where a series that is still running always ends.
			r.Title = fmt.Sprintf("%s to %s", first, last)
		}
		switch {
		case n == 1:
			r.Detail = fmt.Sprintf("1 chapter: %s", first)
		default:
			r.Detail = fmt.Sprintf("%d chapters, %s to %s", n, first, last)
		}
		r.DocumentUUID = volumeDocument(p, stored)
		rows = append(rows, r)
	}
	return rows
}

// volumeDocument is the document holding this whole volume, if one is on the
// tablet.
//
// Every chapter has to resolve to the same document. A volume whose chapters
// were downloaded one at a time is several documents, not this one, and
// offering "Read" on it would open whichever of them happened to be found
// first — a row that says it will open the volume and opens a chapter.
//
// A volume split into parts to fit the upload cap is likewise not one document,
// and its rows keep offering the download. The download itself is close to free
// in that case: §6 M4's resume skips page files already on disk, so the pages
// are not fetched twice.
func volumeDocument(p volumePlan, stored map[string]library.Record) string {
	if len(stored) == 0 {
		return ""
	}
	uuid := ""
	for _, c := range p.Chapters {
		rec, ok := stored[c.ID]
		if !ok || rec.DocumentUUID == "" {
			return ""
		}
		if uuid == "" {
			uuid = rec.DocumentUUID
			continue
		}
		if rec.DocumentUUID != uuid {
			return ""
		}
	}
	return uuid
}

// legacyGrouping redoes the grouping **the way it was done before 2026-09-16**,
// for the sole benefit of library records written back then.
//
// It is deliberately not groupVolumes. A record already on the tablet was
// written when the source's volume labels decided the grouping on their own —
// with no volume view to opt into and no check that the labels were worth
// offering — so re-deriving it through today's rules would look for "Vol 3"
// among documents grouped some other way, find nothing, and quietly stop
// offering Read on a volume sitting on the tablet. Changing how grouping is
// chosen must not orphan what is already downloaded.
//
// The one thing it keeps from today is the ordering contract, because that was
// true then too: a list whose order the theme could not establish was never
// assembled into a volume, so there is no legacy volume to find.
func legacyGrouping(seriesTitle string, chapters []theme.Chapter) []volumePlan {
	if seriesTitle == "" {
		seriesTitle = "Series"
	}
	flat := flattenChapters(chapters)
	if !theme.OrderIsKnown(chapters) {
		return perChapterVolumes(seriesTitle, flat, true)
	}
	return labelledVolumes(seriesTitle, flat)
}

// flattenChapters converts a theme's chapter list into the assembler's, in the
// order the theme gave it. PLAN §7.2 requires that order to be ascending
// reading order, and nothing here re-sorts: two places normalising order is how
// order drifts.
func flattenChapters(chapters []theme.Chapter) []assemble.Chapter {
	flat := make([]assemble.Chapter, 0, len(chapters))
	for _, c := range chapters {
		flat = append(flat, assemble.Chapter{
			ID:     c.ID,
			Title:  c.Title,
			Number: chapterNumber(c),
			Volume: c.Volume,
		})
	}
	return flat
}

// volumesAvailable reports whether this series has volumes worth offering as a
// view of its own (PLAN §6 M4, revised 2026-09-16).
//
// The affordance is decided from the data, not from a setting, because an empty
// tab is a worse answer than no tab. Three things have to hold:
//
//  1. **The reading order is known.** §7.2's contract: a list we could not
//     order must never be assembled into a multi-chapter PDF, so offering to
//     is offering a scrambled book.
//  2. **The source publishes at least one real label.** Numbers Quire made up
//     by counting to ten are not volumes, and "the second group of ten" means
//     nothing to a reader looking at a site that has no volumes.
//  3. **Grouping by those labels actually groups.** A series whose every
//     chapter carries a distinct label produces a volume view that is the
//     chapter list with different words on it — the same emptiness as an empty
//     tab, one step further in.
func volumesAvailable(chapters []theme.Chapter) bool {
	if len(chapters) == 0 || !theme.OrderIsKnown(chapters) {
		return false
	}
	labelled := false
	for _, c := range chapters {
		if strings.TrimSpace(c.Volume) != "" {
			labelled = true
			break
		}
	}
	if !labelled {
		return false
	}
	return len(assemble.GroupIntoVolumes("", flattenChapters(chapters),
		assemble.DefaultChaptersPerVolume)) < len(chapters)
}

// splitToBudget splits a downloaded volume into parts that each fit inside
// xochitl's upload cap, and returns the volume unchanged when it already does.
//
// **Between chapters first, mid-chapter only as a last resort.** A part
// boundary inside a chapter is a seam the reader trips over: you finish a page,
// the chapter is not over, and the rest is in a different document. Between
// chapters the seam lands where the source already put one. Evenly-sized parts
// are worth less than that, and the measured data says the cost is small —
// M4 measured ~307 KiB/page, so the 90 MB budget is about 300 pages while a
// chapter is typically 20–40. Lopsidedness is bounded by one chapter.
//
// A single chapter that is itself over budget cannot be helped that way, and
// refusing would leave the user with nothing, so that one chapter is split
// across parts. It takes roughly a 300-page chapter to get there.
func splitToBudget(vol volumePlan, budget int64) ([]volumePlan, error) {
	sizes := make([]int64, len(vol.Chapters))
	var total int64
	for i, ch := range vol.Chapters {
		n, err := chapterBytes(ch)
		if err != nil {
			return nil, err
		}
		sizes[i], total = n, total+n
	}
	if budget <= 0 || total <= budget {
		return []volumePlan{vol}, nil
	}

	var groups [][]assemble.Chapter
	var current []assemble.Chapter
	var currentBytes int64

	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
			current, currentBytes = nil, 0
		}
	}

	for i, ch := range vol.Chapters {
		if sizes[i] > budget {
			// This one chapter does not fit on its own. Everything queued so
			// far becomes a part, then the chapter is cut into page runs.
			flush()
			pieces, err := splitChapter(ch, budget)
			if err != nil {
				return nil, err
			}
			for _, piece := range pieces {
				groups = append(groups, []assemble.Chapter{piece})
			}
			continue
		}
		if currentBytes+sizes[i] > budget {
			flush()
		}
		current = append(current, ch)
		currentBytes += sizes[i]
	}
	flush()

	parts := make([]volumePlan, 0, len(groups))
	for i, g := range groups {
		part := vol
		part.Volume.Chapters = g
		part.Part, part.Parts = i+1, len(groups)
		// The label is the store key and the PDF's filename stem, so each part
		// needs its own. It is also what documentName turns into the name on
		// the tablet, which is why it reads the way PLAN §6 M4 asks:
		// "Vol 3 (part 1 of 2)".
		part.Volume.Label = fmt.Sprintf("%s (part %d of %d)", vol.Label, i+1, len(groups))
		part.Volume.Title = fmt.Sprintf("%s (part %d of %d)", vol.Title, i+1, len(groups))
		parts = append(parts, part)
	}
	return parts, nil
}

// splitChapter cuts one over-budget chapter into runs of pages that fit.
//
// The pieces keep the chapter's ID: it is the key of the chapter → (PDF, page
// offset) map, and a chapter that genuinely spans two documents should be
// findable from both. The map resolves to the first piece, which is where a
// reader opening that chapter wants to start.
func splitChapter(ch assemble.Chapter, budget int64) ([]assemble.Chapter, error) {
	var (
		pieces  []assemble.Chapter
		current []assemble.Page
		size    int64
	)
	for _, page := range ch.Pages {
		n, err := fileBytes(page.Path)
		if err != nil {
			return nil, err
		}
		if len(current) > 0 && size+n > budget {
			pieces = append(pieces, chapterPiece(ch, current, len(pieces)))
			current, size = nil, 0
		}
		current = append(current, page)
		size += n
	}
	if len(current) > 0 {
		pieces = append(pieces, chapterPiece(ch, current, len(pieces)))
	}
	return pieces, nil
}

func chapterPiece(ch assemble.Chapter, pages []assemble.Page, index int) assemble.Chapter {
	piece := ch
	// Page.Index is the 0-based position *within its chapter*, and assemble
	// rejects a chapter whose pages do not start at 0 and run in order. Each
	// piece is a chapter in its own PDF, so each is renumbered from 0; the file
	// paths are what carry the real reading order.
	piece.Pages = make([]assemble.Page, len(pages))
	for i, pg := range pages {
		pg.Index = i
		piece.Pages[i] = pg
	}
	if index > 0 {
		piece.Title = ch.Title + " (continued)"
	}
	return piece
}

func chapterBytes(ch assemble.Chapter) (int64, error) {
	var total int64
	for _, page := range ch.Pages {
		n, err := fileBytes(page.Path)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func fileBytes(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("measuring %s: %w", path, err)
	}
	return st.Size(), nil
}

// chapterIDs lists a volume's chapters in reading order, without repeating one
// that was cut across pieces.
func chapterIDs(vol assemble.Volume) []string {
	ids := make([]string, 0, len(vol.Chapters))
	seen := map[string]bool{}
	for _, ch := range vol.Chapters {
		if seen[ch.ID] {
			continue
		}
		seen[ch.ID] = true
		ids = append(ids, ch.ID)
	}
	return ids
}

// uploadBudget is the byte budget one document must fit in.
//
// It is deliberately not the download budget: that one fails a run that is
// taking too much disk, this one only decides where a volume is cut.
func (s *Service) uploadBudget() int64 {
	if s.uploadBudgetBytes > 0 {
		return s.uploadBudgetBytes
	}
	return library.UploadBudgetBytes
}

// perChapterVolumes is one chapter per PDF, each correct on its own.
//
// It is both the default grouping (PLAN §6 M4, reversed 2026-09-16) and the
// fallback for a series whose reading order the theme could not establish.
// orderUnknown says which, because only the second one owes the user an
// explanation.
func perChapterVolumes(seriesTitle string, chapters []assemble.Chapter, orderUnknown bool) []volumePlan {
	plans := make([]volumePlan, 0, len(chapters))
	for _, ch := range chapters {
		label := strings.TrimSpace(ch.Number)
		if label == "" {
			// The label is the store key and the PDF's filename stem, so it
			// has to be stable and unique per chapter. The chapter ID is both.
			label = safeSegment(ch.ID)
		}
		plans = append(plans, volumePlan{
			Volume: assemble.Volume{
				Series: seriesTitle,
				Label:  label,
				// The document's name, and what sorts it in a flat Comics
				// folder: "Snotgirl — Ch 0012.5". See
				// assemble.ChapterDocumentLabel for why it is padded.
				Title:    seriesTitle + " — " + assemble.ChapterDocumentLabel(ch),
				Chapters: []assemble.Chapter{ch},
			},
			PerChapter:   true,
			OrderUnknown: orderUnknown,
		})
	}
	return plans
}

// chapterNumber renders theme.Chapter's float number as the text assemble
// wants. -1 means the title carried no number at all.
func chapterNumber(c theme.Chapter) string {
	if c.Number < 0 {
		return ""
	}
	return strconv.FormatFloat(c.Number, 'f', -1, 64)
}

// splitSentence describes strip splitting in plain language, or returns "" when
// nothing was split.
//
// The empty case is the important half. A line that appears on every download
// is a line nobody reads, so an ordinary manga volume says nothing at all — the
// same rule as PLAN §12.2's empty summary. When it does appear it is the user's
// evidence that Quire cut something up, which is the only way they can check
// the promise that ordinary pages are left alone.
func splitSentence(stats download.Stats) string {
	if stats.PagesSplit <= 0 || stats.PagesFromSplit <= 0 {
		return ""
	}
	images := "tall images"
	if stats.PagesSplit == 1 {
		images = "tall image"
	}
	pages := "pages"
	if stats.PagesFromSplit == 1 {
		pages = "page"
	}
	return fmt.Sprintf("Quire split %d %s into %d %s.",
		stats.PagesSplit, images, stats.PagesFromSplit, pages)
}

var unsafeSegment = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// safeSegment turns an ID into one path component. Source and series IDs come
// from the site and can hold anything, including slashes.
func safeSegment(id string) string {
	s := strings.Trim(unsafeSegment.ReplaceAllString(id, "-"), "-.")
	if s == "" {
		s = "unnamed"
	}
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-.")
	}
	return s
}

// sourceFetcher adapts the guarded HTTP client to download.Fetcher.
//
// The source's own fetch.Policy is what makes this safe: the rate limit, the
// robots check, the SSRF guard and — the reason this is not just
// client.Get(url) — the source's allowedHosts, without which a page image on
// a CDN outside the source's registrable domain is refused.
type sourceFetcher struct {
	fetch theme.Fetcher
	src   *theme.Source
}

// Get fetches one page image, naming the chapter page the address came out of
// when there is one to name.
//
// referer arrives from the job rather than being worked out here, because here
// is exactly where it could not be worked out honestly: the queue serves a
// whole volume and the chapter is long gone. An empty referer yields the zero
// fetch.Referrer, which sends no header at all — PLAN §7.6's answer for "we do
// not know", and never a stand-in.
func (f *sourceFetcher) Get(ctx context.Context, rawurl, referer string) (io.ReadCloser, error) {
	pol, err := f.src.Policy()
	if err != nil {
		return nil, err
	}
	var from fetch.Referrer
	if referer != "" {
		// Already validated at enqueue time; re-derived here because the queue
		// carries the address, not the type.
		if from, err = fetch.PageReferrer(referer); err != nil {
			return nil, err
		}
	}
	resp, err := f.fetch.GetRetrievalFrom(ctx, pol, rawurl, from)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s answered %d", rawurl, resp.StatusCode)
	}
	return io.NopCloser(bytes.NewReader(resp.Body)), nil
}
