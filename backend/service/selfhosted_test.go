package service_test

import (
	"context"
	"encoding/json"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/rickl/quire/backend/appload"
	"github.com/rickl/quire/backend/covers"
	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/service"
	"github.com/rickl/quire/backend/state"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/themetest"
)

// The add-source flow end to end, for the source that could not be added at
// all until now: a service on the user's own network, optionally reached
// through a proxy. Everything between the socket and sources.json is exercised,
// because the two halves that matter — that the offer is a *question* and that
// the answer is written by the store methods that own it — live at opposite
// ends of it.

// homeNetworkGuard refuses a private address and can say which one, the way the
// real fetch.Guard does. It relents only when the policy carries the
// confirmation.
type homeNetworkGuard struct{}

func (homeNetworkGuard) CheckURL(_ context.Context, u *url.URL, p *fetch.Policy) error {
	if p != nil && p.SelfHostedHost == u.Hostname() {
		return nil
	}
	return &fetch.GuardError{
		URL:    u.Redacted(),
		Reason: "private addresses are not reachable sources",
		Kind:   fetch.ErrBlockedAddress,
	}
}

func (homeNetworkGuard) SelfHostableTarget(context.Context, *url.URL) (netip.Addr, bool) {
	return netip.MustParseAddr("100.79.171.1"), true
}

func newHomeService(t *testing.T) (*service.Service, *state.Store, *recorder) {
	t.Helper()
	f := themetest.New(t, routes())
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
		ProbeGuard: homeNetworkGuard{},
	})
	t.Cleanup(svc.Close)
	return svc, store, &recorder{}
}

// waitForQuestion polls the progress stream for the one message carrying a
// question, the way the wizard does.
func waitForQuestion(t *testing.T, rec *recorder) *prober.Question {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		for _, f := range rec.sent {
			if f.Type != appload.MessageProbeProgress {
				continue
			}
			var msg struct {
				Question *prober.Question `json:"question"`
			}
			if err := json.Unmarshal(f.Payload, &msg); err == nil && msg.Question != nil {
				rec.mu.Unlock()
				return msg.Question
			}
		}
		rec.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the probe never asked about the private address")
	return nil
}

// TestSelfHostedSourceIsAddedThroughTheQuestion is the flow the user walks:
// paste an address that resolves somewhere private, be told what it resolved
// to, say yes, hand over a proxy, and end up with a source.
func TestSelfHostedSourceIsAddedThroughTheQuestion(t *testing.T) {
	svc, store, rec := newHomeService(t)

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)

	q := waitForQuestion(t, rec)
	if q.Kind != "selfhosted" {
		t.Fatalf("question kind = %q, want selfhosted", q.Kind)
	}
	// The question and its field both survive the socket: the wizard can only
	// draw what arrives.
	if q.Input == nil || q.Input.Label == "" {
		t.Fatalf("the question reached the frontend without its proxy field: %+v", q)
	}

	handle(t, svc, rec, appload.MessageProbeAnswer, `{"id":"continue","text":"http://localhost:1055"}`)

	var verdict struct {
		Verdict string `json:"verdict"`
		Addable bool   `json:"addable"`
		Theme   string `json:"theme"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Verdict != theme.VerdictOK || !verdict.Addable {
		t.Fatalf("verdict = %+v; confirming the address should have let the probe finish", verdict)
	}

	handle(t, svc, rec, appload.MessageConfirmAddSource,
		`{"url":"`+verdict.URL+`","theme":"`+verdict.Theme+`","name":"`+verdict.Name+`","lang":"en"}`)

	got := store.List()
	if len(got) != 1 {
		t.Fatalf("store holds %d source(s): %+v", len(got), got)
	}
	src := got[0]
	if src.SelfHosted == nil {
		t.Fatal("the stored source carries no confirmation")
	}
	if src.SelfHosted.ConfirmedAddr != "100.79.171.1" {
		t.Errorf("ConfirmedAddr = %q, want the address the probe resolved", src.SelfHosted.ConfirmedAddr)
	}
	if src.SelfHosted.ConfirmedAt.IsZero() {
		t.Error("ConfirmedAt is zero; the record should say when consent was given")
	}
	if src.Proxy != "http://localhost:1055" {
		t.Errorf("stored proxy = %q, want the one typed into the question", src.Proxy)
	}

	// And the stored source produces a policy the fetch layer can act on: the
	// exemption and the proxy scoped to its own host and nothing else.
	p, err := src.Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.SelfHostedHost != "example.invalid" || p.Proxy == nil {
		t.Errorf("policy = selfHosted %q, proxy %v; want both on the source's own host", p.SelfHostedHost, p.Proxy)
	}
}

// TestDecliningTheOfferAddsNothing. The default is no, and no is a complete
// answer: the refusal that was already in force stays in force.
func TestDecliningTheOfferAddsNothing(t *testing.T) {
	svc, store, rec := newHomeService(t)

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)
	waitForQuestion(t, rec)
	handle(t, svc, rec, appload.MessageProbeAnswer, `{"id":"cancel"}`)

	var verdict struct {
		Verdict string `json:"verdict"`
		Addable bool   `json:"addable"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	// The refusal that was already in force stays in force, as itself.
	if verdict.Verdict != theme.VerdictBlockedAddress || verdict.Addable {
		t.Errorf("verdict = %+v, want the refusal it started as and nothing to add", verdict)
	}
	if len(store.List()) != 0 {
		t.Fatal("a private address the user declined was added anyway")
	}
}

// TestAConfirmedSourceIsWrittenByTheStoreMethodsThatOwnIt.
//
// state.Store.ConfirmSelfHosted is documented as the only thing in Quire that
// ever writes a confirmation, and SetProxy is its neighbour. The add flow goes
// through both rather than letting the draft carry them into Add — which is why
// a source added with no answer given has neither field, even though the same
// code path wrote it.
func TestAnOrdinarySourceGetsNeitherField(t *testing.T) {
	svc, store, rec := newService(t, routes()) // allowGuard: nothing is refused

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)
	var verdict struct {
		Theme string `json:"theme"`
		Name  string `json:"name"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	handle(t, svc, rec, appload.MessageConfirmAddSource,
		`{"url":"`+verdict.URL+`","theme":"`+verdict.Theme+`","name":"`+verdict.Name+`","lang":"en"}`)

	got := store.List()
	if len(got) != 1 {
		t.Fatalf("store holds %d source(s)", len(got))
	}
	if got[0].SelfHosted != nil || got[0].Proxy != "" {
		t.Errorf("an ordinary source was stored with %+v / proxy %q", got[0].SelfHosted, got[0].Proxy)
	}
	p, err := got[0].Policy()
	if err != nil {
		t.Fatal(err)
	}
	if p.Proxy != nil || p.SelfHostedHost != "" {
		t.Errorf("an ordinary source's policy = proxy %v, selfHosted %q; want neither", p.Proxy, p.SelfHostedHost)
	}
}

// unresolvableGuard is the device as measured on 2026-09-20: the name does not
// resolve here at all, and the proxy is what reaches it.
type unresolvableGuard struct{}

func (unresolvableGuard) CheckURL(_ context.Context, u *url.URL, p *fetch.Policy) error {
	if p != nil && p.SelfHostedHost == u.Hostname() && p.Proxy != nil {
		return nil
	}
	return &fetch.GuardError{
		URL:        u.Redacted(),
		Reason:     `cannot resolve "example.invalid": no such host`,
		Kind:       fetch.ErrInvalidURL,
		Unresolved: true,
	}
}

func (unresolvableGuard) SelfHostableTarget(context.Context, *url.URL) (netip.Addr, bool) {
	// Nothing resolved, so there is nothing to offer an address for.
	return netip.Addr{}, false
}

// TestASourceThatResolvesNowhereIsAddedThroughItsProxy is the flow on the
// owner's tablet: the name exists only inside their mesh, the lookup fails, the
// question offers a proxy, and the stored source records the proxy as the
// evidence rather than an address nobody has.
func TestASourceThatResolvesNowhereIsAddedThroughItsProxy(t *testing.T) {
	f := themetest.New(t, routes())
	reg := theme.NewRegistry()
	reg.MustRegister(madara.NewWithClock(f, func() time.Time { return fixedNow }))
	dir := t.TempDir()
	store, err := state.Open(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Options{
		Store: store, Registry: reg, Fetcher: f,
		Covers:     covers.New(dir+"/covers", f),
		Now:        func() time.Time { return fixedNow },
		ProbeGuard: unresolvableGuard{},
	})
	t.Cleanup(svc.Close)
	rec := &recorder{}

	handle(t, svc, rec, appload.MessageProbeSource, `{"url":"https://example.invalid"}`)
	q := waitForQuestion(t, rec)
	if q.Kind != "selfhosted" {
		t.Fatalf("question kind = %q, want selfhosted", q.Kind)
	}
	handle(t, svc, rec, appload.MessageProbeAnswer, `{"id":"continue","text":"http://localhost:1055"}`)

	var verdict struct {
		Verdict string `json:"verdict"`
		Addable bool   `json:"addable"`
		Theme   string `json:"theme"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(rec.wait(t, appload.MessageProbeVerdict), &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Verdict != theme.VerdictOK || !verdict.Addable {
		t.Fatalf("verdict = %+v; a proxy should have got the probe through", verdict)
	}

	handle(t, svc, rec, appload.MessageConfirmAddSource,
		`{"url":"`+verdict.URL+`","theme":"`+verdict.Theme+`","name":"`+verdict.Name+`","lang":"en"}`)

	got := store.List()
	if len(got) != 1 {
		t.Fatalf("store holds %d source(s): %+v", len(got), got)
	}
	src := got[0]
	if src.Proxy != "http://localhost:1055" {
		t.Errorf("stored proxy = %q", src.Proxy)
	}
	if src.SelfHosted == nil || !src.SelfHosted.ViaProxy {
		t.Fatalf("stored confirmation = %+v, want one recorded as reached through a proxy", src.SelfHosted)
	}
	if src.SelfHosted.ConfirmedAddr != "" {
		t.Errorf("ConfirmedAddr = %q; nothing resolved, so nothing may be recorded", src.SelfHosted.ConfirmedAddr)
	}
	if src.SelfHosted.ConfirmedAt.IsZero() {
		t.Error("ConfirmedAt is zero")
	}
	// It survives a reload, which is the store validating the pair on the way
	// back in: the flag is only legible because the proxy is there with it.
	again, err := state.Open(dir, reg)
	if err != nil {
		t.Fatalf("the stored source did not survive a reload: %v", err)
	}
	if reloaded := again.List(); len(reloaded) != 1 || reloaded[0].SelfHosted == nil {
		t.Fatalf("after reload: %+v", reloaded)
	}
}
