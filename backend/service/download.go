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
// VolumeID is the chapter the user tapped. PLAN §6 M4 is emphatic that the
// unit of a download is a *volume* and never a single chapter — a library of
// chapter PDFs makes xochitl's reading position meaningless — so the chapter
// is used to pick the volume it belongs to, and the whole volume is fetched.
type downloadRequest struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`
	VolumeID string `json:"volumeId"`

	// Confirmed is set on the second send, after the user has been told what
	// the volume covers. A tap that quietly queues ten chapters and a few
	// hundred megabytes is not the UI saying what it is doing (PLAN §6 M3), so
	// the first send answers with a question instead of starting work.
	Confirmed bool `json:"confirmed"`
}

// downloadProgress is the MessageDownloadProgress payload.
type downloadProgress struct {
	SourceID string `json:"sourceId"`
	SeriesID string `json:"seriesId"`
	VolumeID string `json:"volumeId"`

	Phase string `json:"phase"`

	// Message is ready to display, in plain language.
	Message string `json:"message"`

	Series string `json:"series,omitempty"`
	Title  string `json:"title,omitempty"`

	PagesDone   int   `json:"pagesDone,omitempty"`
	PagesTotal  int   `json:"pagesTotal,omitempty"`
	BytesStored int64 `json:"bytesStored,omitempty"`

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
		Phase:   phaseCancelled,
		Message: "Stopped.",
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
		go s.askToConfirm(ctx, out, req)
		return nil
	}

	s.dlOnce.Do(func() {
		s.dlQueue = make(chan downloadJob, downloadQueueDepth)
		go s.downloadWorker(ctx)
	})

	select {
	case s.dlQueue <- downloadJob{out: out, req: req}:
		return send(out, appload.MessageDownloadProgress, downloadProgress{
			SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID,
			Phase:   phaseQueued,
			Message: "Queued.",
		})
	default:
		return s.sendError(out, "busy",
			"Quire is already busy with as many downloads as it will queue. Try again when one has finished.")
	}
}

// askToConfirm answers the first tap with what the volume actually is.
//
// A single-chapter volume is enqueued without asking: there is nothing to
// warn about, and a confirm step that always says "download this one chapter?"
// is a step the user learns to tap through without reading.
func (s *Service) askToConfirm(ctx context.Context, out Sender, req downloadRequest) {
	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID}

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
	vol, ok := volumeContaining(series.Title, chapters, req.VolumeID)
	if !ok {
		_ = s.sendError(out, "not_found",
			fmt.Sprintf("That chapter is no longer in %s's chapter list.", series.Title))
		return
	}

	if len(vol.Chapters) <= 1 {
		req.Confirmed = true
		if err := s.enqueueDownload(ctx, out, req); err != nil {
			s.log.Warn("could not enqueue a single-chapter volume", "err", err)
		}
		return
	}

	first := chapterLabel(vol.Chapters[0])
	last := chapterLabel(vol.Chapters[len(vol.Chapters)-1])
	n := len(vol.Chapters)

	// Name the source's own volume when there is one: "Volume 3" is something
	// the reader recognises from the site. A number Quire made up by counting
	// chapters is not, so that case says what it actually did instead.
	var what string
	if vol.SourceLabelled {
		what = fmt.Sprintf("Volume %s of %s is %d chapters, %s to %s.", vol.Label, vol.Series, n, first, last)
	} else {
		what = fmt.Sprintf("%s doesn’t number its volumes, so Quire groups it into runs of %d. "+
			"This one is %s to %s.", vol.Series, assemble.DefaultChaptersPerVolume, first, last)
	}

	p.Phase = phaseConfirm
	p.Series, p.Title = vol.Series, vol.Title
	p.ChapterCount = n
	p.FirstChapter, p.LastChapter = first, last
	p.VolumeLabel = vol.Label
	p.Message = fmt.Sprintf("%s Quire downloads a whole volume at a time, so your place in the "+
		"reader carries across chapters. Download all %d?", what, n)
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
					VolumeID: job.req.VolumeID,
					Phase:    phaseCancelled,
					Message:  "Stopped before it started.",
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
	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID}

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

	vol, ok := volumeContaining(series.Title, chapters, req.VolumeID)
	if !ok {
		fail("That chapter is no longer in %s's chapter list.", series.Title)
		return
	}
	p.Series, p.Title = vol.Series, vol.Title
	if vol.PerChapter {
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
		chs = append(chs, download.Chapter{
			ID: ch.ID, Title: ch.Title, Number: ch.Number, Volume: ch.Volume, PageURLs: urls,
		})
	}

	dir := filepath.Join(s.downloadDir, safeSegment(src.ID), safeSegment(req.SeriesID))

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
	}

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
	_ = send(out, appload.MessageDownloadProgress, p)
}

// downloadPages runs the page queue, streaming progress to the frontend.
func (s *Service) downloadPages(ctx context.Context, out Sender, src *theme.Source, dir string,
	chs []download.Chapter, p *downloadProgress) ([]assemble.Chapter, download.Stats, error) {

	opts := s.downloadOptions
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

// documentName is what the volume is called on the tablet: "<Series> — Vol N".
//
// The series is in the name because the library is flat (PLAN §6 M5): Comics
// holds volumes from every series side by side, so a name that does not say
// which series it belongs to is useless the moment there are two.
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
		// names itself.
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
func volumeContaining(seriesTitle string, chapters []theme.Chapter, chapterID string) (volumePlan, bool) {
	for _, v := range groupVolumes(seriesTitle, chapters) {
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

	// PerChapter is set when the theme could not establish a reading order, so
	// this "volume" is a single chapter saved on its own.
	PerChapter bool

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
func groupVolumes(seriesTitle string, chapters []theme.Chapter) []volumePlan {
	if seriesTitle == "" {
		seriesTitle = "Series"
	}
	flat := make([]assemble.Chapter, 0, len(chapters))
	for _, c := range chapters {
		flat = append(flat, assemble.Chapter{
			ID:     c.ID,
			Title:  c.Title,
			Number: chapterNumber(c),
			Volume: c.Volume,
		})
	}

	if !theme.OrderIsKnown(chapters) {
		return perChapterVolumes(seriesTitle, flat)
	}

	// GroupIntoVolumes groups on the source's own Volume label wherever there
	// is one and falls back to runs of ten only where there is not — which is
	// what PLAN §6 M4 meant by "one PDF per volume" all along.
	grouped := assemble.GroupIntoVolumes(seriesTitle, flat, 0)
	plans := make([]volumePlan, 0, len(grouped))
	for _, v := range grouped {
		plans = append(plans, volumePlan{
			Volume:         v,
			SourceLabelled: len(v.Chapters) > 0 && v.Chapters[0].Volume != "",
		})
	}
	return plans
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

// perChapterVolumes is the fallback for a series whose reading order the theme
// could not establish: one chapter per PDF, each correct on its own.
func perChapterVolumes(seriesTitle string, chapters []assemble.Chapter) []volumePlan {
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
				Series:   seriesTitle,
				Label:    label,
				Title:    seriesTitle + " — " + chapterLabel(ch),
				Chapters: []assemble.Chapter{ch},
			},
			PerChapter: true,
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

func (f *sourceFetcher) Get(ctx context.Context, rawurl string) (io.ReadCloser, error) {
	pol, err := f.src.Policy()
	if err != nil {
		return nil, err
	}
	resp, err := f.fetch.GetRetrieval(ctx, pol, rawurl)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s answered %d", rawurl, resp.StatusCode)
	}
	return io.NopCloser(bytes.NewReader(resp.Body)), nil
}
