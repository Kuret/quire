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
}

// downloadJob is one queued request and the connection that asked for it.
type downloadJob struct {
	out Sender
	req downloadRequest
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

	p.Phase = phaseConfirm
	p.Series, p.Title = vol.Series, vol.Title
	p.ChapterCount = len(vol.Chapters)
	p.FirstChapter, p.LastChapter = first, last
	p.Message = fmt.Sprintf(
		"Volume %s of %s is %d chapters, %s to %s. Quire downloads a whole volume at a time, "+
			"so your place in the reader carries across chapters. Download all %d?",
		vol.Label, vol.Series, len(vol.Chapters), first, last, len(vol.Chapters))
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
			s.runDownload(ctx, job.out, job.req)
		}
	}
}

// runDownload is the whole path: chapter list → pages → images on disk → one
// PDF → the reMarkable library → a remembered UUID.
func (s *Service) runDownload(ctx context.Context, out Sender, req downloadRequest) {
	p := downloadProgress{SourceID: req.SourceID, SeriesID: req.SeriesID, VolumeID: req.VolumeID}

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
		fail("Quire could not read that series: %s", plain(err))
		return
	}
	chapters, err := th.Chapters(ctx, src, req.SeriesID)
	if err != nil {
		fail("Quire could not read the chapter list: %s", plain(err))
		return
	}

	vol, ok := volumeContaining(series.Title, chapters, req.VolumeID)
	if !ok {
		fail("That chapter is no longer in %s's chapter list.", series.Title)
		return
	}
	p.Series, p.Title = vol.Series, vol.Title

	// Page URLs come one chapter at a time, through the source's own policy —
	// so its allowedHosts apply, which is what lets a source whose images live
	// on a CDN (MangaDex serves from *.mangadex.network) work at all.
	step(phasePreparing, fmt.Sprintf("Finding the pages of %s…", vol.Title))
	chs := make([]download.Chapter, 0, len(vol.Chapters))
	for _, ch := range vol.Chapters {
		urls, err := th.Pages(ctx, src, ch.ID)
		if err != nil {
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

	pages, stats, err := s.downloadPages(ctx, out, src, dir, chs, &p)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fail("Download stopped.")
			return
		}
		fail("Quire could not download the pages: %s", plain(err))
		return
	}
	vol.Chapters = pages

	step(phaseAssembling, fmt.Sprintf("Building the PDF of %s…", vol.Title))
	manifest, err := assemble.Assemble(ctx, dir, vol, assemble.DefaultOptions())
	if err != nil {
		fail("Quire could not build the PDF: %s", plain(err))
		return
	}
	p.PagesDone, p.PagesTotal = manifest.PageCount, manifest.PageCount
	p.BytesStored = stats.BytesStored

	step(phaseStoring, "Putting it in your reMarkable library…")
	res, place, err := s.storeVolume(ctx, vol, dir, manifest)
	if err != nil {
		fail("%s", plain(err))
		return
	}
	p.DocumentUUID = res.DocumentUUID
	p.FolderPath = place.Path
	p.Note = place.Remedy()

	rec := library.Record{
		Key:          library.Key{Source: src.ID, Series: req.SeriesID, Volume: vol.Label},
		DocumentUUID: res.DocumentUUID,
		FolderUUID:   res.FolderUUID,
		FolderPath:   place.Path,
		VisibleName:  res.VisibleName,
		PDF:          assemble.PDFPath(dir, vol.Slug()),
		Pages:        manifest.PageCount,
		Bytes:        manifest.Bytes,
		StoredAt:     time.Now(),
	}
	if err := s.libStore.Put(rec); err != nil {
		// The document is on the tablet either way. Losing the UUID only
		// costs M6's "Read" button, so say so rather than calling the whole
		// download a failure.
		s.log.Error("could not remember the document UUID", "uuid", res.DocumentUUID, "err", err)
		p.Note = strings.TrimSpace(p.Note + " Quire could not remember this volume, so opening it from Quire may not work.")
	}

	where := "My Files"
	if len(place.Path) > 0 {
		where += " → " + strings.Join(place.Path, " → ")
	}
	p.Phase = phaseDone
	p.Message = fmt.Sprintf("%s is in %s.", res.VisibleName, where)
	s.log.Info("volume stored", "document", res.DocumentUUID, "folder", res.FolderUUID,
		"name", res.VisibleName, "pages", manifest.PageCount)
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
func (s *Service) storeVolume(ctx context.Context, vol assemble.Volume, dir string,
	manifest *assemble.Manifest) (library.Result, library.Placement, error) {

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
func documentName(vol assemble.Volume, manifest *assemble.Manifest) string {
	series := strings.TrimSpace(vol.Series)
	label := strings.TrimSpace(vol.Label)

	var name string
	switch {
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
func volumeContaining(seriesTitle string, chapters []theme.Chapter, chapterID string) (assemble.Volume, bool) {
	if seriesTitle == "" {
		seriesTitle = "Series"
	}
	flat := make([]assemble.Chapter, 0, len(chapters))
	for _, c := range chapters {
		flat = append(flat, assemble.Chapter{
			ID:     c.ID,
			Title:  c.Title,
			Number: chapterNumber(c),
		})
	}
	for _, v := range assemble.GroupIntoVolumes(seriesTitle, flat, 0) {
		for _, c := range v.Chapters {
			if c.ID == chapterID {
				return v, true
			}
		}
	}
	return assemble.Volume{}, false
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
