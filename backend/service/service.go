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

	// dlQueue serialises downloads; see enqueueDownload for why there is
	// exactly one worker behind it.
	dlOnce  sync.Once
	dlQueue chan downloadJob

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

		library:         opts.Library,
		libStore:        opts.LibraryStore,
		downloadDir:     opts.DownloadDir,
		downloadOptions: opts.DownloadOptions,

		uploadBudgetBytes: opts.UploadBudgetBytes,
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
		go s.runProbe(ctx, out, req.URL)
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
		return true, s.sendSources(out)

	case appload.MessageSearch:
		var req struct {
			SourceID string `json:"sourceId"`
			Query    string `json:"query"`
			Page     int    `json:"page"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		go s.runSearch(ctx, out, req.SourceID, req.Query, req.Page)
		return true, nil

	case appload.MessageBrowse:
		var req struct {
			SourceID string `json:"sourceId"`
			Page     int    `json:"page"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		// Browse is search with no query: on every theme we have, that is the
		// site's own recent/popular listing, which is also what PLAN §7.5 stage
		// 5 falls back to.
		go s.runSearch(ctx, out, req.SourceID, "", req.Page)
		return true, nil

	case appload.MessageSeriesDetail:
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		go s.runSeriesDetail(ctx, out, req.SourceID, req.SeriesID)
		return true, nil

	case appload.MessageRequestCover:
		var req struct {
			SourceID string `json:"sourceId"`
			SeriesID string `json:"seriesId"`
			URL      string `json:"url"`
		}
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		go s.runCover(ctx, out, req.SourceID, req.SeriesID, req.URL)
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

	case appload.MessageEnqueueDownload:
		var req downloadRequest
		if err := decode(payload, &req); err != nil {
			return true, s.sendError(out, "bad_request", err.Error())
		}
		return true, s.enqueueDownload(ctx, out, req)
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
		v := sourceView{
			ID: src.ID, Name: src.Name, BaseURL: src.BaseURL,
			Theme: src.Theme, Lang: src.Lang, Enabled: src.IsEnabled(),
			Status: "Not checked yet",
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

func (s *Service) runSearch(ctx context.Context, out Sender, sourceID, query string, page int) {
	th, src, err := s.themeFor(sourceID)
	if err != nil {
		_ = s.sendError(out, "not_found", err.Error())
		return
	}
	if page < 1 {
		page = 1
	}
	stubs, err := th.Search(ctx, src, query, page)
	if err != nil {
		_ = s.sendError(out, "search_failed", plain(err))
		return
	}
	rows := make([]seriesRow, 0, len(stubs))
	for _, st := range stubs {
		rows = append(rows, seriesRow{ID: st.ID, Title: st.Title, CoverURL: st.CoverURL})
	}
	_ = send(out, appload.MessageSearchResults, map[string]any{
		"sourceId": sourceID,
		"query":    query,
		"page":     page,
		"series":   rows,
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
	_ = send(out, appload.MessageSeriesDetailResult, map[string]any{
		"sourceId": sourceID,
		"series":   series,
		"chapters": rows,
	})
}

func (s *Service) runCover(ctx context.Context, out Sender, sourceID, seriesID, url string) {
	if s.covers == nil {
		return
	}
	src, ok := s.store.Get(sourceID)
	if !ok {
		return
	}
	path, err := s.covers.Path(ctx, src, url)
	if err != nil {
		// A missing cover is a blank tile, not an error dialogue: the grid is
		// still usable and the titles are still readable.
		s.log.Debug("cover unavailable", "source", sourceID, "series", seriesID, "err", err)
		return
	}
	_ = send(out, appload.MessageCoverReady, map[string]any{
		"sourceId": sourceID,
		"seriesId": seriesID,
		"path":     path,
	})
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
func plain(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i > 0 && i < len(msg)-2 {
		msg = msg[i+2:]
	}
	return msg
}
