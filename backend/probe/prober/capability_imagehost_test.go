package prober_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// PLAN §7.5 stage 5's other question (added 2026-09-21): a page image that
// only failed because its host is off the source's registrable domain and was
// not already declared. Measured against a real site, toonily.com: it
// fingerprints as madara, search and series both work, and stage 5 then died
// at the last step because its page images are served from a different
// registrable domain (static.tnlycdn.com) with no way for the user to say
// that is expected.
//
// These tests fake the one thing themetest.Fetcher does not model — the SSRF
// guard's off-domain refusal — with a small wrapper that returns a real
// fetch.GuardError until the image host is on the policy's AllowedHosts,
// which is exactly what fetch.Guard itself does once a "yes" is recorded on
// the draft source.

// offDomainImageFetcher wraps a themetest.Fetcher and answers a request for
// imageHost with refusal() until imageHost appears in the policy's
// AllowedHosts, at which point it behaves exactly like the wrapped fetcher.
type offDomainImageFetcher struct {
	*themetest.Fetcher
	imageHost string
	refusal   func() error
}

func (f *offDomainImageFetcher) GetFrom(ctx context.Context, p *fetch.Policy, rawurl string, from fetch.Referrer) (*fetch.Response, error) {
	if u, err := url.Parse(rawurl); err == nil && u.Hostname() == f.imageHost {
		allowed := false
		for _, h := range p.AllowedHosts {
			if h == f.imageHost {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, f.refusal()
		}
	}
	return f.Fetcher.GetFrom(ctx, p, rawurl, from)
}

// offDomainRefusal is the real shape fetch.Guard.checkDomain returns, so the
// test exercises the same structural signal the production guard sets rather
// than a shape invented for the test.
func offDomainRefusal(rawurl string) error {
	return &fetch.GuardError{
		URL:       rawurl,
		Reason:    `leaves the source's domain "example.invalid"; add it to allowedHosts if that is intended`,
		Kind:      fetch.ErrBlockedAddress,
		OffDomain: true,
	}
}

func runImageHostProbe(t *testing.T, refusal func() error, ui *recordUI) (prober.Result, *offDomainImageFetcher) {
	t.Helper()
	base := themetest.New(t, map[string]themetest.Route{
		"GET /":        {File: "home-unrecognised.html"},
		"GET /p/1.jpg": imageRoute(),
	})
	f := &offDomainImageFetcher{
		Fetcher:   base,
		imageHost: "cdn.other.invalid",
		refusal:   refusal,
	}
	reg := theme.NewRegistry()
	reg.MustRegister(stubTheme{id: "alpha", score: 90, pageURL: "https://cdn.other.invalid/p/1.jpg"})
	res, err := prober.New(prober.Options{
		Fetcher: f, Registry: reg, Guard: allowGuard{}, Now: clock,
		NewID: func() string { return "src-test" },
	}).Run(context.Background(), "https://example.invalid", ui)
	if err != nil {
		t.Fatalf("probe returned an error: %v", err)
	}
	return res, f
}

// The case toonily.com was measured against: a "yes" is granted, so the fetch
// is retried, the image is fetched, and the source ends up addable with the
// host recorded on its own AllowedHosts (not the CDN's registrable domain,
// and not any wildcard).
func TestImageHostOffDomainQuestionGrantedWidensAllowedHosts(t *testing.T) {
	ui := &recordUI{answers: []string{"continue"}}
	res, f := runImageHostProbe(t, func() error { return offDomainRefusal("https://cdn.other.invalid/p/1.jpg") }, ui)

	if len(ui.questions) != 1 || ui.questions[0].Kind != "imagehost" {
		t.Fatalf("expected exactly one imagehost question, got %+v", ui.questions)
	}
	if !strings.Contains(ui.questions[0].Text, "cdn.other.invalid") {
		t.Errorf("question %q does not name the image host plainly", ui.questions[0].Text)
	}

	// MUTATION (b): if a "yes" here were not threaded into the policy the
	// rest of the run uses, the retried fetch would fail exactly as the first
	// one did and the probe would end in `partial`, not `ok`. This is what
	// catches that.
	if res.Verdict != theme.VerdictOK || !res.Addable || res.Draft == nil {
		t.Fatalf("verdict = %q (%s), addable = %v; a granted image-host question must let stage 5 finish addable",
			res.Verdict, res.Detail, res.Addable)
	}
	found := false
	for _, h := range res.Draft.AllowedHosts {
		if h == "cdn.other.invalid" {
			found = true
		}
		// Scoped to the exact host: never the CDN's own wider form.
		if h == "*.other.invalid" || h == "other.invalid" {
			t.Errorf("allowedHosts widened past the exact host: %v", res.Draft.AllowedHosts)
		}
	}
	if !found {
		t.Errorf("draft.allowedHosts = %v, want it to carry cdn.other.invalid", res.Draft.AllowedHosts)
	}
	if !f.Requested("GET", "/p/1.jpg") {
		t.Error("the image was never actually fetched after being granted")
	}
}

// A "no" leaves the refusal that was already in force standing, reported as
// itself — never a generic "stopped", which is what declining the redirect
// question produces and which would misreport what actually happened here.
func TestImageHostOffDomainQuestionDeclined(t *testing.T) {
	ui := &recordUI{answers: []string{"cancel"}}
	res, f := runImageHostProbe(t, func() error { return offDomainRefusal("https://cdn.other.invalid/p/1.jpg") }, ui)

	if len(ui.questions) != 1 || ui.questions[0].Kind != "imagehost" {
		t.Fatalf("expected exactly one imagehost question, got %+v", ui.questions)
	}
	if res.Verdict != theme.VerdictPartial || res.Addable || res.Draft != nil {
		t.Fatalf("verdict = %q (%s), addable = %v; a declined image-host question must refuse, not add", res.Verdict, res.Detail, res.Addable)
	}
	// MUTATION (c): declining must report the *original* refusal, as itself —
	// not a generic "stopped" the way declining the redirect question does.
	if strings.Contains(res.Detail, "Stopped") {
		t.Errorf("detail %q reports a generic stop rather than the refusal that actually happened", res.Detail)
	}
	if !strings.Contains(res.Detail, "leaves the source's domain") {
		t.Errorf("detail %q does not carry the original refusal's own words", res.Detail)
	}
	if f.Requested("GET", "/p/1.jpg") {
		t.Error("a declined image-host question must not fetch the image anyway")
	}
}

// MUTATION (a): a refusal that is not the specific off-domain-image-host
// shape — here, an ordinary fetch.GuardError with OffDomain left false, as a
// private-address or any other ErrBlockedAddress refusal would be — must not
// raise this question at all. Getting this wrong turns a security boundary
// into a habit of clicking yes.
func TestImageHostQuestionOnlyForOffDomainRefusal(t *testing.T) {
	ui := &recordUI{}
	res, _ := runImageHostProbe(t, func() error {
		return &fetch.GuardError{
			URL:    "https://cdn.other.invalid/p/1.jpg",
			Reason: "some other refusal entirely",
			Kind:   fetch.ErrBlockedAddress,
			// OffDomain deliberately left false.
		}
	}, ui)

	if len(ui.questions) != 0 {
		t.Fatalf("a non-off-domain refusal must not raise the imagehost question, got %+v", ui.questions)
	}
	if res.Verdict != theme.VerdictPartial || res.Addable {
		t.Fatalf("verdict = %q (%s), addable = %v; want a plain refusal", res.Verdict, res.Detail, res.Addable)
	}
	if !strings.Contains(res.Detail, "some other refusal entirely") {
		t.Errorf("detail %q does not carry the refusal's own reason", res.Detail)
	}
}

// A non-guard failure — a plain fetch error with nothing GuardError about it
// — must also never raise the question. This is the ordinary "any other
// reason" case (a challenge, a 404, a bad handshake) PLAN's proof asks for.
func TestImageHostQuestionNotRaisedForOrdinaryFailure(t *testing.T) {
	ui := &recordUI{}
	res, _ := runImageHostProbe(t, func() error {
		return errors.New("dial tcp: connection refused")
	}, ui)

	if len(ui.questions) != 0 {
		t.Fatalf("an ordinary transport failure must not raise the imagehost question, got %+v", ui.questions)
	}
	if res.Verdict != theme.VerdictPartial || res.Addable {
		t.Fatalf("verdict = %q (%s), addable = %v; want a plain refusal", res.Verdict, res.Detail, res.Addable)
	}
}
