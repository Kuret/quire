package service_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/internal/nonet"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

func TestMain(m *testing.M) {
	nonet.ForbidMain()
	os.Exit(m.Run())
}

var fixedNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

// recorder collects what the backend sent, the way the frontend would see it.
type recorder struct {
	mu   sync.Mutex
	sent []frame
}

type frame struct {
	Type    int32
	Payload []byte
}

func (r *recorder) Send(msgType int32, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, frame{msgType, append([]byte(nil), payload...)})
	return nil
}

// wait polls for a frame of the given type, so a test does not have to know
// how many goroutines the service used.
func (r *recorder) wait(t *testing.T, msgType int32) []byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, f := range r.sent {
			if f.Type == msgType {
				r.mu.Unlock()
				return f.Payload
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var got []string
	for _, f := range r.sent {
		got = append(got, appload.Name(f.Type)+" "+string(f.Payload))
	}
	t.Fatalf("no %s arrived; got:\n  %s", appload.Name(msgType), strings.Join(got, "\n  "))
	return nil
}

type allowGuard struct{}

func (allowGuard) CheckURL(context.Context, *url.URL, *fetch.Policy) error { return nil }

func newService(t *testing.T, routes map[string]themetest.Route) (*service.Service, *state.Store, *recorder) {
	t.Helper()
	f := themetest.New(t, routes)
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))

	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store:      store,
		Registry:   reg,
		Fetcher:    f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: allowGuard{},
	})
	return svc, store, &recorder{}
}

func routes() map[string]themetest.Route {
	return map[string]themetest.Route{
		"GET /":                                         {File: "home-madara.html"},
		"GET /?post_type=wp-manga&s=a":                  {File: "search.html"},
		"GET /?post_type=wp-manga&s=":                   {File: "search.html"},
		"GET /?post_type=wp-manga&s=lantern":            {File: "search.html"},
		"GET /manga/the-lantern-keeper/":                {File: "series.html"},
		"POST /manga/the-lantern-keeper/ajax/chapters/": {File: "chapters-ajax.html"},
		"GET /manga/the-lantern-keeper/chapter-4/":      {File: "reader.html"},
	}
}

func handle(t *testing.T, svc *service.Service, out service.Sender, msgType int32, payload string) {
	t.Helper()
	handled, err := svc.Handle(context.Background(), out, msgType, []byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatalf("%s was not handled", appload.Name(msgType))
	}
}

// The M3 acceptance path as the frontend walks it: probe, confirm, list.
func TestProbeThenConfirmAddsASource(t *testing.T) {
	svc, store, rec := newService(t, routes())

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)

	var verdict struct {
		Verdict  string `json:"verdict"`
		Headline string `json:"headline"`
		Detail   string `json:"detail"`
		Theme    string `json:"theme"`
		Addable  bool   `json:"addable"`
		Name     string `json:"name"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Verdict != theme.VerdictOK || !verdict.Addable {
		t.Fatalf("verdict = %+v, want an addable ok", verdict)
	}
	if verdict.Headline == "" || verdict.Headline == verdict.Verdict {
		t.Errorf("headline = %q; the UI must never have to show the enum value", verdict.Headline)
	}

	// Progress arrived before the verdict, one stage at a time.
	if payload := rec.wait(t, appload.MessageProbeProgress); len(payload) == 0 {
		t.Error("no progress was streamed")
	}

	handle(t, svc, rec, appload.MessageConfirmAddSource,
		`{"url":"`+verdict.URL+`","theme":"`+verdict.Theme+`","name":"`+verdict.Name+`","lang":"en"}`)

	if got := store.List(); len(got) != 1 || got[0].Name != "Example Reader" {
		t.Fatalf("store holds %d source(s): %+v", len(got), got)
	}

	var sources struct {
		Sources []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Enabled bool   `json:"enabled"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources.Sources) != 1 || !sources.Sources[0].Enabled {
		t.Fatalf("source list = %+v", sources.Sources)
	}
	if sources.Sources[0].Status != "Working" {
		t.Errorf("status = %q, want plain language", sources.Sources[0].Status)
	}
}

// Nothing may be added that was not probed. A frontend that skipped the wizard
// — or a replayed message — must not be able to write a source.
func TestConfirmWithoutAProbeIsRefused(t *testing.T) {
	svc, store, rec := newService(t, routes())

	handle(t, svc, rec, appload.MessageConfirmAddSource,
		`{"url":"https://example.invalid","theme":"madara","name":"Sneaky","lang":"en"}`)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageError), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != "not_probed" {
		t.Errorf("code = %q, want not_probed", e.Code)
	}
	if len(store.List()) != 0 {
		t.Error("a source was added without a probe")
	}
}

// A refused verdict leaves nothing to confirm (PLAN §7.6).
func TestChallengeVerdictLeavesNothingToAdd(t *testing.T) {
	r := routes()
	r["GET /"] = themetest.Route{
		Status: 503,
		Body:   `<html><head><title>Just a moment...</title></head><body><script src="/cdn-cgi/challenge-platform/h/b/scripts/jsd/main.js"></script></body></html>`,
		Header: map[string][]string{"Server": {"cloudflare"}, "Cf-Ray": {"0-AMS"}},
	}
	svc, store, rec := newService(t, r)

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)
	var verdict struct {
		Verdict  string `json:"verdict"`
		Headline string `json:"headline"`
		Addable  bool   `json:"addable"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Verdict != theme.VerdictBlockedChallenge || verdict.Addable {
		t.Fatalf("verdict = %+v, want a refused blocked_challenge", verdict)
	}

	handle(t, svc, rec, appload.MessageConfirmAddSource, `{"url":"https://example.invalid"}`)
	if len(store.List()) != 0 {
		t.Fatal("a challenge-protected site was added anyway")
	}
}

// The probe's questions reach the frontend and its answers reach the probe.
func TestProbeQuestionIsAnswerableOverTheSocket(t *testing.T) {
	r := routes()
	r["GET /"] = themetest.Route{File: "home-madara.html", FinalURL: "https://elsewhere.invalid/"}
	svc, _, rec := newService(t, r)

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)

	// Wait for a progress message carrying a question.
	deadline := time.Now().Add(2 * time.Second)
	var q struct {
		Question *prober.Question `json:"question"`
	}
	for time.Now().Before(deadline) && q.Question == nil {
		rec.mu.Lock()
		for _, f := range rec.sent {
			if f.Type != appload.MessageProbeProgress {
				continue
			}
			var probeMsg struct {
				Question *prober.Question `json:"question"`
			}
			if err := json.Unmarshal(f.Payload, &probeMsg); err == nil && probeMsg.Question != nil {
				q.Question = probeMsg.Question
			}
		}
		rec.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if q.Question == nil {
		t.Fatal("the probe never asked about the redirect")
	}
	if q.Question.Kind != "redirect" || len(q.Question.Options) == 0 {
		t.Fatalf("question = %+v", q.Question)
	}

	handle(t, svc, rec, appload.MessageProbeAnswer, `{"id":"cancel"}`)

	var verdict struct {
		Cancelled bool `json:"cancelled"`
		Addable   bool `json:"addable"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	if !verdict.Cancelled || verdict.Addable {
		t.Errorf("verdict = %+v, want a cancelled probe with nothing to add", verdict)
	}
}

func TestSearchBrowseAndSeriesDetail(t *testing.T) {
	svc, store, rec := newService(t, routes())

	// Add a source the direct way; the probe path is covered above.
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageSearch, `{"sourceId":"example-reader","query":"lantern"}`)
	var results struct {
		SourceID string `json:"sourceId"`
		Series   []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			CoverURL string `json:"coverUrl"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSearchResults), &results); err != nil {
		t.Fatal(err)
	}
	if len(results.Series) == 0 || results.Series[0].Title == "" {
		t.Fatalf("search returned %+v", results)
	}

	handle(t, svc, rec, appload.MessageSeriesDetail,
		`{"sourceId":"example-reader","seriesId":"`+results.Series[0].ID+`"}`)
	var detail struct {
		Series struct {
			Title string `json:"title"`
		} `json:"series"`
		Chapters []struct {
			ID     string  `json:"id"`
			Number float64 `json:"number"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSeriesDetailResult), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Series.Title == "" || len(detail.Chapters) == 0 {
		t.Fatalf("series detail = %+v", detail)
	}
}

// Toggling and removing a source both answer with the whole list, so the UI
// never has to work out what changed.
func TestToggleAndRemove(t *testing.T) {
	svc, store, rec := newService(t, routes())
	if _, err := store.Add(&theme.Source{
		Name: "Example Reader", Lang: "en", Theme: madara.ID,
		BaseURL: "https://example.invalid", AddedAt: fixedNow,
	}); err != nil {
		t.Fatal(err)
	}

	handle(t, svc, rec, appload.MessageSetSourceEnabled, `{"sourceId":"example-reader","enabled":false}`)
	var sources struct {
		Sources []struct {
			Enabled bool `json:"enabled"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageSources), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources.Sources) != 1 || sources.Sources[0].Enabled {
		t.Fatalf("after disabling: %+v", sources.Sources)
	}

	handle(t, svc, rec, appload.MessageRemoveSource, `{"sourceId":"example-reader"}`)
	if len(store.List()) != 0 {
		t.Error("the source was not removed")
	}
}

// An unknown type is not the service's business, and it must say so rather than
// swallowing it.
func TestUnknownMessageIsNotHandled(t *testing.T) {
	svc, _, rec := newService(t, routes())
	handled, err := svc.Handle(context.Background(), rec, appload.MessagePing, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Error("the service claimed Ping")
	}
}
