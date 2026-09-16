// Package service is the backend half of the AppLoad conversation: it turns
// PLAN §7.1 messages into work on the theme engine, the probe, the source store
// and the cover cache, and turns the results back into messages.
//
// It exists so that PLAN §2's rule — "the QML frontend is a dumb view" — has
// somewhere to be true from. Every decision the UI appears to make (which
// verdict text to show, whether a source may be added, what a source list row
// says) is made here and sent as data.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/download"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/imageproc"
	"github.com/rickl/quire/backend/library"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
)

// Sender is the slice of appload.Conn the service uses. Sends are made from
// worker goroutines as well as from the message loop; appload.Conn serialises
// them behind its own mutex.
type Sender interface {
	Send(msgType int32, payload []byte) error
}

// Options configures a Service.
type Options struct {
	Store    *state.Store
	Registry *theme.Registry
	Fetcher  theme.Fetcher
	Covers   *covers.Cache
	Log      *slog.Logger

	// Library and LibraryStore are M5: the reMarkable library and the record
	// of what Quire has put in it. Both nil means downloads are refused with
	// a plain answer rather than half-done — the AppLoad PC emulator has no
	// xochitl to upload to.
	Library      *library.Library
	LibraryStore *library.Store

	// DownloadDir is where page images and assembled PDFs live. It must be
	// under /home: / has ~47 MB free (docs/DEVICE-NOTES.md §3.3).
	DownloadDir string

	// DownloadOptions tunes the page queue. The zero value is the defaults
	// backend/download documents.
	DownloadOptions download.Options

	// UploadBudgetBytes overrides the size one document may reach before a
	// volume is split into parts. Zero means library.UploadBudgetBytes, which
	// is the real device limit with margin. Tests set it small.
	UploadBudgetBytes int64

	// PreviousSessionCrashed says the last session did not end properly, so
	// the frontend can be told quietly on attach. See AbnormalExitNotice.
	PreviousSessionCrashed bool

	// Now is injectable for tests.
	Now func() time.Time

	// ProbeGuard overrides the probe's stage 1 guard. Nil means the real one.
	ProbeGuard prober.AddressGuard
}

// Service handles messages.
type Service struct {
	store  *state.Store
	reg    *theme.Registry
	fetch  theme.Fetcher
	covers *covers.Cache
	log    *slog.Logger
	now    func() time.Time
	guard  prober.AddressGuard

	library           *library.Library
	libStore          *library.Store
	downloadDir       string
	downloadOptions   download.Options
	uploadBudgetBytes int64

	previousSessionCrashed bool

	// bg owns every goroutine the service starts, so that Close can wait for
	// them. See background.go for why nothing here uses a bare `go`.
	bg *background

	// reprobes rate-limits the automatic re-probe; see maybeReprobe.
	reprobes reprobeState

	// watchMu guards PLAN §12.2's watched-series check: the single run in
	// flight, its cancel, and the one chapter list last served to the UI (which
	// is what a fresh watch is seeded from). See watch.go.
	watchMu       sync.Mutex
	watchRunning  bool
	watchCancel   context.CancelFunc
	lastDetailKey watchKey
	lastDetailIDs []string

	// pager caches the listing the frontend is paging through, so that a page
	// turn is not an HTTP request (PLAN §12.1). See paging.go.
	pagerMu  sync.Mutex
	pagerKey pagerKey
	pager    *seriesPager

	// coverMu guards the cover batch in flight. The frontend sends the set of
	// tiles now on screen; the previous set is cancelled, because a page turn
	// makes those requests work nobody will see (PLAN §12.1).
	coverMu     sync.Mutex
	coverBatch  context.Context
	coverCancel context.CancelFunc

	// coverRefs remembers, per source and cover URL, the page that URL was
	// parsed out of — PLAN §7.6's truthful `Referer`, for the hosts that answer
	// 403 without one. See rememberCoverReferrer for why it is remembered here
	// rather than asked for at fetch time.
	coverRefMu sync.Mutex
	coverRefs  map[string]string

	// dlQueue serialises downloads; see enqueueDownload for why there is
	// exactly one worker behind it.
	dlOnce  sync.Once
	dlQueue chan downloadJob

	// dlMu guards the cancellation registry. dlActive holds the cancel func of
	// the download currently running; dlCancelled remembers a Stop that
	// arrived while the request was still queued.
	dlMu        sync.Mutex
	dlActive    map[downloadKey]context.CancelFunc
	dlCancelled map[downloadKey]bool

	// mu guards the single in-flight probe. There is deliberately only one:
	// the wizard is a single screen, and a second probe started behind it would
	// interleave its progress messages with the first's.
	mu      sync.Mutex
	probing *probeSession
	drafts  map[string]*theme.Source
}

// New builds a Service.
func New(opts Options) *Service {
	s := &Service{
		store:  opts.Store,
		reg:    opts.Registry,
		fetch:  opts.Fetcher,
		covers: opts.Covers,
		log:    opts.Log,
		now:    opts.Now,
		guard:  opts.ProbeGuard,
		drafts: map[string]*theme.Source{},
		bg:     newBackground(),

		library:         opts.Library,
		libStore:        opts.LibraryStore,
		downloadDir:     opts.DownloadDir,
		downloadOptions: opts.DownloadOptions,

		uploadBudgetBytes: opts.UploadBudgetBytes,

		previousSessionCrashed: opts.PreviousSessionCrashed,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Handle answers one message. It reports whether the message was one of the
// service's; anything else is left to the caller.
//
// Work that touches the network runs on its own goroutine: the AppLoad message
// loop must stay free to receive, or a probe would make the app unable to
// answer its own questions.
func (s *Service) Handle(ctx context.Context, out Sender, msgType int32, payload []byte) (bool, error) {
	switch msgType {
	case appload.MessageListSources:
		return true, s.sendSources(out)

	case appload.MessageProbeSource:
		var req struct {
			URL string `json:"url"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		s.goBackground(ctx, func(ctx context.Context) { s.runProbe(ctx, out, req.URL) })
		return true, nil

	case appload.MessageProbeAnswer:
		var ans prober.Answer
		if err := decode(payload, &ans); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		s.answerProbe(ans)
		return true, nil

	case appload.MessageConfirmAddSource:
		var req struct {
			URL   string `json:"url"`
			Theme string `json:"theme"`
			Name  string `json:"name"`
			Lang  string `json:"lang"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.confirmAdd(out, req.URL, req.Theme, req.Name, req.Lang)

	case appload.MessageSetSourceEnabled:
		var req struct {
			SourceID string `json:"sourceId"`
			Enabled  bool   `json:"enabled"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		if err := s.store.SetEnabled(req.SourceID, req.Enabled); err != nil {
			return true, s.sendError(out, "not_found", err.Error())
		}
		return true, s.sendSources(out)

	case appload.MessageRemoveSource:
		var req struct {
			SourceID string `json:"sourceId"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		if err := s.store.Remove(req.SourceID); err != nil {
			return true, s.sendError(out, "not_found", err.Error())
		}
		// The thumbnails are ours and are worthless now. Downloaded volumes are
		// the user's documents by this point and are left alone.
		if s.covers != nil {
			if err := s.covers.Forget(req.SourceID); err != nil {
				s.log.Warn("could not drop cached covers", "source", req.SourceID, "err", err)
			}
		}
		// The cached listing belonged to a source that no longer exists.
		s.dropPagers()
		// The store drops this source's watched series with it (PLAN §12.2),
		// so the list on screen has to be told, or it keeps drawing rows for a
		// source that is gone.
		if err := s.sendWatchList(out); err != nil {
			return true, err
		}
		return true, s.sendSources(out)

	case appload.MessageSearch:
		var req struct {
			SourceID string `json:"sourceId"`
			Query    string `json:"query"`
			Page     int    `json:"page"`
			PageSize int    `json:"pageSize"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		s.goBackground(ctx, func(ctx context.Context) {
			s.runSearch(ctx, out, req.SourceID, req.Query, req.Page, req.PageSize)
		})
		return true, nil

	case appload.MessageBrowse:
		var req struct {
			SourceID string `json:"sourceId"`
			Page     int    `json:"page"`
			PageSize int    `json:"pageSize"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		// Browse is search with no query: on every theme we have, that is the
		// site's own recent/popular listing, which is also what PLAN §7.5 stage
		// 5 falls back to.
		s.goBackground(ctx, func(ctx context.Context) {
			s.runSearch(ctx, out, req.SourceID, "", req.Page, req.PageSize)
		})
		return true, nil

	case appload.MessageSeriesDetail:
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		s.goBackground(ctx, func(ctx context.Context) { s.runSeriesDetail(ctx, out, req.SourceID, req.SeriesID) })
		return true, nil

	case appload.MessageRequestCover:
		// The payload is the set of tiles *now on screen*. A single
		// {seriesId,url} is still accepted and means a set of one.
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			URL      string `json:"url"`
			Covers   []struct {
				SeriesID string `json:"seriesId"`
				URL      string `json:"url"`
			} `json:"covers"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		want := make([]coverWant, 0, len(req.Covers)+1)
		for _, c := range req.Covers {
			want = append(want, coverWant{SeriesID: c.SeriesID, URL: c.URL})
		}
		if len(want) == 0 && req.SeriesID != "" {
			want = append(want, coverWant{SeriesID: req.SeriesID, URL: req.URL})
		}
		s.runCoverBatch(ctx, out, req.SourceID, want)
		return true, nil

	case appload.MessageOpenInReader:
		var req openRequest
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.openInReader(out, req)

	case appload.MessageRenameSource:
		var req struct {
			SourceID string `json:"sourceId"`
			Name     string `json:"name"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		switch err := s.store.Rename(req.SourceID, req.Name); {
		case errors.Is(err, state.ErrBadName):
			return true, s.sendError(out, "bad_name",
				"A source needs a name, and it has to be shorter than 120 characters.")
		case err != nil:
			return true, s.sendError(out, "not_found", plain(err))
		}
		return true, s.sendSources(out)

	case appload.MessageSetSourceSplitStrips:
		var req struct {
			SourceID    string `json:"sourceId"`
			SplitStrips string `json:"splitStrips"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		switch err := s.store.SetSplitStrips(req.SourceID, req.SplitStrips); {
		case errors.Is(err, state.ErrBadSplitStrips):
			return true, s.sendError(out, "bad_request",
				"Splitting can be set to automatic, never or always.")
		case err != nil:
			return true, s.sendError(out, "not_found", plain(err))
		}
		return true, s.sendSources(out)

	case appload.MessageCancelDownload:
		var req downloadRequest
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.cancelDownload(out, req)

	case appload.MessageEnqueueDownload:
		var req downloadRequest
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.enqueueDownload(ctx, out, req)

	case robotsMessage:
		return s.handleRobots(out, payload)

	case appload.MessageWatchSeries:
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			Title    string `json:"title"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.watchSeries(out, req.SourceID, req.SeriesID, req.Title)

	case appload.MessageUnwatchSeries:
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.unwatchSeries(out, req.SourceID, req.SeriesID)

	case appload.MessageCheckWatched:
		// "Check now", so the per-source cooldown is overridden. An empty
		// payload means all of them; naming a series checks that one.
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
		}
		if len(payload) > 0 {
			if err := decode(payload, &req); err != nil {
				return true, s.sendError(out, "bad_request", err.Error())
			}
		}
		var only *watchKey
		if req.SourceID != "" && req.SeriesID != "" {
			only = &watchKey{SourceID: req.SourceID, SeriesID: req.SeriesID}
		}
		s.startWatchCheck(out, true, only)
		return true, nil
	}
	return false, nil
}

// --- sources ---------------------------------------------------------------

// sourceView is one row of the source list. It is a view rather than the stored
// entry because the UI has no business with overrides, selectors or a script.
type sourceView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Theme   string `json:"theme"`
	Lang    string `json:"lang"`
	Enabled bool   `json:"enabled"`

	// SplitStrips is the strip-splitting override (PLAN §12.3), always one of
	// "auto", "never" or "always" — never empty, even though the stored value
	// can be. Resolving "unset" to "auto" here rather than in the UI means the
	// control has a value to show on every row, and means one place decides
	// what absent means.
	SplitStrips string `json:"splitStrips"`

	// Status and StatusDetail are the last probe, in plain language. A source
	// that has never been probed says so rather than showing an empty row.
	Status       string `json:"status"`
	StatusDetail string `json:"statusDetail,omitempty"`
	StatusAt     string `json:"statusAt,omitempty"`
}

func (s *Service) sendSources(out Sender) error {
	list := s.store.List()
	views := make([]sourceView, 0, len(list))
	for _, src := range list {
		split := src.SplitStrips
		if split == "" {
			split = imageproc.SplitAuto.String()
		}
		v := sourceView{
			ID: src.ID, Name: src.Name, BaseURL: src.BaseURL,
			Theme: src.Theme, Lang: src.Lang, Enabled: src.IsEnabled(),
			SplitStrips: split,
			Status:      "Not checked yet",
		}
		if src.LastProbe != nil {
			v.Status = VerdictHeadline(src.LastProbe.Verdict)
			v.StatusDetail = src.LastProbe.Detail
			v.StatusAt = src.LastProbe.At.UTC().Format(time.RFC3339)
		}
		views = append(views, v)
	}
	return send(out, appload.MessageSources, map[string]any{"sources": views})
}

// VerdictHeadline is the one-line form of a verdict, in plain language. PLAN §6
// M3 is explicit that a verdict is never surfaced as a code, and having exactly
// one place that translates them keeps the wording consistent between the probe
// wizard and the source list.
func VerdictHeadline(verdict string) string {
	switch verdict {
	case theme.VerdictOK:
		return "Working"
	case theme.VerdictPartial:
		return "Working with gaps"
	case theme.VerdictUnrecognised:
		return "Layout not recognised"
	case theme.VerdictBlockedChallenge:
		return "Blocked by a browser challenge"
	case theme.VerdictRobotsDenied:
		return "The site asks tools not to read it"
	case theme.VerdictUnreachable:
		return "Couldn't be reached"
	case theme.VerdictInvalidURL:
		return "Address not usable"
	case theme.VerdictBlockedAddress:
		return "Address refused"
	case "":
		return "Not checked yet"
	}
	return "Not checked yet"
}

func (s *Service) confirmAdd(out Sender, url, themeID, name, lang string) error {
	s.mu.Lock()
	draft := s.drafts[strings.TrimSuffix(url, "/")]
	s.mu.Unlock()

	if draft == nil {
		// Nothing may be added that was not probed. This is the sharp end of
		// PLAN §7.5: a source arriving without a verdict would be exactly the
		// "add it anyway" path stage 3 exists to prevent.
		return s.sendError(out, "not_probed", "Quire hasn't checked that address yet, so it can't add it.")
	}
	src := *draft
	if themeID != "" && themeID != src.Theme {
		return s.sendError(out, "bad_request", "That isn't the site type Quire detected.")
	}
	if n := strings.TrimSpace(name); n != "" {
		src.Name = n
	}
	if l := strings.TrimSpace(lang); l != "" {
		src.Lang = l
	}
	if _, err := s.store.Add(&src); err != nil {
		return s.sendError(out, "not_added", err.Error())
	}
	s.mu.Lock()
	delete(s.drafts, strings.TrimSuffix(url, "/"))
	s.mu.Unlock()
	return s.sendSources(out)
}

// --- the probe -------------------------------------------------------------

// probeSession is one running probe, and the channel its questions are
// answered on.
type probeSession struct {
	answers chan prober.Answer
}

// probeUI adapts prober.UI onto the socket.
type probeUI struct {
	out     Sender
	session *probeSession
	log     *slog.Logger
}

func (u *probeUI) Progress(p prober.Progress) {
	if err := send(u.out, appload.MessageProbeProgress, p); err != nil {
		u.log.Warn("could not send probe progress", "err", err)
	}
}

func (u *probeUI) Ask(ctx context.Context, q prober.Question) (prober.Answer, error) {
	// A question is a progress message carrying a question: the wizard is
	// already listening to that stream, and a separate type would be a second
	// thing to keep in step for no gain.
	if err := send(u.out, appload.MessageProbeProgress, map[string]any{
		"stage": 0, "total": prober.StageCount, "name": "Needs your decision", "question": q,
	}); err != nil {
		return prober.Answer{}, err
	}
	select {
	case a := <-u.session.answers:
		return a, nil
	case <-ctx.Done():
		return prober.Answer{}, ctx.Err()
	}
}

func (s *Service) runProbe(ctx context.Context, out Sender, rawurl string) {
	session := &probeSession{answers: make(chan prober.Answer, 1)}

	s.mu.Lock()
	if s.probing != nil {
		s.mu.Unlock()
		_ = s.sendError(out, "busy", "Quire is already checking a site. Wait for that to finish.")
		return
	}
	s.probing = session
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.probing = nil
		s.mu.Unlock()
	}()

	p := prober.New(prober.Options{
		Fetcher:  s.fetch,
		Registry: s.reg,
		Guard:    s.guard,
		Now:      s.now,
	})
	res, err := p.Run(ctx, rawurl, &probeUI{out: out, session: session, log: s.log})
	if err != nil {
		_ = s.sendError(out, "probe_failed", err.Error())
		return
	}

	// The draft is kept here rather than sent and echoed back: PLAN §7.1 wants
	// small payloads, and a source entry the UI could edit in flight is a way
	// for an unprobed source to reach the store.
	if res.Addable && res.Draft != nil {
		s.mu.Lock()
		s.drafts[strings.TrimSuffix(res.Draft.BaseURL, "/")] = res.Draft
		s.mu.Unlock()
	}

	// What goes over the wire is the verdict, its plain-language detail, and
	// the few fields the wizard shows. Never the draft.
	_ = send(out, appload.MessageProbeVerdict, map[string]any{
		"verdict":     res.Verdict,
		"headline":    VerdictHeadline(res.Verdict),
		"cancelled":   res.Cancelled,
		"detail":      res.Detail,
		"theme":       res.ThemeID,
		"themeScores": res.ThemeScores,
		"warnings":    res.Warnings,
		"addable":     res.Addable,
		"name":        draftName(res),
		"lang":        draftLang(res),
		"url":         draftURL(res),
		"at":          res.At.Format(time.RFC3339),
	})
}

func draftName(r prober.Result) string {
	if r.Draft != nil {
		return r.Draft.Name
	}
	return r.Title
}

func draftLang(r prober.Result) string {
	if r.Draft != nil {
		return r.Draft.Lang
	}
	return ""
}

func draftURL(r prober.Result) string {
	if r.Draft != nil {
		return r.Draft.BaseURL
	}
	return r.FinalURL
}

func (s *Service) answerProbe(a prober.Answer) {
	s.mu.Lock()
	session := s.probing
	s.mu.Unlock()
	if session == nil {
		return
	}
	select {
	case session.answers <- a:
	default:
		// Nothing is waiting. A stray answer is not an error worth showing.
	}
}

// --- browsing --------------------------------------------------------------

type seriesRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	CoverURL string `json:"coverUrl,omitempty"`
}

func (s *Service) themeFor(sourceID string) (theme.Theme, *theme.Source, error) {
	src, ok := s.store.Get(sourceID)
	if !ok {
		return nil, nil, fmt.Errorf("no source called %q", sourceID)
	}
	th, ok := s.reg.Lookup(src.Theme)
	if !ok {
		return nil, nil, fmt.Errorf("%s uses a site type Quire no longer has", src.Name)
	}
	return th, src, nil
}

// defaultPageSize is used only when the frontend did not say how many tiles fit
// its viewport — an older frontend, or a test. PLAN §12.1 forbids hardcoding a
// page size in the UI precisely because the real one comes from the geometry;
// this is a fallback, not the rule.
const defaultPageSize = 9

func (s *Service) runSearch(ctx context.Context, out Sender, sourceID, query string, page, pageSize int) {
	th, src, err := s.themeFor(sourceID)
	if err != nil {
		_ = s.sendError(out, "not_found", err.Error())
		return
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}

	// The display page the frontend asked for is served out of the cache; the
	// pager reaches for the network only when the cache runs short (PLAN §12.1
	// — one tap must not equal one HTTP request).
	pager := s.pagerFor(pagerKey{sourceID: sourceID, query: query})
	res, err := pager.Page(ctx, page, pageSize, func(ctx context.Context, sourcePage int) ([]theme.SeriesStub, error) {
		return th.Search(ctx, src, query, sourcePage)
	})
	if err != nil && len(res.Items) == 0 {
		_ = s.sendError(out, "search_failed", plain(err))
		return
	}
	if err != nil {
		// There is a screenful to show and a reason the next one is missing.
		// Both are true, so say both rather than picking one.
		s.log.Warn("could not extend the listing", "source", sourceID, "err", err)
		_ = s.sendError(out, "search_failed", plain(err))
	}

	rows := make([]seriesRow, 0, len(res.Items))
	for _, st := range res.Items {
		s.rememberCoverReferrer(sourceID, st.CoverURL, st.CoverReferrer)
		rows = append(rows, seriesRow{ID: st.ID, Title: st.Title, CoverURL: st.CoverURL})
	}
	// An *empty listing* is evidence the site changed; an empty search is not.
	// See maybeReprobe for why that distinction is the whole trigger.
	if len(rows) == 0 && err == nil && strings.TrimSpace(query) == "" && page == 1 {
		s.goBackground(ctx, func(ctx context.Context) { s.maybeReprobe(ctx, out, src, reasonEmptyListing) })
	}

	_ = send(out, appload.MessageSearchResults, map[string]any{
		"sourceId": sourceID,
		"query":    query,
		"page":     res.Page,
		"pageSize": pageSize,
		// 0 means "the source has not said how much there is". The frontend
		// shows "Page 3" rather than inventing a denominator.
		"totalPages": res.TotalPages,
		"hasMore":    res.HasMore,
		"series":     rows,
	})
}

func (s *Service) runSeriesDetail(ctx context.Context, out Sender, sourceID, seriesID string) {
	th, src, err := s.themeFor(sourceID)
	if err != nil {
		_ = s.sendError(out, "not_found", err.Error())
		return
	}
	series, err := th.Series(ctx, src, seriesID)
	if err != nil {
		_ = s.sendError(out, "series_failed", plain(err))
		return
	}
	s.rememberCoverReferrer(sourceID, series.CoverURL, series.CoverReferrer)
	chapters, err := th.Chapters(ctx, src, seriesID)
	if err != nil {
		_ = s.sendError(out, "chapters_failed", plain(err))
		return
	}

	// Chapter rows are trimmed to what the list shows. A long series can carry
	// hundreds of them and the socket's real ceiling is a few hundred KB
	// (PLAN §3.1), so this is not a micro-optimisation.
	type chapterRow struct {
		ID        string  `json:"id"`
		Title     string  `json:"title"`
		Number    float64 `json:"number"`
		Published string  `json:"published,omitempty"`
		Scanlator string  `json:"scanlator,omitempty"`

		// DocumentUUID is set when this chapter's volume is already on the
		// tablet. It is what turns the row's button from "Download" into
		// "Read" (PLAN §6 M6) — without it, a volume downloaded last week is
		// indistinguishable from one never fetched.
		DocumentUUID string `json:"documentUuid,omitempty"`
		VolumeLabel  string `json:"volumeLabel,omitempty"`
	}
	stored := s.storedVolumes(sourceID, seriesID, series.Title, chapters)

	rows := make([]chapterRow, 0, len(chapters))
	for _, c := range chapters {
		r := chapterRow{ID: c.ID, Title: c.Title, Number: c.Number, Scanlator: c.Scanlator}
		if !c.Published.IsZero() {
			r.Published = c.Published.UTC().Format("2006-01-02")
		}
		if rec, ok := stored[c.ID]; ok {
			r.DocumentUUID, r.VolumeLabel = rec.DocumentUUID, rec.Volume
		}
		rows = append(rows, r)
	}
	// The series page resolved and yielded no chapters at all: that is a parse
	// that no longer matches the page, not a series with nothing in it.
	if len(chapters) == 0 {
		s.goBackground(ctx, func(ctx context.Context) { s.maybeReprobe(ctx, out, src, reasonEmptyChapters) })
	}

	// The volume view, when there is one to show (PLAN §6 M4, revised
	// 2026-09-16). An empty list means the screen offers chapters and nothing
	// else: the affordance is decided here, from the data, because an empty tab
	// is a worse answer than no tab and the frontend has no way to tell.
	_ = send(out, appload.MessageSeriesDetailResult, map[string]any{
		"sourceId": sourceID,
		"series":   series,
		"chapters": rows,
		"volumes":  volumeRows(series.Title, chapters, stored),
	})

	// PLAN §12.2. Serving the chapter list is the one moment Quire can honestly
	// say the user has looked at the series, so it is where "new" is cleared —
	// and where a watch made a moment later gets its baseline from, rather than
	// announcing the back catalogue.
	if len(chapters) > 0 {
		ids := make([]string, 0, len(chapters))
		for _, c := range chapters {
			ids = append(ids, c.ID)
		}
		s.rememberServedChapters(sourceID, seriesID, ids)
		s.seriesSeen(out, sourceID, seriesID, ids)
	}
}

// --- plumbing --------------------------------------------------------------

func decode(payload []byte, into any) error {
	if len(payload) == 0 {
		return fmt.Errorf("the message had no payload")
	}
	if err := json.Unmarshal(payload, into); err != nil {
		return fmt.Errorf("the message was not readable JSON: %v", err)
	}
	return nil
}

func send(out Sender, msgType int32, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return out.Send(msgType, b)
}

func (s *Service) sendError(out Sender, code, message string) error {
	s.log.Warn("error to frontend", "code", code, "message", message)
	return send(out, appload.MessageError, map[string]string{"code": code, "message": message})
}

// plain trims Go error wrapping down to something a person can read.
//
// A refusal mid-download reduces to a bare "HTTP 429" once the wrapping is
// gone — the same hole the probe had, in the place a rate limit is most likely
// to show up — so a trailing status becomes a sentence the reader can act on
// (PLAN §6 M3). No retry is added here: PLAN §7.4's client already backs off.
func plain(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > 0 && i < len(msg)-2 {
		msg = msg[i+2:]
	}
	return fetch.ExplainStatus(msg, "the site")
}
